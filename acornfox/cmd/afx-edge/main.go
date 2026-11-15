// Command afx-edge is the adapter inside the entry proxy container: the
// container's first process. It starts the upstream proxy software and
// supervises it, serves the acornfox program on its own management endpoint
// (protocol in internal/edge/wire.go), translates each configuration into the
// proxy's settings, keeps the last one on the settings volume and applies it
// again whenever the container starts.
//
// It is built separately from the acornfox program (scripts/build-release.sh)
// and only ever runs inside the entry proxy container.
//
// Usage:
//
//	afx-edge run --admin-listen 127.0.0.1:18807   # the container's command
//	afx-edge health [--json] [--want <version>]   # health check inside the container
//	afx-edge version
//
// Exit codes: run returns 0 after a stop signal (the proxy was stopped
// gracefully), 1 when the proxy exited on its own or could not start (Docker
// then restarts the container), 2 for a usage error. health returns 0 when
// healthy and 1 otherwise.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
	"github.com/acornfox/acornfox/internal/edgeadapter"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usage = `afx-edge：入口代理容器里的适配程序（容器的第一个进程）。

用法：
  afx-edge run --admin-listen 127.0.0.1:18807   拉起代理程序并管它（容器的启动命令）
  afx-edge health [--json] [--want 版本号]       在容器里检查：设置已装上、版本号对、端口都在听
  afx-edge version                               显示入口镜像版本

run 的其他参数（一般不用改，默认就是容器里的位置）：
  --token-file   管理令牌文件（默认 ` + edge.TokenFile + `）
  --state-dir    保存设置的目录（默认 ` + edge.StateDir + `）
  --runtime-dir  运行时目录（默认 ` + edgeadapter.DefaultRuntimeDir + `）
  --proxy        代理程序（默认 ` + edgeadapter.DefaultProxyBin + `）

副作用：run 只写设置目录和运行时目录；health 和 version 只读。
`

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "version", "--version":
		fmt.Fprintln(stdout, edge.ImageVersion)
		return 0
	case "run":
		return runRun(args[1:], stderr)
	case "health":
		return runHealth(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "不认识的子命令 %q\n\n%s", args[0], usage)
	return 2
}

func runRun(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o edgeadapter.Options
	fs.StringVar(&o.AdminListen, "admin-listen", "", "管理入口地址")
	fs.StringVar(&o.TokenFile, "token-file", "", "管理令牌文件")
	fs.StringVar(&o.StateDir, "state-dir", "", "保存设置的目录")
	fs.StringVar(&o.RuntimeDir, "runtime-dir", "", "运行时目录")
	fs.StringVar(&o.ProxyBin, "proxy", "", "代理程序")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if o.AdminListen == "" || fs.NArg() != 0 {
		fmt.Fprint(stderr, "run 要写 --admin-listen 地址，不接受别的参数\n")
		return 2
	}
	o.CheckDefaultAdmin = true
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := edgeadapter.Run(ctx, o); err != nil {
		fmt.Fprintln(stderr, "afx-edge:", err)
		return 1
	}
	return 0
}

func runHealth(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "输出 JSON")
	var o edgeadapter.CheckOptions
	fs.StringVar(&o.Want, "want", "", "要求的设置版本号")
	fs.StringVar(&o.StateDir, "state-dir", "", "保存设置的目录")
	fs.StringVar(&o.RuntimeDir, "runtime-dir", "", "运行时目录")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rep := edgeadapter.Check(ctx, o)
	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(rep)
	} else if rep.OK {
		fmt.Fprintf(stdout, "正常：设置 %s，%d 条路由，%d 个端口在听\n", rep.Version, rep.Routes, len(rep.Ports))
	} else {
		fmt.Fprintf(stdout, "有问题：%s\n", strings.Join(rep.Problems, "；"))
	}
	if !rep.OK {
		return 1
	}
	return 0
}
