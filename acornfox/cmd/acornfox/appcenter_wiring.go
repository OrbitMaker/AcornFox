package main

import (
	"context"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/cli"
	"github.com/acornfox/acornfox/internal/gatewayclient"
	"github.com/acornfox/acornfox/internal/hostcheck"
	"github.com/acornfox/acornfox/internal/identity"
	"github.com/acornfox/acornfox/internal/platform"
	"github.com/acornfox/acornfox/internal/state"
)

// newAppCenter builds the app center (catalog + install engine) for the
// server. The catalog lives under <data>/catalog; the download gateway is
// used for large images that name an artifact and for private catalogs
// (connect, their indexes and packages). The machine key is created only
// when the gateway is actually contacted.
func newAppCenter(st *state.Store, dataDir, uploadDir string, runnerAPI serverRunner) *appinstall.Engine {
	var gw appinstall.Gateway
	var cgw appinstall.CatalogGateway
	network := setupNetwork(dataDir)
	if base := gatewayBase(dataDir); base != "" {
		l := &lazyIssuer{base: base, dataDir: dataDir}
		gw, cgw = l, l
	}
	return appinstall.New(appinstall.Config{
		Store:  st,
		Runner: runnerAPI,
		Catalog: appinstall.NewCatalog(appinstall.CatalogConfig{
			CacheDir: filepath.Join(dataDir, "catalog"),
			Sources:  catalog.PublicSources(os.Getenv(catalog.SourcesEnv)),
			Keys:     catalogKeys(),
			Gateway:  cgw,
		}),
		DataDir:   dataDir,
		UploadDir: uploadDir,
		Gateway:   gw,
		Version:   cli.Version,
		Platform: appinstall.PlatformEnv{
			// Same rule as downloadSources: a Linux server without
			// install.env pulls as outside China, so apps get that region.
			China: network.China && (network.Source == "install.env" || platform.DesktopMode() || platform.GOOS != "linux"),
			Cloud: platform.LinuxInstallCloud(linuxInstallEnv),
			TZ:    hostTimezone(),
		},
		DiskFree: func(ctx context.Context) (appinstall.DiskSpace, error) {
			return appDiskFree(ctx, runnerAPI, uploadDir)
		},
	})
}

// appDiskFree is the free space an install can count on: the partition
// Docker keeps images on (as the runner reports it) and the one downloaded
// image archives land on (uploadDir), and whether they are the same. Not
// implemented off Linux (hostcheck.FreeBytes errors there).
func appDiskFree(ctx context.Context, r serverRunner, uploadDir string) (appinstall.DiskSpace, error) {
	root := hostcheck.DefaultDockerRoot
	if ping, err := r.Ping(ctx); err == nil && ping.DockerRootDir != "" {
		root = ping.DockerRootDir
	}
	rootDir := hostcheck.ExistingDir(root)
	free, err := hostcheck.FreeBytes(rootDir)
	if err != nil {
		return appinstall.DiskSpace{}, err
	}
	ds := appinstall.DiskSpace{DockerFree: free, DockerPath: root}
	upDir := hostcheck.ExistingDir(uploadDir)
	if up, err := hostcheck.FreeBytes(upDir); err == nil {
		ds.UploadFree, ds.UploadPath = up, uploadDir
		d1, d2 := hostcheck.Device(rootDir), hostcheck.Device(upDir)
		ds.Same = d1 != 0 && d1 == d2
	}
	return ds, nil
}

// hostTimezone is this machine's IANA time zone for the platform variable
// tz: $TZ, the zone /etc/localtime links to, or /etc/timezone; "" when none
// says (the engine then uses UTC).
func hostTimezone() string {
	if tz := strings.TrimPrefix(strings.TrimSpace(os.Getenv("TZ")), ":"); tz != "" && !strings.HasPrefix(tz, "/") {
		return tz
	}
	// /etc/localtime is what the system actually uses (glibc, systemd);
	// /etc/timezone can be stale after "ln -sf …/zoneinfo/X /etc/localtime".
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, tz, ok := strings.Cut(target, "zoneinfo/"); ok {
			return strings.TrimPrefix(strings.TrimPrefix(tz, "posix/"), "right/")
		}
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(b)); tz != "" {
			return tz
		}
	}
	return ""
}

// catalogKeys is the trusted catalog keys: the program's own
// (catalog.TrustedKeys), plus test keys only in a binary built with the
// acornfox_testkeys tag (testkeys_tag.go). nil keeps the package default.
func catalogKeys() []ed25519.PublicKey {
	extra := testCatalogKeys()
	if len(extra) == 0 {
		return nil
	}
	return append(append([]ed25519.PublicKey{}, catalog.TrustedKeys...), extra...)
}

// lazyIssuer loads (or creates) the machine key on the first gateway call.
type lazyIssuer struct {
	base, dataDir string
	mu            sync.Mutex
	c             *gatewayclient.Client
}

func (g *lazyIssuer) client() (*gatewayclient.Client, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.c == nil {
		id, err := identity.LoadOrCreate(g.dataDir)
		if err != nil {
			return nil, err
		}
		g.c = &gatewayclient.Client{BaseURL: g.base, Identity: id, Info: gatewayclient.DefaultInfo(cli.Version)}
	}
	return g.c, nil
}

// ClockOffset is the gateway's clock minus this machine's as the client
// learned it (0 before the first gateway call), so the app center can judge
// an address's expiry by this machine's clock.
func (g *lazyIssuer) ClockOffset() time.Duration {
	g.mu.Lock()
	c := g.c
	g.mu.Unlock()
	if c == nil {
		return 0
	}
	return c.ClockOffset()
}

func (g *lazyIssuer) Issue(ctx context.Context, fileID, purpose string) (gatewayclient.Issued, error) {
	c, err := g.client()
	if err != nil {
		return gatewayclient.Issued{}, err
	}
	return c.Issue(ctx, fileID, purpose)
}

func (g *lazyIssuer) IssueApp(ctx context.Context, fileID string, app gatewayclient.AppRef) (gatewayclient.Issued, error) {
	c, err := g.client()
	if err != nil {
		return gatewayclient.Issued{}, err
	}
	return c.IssueApp(ctx, fileID, app)
}

func (g *lazyIssuer) Connect(ctx context.Context, code string) (gatewayclient.Connected, error) {
	c, err := g.client()
	if err != nil {
		return gatewayclient.Connected{}, err
	}
	return c.Connect(ctx, code)
}

func (g *lazyIssuer) CatalogIndex(ctx context.Context, known map[string]int64) (gatewayclient.PrivateIndex, error) {
	c, err := g.client()
	if err != nil {
		return gatewayclient.PrivateIndex{}, err
	}
	return c.CatalogIndex(ctx, known)
}
