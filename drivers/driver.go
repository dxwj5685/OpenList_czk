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
		Code int         `json:"code"`
		Msg  string      `json:"msg"`
		Data interface{} `json:"data"`
	}
	_, err := base.RestyClient.R().
		SetQueryParams(map[string]string{"aid": d.AID, "key": d.Key}).
		SetResult(&resp).
		Get("https://api.1785677.xyz/opapi/GetAuthCode")
	if err != nil {
		return err
	}
	if resp.Code != 200 {
		return fmt.Errorf("%s", resp.Msg)
	}
	if authCode, ok := resp.Data.(string); ok {
		d.AuthCode = authCode
	} else {
		return fmt.Errorf("获取AuthCode失败: 返回数据格式错误")
	}
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
		SetFormData(map[string]string{"id": "[" + file.GetID() + "]"}).
		SetResult(&resp).
		Post("https://api.1785677.xyz/opapi/downAllPath")
	if err != nil {
		return nil, err
	}
	if resp.Code != 200 {
		return nil, fmt.Errorf("%s", resp.Msg)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("未返回下载地址")
	}
	return &model.Link{URL: resp.Data[0].URL}, nil
}

func (d *XingChen) MakeDir(_ context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
	var resp BaseResp
	formData := map[string]string{"c_name": dirName}
	if parentDir.GetID() != "" && parentDir.GetID() != "0" {
		formData["c_fid"] = parentDir.GetID()
	}
	_, err := base.RestyClient.R().
		SetQueryParam("authcode", d.AuthCode).
		SetFormData(formData).
		SetResult(&resp).
		Post("https://api.1785677.xyz/opapi/addPath")
	if err != nil {
		return nil, err
	}
	if resp.Code != 200 {
		return nil, fmt.Errorf("%s", resp.Msg)
	}
	return nil, nil
}

func (d *XingChen) Remove(_ context.Context, obj model.Obj) error {
	var resp BaseResp
	_, err := base.RestyClient.R().
		SetQueryParam("authcode", d.AuthCode).
		SetFormData(map[string]string{"id": "[" + obj.GetID() + "]"}).
		SetResult(&resp).
		Post("https://api.1785677.xyz/opapi/delPath")
	if err != nil {
		return err
	}
	if resp.Code != 200 {
		return fmt.Errorf("%s", resp.Msg)
	}
	return nil
}

func (d *XingChen) Rename(_ context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	var resp BaseResp
	_, err := base.RestyClient.R().
		SetQueryParam("authcode", d.AuthCode).
		SetFormData(map[string]string{
			"id":     srcObj.GetID(),
			"c_name": newName,
		}).
		SetResult(&resp).
		Post("https://api.1785677.xyz/opapi/editPath")
	if err != nil {
		return nil, err
	}
	if resp.Code != 200 {
		return nil, fmt.Errorf("%s", resp.Msg)
	}
	return nil, nil
}

func (d *XingChen) Move(_ context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	var resp BaseResp
	formData := map[string]string{"id": "[" + srcObj.GetID() + "]"}
	if dstDir.GetID() != "" && dstDir.GetID() != "0" {
		formData["fid"] = dstDir.GetID()
	}
	_, err := base.RestyClient.R().
		SetQueryParam("authcode", d.AuthCode).
		SetFormData(formData).
		SetResult(&resp).
		Post("https://api.1785677.xyz/opapi/transferPath")
	if err != nil {
		return nil, err
	}
	if resp.Code != 200 {
		return nil, fmt.Errorf("%s", resp.Msg)
	}
	return nil, nil
}

func (d *XingChen) Put(_ context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
	const chunkSize int64 = 100 * 1024 * 1024 // 100MB per chunk
	fileSize := stream.GetSize()
	fileName := stream.GetName()

	params := map[string]string{"authcode": d.AuthCode}
	if dstDir.GetID() != "" && dstDir.GetID() != "0" {
		params["fid"] = dstDir.GetID()
	}

	// 获取上传地址
	var uploadResp UploadResp
	_, err := base.RestyClient.R().SetQueryParams(params).SetResult(&uploadResp).Get("https://api.1785677.xyz/opapi/Getuploads")
	if err != nil {
		return nil, err
	}
	if uploadResp.Code != 200 {
		return nil, fmt.Errorf("获取上传地址失败: %s", uploadResp.Msg)
	}
	if uploadResp.Data.URL == "" {
		return nil, fmt.Errorf("上传节点似乎离线，请稍后重试")
	}

	// 小于1G使用普通上传
	if fileSize <= 1024*1024*1024 {
		uploadURL := uploadResp.Data.URL + "/upload?" + uploadResp.Data.Query
		_, err = base.RestyClient.R().SetFileReader("file", fileName, io.NopCloser(stream)).Post(uploadURL)
		return nil, err
	}

	// 大于1G使用分片上传
	// 计算文件hash（前10MB的MD5）
	hashSize := int64(10 * 1024 * 1024) // 10MB
	if hashSize > fileSize {
		hashSize = fileSize
	}
	hashData := make([]byte, hashSize)
	n, err := io.ReadFull(stream, hashData)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("读取文件计算hash失败: %v", err)
	}
	hashData = hashData[:n]
	fileHash := utils.HashData(utils.MD5, hashData)
	totalChunks := int((fileSize + chunkSize - 1) / chunkSize)

	// 获取已上传分片（断点续传）
	// URL格式: url/uploadedChunks?hash=xxx&[query]
	var uploadedResp struct {
		Data []int `json:"data"`
	}
	uploadedChunksURL := fmt.Sprintf("%s/uploadedChunks?hash=%s&%s", uploadResp.Data.URL, fileHash, uploadResp.Data.Query)
	base.RestyClient.R().SetResult(&uploadedResp).Get(uploadedChunksURL)
	uploadedSet := make(map[int]bool)
	for _, c := range uploadedResp.Data {
		uploadedSet[c] = true
	}

	// 分片上传
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
		// URL格式: url/uploadChunk?hash=xxx&[query]&index=0&totalChunks=10&filename=xxx
		chunkURL := fmt.Sprintf("%s/uploadChunk?hash=%s&%s&index=%d&totalChunks=%d&filename=%s",
			uploadResp.Data.URL, fileHash, uploadResp.Data.Query, i, totalChunks, fileName)
		var chunkResp BaseResp
		_, err = base.RestyClient.R().
			SetFileReader("file", fileName, io.NopCloser(chunk)).
			SetResult(&chunkResp).
			Post(chunkURL)
		if err != nil {
			return nil, fmt.Errorf("分片%d上传失败: %v", i, err)
		}
		if chunkResp.Code != 0 && chunkResp.Code != 200 {
			return nil, fmt.Errorf("分片%d上传失败: %s", i, chunkResp.Msg)
		}
		up(float64(i+1) / float64(totalChunks) * 100)
	}

	// 合并分片
	// URL格式: url/mergeChunks?[query]
	// body参数：filename, hash, totalChunks
	mergeURL := fmt.Sprintf("%s/mergeChunks?%s", uploadResp.Data.URL, uploadResp.Data.Query)
	var mergeResp BaseResp
	_, err = base.RestyClient.R().
		SetFormData(map[string]string{
			"filename":    fileName,
			"hash":        fileHash,
			"totalChunks": fmt.Sprintf("%d", totalChunks),
		}).
		SetResult(&mergeResp).
		Post(mergeURL)
	if err != nil {
		return nil, fmt.Errorf("合并分片失败: %v", err)
	}
	if mergeResp.Code != 0 && mergeResp.Code != 200 {
		return nil, fmt.Errorf("合并分片失败: %s", mergeResp.Msg)
	}
	return nil, nil
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
	if resp.Code != 200 {
		return nil, fmt.Errorf("%s", resp.Msg)
	}
	return resp.Data, nil
}

var _ driver.Driver = (*XingChen)(nil)
