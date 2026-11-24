package macvm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/gatewayclient"
	"github.com/acornfox/acornfox/internal/identity"
)

// Artifact is one file with a pinned checksum. It is fetched from the first
// source that works: the AcornFox download gateway (when it has the file),
// then the mirrors in mainland China and the original address, in the order
// that suits the region.
type Artifact struct {
	ID        string
	Title     string
	File      string
	SHA256    string
	Size      int64
	GatewayID string // the file's ID on the gateway; "" = the gateway does not have it
	Mirrors   []string
	Upstream  []string
}

const ghMirror = "https://files.m.daocloud.io/github.com/"

func ghArtifact(id, title, file, path, sum string, size int64, gatewayID string) Artifact {
	return Artifact{ID: id, Title: title, File: file, SHA256: sum, Size: size, GatewayID: gatewayID,
		Mirrors: []string{ghMirror + path}, Upstream: []string{"https://github.com/" + path}}
}

// ContainerPkg is Apple's signed installer of container 1.7.1 (on the
// gateway since 2026-11-19).
func ContainerPkg() Artifact {
	return ghArtifact("container-pkg", "苹果的 container 安装包", "container-1.7.1-installer-signed.pkg",
		"apple/container/releases/download/1.7.1/container-1.7.1-installer-signed.pkg",
		"b6739caf3d47ca53a78f1a7edf32521420a75c7941838c3cefa75f30b80f5a97", 169630704, "apple-container-1.7.1-pkg")
}

// KataKernel is the archive the default kernel of container 1.7.1 comes from.
func KataKernel() Artifact {
	return ghArtifact("kernel", "虚拟机内核", "kata-static-4.3.1-arm64.tar.zst",
		"kata-containers/kata-containers/releases/download/4.3.1/kata-static-4.3.1-arm64.tar.zst",
		"2c5bf7cde334ad5307d482f45cceda496f91c9b6b96f5278c47f4f4eac1bbae6", 676287269, "kata-static-4.3.1-arm64")
}

// KernelMember is the kernel's path inside KataKernel's archive.
const KernelMember = "opt/kata/share/kata-containers/vmlinux-6.18.56-208-debug"

// Fetcher downloads Artifacts.
type Fetcher struct {
	Dir     string // finished and half-finished downloads
	China   bool
	Gateway string // the download gateway's address; "" = not used
	DataDir string // where the machine key lives (<DataDir>/identity)
	Version string // reported to the gateway
	HTTP    *http.Client
	Say     func(format string, args ...any)
	// Progress is called about once a second while a file comes in.
	Progress func(a Artifact, source string, p appinstall.FileProgress)

	gw *gatewayclient.Client
}

func (f *Fetcher) say(format string, args ...any) {
	if f.Say != nil {
		f.Say(format, args...)
	}
}

func (f *Fetcher) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second}}
}

func (f *Fetcher) gateway() (*gatewayclient.Client, error) {
	if f.gw != nil {
		return f.gw, nil
	}
	id, err := identity.LoadOrCreate(f.DataDir)
	if err != nil {
		return nil, err
	}
	v := f.Version
	if v == "" {
		v = "dev"
	}
	f.gw = &gatewayclient.Client{BaseURL: f.Gateway, Identity: id, Info: gatewayclient.DefaultInfo(strings.TrimPrefix(v, "v"))}
	return f.gw, nil
}

type candidate struct {
	name    string
	resolve func(ctx context.Context) (appinstall.FileSource, error)
}

func (f *Fetcher) candidates(a Artifact) []candidate {
	var out []candidate
	if f.Gateway != "" && a.GatewayID != "" {
		out = append(out, candidate{name: "AcornFox 下载网关", resolve: func(ctx context.Context) (appinstall.FileSource, error) {
			gw, err := f.gateway()
			if err != nil {
				return appinstall.FileSource{}, err
			}
			iss, err := gw.Issue(ctx, a.GatewayID, "runtime")
			if err != nil {
				return appinstall.FileSource{}, fmt.Errorf("下载网关没有给出地址：%w", err)
			}
			conv := func(iss gatewayclient.Issued) (string, time.Time) {
				// The expiry is by the gateway's clock; judge it by how long it has left on ours.
				exp := iss.ExpiresAt.Add(-gw.ClockOffset())
				if rem := time.Until(exp); rem < 30*time.Second || rem > 20*time.Minute {
					exp = time.Time{}
				}
				return iss.URL, exp
			}
			u, exp := conv(iss)
			return appinstall.FileSource{URL: u, ExpiresAt: exp, Reissue: func(ctx context.Context) (string, time.Time, error) {
				again, err := gw.Issue(ctx, a.GatewayID, "runtime")
				if err != nil {
					return "", time.Time{}, err
				}
				u, exp := conv(again)
				return u, exp, nil
			}}, nil
		}})
	}
	first, second := a.Upstream, a.Mirrors
	if f.China {
		first, second = a.Mirrors, a.Upstream
	}
	for _, u := range append(append([]string{}, first...), second...) {
		u := u
		out = append(out, candidate{name: hostOf(u), resolve: func(context.Context) (appinstall.FileSource, error) {
			return appinstall.FileSource{URL: u}, nil
		}})
	}
	return out
}

func hostOf(u string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return s
}

// DownloadError says which file could not be brought and why, source by source.
type DownloadError struct {
	File    string
	Reasons []string
}

func (e *DownloadError) Error() string {
	return fmt.Sprintf("%s 下载失败：%s", e.File, strings.Join(e.Reasons, "；"))
}

// Fetch returns the path of the verified file, downloading it when it is not
// already in Dir.
func (f *Fetcher) Fetch(ctx context.Context, a Artifact) (string, error) {
	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return "", err
	}
	dest := filepath.Join(f.Dir, a.File)
	if fileHasSHA(dest, a.SHA256) {
		f.say("%s 已经下载过，校验通过，直接用", a.Title)
		return dest, nil
	}
	var reasons []string
	for _, c := range f.candidates(a) {
		src, err := c.resolve(ctx)
		if err != nil {
			reasons = append(reasons, c.name+"："+err.Error())
			continue
		}
		f.say("下载 %s（来源：%s）", a.Title, c.name)
		var prog func(appinstall.FileProgress)
		if f.Progress != nil {
			name := c.name
			prog = func(p appinstall.FileProgress) { f.Progress(a, name, p) }
		}
		err = appinstall.DownloadFile(ctx, f.client(), src, filepath.Join(f.Dir, "dl-cache"), dest, a.SHA256, a.Size, prog)
		if err == nil {
			return dest, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		reasons = append(reasons, c.name+"："+err.Error())
		f.say("这个来源没成（%s），换下一个", firstLine(err.Error()))
	}
	return "", &DownloadError{File: a.File, Reasons: reasons}
}

func fileHasSHA(path, want string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want)
}

// FileSHA256 is a file's sha256 in lower-case hex.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// simpleGet downloads a URL into dest with one connection (files whose
// checksum is not pinned: the Debian root file system).
func simpleGet(ctx context.Context, hc *http.Client, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("连不上 %s：%w", hostOf(url), errors.Unwrap(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s 返回 HTTP %d", hostOf(url), resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

// getText reads a small page.
func getText(ctx context.Context, hc *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(b), err
}

var lxcDirRE = regexp.MustCompile(`href="(\d{8}_\d{2}(?::|%3[Aa])\d{2})/"`)

// LatestLXCDir picks the newest dated folder out of a listing page of an LXC
// image tree (…/debian/trixie/arm64/default/).
func LatestLXCDir(listing string) string {
	var dirs []string
	for _, m := range lxcDirRE.FindAllStringSubmatch(listing, -1) {
		dirs = append(dirs, strings.ReplaceAll(strings.ReplaceAll(m[1], "%3A", ":"), "%3a", ":"))
	}
	if len(dirs) == 0 {
		return ""
	}
	sort.Strings(dirs)
	return dirs[len(dirs)-1]
}

// LXCRootfsBases are the places a Debian 13 arm64 root file system (with
// systemd; LXC image format) is kept, the ones in mainland China first when
// china is set.
func LXCRootfsBases(china bool) []string {
	cn := []string{
		"https://mirrors.tuna.tsinghua.edu.cn/lxc-images/images/debian/trixie/arm64/default/",
		"https://mirrors.ustc.edu.cn/lxc-images/images/debian/trixie/arm64/default/",
	}
	world := []string{"https://images.linuxcontainers.org/images/debian/trixie/arm64/default/"}
	if china {
		return append(cn, world...)
	}
	return append(world, cn...)
}
