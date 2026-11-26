package main

// 来源标记报告的接线（JW15-3、LW10，internal/installfrom）：服务启动时建一个 Reporter，后台等约 1 分钟后，
// 如果数据目录里有安装脚本写的 install-from 文件、又没报过，就用下载网关客户端调 W1 登记，client.from 填这个标记。
// 没有这个文件（没带标记的安装）什么都不做，不联系网关。环境变量 ACORNFOX_INSTALL_FROM=off 关掉。
// 只有网关应答里 from_recorded 为 true 才算报过；200 但没有它（老网关）按没报成，继续退避重试，最多 7 天。

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/acornfox/acornfox/internal/cli"
	"github.com/acornfox/acornfox/internal/gatewayclient"
	"github.com/acornfox/acornfox/internal/identity"
	"github.com/acornfox/acornfox/internal/installfrom"
)

// newInstallFromReporter builds the reporter for this server. Each try reads the gateway address again
// (gatewayBase), so a gateway turned on or off while the server runs is followed.
func newInstallFromReporter(o serverOptions, logw io.Writer) *installfrom.Reporter {
	var cfg installfrom.Config
	cfg.FromEnv(os.Getenv)
	cfg.DataDir = o.DataDir
	cfg.Logf = func(f string, a ...any) {
		fmt.Fprintf(logw, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(f, a...))
	}
	cfg.Register = func(ctx context.Context, tag string) error {
		return registerWithFrom(ctx, o.DataDir, gatewayBase(o.DataDir), tag)
	}
	return installfrom.New(cfg)
}

// registerWithFrom is W1 with the tag; nil only when the gateway says it recorded the tag.
func registerWithFrom(ctx context.Context, dataDir, base, tag string) error {
	if base == "" {
		return installfrom.ErrGatewayOff
	}
	id, err := identity.LoadOrCreate(dataDir)
	if err != nil {
		return err
	}
	info := gatewayclient.DefaultInfo(cli.Version)
	info.From = tag
	c := &gatewayclient.Client{BaseURL: base, Identity: id, Info: info}
	r, err := c.Register(ctx, info)
	if err != nil {
		return err
	}
	if !r.FromRecorded {
		return installfrom.ErrNotRecorded
	}
	return nil
}
