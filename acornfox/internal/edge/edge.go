// Package edge is the one interface between the acornfox main program and
// the entry proxy: the process in front of every app that listens on the
// apps' public ports and on 80/443, matches custom domains, obtains
// certificates and forwards to the app containers.
//
// The main program only ever talks to a Proxy. It hands over the whole
// desired configuration at once (Apply), checks which configuration is in
// effect (Version), puts up and takes down the one-off domain challenge of
// the public console (SetChallenge, ClearChallenge) and asks whether the
// proxy is healthy (Health). Which proxy software runs behind it and how the
// configuration is translated is the driver's business; the main program
// never builds proxy-specific configuration.
//
// # Contract every driver must keep
//
//  1. Apply is idempotent and replaces the whole configuration: routes that
//     are not in the new Config disappear. Parts of the proxy configuration
//     the driver did not create are left alone.
//  2. Version returns the version of the configuration really in effect
//     (Config.Version of what the proxy serves now). After the proxy lost its
//     configuration (for example it restarted without keeping it) Version
//     differs from the last Apply, and the caller applies again.
//  3. A request the proxy forwards to an upstream on the loopback address
//     carries the visitor's address as the last element of X-Forwarded-For.
//     The public console relies on it to know who is logging in.
//  4. A route with Domains is served over HTTPS on HTTPS.HTTPSPort with a
//     certificate the proxy obtains by itself (HTTPS.Issuer), and plain HTTP
//     requests for those domains on HTTPS.HTTPPort are redirected to HTTPS.
//  5. Routes with PublicPort are plain HTTP on that port (no certificate, no
//     redirect), bound to Listen.
//  6. The challenge set by SetChallenge answers on HTTPS.HTTPPort over plain
//     HTTP, also while domain routes exist, and survives Apply.
//  7. The proxy's management endpoint is reachable from this computer only
//     and refuses requests that do not carry the token. Health reports a
//     management endpoint that answers without the token as an error.
package edge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// Route exposes one app (or the public console) through the proxy.
//
// A route with PublicPort > 0 gets its own plain-HTTP listener on
// Listen:PublicPort (and on LANPort when set). A route with PublicPort 0 is
// domain-only: it is served solely on the shared HTTPS port for its Domains.
type Route struct {
	// App names the route; unique within a Config.
	App string `json:"app"`
	// PublicPort is the app's own port; 0 = domain-only route.
	PublicPort int `json:"public_port"`
	// Listen is the address PublicPort is bound to: "" for every interface
	// (exposure lan/public, and bridge mode where the container's port
	// publishing decides), "127.0.0.1" for exposure local on Linux.
	Listen string `json:"listen,omitempty"`
	// LANPort is a second port the route listens on (bridge mode: the edge
	// container publishes <LAN address>:PublicPort to it); 0 = none.
	LANPort int `json:"lan_port,omitempty"`
	// Upstream is the host:port the proxy forwards to.
	Upstream string `json:"upstream"`
	// Domains are host-matched on the shared HTTPS port (contract 4).
	Domains []string `json:"domains,omitempty"`
}

// Issuers of HTTPS.Issuer.
const (
	IssuerACME     = "acme"     // public ACME (Let's Encrypt, ZeroSSL); also what "" means
	IssuerInternal = "internal" // the proxy's own local CA (development and tests only)
)

// Default ports used when HTTPS leaves them at zero.
const (
	DefaultHTTPPort  = 80
	DefaultHTTPSPort = 443
)

// HTTPS configures the shared domain listener and certificates.
type HTTPS struct {
	HTTPPort  int    `json:"http_port"`  // 0 = DefaultHTTPPort (ACME HTTP-01, redirect, challenge)
	HTTPSPort int    `json:"https_port"` // 0 = DefaultHTTPSPort
	Issuer    string `json:"issuer"`     // "" or IssuerACME, or IssuerInternal
	Email     string `json:"email,omitempty"`
}

// WithDefaults fills zero ports with the defaults and "" Issuer with
// IssuerACME.
func (h HTTPS) WithDefaults() HTTPS {
	if h.HTTPPort == 0 {
		h.HTTPPort = DefaultHTTPPort
	}
	if h.HTTPSPort == 0 {
		h.HTTPSPort = DefaultHTTPSPort
	}
	if h.Issuer == "" {
		h.Issuer = IssuerACME
	}
	return h
}

// Config is the whole configuration handed to the proxy in one Apply.
type Config struct {
	Routes []Route `json:"routes"`
	HTTPS  HTTPS   `json:"https"`
}

// versionPrefix marks the algorithm of Version; change it whenever the
// canonical form changes so old and new versions never compare equal.
const versionPrefix = "v1-"

// Normalized returns the canonical form of c: routes sorted by App (then by
// port and upstream), each route's Domains sorted with empty and duplicate
// names removed, and HTTPS with defaults filled in. Two configurations that
// mean the same thing to the proxy have equal normalized forms.
func (c Config) Normalized() Config {
	out := Config{HTTPS: c.HTTPS.WithDefaults(), Routes: make([]Route, 0, len(c.Routes))}
	for _, r := range c.Routes {
		r.Domains = normalizeDomains(r.Domains)
		out.Routes = append(out.Routes, r)
	}
	sort.SliceStable(out.Routes, func(i, j int) bool {
		a, b := out.Routes[i], out.Routes[j]
		if a.App != b.App {
			return a.App < b.App
		}
		if a.PublicPort != b.PublicPort {
			return a.PublicPort < b.PublicPort
		}
		return a.Upstream < b.Upstream
	})
	return out
}

func normalizeDomains(ds []string) []string {
	if len(ds) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ds))
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		if d == "" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// Version is the version of c: a short hash of its normalized form. Equal
// configurations have equal versions, whatever the order of routes and
// domains.
func (c Config) Version() string {
	b, err := json.Marshal(c.Normalized())
	if err != nil {
		// Only plain strings and ints: Marshal cannot fail.
		panic(err)
	}
	sum := sha256.Sum256(b)
	return versionPrefix + hex.EncodeToString(sum[:16])
}

// Health states.
const (
	StateRunning  = "running"  // serving, configuration can be applied
	StateStarting = "starting" // the proxy is coming up; try again shortly
	StateError    = "error"    // see Reason and Cause
)

// Causes of an error Health, for diagnosis codes and doctor.
const (
	CauseUnreachable = "unreachable" // the management endpoint does not answer
	CauseAuth        = "auth"        // the management endpoint refuses our token
	CauseExposed     = "exposed"     // the management endpoint answers without the token, or another unprotected one is open
	CauseOther       = "other"
)

// Health is the proxy's own view of whether it works.
type Health struct {
	State  string `json:"state"`
	Cause  string `json:"cause,omitempty"`  // set when State is StateError
	Reason string `json:"reason,omitempty"` // a sentence for the user; set unless running
}

// OK reports whether the proxy is running.
func (h Health) OK() bool { return h.State == StateRunning }

// Challenger puts up and takes down the public console's one-off domain
// challenge (contract 6).
type Challenger interface {
	// SetChallenge answers GET http://host/path with body (text/plain) on
	// HTTPS.HTTPPort. Calling it again replaces the previous challenge.
	SetChallenge(ctx context.Context, host, path, body string) error
	// ClearChallenge removes the challenge; a no-op when there is none.
	ClearChallenge(ctx context.Context) error
}

// Proxy is everything the main program needs from the entry proxy.
type Proxy interface {
	// Apply replaces the proxy's whole configuration with cfg (contract 1)
	// and returns cfg's version.
	Apply(ctx context.Context, cfg Config) (string, error)
	// Version returns the version of the configuration in effect now
	// (contract 2). An error means the proxy cannot be asked.
	Version(ctx context.Context) (string, error)
	Challenger
	// Health reports whether the proxy works (contract 7).
	Health(ctx context.Context) Health
}
