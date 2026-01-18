package xingchen

import (
	"context"
	"fmt"
	"io"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

type XingChen struct {
	model.Storage
	Addition
}

func (d *XingChen) Config() driver.Config {
	return config
}

func (d *XingChen) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *XingChen) Init(_ context.Context) error {
	return d.getAuthCode()
}

func (d *XingChen) getAuthCode() error {
	var resp struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	_, err := base.RestyClient.R().
		SetQueryParams(map[string]string{"aid": d.AID, "key": d.Key}).
		SetResult(&resp).
		Get("https://api.1785677.xyz/opapi/GetAuthCode")
	if err != nil {
		return err
	}
	d.AuthCode = resp.Data
	return nil
}

func (d *XingChen) Drop(_ context.Context) error {
	return nil
}

func (d *XingChen) List(_ context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	files, err := d.getFiles(dir.GetID())
	if err != nil {
		return nil, err
	}
	return utils.SliceConvert(files, func(src File) (model.Obj, error) {
		return &src, nil
	})
}

func (d *XingChen) Link(_ context.Context, file model.Obj, _ model.LinkArgs) (*model.Link, error) {
	var resp DownloadResp
	_, err := base.RestyClient.R().
		SetQueryParam("authcode", d.AuthCode).
		SetFormData(map[string]string{"id": file.GetID()}).
		SetResult(&resp).
		Post("https://api.1785677.xyz/opapi/downAllPath")
	if err != nil {
		return nil, err
	}
	return &model.Link{URL: resp.Data.URL}, nil
}

func (d *XingChen) MakeDir(_ context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
	formData := map[string]string{"c_name": dirName}
	if parentDir.GetID() != "" && parentDir.GetID() != "0" {
		formData["c_fid"] = parentDir.GetID()
	}
	_, err := base.RestyClient.R().
		SetQueryParam("authcode", d.AuthCode).
		SetFormData(formData).
		Post("https://api.1785677.xyz/opapi/addPath")
	return nil, err
}

func (d *XingChen) Remove(_ context.Context, obj model.Obj) error {
	_, err := base.RestyClient.R().
		SetQueryParam("authcode", d.AuthCode).
		SetFormData(map[string]string{"id": "[" + obj.GetID() + "]"}).
		Post("https://api.1785677.xyz/opapi/delPath")
	return err
}

func (d *XingChen) Rename(_ context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	_, err := base.RestyClient.R().
		SetQueryParam("authcode", d.AuthCode).
		SetFormData(map[string]string{
			"id":     srcObj.GetID(),
			"c_name": newName,
		}).
		Post("https://api.1785677.xyz/opapi/editPath")
	return nil, err
}

func (d *XingChen) Move(_ context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	formData := map[string]string{"id": "[" + srcObj.GetID() + "]"}
	if dstDir.GetID() != "" && dstDir.GetID() != "0" {
		formData["fid"] = dstDir.GetID()
	}
	_, err := base.RestyClient.R().
		SetQueryParam("authcode", d.AuthCode).
		SetFormData(formData).
		Post("https://api.1785677.xyz/opapi/transferPath")
	return nil, err
}

func (d *XingChen) Put(_ context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
	const chunkSize int64 = 100 * 1024 * 1024 // 100MB per chunk
	fileSize := stream.GetSize()

	params := map[string]string{"authcode": d.AuthCode}
	if dstDir.GetID() != "" && dstDir.GetID() != "0" {
		params["fid"] = dstDir.GetID()
	}

	// 小于1G使用普通上传
	if fileSize <= 1024*1024*1024 {
		var uploadResp UploadResp
		_, err := base.RestyClient.R().SetQueryParams(params).SetResult(&uploadResp).Get("https://api.1785677.xyz/opapi/Getuploads")
		if err != nil {
			return nil, err
		}
		uploadURL := uploadResp.Data.URL + "?" + uploadResp.Data.Query
		_, err = base.RestyClient.R().SetFileReader("file", stream.GetName(), io.NopCloser(stream)).Post(uploadURL)
		return nil, err
	}

	// 大于1G使用分片上传
	var uploadResp UploadResp
	_, err := base.RestyClient.R().SetQueryParams(params).SetResult(&uploadResp).Get("https://api.1785677.xyz/opapi/Getuploads")
	if err != nil {
		return nil, err
	}

	uploadURL := uploadResp.Data.URL + "?" + uploadResp.Data.Query

	// 获取已上传分片（断点续传）
	var uploadedResp struct {
		Data []int `json:"data"`
	}
	base.RestyClient.R().SetQueryParam("query", uploadResp.Data.Query).SetResult(&uploadedResp).Get("https://api.1785677.xyz/uploadedChunks")
	uploadedSet := make(map[int]bool)
	for _, c := range uploadedResp.Data {
		uploadedSet[c] = true
	}

	// 分片上传
	totalChunks := int((fileSize + chunkSize - 1) / chunkSize)
	for i := 0; i < totalChunks; i++ {
		chunkLen := chunkSize
		if int64(i)*chunkSize+chunkLen > fileSize {
			chunkLen = fileSize - int64(i)*chunkSize
		}

		// 跳过已上传的分片
		if uploadedSet[i] {
			io.CopyN(io.Discard, stream, chunkLen)
			up(float64(i+1) / float64(totalChunks) * 100)
			continue
		}

		chunk := io.LimitReader(stream, chunkLen)
		_, err = base.RestyClient.R().
			SetFileReader("file", stream.GetName(), io.NopCloser(chunk)).
			SetFormData(map[string]string{
				"chunk":  fmt.Sprintf("%d", i),
				"chunks": fmt.Sprintf("%d", totalChunks),
			}).
			Post(uploadURL)
		if err != nil {
			return nil, err
		}
		up(float64(i+1) / float64(totalChunks) * 100)
	}

	// 合并分片
	_, err = base.RestyClient.R().SetQueryParam("query", uploadResp.Data.Query).Post("https://api.1785677.xyz/mergeChunks")
	return nil, err
}

func (d *XingChen) getFiles(parentID string) ([]File, error) {
	var resp FileListResp
	params := map[string]string{
		"authcode": d.AuthCode,
		"type":     "1",
	}
	if parentID != "" && parentID != "0" {
		params["fid"] = parentID
	}
	_, err := base.RestyClient.R().
		SetQueryParams(params).
		SetResult(&resp).
		Get("https://api.1785677.xyz/opapi/getFileList")
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

var _ driver.Driver = (*XingChen)(nil)
