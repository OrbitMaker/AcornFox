package setup

import "github.com/acornfox/acornfox/internal/platform"

// Mac local mode switch.
//
// This release ships the Mac build as a command-line client only: it manages
// AcornFox on a Linux server (acornfox target add, deploy, status, logs ...)
// and does not run AcornFox on the Mac itself. The commands that would set up
// the Mac's own runtime (install, serve, setup and doctor against this Mac)
// stop at MacLocalBlocked with fault SETUP-MAC-01 instead of downloading and
// starting the built-in virtual machine. The Mac runtime code (Colima, Lima,
// runtime_darwin.go) stays in the tree for when local runs are released.
//
// To turn the Mac local mode back on, build with the tag acornfox_maclocal
// (go build -tags acornfox_maclocal ./cmd/acornfox), or flip the constant in
// maclocal_off.go to true once Mac local runs are released.

// MacLocalCode is the fault code of the blocked Mac local mode.
const MacLocalCode = "SETUP-MAC-01"

// MacLocalMessage is what the blocked commands print.
const MacLocalMessage = "Mac 版只有命令行客户端，不能在 Mac 本机运行 AcornFox 服务；请把 AcornFox 装在一台 Linux 服务器上，用这台 Mac 上的命令行管理（acornfox target add …）"

// MacLocalHint is the next step shown with MacLocalMessage.
const MacLocalHint = "1. 在一台 Linux 服务器（amd64 或 arm64）上按使用文档用 install.sh 安装 AcornFox；" +
	"2. 在这台 Mac 上运行 acornfox target add 名称 --ssh 用户@服务器地址；" +
	"3. 之后 acornfox deploy、status、logs 等命令都作用在这台服务器上。" +
	"检查服务器用 ssh 用户@服务器地址 sudo acornfox doctor"

// MacLocalBlocked reports whether this computer is a Mac whose local mode is
// switched off (see the comment above).
func MacLocalBlocked() bool { return platform.GOOS == "darwin" && !MacLocalEnabled }
