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
// nginx) are pulled. Pulls from Docker Hub can hang without an error from
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

// DefaultRegistryMirrors is used until cr.acornfox.com is ready.
var DefaultRegistryMirrors = []string{"docker.m.daocloud.io"}

var errPullStalled = errors.New("拉取镜像长时间没有进展")

// Registry selects the pull order.
type Registry struct {
	China   bool     // mirrors first
	Mirrors []string // nil => registryMirrors()
}

// SetRegistry sets how Docker Hub images are pulled.
func (d *Docker) SetRegistry(r Registry) { d.registry = r }

func (d *Docker) mirrors() []string {
	if d.registry.Mirrors != nil {
		return d.registry.Mirrors
	}
	return registryMirrors()
}

func registryMirrors() []string {
	v, ok := os.LookupEnv("ACORNFOX_REGISTRY_MIRRORS")
	if !ok {
		return DefaultRegistryMirrors
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
func mirrorRef(ref, mirror string) string {
	named, err := reference.ParseNormalizedNamed(ref)
	if err != nil || reference.Domain(named) != "docker.io" {
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
	resp, err := d.cli.ImagePull(pctx, ref, client.ImagePullOptions{Platforms: platforms})
	if err != nil {
		return err
	}
	progress := make(chan struct{}, 1)
	done := make(chan error, 1)
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
func (d *Docker) pullFallback(ctx context.Context, ref string, platforms []ocispec.Platform) error {
	var mirrorRefs []string
	for _, m := range d.mirrors() {
		if mref := mirrorRef(ref, m); mref != "" {
			mirrorRefs = append(mirrorRefs, mref)
		}
	}
	tryMirrors := func() error {
		var last error = errors.New("没有可用的镜像源")
		for _, mref := range mirrorRefs {
			if err := d.pullWatched(ctx, mref, platforms, pullStallTimeout); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				last = fmt.Errorf("%s: %w", mref, err)
				continue
			}
			if _, err := d.cli.ImageTag(ctx, client.ImageTagOptions{Source: mref, Target: ref}); err != nil {
				return err
			}
			// Drop the mirror name; the layers stay under the original tag.
			_, _ = d.cli.ImageRemove(ctx, mref, client.ImageRemoveOptions{})
			return nil
		}
		return last
	}
	if d.registry.China && len(mirrorRefs) > 0 {
		merr := tryMirrors()
		if merr == nil || ctx.Err() != nil || noMatchingPlatform(merr) {
			return merr
		}
		if err := d.pullWatched(ctx, ref, platforms, pullStallTimeout); err != nil {
			return fmt.Errorf("国内镜像源和 Docker Hub 都没拉成：%v；%w", merr, err)
		}
		return nil
	}
	err := d.pullWatched(ctx, ref, platforms, pullStallTimeout)
	if err == nil || ctx.Err() != nil || noMatchingPlatform(err) || len(mirrorRefs) == 0 ||
		(pullNotFoundPattern.MatchString(err.Error()) && !errors.Is(err, errPullStalled)) {
		return err
	}
	if merr := tryMirrors(); merr != nil {
		return fmt.Errorf("%w（%s）；镜像源：%v", err, ref, merr)
	}
	return nil
}

// EdgeImagePresent reports whether a Caddy image for the edge is available
// locally (doctor, install).
func (d *Docker) EdgeImagePresent(ctx context.Context) bool {
	for _, ref := range []string{EdgeImageLocal, EdgeImagePublic} {
		if _, err := d.cli.ImageInspect(ctx, ref); err == nil {
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
