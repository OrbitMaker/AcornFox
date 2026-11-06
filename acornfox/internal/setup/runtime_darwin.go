//go:build darwin

package setup

// The macOS built-in runtime: Colima + Lima (Apple Virtualization framework,
// no window), with Docker inside an Ubuntu VM. Colima's own files live under
// ~/.afx/colima (short, because the Docker socket path must stay under 104
// bytes); the programs under <data>/runtime/. The VM image is downloaded by
// our downloader (mirrors, resume, checksum) and handed to Colima with
// --disk-image, so Colima does not fetch it from GitHub itself.
//
// Status (2026-11-01): written, not yet run on a Mac (Mac testing deferred).

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	builtinSupported = true
	colimaProfile    = "acornfox"
	colimaName       = "AcornFox 自带（Colima）"
)

type osRuntime struct{}

func newOSRuntime() *osRuntime { return &osRuntime{} }

func desktopInstalled() (bool, string) {
	if _, err := os.Stat("/Applications/Docker.app"); err == nil {
		return true, ""
	}
	return false, ""
}

func openDockerDesktop(ctx context.Context) error {
	return exec.CommandContext(ctx, "open", "-g", "-a", "Docker").Run()
}

func colimaHome() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".afx", "colima")
}

func colimaSocket() string {
	return filepath.Join(colimaHome(), colimaProfile, "docker.sock")
}

func (d *desktopRuntime) builtinRun(ctx context.Context, opts StartOptions, r Reporter) (Detection, error) {
	host := "unix://" + colimaSocket()
	if v, _, _, err := pingDocker(ctx, host); err == nil {
		return Detection{Reachable: true, Host: host, Kind: RuntimeBuiltin, Name: colimaName, Version: v}, nil
	}
	rt := filepath.Join(d.cfg.DataDir, "runtime")
	bin := filepath.Join(rt, "bin")
	limaDir := filepath.Join(rt, "lima")
	dl := d.downloader()
	if opts.Mirror != "" {
		dl.Sources.Base = opts.Mirror
	}
	report := func(it Item) { r.Item(it) }
	arch := runtime.GOARCH

	// 1. Programs: colima and lima (limactl + guest agents).
	colima := filepath.Join(bin, "colima")
	if _, err := os.Stat(colima); err != nil {
		p, err := dl.Fetch(ctx, colimaBinary(arch), report)
		if err != nil {
			return Detection{}, err
		}
		if err := os.MkdirAll(bin, 0o700); err != nil {
			return Detection{}, err
		}
		if err := copyExec(p, colima); err != nil {
			return Detection{}, err
		}
	}
	if _, err := os.Stat(filepath.Join(limaDir, "bin", "limactl")); err != nil {
		p, err := dl.Fetch(ctx, limaTarball(arch), report)
		if err != nil {
			return Detection{}, err
		}
		if err := os.MkdirAll(limaDir, 0o700); err != nil {
			return Detection{}, err
		}
		if out, err := exec.CommandContext(ctx, "tar", "-xzf", p, "-C", limaDir).CombinedOutput(); err != nil {
			return Detection{}, Faultf("SETUP-RT-02", err, "解压 Lima 失败（%s）", tail(string(out), 200))
		}
	}

	// 2. The VM image (gzip-compressed raw disk), unpacked once.
	img := filepath.Join(rt, "downloads", "colima-docker-"+arch+".raw")
	if _, err := os.Stat(img); err != nil {
		gz, err := dl.Fetch(ctx, colimaImage(arch), report)
		if err != nil {
			return Detection{}, err
		}
		r.Item(Item{ID: "unpack", Title: "解压虚拟机镜像", State: StateRunning})
		if err := gunzip(gz, img); err != nil {
			return Detection{}, Faultf("SETUP-RT-02", err, "解压虚拟机镜像失败")
		}
		r.Item(Item{ID: "unpack", Title: "解压虚拟机镜像", State: StateDone})
	}

	// 3. Start the VM (first start creates it).
	r.Item(Item{ID: "start", Title: "启动运行环境", State: StateRunning, Detail: "2 核 4 GB"})
	c := exec.CommandContext(ctx, colima, "start", colimaProfile,
		"--runtime", "docker", "--vm-type", "vz", "--cpu", "2", "--memory", "4", "--disk", "60",
		"--disk-image", img)
	c.Env = append(os.Environ(),
		"COLIMA_HOME="+colimaHome(),
		"PATH="+filepath.Join(limaDir, "bin")+":"+bin+":"+os.Getenv("PATH"))
	out, err := c.CombinedOutput()
	d.cfg.Logf("setup colima start: %s", tail(string(out), 2000))
	if err != nil {
		return Detection{}, Faultf("SETUP-RT-02", err, "自带运行环境没能启动（%s）", tail(string(out), 300))
	}
	v, err := waitDocker(ctx, host, 60*time.Second)
	if err != nil {
		return Detection{}, Faultf("SETUP-RT-02", err, "自带运行环境启动后 Docker 没有应答")
	}
	r.Item(Item{ID: "start", Title: "启动运行环境", State: StateDone, Detail: "Docker " + v})
	return Detection{Reachable: true, Host: host, Kind: RuntimeBuiltin, Name: colimaName, Version: v}, nil
}

func copyExec(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(dst+".tmp", dst)
}

func gunzip(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	zr, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, zr); err != nil {
		out.Close()
		os.Remove(dst + ".tmp")
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if !strings.HasSuffix(dst, ".raw") {
		return errors.New("unexpected image name")
	}
	return os.Rename(dst+".tmp", dst)
}
