package xingchen

import (
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

type File struct {
	ID       string `json:"id"`
	FileName string `json:"file_name"`
	Size     int64  `json:"size"`
	IsDir_   int    `json:"is_dir"`
	UpdateAt string `json:"update_at"`
}

func (f *File) GetID() string           { return f.ID }
func (f *File) GetName() string         { return f.FileName }
func (f *File) GetSize() int64          { return f.Size }
func (f *File) IsDir() bool             { return f.IsDir_ == 1 }
func (f *File) ModTime() time.Time      { t, _ := time.Parse("2006-01-02 15:04:05", f.UpdateAt); return t }
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
	Code int    `json:"code"`
	Data string `json:"data"`
}
