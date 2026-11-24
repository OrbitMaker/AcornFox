//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
)

// The server + runner pair does not run as a Windows program: on Windows
// AcornFox is the Linux build inside WSL (install-windows.ps1 installs it; the
// acornfox command on Windows forwards to it). This Windows build is a
// command-line client only.

const serverOnly = "acornfox: 该命令只在 Linux 里运行。Windows 上的 AcornFox 装在 WSL 里（用 get.ps1 安装），Windows 上的 acornfox 命令会转给它"

func runServer(_ []string, _ io.Writer, stderr io.Writer) int {
	fmt.Fprintln(stderr, serverOnly)
	return 2
}

func runRunner(_ ...string) int {
	fmt.Fprintln(os.Stderr, serverOnly)
	return 2
}
