package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/acornfox/acornfox/internal/cli"
	"github.com/acornfox/acornfox/internal/edge"
)

// runVersion implements `acornfox version`: print version information. The
// version string is cli.Version, set at build time via
// -ldflags "-X github.com/acornfox/acornfox/internal/cli.Version=x.y.z".
// `acornfox --json version` and `acornfox version --json` are handled by
// internal/cli instead.
//
// `acornfox version --edge` prints only the entry proxy image version this
// program runs (edge.ImageVersion, e.g. 2.8.4-1). The install and upgrade
// scripts ask the new program for it to pick the entry package
// acornfox-edge_<version>_linux_<arch>.tar.gz and the image tag. Read only.
func runVersion(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	edgeOnly := fs.Bool("edge", false, "只打印入口镜像版本")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *edgeOnly {
		fmt.Fprintln(stdout, edge.ImageVersion)
		return 0
	}
	fmt.Fprintf(stdout, "acornfox %s\n", cli.Version)
	return 0
}
