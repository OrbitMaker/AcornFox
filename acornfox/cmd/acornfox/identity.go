package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/acornfox/acornfox/internal/identity"
	"github.com/acornfox/acornfox/internal/platform"
)

const identityHelp = `用法：acornfox identity show [--json] [--data-dir 目录]
      acornfox identity reset [--yes] [--json] [--data-dir 目录]

这台机器在 AcornFox 下载网关上的身份：一把随机生成的 ECDSA P-256 钥匙和由它算出的安装编号。
不读任何硬件信息。第一次通过下载网关下载运行环境文件时自动生成，存在 <数据目录>/identity/。

  show    只读：打印安装编号和公钥（还没生成时说明原因，不会生成）。
  reset   重新生成钥匙，安装编号随之改变，网关会把这台机器当成新机器。
          默认只试跑（说明会做什么）；加 --yes 才执行。旧钥匙留在 machine-key.pem.prev，
          想撤销就把它改回 machine-key.pem。每次生成、重置都记在 identity/identity.log。

数据目录：Linux /var/lib/acornfox（要 root 权限），Mac ~/Library/Application Support/AcornFox，
Windows %LOCALAPPDATA%\AcornFox；ACORNFOX_DATA_DIR 或 --data-dir 可以改。
关掉下载网关：设置环境变量 ACORNFOX_GATEWAY=（空值），之后不会再联系网关。

例子：
  acornfox identity show --json
  acornfox identity reset          # 试跑
  acornfox identity reset --yes    # 执行
`

type identityOut struct {
	OK        bool   `json:"ok"`
	Exists    bool   `json:"exists"`
	InstallID string `json:"install_id,omitempty"`
	PublicKey string `json:"public_key,omitempty"`
	KeyPath   string `json:"key_path"`
	DryRun    bool   `json:"dry_run,omitempty"`
	Previous  string `json:"previous_install_id,omitempty"`
	Message   string `json:"message,omitempty"`
	Code      string `json:"code,omitempty"`
}

// runIdentity implements `acornfox identity show|reset`. Error codes:
// ID-KEY-01 the key file cannot be read or is not a P-256 key;
// ID-KEY-02 a new key cannot be saved (permissions, disk).
func runIdentity(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, identityHelp)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	sub := args[0]
	fs := flag.NewFlagSet("identity "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, identityHelp) }
	asJSON := fs.Bool("json", false, "输出 JSON")
	dataDir := fs.String("data-dir", platform.DefaultPaths().DataDir, "数据目录")
	yes := fs.Bool("yes", false, "reset 时真正执行（默认只试跑）")
	apply := fs.Bool("apply", false, "同 --yes")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	emit := func(o identityOut, rc int) int {
		if *asJSON {
			b, _ := json.Marshal(o)
			fmt.Fprintln(stdout, string(b))
			return rc
		}
		w := stdout
		if !o.OK {
			w = stderr
		}
		if o.Code != "" {
			fmt.Fprintf(w, "[%s] ", o.Code)
		}
		if o.Message != "" {
			fmt.Fprintln(w, o.Message)
		}
		if o.Exists {
			fmt.Fprintln(w, "安装编号："+o.InstallID)
			fmt.Fprintln(w, "公钥："+o.PublicKey)
		}
		fmt.Fprintln(w, "钥匙文件："+o.KeyPath)
		return rc
	}
	keyPath := identity.KeyPath(*dataDir)
	switch sub {
	case "show":
		id, err := identity.Load(*dataDir)
		if errors.Is(err, identity.ErrNoKey) {
			return emit(identityOut{OK: true, KeyPath: keyPath, Message: err.Error()}, 0)
		}
		if err != nil {
			return emit(identityOut{KeyPath: keyPath, Code: "ID-KEY-01", Message: err.Error()}, 1)
		}
		return emit(identityOut{OK: true, Exists: true, InstallID: id.InstallID, PublicKey: id.PublicKey, KeyPath: keyPath}, 0)
	case "reset":
		old, oldErr := identity.Load(*dataDir)
		prev := ""
		if oldErr == nil {
			prev = old.InstallID
		}
		if !*yes && !*apply {
			msg := "试跑：会生成一把新钥匙，安装编号会改变"
			if prev != "" {
				msg += "（现在是 " + prev + "，旧钥匙会留在 machine-key.pem.prev）"
			}
			return emit(identityOut{OK: true, DryRun: true, KeyPath: keyPath, Previous: prev, Message: msg + "。加 --yes 才执行。"}, 0)
		}
		id, err := identity.Reset(*dataDir)
		if err != nil {
			return emit(identityOut{KeyPath: keyPath, Previous: prev, Code: "ID-KEY-02", Message: "重新生成失败：" + err.Error()}, 1)
		}
		return emit(identityOut{OK: true, Exists: true, InstallID: id.InstallID, PublicKey: id.PublicKey, KeyPath: keyPath, Previous: prev,
			Message: "已重新生成钥匙，下次通过下载网关下载时会按新机器登记"}, 0)
	default:
		fmt.Fprintln(stderr, "未知子命令："+sub)
		fmt.Fprint(stderr, identityHelp)
		return 2
	}
}
