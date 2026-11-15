package setup

// The entry package on macOS and Windows: the same release file the Linux
// install script loads (acornfox-edge_<entry version>_linux_<arch>.tar.gz,
// a root file system for docker import), fetched from the same place as the
// program itself and checked against the same SHA256SUMS. The entry proxy
// image never comes from an image registry.
//
// Sources, in order: ACORNFOX_RELEASE_BASE alone when it is set (tests, a
// mirror of one's own: <base>/releases/download/<tag>/<file>); otherwise the
// Gitee release then the GitHub release in mainland China, the other way
// round elsewhere (ACORNFOX_GITEE_REPO, ACORNFOX_REPO override the
// repositories, as for the install script; empty means the default).

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
)

// Release repositories (the same defaults as the install script).
const (
	defaultReleaseRepo = "OrbitMaker/AcornFox"
	defaultGiteeRepo   = "VIP13390/AcornFox"
)

// EdgePackageFile is the entry package's file name for a Docker daemon of
// goarch (amd64 or arm64).
func EdgePackageFile(goarch string) string {
	return "acornfox-edge_" + edge.ImageVersion + "_linux_" + goarch + ".tar.gz"
}

// releaseBases are the release download roots to try for tag, in order.
func releaseBases(tag string, china bool, getenv func(string) string) []string {
	if b := strings.TrimRight(getenv("ACORNFOX_RELEASE_BASE"), "/"); b != "" {
		return []string{b + "/releases/download/" + tag}
	}
	gh := getenv("ACORNFOX_REPO")
	if gh == "" {
		gh = defaultReleaseRepo
	}
	github := "https://github.com/" + gh + "/releases/download/" + tag
	gr := getenv("ACORNFOX_GITEE_REPO")
	if gr == "" {
		gr = defaultGiteeRepo
	}
	gitee := "https://gitee.com/" + gr + "/releases/download/" + tag
	var out []string
	if china {
		out = append(out, gitee)
	}
	out = append(out, github)
	if !china {
		out = append(out, gitee)
	}
	return out
}

// releaseTag is the release a program version comes from ("0.6.0" ->
// "v0.6.0").
func releaseTag(version string) string {
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

// sumsFromRelease reads the SHA-256 of file from the release's SHA256SUMS,
// trying the bases in order.
func sumsFromRelease(ctx context.Context, c *http.Client, bases []string, file string) (string, error) {
	var lastErr error
	for _, b := range bases {
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		req, err := http.NewRequestWithContext(rctx, http.MethodGet, b+"/SHA256SUMS", nil)
		if err != nil {
			cancel()
			return "", err
		}
		resp, err := c.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s/SHA256SUMS：HTTP %d", b, resp.StatusCode)
			continue
		}
		for _, line := range strings.Split(string(body), "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && strings.TrimPrefix(f[1], "*") == file && len(f[0]) == 64 {
				return strings.ToLower(f[0]), nil
			}
		}
		lastErr = fmt.Errorf("%s/SHA256SUMS 里没有 %s", b, file)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用的下载地址")
	}
	return "", lastErr
}

// FetchEdgePackage downloads the entry package for the program version and
// a Docker daemon of goarch, verified against the release's SHA256SUMS, and
// returns its local path (kept in d.Dir; a verified file is not downloaded
// again). Faults: SETUP-NET-01, SETUP-NET-02, SETUP-SUM-01 like the other
// downloads; a release without the file names it.
func (d *Downloader) FetchEdgePackage(ctx context.Context, version, goarch string, china bool, getenv func(string) string, report func(Item)) (string, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	file := EdgePackageFile(goarch)
	bases := releaseBases(releaseTag(version), china, getenv)
	sum, err := sumsFromRelease(ctx, d.client(), bases, file)
	if err != nil {
		return "", Faultf("SETUP-NET-01", err, "取不到入口包 %s 的校验值", file)
	}
	a := Artifact{ID: "edge-image", Title: "入口镜像", File: file, SHA256: sum}
	for _, b := range bases {
		a.Upstream = append(a.Upstream, b+"/"+file)
	}
	// The order is already the one for this network; Sources must not turn
	// it round again.
	dl := *d
	dl.Sources = Sources{}
	return dl.Fetch(ctx, a, report)
}
