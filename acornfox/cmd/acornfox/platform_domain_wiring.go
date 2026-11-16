package main

import (
	"context"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/platformdomain"
	"github.com/acornfox/acornfox/internal/publicconsole"
	"github.com/acornfox/acornfox/internal/reconcile"
)

// newPlatformDomain wires the platform domain (package platformdomain): kept
// in <data directory>/platform-domain.json; the reconciler puts the names
// under it on the entry proxy's routes (rec, set once the reconciler
// exists, gives the names in use and hands the routes over after a change);
// the cert check makes one TLS handshake with the entry proxy on this
// machine, verified like a bound domain's (HTTPSCAFile, else the system
// roots).
func newPlatformDomain(o serverOptions, rec func() *reconcile.Reconciler) *platformdomain.Service {
	https := o.HTTPS.WithDefaults()
	httpsAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(https.HTTPSPort))
	roots := caRootsOnce(o.HTTPSCAFile)
	return platformdomain.New(platformdomain.Config{
		Path:    filepath.Join(o.DataDir, platformdomain.FileName),
		Machine: platformdomain.Machine{PublicHost: o.PublicHost, Desktop: o.LANHost != nil, ServerLAN: o.ServerLAN},
		DetectIPs: func(ctx context.Context) []string {
			return publicconsole.DetectPublicIPs(ctx, publicconsole.HTTPFetcher(3*time.Second)).All()
		},
		China: setupNetwork(o.DataDir).China,
		CertProbe: func(ctx context.Context, host string) error {
			return reconcile.DefaultDomainProbe(ctx, httpsAddr, host, roots())
		},
		ActiveHosts: func(ctx context.Context) []string {
			if r := rec(); r != nil {
				return r.PlatformHosts(ctx)
			}
			return nil
		},
		HTTPPort:  https.HTTPPort,
		HTTPSPort: https.HTTPSPort,
		OnChange: func(ctx context.Context) {
			if r := rec(); r != nil {
				r.RefreshRoutes(ctx)
			}
		},
	})
}

// caRootsOnce reads the PEM roots of file once ("" or unreadable: nil, the
// system roots).
func caRootsOnce(file string) func() *x509.CertPool {
	var once sync.Once
	var pool *x509.CertPool
	return func() *x509.CertPool {
		once.Do(func() {
			if file == "" {
				return
			}
			pem, err := os.ReadFile(file)
			if err != nil {
				return
			}
			p := x509.NewCertPool()
			if p.AppendCertsFromPEM(pem) {
				pool = p
			}
		})
		return pool
	}
}
