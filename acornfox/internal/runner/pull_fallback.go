package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// How Docker Hub images (Caddy, the add-on databases, and app images such as
// nginx) and ghcr.io images are pulled. Pulls from Docker Hub can hang without an error from
// inside mainland China (seen on macOS Docker Desktop, which has no registry
// mirror by default: caddy:2.8.4-alpine made no progress for 7 minutes).
//
//   - Region "cn" (users in mainland China use domestic sources): pull from the
//     mirrors first by full reference and tag the result back to the
//     original name; Docker Hub is only the last resort.
//   - Otherwise: Docker Hub first, the mirrors only when it stalls or fails.
//
// A pull that sends no progress for pullStallTimeout is abandoned and the
// next source is tried. ACORNFOX_REGISTRY_MIRRORS overrides the mirror hosts
// (comma separated; an empty value disables mirrors). On Linux the installer
// also configures the Docker daemon's registry-mirrors for China, so daemon
// pulls go to the mirror even before this code runs.
const pullStallTimeout = 45 * time.Second

// DefaultRegistryMirrors mirror Docker Hub, fastest first as measured from
// mainland China (DaoCloud ahead of 1Panel's mirror; both are slow, so our
// own source will go first once it exists).
var DefaultRegistryMirrors = []string{"docker.m.daocloud.io", "docker.1panel.live"}

// TencentRegistryMirror is Tencent Cloud's internal Docker Hub accelerator;
// it only answers from Tencent Cloud servers, which put it first.
const TencentRegistryMirror = "mirror.ccs.tencentyun.com"

// CloudRegistryMirrors returns the Docker Hub mirrors for a server in
// mainland China on cloud, the vendor the Linux installer detected from the
// instance metadata (ACORNFOX_CLOUD in /etc/acornfox/install.env). nil means
// "use the default order" (DefaultRegistryMirrors or
// ACORNFOX_REGISTRY_MIRRORS).
//
//   - tencent: the free internal accelerator first, then the defaults.
//   - aliyun, huawei: their Docker Hub accelerators are per-account addresses
//     (one per user's container registry), there is no shared free one to
//     put in code, so the default order stays.
//   - volcengine: its internal mirror site carries APT and docker-ce
//     packages, not a Docker Hub registry mirror, so the default order stays.
//   - anything else, or empty: the default order.
//
// An explicit ACORNFOX_REGISTRY_MIRRORS always wins over the vendor's mirror.
func CloudRegistryMirrors(cloud string) []string {
	if _, set := os.LookupEnv("ACORNFOX_REGISTRY_MIRRORS"); set {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(cloud)) {
	case "tencent":
		return append([]string{TencentRegistryMirror}, DefaultRegistryMirrors...)
	}
	return nil
}

// DefaultGHCRMirrors mirror ghcr.io (Open WebUI and others publish there).
// Nanjing University's mirror measured about 15 MB/s from Aliyun and Tencent
// servers. ACORNFOX_GHCR_MIRRORS overrides it like ACORNFOX_REGISTRY_MIRRORS.
var DefaultGHCRMirrors = []string{"ghcr.nju.edu.cn"}

var errPullStalled = errors.New("拉取镜像长时间没有进展")

// IsPullStalled reports whether err comes from a pull that made no progress
// on every source (the setup shows fault SETUP-NET-02 for it).
func IsPullStalled(err error) bool {
	return err != nil && (errors.Is(err, errPullStalled) || strings.Contains(err.Error(), errPullStalled.Error()))
}

// PullProgress receives the progress of an image pull: source is the
// reference being pulled (a mirror or Docker Hub), done and total are bytes
// summed over the layers seen so far (total grows as layers are announced).
type PullProgress func(source string, done, total int64)

type pullProgressKey struct{}

// WithPullProgress makes pulls under ctx report their progress to fn (the
// setup page shows it for the gateway image).
func WithPullProgress(ctx context.Context, fn PullProgress) context.Context {
	return context.WithValue(ctx, pullProgressKey{}, fn)
}

// Registry selects the pull order.
type Registry struct {
	China   bool     // mirrors first
	Mirrors []string // nil => registryMirrors()
}

// SetRegistry sets how Docker Hub images are pulled.
func (d *Docker) SetRegistry(r Registry) { d.registry = r }

func (d *Docker) mirrors() []string { return d.registry.mirrors() }

func (r Registry) mirrors() []string {
	if r.Mirrors != nil {
		return r.Mirrors
	}
	return registryMirrors()
}

// DockerHubRegistry is Docker Hub's registry host; PullSources names Docker
// Hub itself with it.
const DockerHubRegistry = "registry-1.docker.io"

// PullSources lists the registry hosts a Docker Hub image is pulled from
// under r, in the order pullFallback tries them: in mainland China the
// mirrors first and Docker Hub last; elsewhere Docker Hub first and the
// mirrors after it. The setup's registry check probes them in this order.
func (r Registry) PullSources() []string {
	mirrors := r.mirrors()
	out := make([]string, 0, len(mirrors)+1)
	if r.China && len(mirrors) > 0 {
		out = append(out, mirrors...)
		return append(out, DockerHubRegistry)
	}
	out = append(out, DockerHubRegistry)
	return append(out, mirrors...)
}

func registryMirrors() []string {
	return mirrorsFromEnv("ACORNFOX_REGISTRY_MIRRORS", DefaultRegistryMirrors)
}

func ghcrMirrors() []string { return mirrorsFromEnv("ACORNFOX_GHCR_MIRRORS", DefaultGHCRMirrors) }

func mirrorsFromEnv(name string, def []string) []string {
	v, ok := os.LookupEnv(name)
	if !ok {
		return def
	}
	var out []string
	for _, m := range strings.Split(v, ",") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

// mirrorRef rewrites a Docker Hub reference to the same image on mirror, or
// returns "" for images from other registries.
func mirrorRef(ref, mirror string) string { return rewriteRef(ref, "docker.io", mirror) }

// ghcrMirrorRef does the same for ghcr.io references.
func ghcrMirrorRef(ref, mirror string) string { return rewriteRef(ref, "ghcr.io", mirror) }

func rewriteRef(ref, domain, mirror string) string {
	named, err := reference.ParseNormalizedNamed(ref)
	if err != nil || reference.Domain(named) != domain {
		return ""
	}
	out := mirror + "/" + reference.Path(named)
	if t, ok := named.(reference.Tagged); ok {
		out += ":" + t.Tag()
	}
	if d, ok := named.(reference.Digested); ok {
		out += "@" + d.Digest().String()
	}
	return out
}

// pullWatched pulls ref and gives up when no progress message arrives for
// stall. It returns errPullStalled in that case.
func (d *Docker) pullWatched(ctx context.Context, ref string, platforms []ocispec.Platform, stall time.Duration) error {
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	resp, err := d.c().ImagePull(pctx, ref, client.ImagePullOptions{Platforms: platforms})
	if err != nil {
		return err
	}
	progress := make(chan struct{}, 1)
	done := make(chan error, 1)
	report, _ := ctx.Value(pullProgressKey{}).(PullProgress)
	type layer struct{ cur, total int64 }
	layers := map[string]layer{}
	go func() {
		for msg, err := range resp.JSONMessages(pctx) {
			if err != nil {
				done <- err
				return
			}
			if msg.Error != nil {
				done <- errors.New(msg.Error.Message)
				return
			}
			if report != nil && msg.ID != "" {
				l := layers[msg.ID]
				switch {
				case msg.Status == "Downloading" && msg.Progress != nil && msg.Progress.Total > 0:
					l = layer{msg.Progress.Current, msg.Progress.Total}
				case msg.Status == "Download complete" || msg.Status == "Pull complete" || msg.Status == "Already exists":
					l.cur = l.total
				}
				layers[msg.ID] = l
				var cur, total int64
				for _, v := range layers {
					cur, total = cur+v.cur, total+v.total
				}
				report(ref, cur, total)
			}
			select {
			case progress <- struct{}{}:
			default:
			}
		}
		done <- nil
	}()
	timer := time.NewTimer(stall)
	defer timer.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-progress:
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(stall)
		case <-timer.C:
			cancel()
			<-done
			return errPullStalled
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// pullFallback pulls ref from the sources in Registry order (see above). The
// caller's context bounds the whole attempt.
func (d *Docker) pullFallback(ctx context.Context, ref string, platforms []ocispec.Platform) (string, error) {
	var mirrorRefs []string
	for _, m := range d.mirrors() {
		if mref := mirrorRef(ref, m); mref != "" {
			mirrorRefs = append(mirrorRefs, mref)
		}
	}
	for _, m := range ghcrMirrors() {
		if mref := ghcrMirrorRef(ref, m); mref != "" {
			mirrorRefs = append(mirrorRefs, mref)
		}
	}
	// Docker refuses to tag with a digest, so an image pinned by digest keeps
	// the mirror's name: the digest already proves it is the same image.
	_, pinned := parseDigested(ref)
	tryMirrors := func() (string, error) {
		var last error = errors.New("没有可用的镜像源")
		for _, mref := range mirrorRefs {
			if err := d.pullWatched(ctx, mref, platforms, pullStallTimeout); err != nil {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				last = fmt.Errorf("%s: %w", mref, err)
				continue
			}
			if pinned {
				return mref, nil
			}
			if _, err := d.c().ImageTag(ctx, client.ImageTagOptions{Source: mref, Target: ref}); err != nil {
				return "", err
			}
			// Drop the mirror name; the layers stay under the original tag.
			_, _ = d.c().ImageRemove(ctx, mref, client.ImageRemoveOptions{})
			return ref, nil
		}
		return "", last
	}
	if d.registry.China && len(mirrorRefs) > 0 {
		local, merr := tryMirrors()
		if merr == nil || ctx.Err() != nil || noMatchingPlatform(merr) {
			return local, merr
		}
		if err := d.pullWatched(ctx, ref, platforms, pullStallTimeout); err != nil {
			return "", fmt.Errorf("国内镜像源和 Docker Hub 都没拉成：%v；%w", merr, err)
		}
		return ref, nil
	}
	err := d.pullWatched(ctx, ref, platforms, pullStallTimeout)
	if err == nil || ctx.Err() != nil || noMatchingPlatform(err) || len(mirrorRefs) == 0 ||
		(pullNotFoundPattern.MatchString(err.Error()) && !errors.Is(err, errPullStalled)) {
		if err != nil {
			return "", err
		}
		return ref, nil
	}
	local, merr := tryMirrors()
	if merr != nil {
		return "", fmt.Errorf("%w（%s）；镜像源：%v", err, ref, merr)
	}
	return local, nil
}

// parseDigested reports whether ref names its image by digest.
func parseDigested(ref string) (reference.Named, bool) {
	named, err := reference.ParseNormalizedNamed(ref)
	if err != nil {
		return nil, false
	}
	_, ok := named.(reference.Digested)
	return named, ok
}

// EdgeImagePresent reports whether a Caddy image for the edge is available
// locally (doctor, install).
func (d *Docker) EdgeImagePresent(ctx context.Context) bool {
	for _, ref := range []string{EdgeImageLocal, EdgeImagePublic} {
		if _, err := d.c().ImageInspect(ctx, ref); err == nil {
			return true
		}
	}
	return false
}

// PrepareEdgeImage makes sure a Caddy image is present, pulling it with the
// configured registry order (install runs this so the first start does not
// download in the background).
func (d *Docker) PrepareEdgeImage(ctx context.Context) (string, error) {
	return d.edgeImage(ctx)
}
