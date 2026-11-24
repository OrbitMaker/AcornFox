package setup

import "github.com/acornfox/acornfox/internal/platform"

// Mac local mode switch.
//
// The Mac build does not run AcornFox as a plain local service (this file's
// switch): it manages AcornFox on a Linux server (acornfox target add, deploy,
// status, logs ...) or on the Debian virtual machine acornfox mac install makes
// (internal/macvm). The commands that would set up
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
const MacLocalMessage = "Mac 版只有命令行客户端，acornfox install、serve 不能在 Mac 上直接装本机服务；要把 AcornFox 装在这台 Mac 上（苹果芯片、macOS 26 起），运行：curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash"

// MacLocalHint is the next step shown with MacLocalMessage.
const MacLocalHint = "1. 要装在这台 Mac 上：运行上面那行命令（装进苹果 container 开的一台 Debian 虚拟机），或先运行 acornfox mac install --plan 看会做什么；" +
	"2. 要管理 Linux 服务器：在服务器上按使用文档用 install.sh 安装 AcornFox，再在这台 Mac 上运行 acornfox target add 名称 --ssh 用户@服务器地址；" +
	"3. 之后 acornfox deploy、status、logs 等命令都作用在登记的服务器上。" +
	"检查服务器用 ssh 用户@服务器地址 sudo acornfox doctor"

// MacLocalBlocked reports whether this computer is a Mac whose local mode is
// switched off (see the comment above).
func MacLocalBlocked() bool { return platform.GOOS == "darwin" && !MacLocalEnabled }
