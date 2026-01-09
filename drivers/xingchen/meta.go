package xingchen

import (
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
)

type Addition struct {
	driver.RootID
	AID      string `json:"aid" required:"true" help:"用户秘钥 AID"`
	Key      string `json:"key" required:"true" help:"用户秘钥 KEY"`
	AuthCode string
}

var config = driver.Config{
	Name:        "星辰云盘",
	DefaultRoot: "0",
	LocalSort:   true,
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &XingChen{}
	})
}
