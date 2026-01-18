package xingchen

import (
	"fmt"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

type File struct {
	ID    int    `json:"id"`
	CName string `json:"c_name"`
	CSize int64  `json:"c_size"`
	CType string `json:"c_type"`
	CTime string `json:"c_time"`
}

func (f *File) GetID() string           { return fmt.Sprintf("%d", f.ID) }
func (f *File) GetName() string         { return f.CName }
func (f *File) GetSize() int64          { return f.CSize }
func (f *File) IsDir() bool             { return f.CType == "folder" }
func (f *File) ModTime() time.Time      { t, _ := time.Parse("2006-01-02 15:04:05", f.CTime); return t }
func (f *File) CreateTime() time.Time   { return f.ModTime() }
func (f *File) GetPath() string         { return "" }
func (f *File) GetHash() utils.HashInfo { return utils.HashInfo{} }

var _ model.Obj = (*File)(nil)

type FileListResp struct {
	Code int    `json:"code"`
	Data []File `json:"data"`
}

type DownloadResp struct {
	Code int `json:"code"`
	Data struct {
		URL string `json:"url"`
	} `json:"data"`
}

type UploadResp struct {
	Code int `json:"code"`
	Data struct {
		URL   string `json:"url"`
		Query string `json:"query"`
	} `json:"data"`
}
