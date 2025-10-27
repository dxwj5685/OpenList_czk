package czk

import (
    "github.com/OpenListTeam/OpenList/v4/internal/driver"
    "github.com/OpenListTeam/OpenList/v4/internal/op"
)

// Addition 定义了用户需要填写的配置项
type Addition struct {
    // API Key 和 API Secret 用于认证
    APIKey    string `json:"api_key" required:"true"`
    APISecret string `json:"api_secret" required:"true"`
}

// config 定义了驱动的基本信息和能力
var config = driver.Config{
    Name:              "星辰云盘", // 驱动名称
    LocalSort:         false,             // 不支持本地排序
    OnlyLinkMFile:     false,             // 不仅支持链接媒体文件
    OnlyProxy:         false,             // 不仅支持代理
    NoCache:           false,             // 支持缓存
    NoUpload:          false,             // 支持上传
    NeedMs:            false,             // 不需要毫秒级时间戳
    DefaultRoot:       "0",               // 默认根目录ID
    CheckStatus:       false,             // 不需要检查状态
    Alert:             "",                // 无特殊警告
    NoOverwriteUpload: false,             // 允许覆盖上传
    NoLinkURL:         false,             // 支持获取直链
}

// init 在包被导入时自动注册驱动
func init() {
    op.RegisterDriver(func() driver.Driver {
        return &CZK{}
    })
}