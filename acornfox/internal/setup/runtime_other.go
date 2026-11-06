//go:build !darwin && !windows

package setup

import (
	"context"
	"errors"
)

// Linux and other systems: no built-in runtime (Linux servers get Docker
// from install.sh); "acornfox serve" on Linux only detects Docker.
const builtinSupported = false

type osRuntime struct{}

func newOSRuntime() *osRuntime { return &osRuntime{} }

func desktopInstalled() (bool, string) { return false, "" }

func openDockerDesktop(context.Context) error { return errors.New("这个系统没有 Docker Desktop") }

func (d *desktopRuntime) builtinRun(context.Context, StartOptions, Reporter) (Detection, error) {
	return Detection{}, Faultf("SETUP-RT-03", nil, "这个系统没有自带运行环境，请先安装 Docker")
}
