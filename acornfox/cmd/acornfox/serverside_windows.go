//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
)

// The Linux server + runner pair does not run on Windows; Windows runs the
// single-process "acornfox serve" instead (installed by "acornfox install").

const serverOnly = "acornfox: 该命令只在 Linux 服务器上运行；Windows 上请用 acornfox install（后台运行 acornfox serve）"

func runServer(_ []string, _ io.Writer, stderr io.Writer) int {
	fmt.Fprintln(stderr, serverOnly)
	return 2
}

func runRunner(_ ...string) int {
	fmt.Fprintln(os.Stderr, serverOnly)
	return 2
}
