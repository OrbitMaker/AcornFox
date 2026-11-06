package setup

import (
	"context"
	"sync"

	"github.com/acornfox/acornfox/internal/gatewayclient"
	"github.com/acornfox/acornfox/internal/identity"
)

// lazyGateway is the GatewayIssuer the desktop runtime uses. The machine key
// is loaded (or generated) only when a download actually reaches the gateway
// source, so with the gateway off, or every file already on disk, nothing is
// written to <data>/identity/ and nothing is sent.
type lazyGateway struct {
	base    string
	dataDir string
	version string

	mu     sync.Mutex
	client *gatewayclient.Client
}

func newLazyGateway(base, dataDir, version string) *lazyGateway {
	if version == "" {
		version = "dev"
	}
	return &lazyGateway{base: base, dataDir: dataDir, version: version}
}

func (g *lazyGateway) Issue(ctx context.Context, fileID, purpose string) (gatewayclient.Issued, error) {
	g.mu.Lock()
	if g.client == nil {
		id, err := identity.LoadOrCreate(g.dataDir)
		if err != nil {
			g.mu.Unlock()
			return gatewayclient.Issued{}, err
		}
		g.client = &gatewayclient.Client{BaseURL: g.base, Identity: id, Info: gatewayclient.DefaultInfo(g.version)}
	}
	c := g.client
	g.mu.Unlock()
	return c.Issue(ctx, fileID, purpose)
}
