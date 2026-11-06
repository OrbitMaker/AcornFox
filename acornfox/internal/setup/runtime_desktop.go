package setup

import (
	"context"
	"path/filepath"

	"github.com/acornfox/acornfox/internal/platform"
)

// DesktopRuntimeConfig configures the macOS / Windows runtime provider.
type DesktopRuntimeConfig struct {
	DataDir string
	Network func() platform.Network
	Logf    func(format string, args ...any)
	Version string // acornfox version, sent to the download gateway
}

// desktopRuntime finds Docker the way every acornfox command does
// (platform.ResolveDocker) and installs the built-in runtime when asked. The
// built-in parts are per OS: builtinRun, builtinHost, desktopInstalled and
// openDockerDesktop live in runtime_darwin.go, runtime_windows.go and
// runtime_other.go.
type desktopRuntime struct {
	cfg DesktopRuntimeConfig
	os  *osRuntime // per-OS state (the WSL supervisor on Windows)
}

// NewDesktopRuntime returns the provider used by "acornfox serve". On Linux
// it only detects (CanBuiltin is false): Linux servers get Docker from
// install.sh.
func NewDesktopRuntime(cfg DesktopRuntimeConfig) RuntimeProvider {
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.Network == nil {
		cfg.Network = func() platform.Network { return platform.LoadNetwork(cfg.DataDir) }
	}
	return &desktopRuntime{cfg: cfg, os: newOSRuntime()}
}

func (d *desktopRuntime) downloader() *Downloader {
	n := d.cfg.Network()
	src := Sources{Base: n.EffectiveDownloadBase(), China: n.China}
	// The download gateway goes right after our own source / --mirror; it
	// is off while EffectiveGateway is empty (ACORNFOX_GATEWAY= turns it off).
	if gw := n.EffectiveGateway(); gw != "" {
		src.Gateway = newLazyGateway(gw, d.cfg.DataDir, d.cfg.Version)
	}
	return &Downloader{
		Dir:     filepath.Join(d.cfg.DataDir, "runtime", "downloads"),
		Sources: src,
	}
}

func (d *desktopRuntime) Detect(ctx context.Context) Detection {
	var out Detection
	// The built-in runtime first when it is installed.
	if r, ok := platform.LoadBuiltinRuntime(d.cfg.DataDir); ok {
		v, _, _, err := pingDocker(ctx, r.Host)
		out.Found = append(out.Found, Found{Kind: RuntimeBuiltin, Name: r.Name, Version: v, Running: err == nil})
		if err == nil {
			out.Reachable, out.Host, out.Kind, out.Name, out.Version = true, r.Host, RuntimeBuiltin, r.Name, v
		}
	}
	ep := platform.ResolveDocker()
	if ep.Source != "AcornFox 自带运行环境" {
		v, osName, name, err := pingDocker(ctx, ep.Host)
		rtName := platform.RuntimeFromInfo(ep.Runtime, osName, name)
		if err == nil {
			out.Found = append(out.Found, Found{Kind: kindOf(rtName), Name: rtName, Version: v, Running: true})
			if !out.Reachable {
				out.Reachable, out.Host, out.Kind, out.Name, out.Version = true, ep.Host, kindOf(rtName), rtName, v
			}
		}
	}
	if ok, ver := desktopInstalled(); ok {
		running := false
		for _, f := range out.Found {
			if f.Kind == RuntimeDockerDesktop {
				running = f.Running
			}
		}
		if !running {
			out.Found = append(out.Found, Found{Kind: RuntimeDockerDesktop, Name: "Docker Desktop", Version: ver, Running: false})
		}
	}
	return out
}

func (d *desktopRuntime) CanBuiltin() bool { return builtinSupported }

func (d *desktopRuntime) StartDockerDesktop(ctx context.Context) error { return openDockerDesktop(ctx) }

func (d *desktopRuntime) Builtin(ctx context.Context, opts StartOptions, r Reporter) (Detection, error) {
	det, err := d.builtinRun(ctx, opts, r)
	if err != nil {
		return det, err
	}
	if err := platform.SaveBuiltinRuntime(d.cfg.DataDir, platform.BuiltinRuntime{Kind: RuntimeBuiltin, Host: det.Host, Name: det.Name}); err != nil {
		return det, err
	}
	return det, nil
}
