package platformvar

import (
	"context"
	"net"
	"strconv"
	"strings"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/domainname"
)

// Machine is this machine's settings the standalone edition fills the
// variables from. The server builds it at start: China from install.env or
// network.json, Cloud from the vendor install.sh detected, TZ from the
// host's zone, Host from its own options.
type Machine struct {
	China bool   // region: cn (true) or global
	Cloud string // cloud vendor install.sh detected (aliyun, tencent, huawei, volcengine) or ""
	TZ    string // IANA time zone of this machine; "" => UTC
	// Host is what the apps' addresses are worked out from.
	Host Host
}

// Host is what the standalone edition works an app's address out from
// (Host.Address). The server fills it from its options at start.
type Host struct {
	// PublicHost is the address the server is reached at (--public-host:
	// on a Linux server what install.sh found, the cloud's public address
	// or the machine's first address; "localhost" on a desktop).
	PublicHost string
	// Desktop: the server runs on a desktop (Mac, Windows): an app only on
	// this machine opens at localhost; on a Linux server at 127.0.0.1.
	Desktop bool
	// HTTPSPort is the entry proxy's HTTPS port; 0 is 443 (no port in the
	// address).
	HTTPSPort int
}

// Address is the standalone edition's algorithm for an app's address, the
// one place it is worked out (app.url, app.scheme, app.host, the address
// `acornfox status` and the console show):
//
//   - only on the internal network: none;
//   - a domain bound: https://<the first domain> (the entry proxy has the
//     certificate and sends http on to https);
//   - a platform domain set and the app public (PlatformHost):
//     https://<app name>.<platform domain> (the entry proxy has that name
//     on the app's route and gets its certificate the same way);
//   - otherwise by its exposure, over http with its port: local is
//     127.0.0.1 (localhost on a desktop), lan and public the server's
//     address (PublicHost);
//   - no port yet, or no address for the server: none.
func (h Host) Address(app App) Address {
	if app.Internal {
		return Address{}
	}
	for _, d := range app.Domains {
		if d = strings.TrimSpace(d); d == "" {
			continue
		}
		return h.httpsAddress(d)
	}
	if d := PlatformHost(app.Name, app.Internal, app.Exposure, app.PlatformDomain); d != "" {
		return h.httpsAddress(d)
	}
	if app.Port <= 0 {
		return Address{}
	}
	host := h.PublicHost
	if app.Exposure == exposureLocal {
		host = "127.0.0.1"
		if h.Desktop {
			host = "localhost"
		}
	}
	if host == "" {
		return Address{}
	}
	return Address{URL: "http://" + net.JoinHostPort(host, strconv.Itoa(app.Port)), Scheme: "http", Host: host}
}

// httpsAddress is the address of an app opened at domain name d.
func (h Host) httpsAddress(d string) Address {
	u := "https://" + d
	if h.HTTPSPort != 0 && h.HTTPSPort != 443 {
		u = "https://" + net.JoinHostPort(d, strconv.Itoa(h.HTTPSPort))
	}
	return Address{URL: u, Scheme: "https", Host: d}
}

// exposureLocal is the exposure of an app only this machine opens (the
// same word as state.ExposureLocal); exposurePublic one opened on every
// interface and by domain names (state.ExposurePublic).
const (
	exposureLocal  = "local"
	exposurePublic = "public"
)

// PlatformHost is the name app gets under this machine's platform domain
// base: "<app>.<base>" for an app whose exposure is public and that is not
// only on the internal network; "" otherwise, when base is "", or when the
// name would not be a valid host name (too long). It is the one rule for
// who gets a platform subdomain: the address (Address), the entry proxy's
// routes (package reconcile) and the API's views all ask it.
func PlatformHost(app string, internal bool, exposure, base string) string {
	base = strings.TrimSpace(base)
	if internal || exposure != exposurePublic || base == "" || app == "" {
		return ""
	}
	name := app + "." + base
	if !domainname.Valid(name) {
		return ""
	}
	return name
}

// Mirror addresses. Public mirrors only: every one answers from anywhere
// (the vendors' internal-network addresses are faster on their own clouds
// but are not verified yet). Outside China the official registries.
var (
	pypiMirrors = map[string]string{
		"":        "https://repo.huaweicloud.com/repository/pypi/simple/",
		"aliyun":  "https://mirrors.aliyun.com/pypi/simple/",
		"tencent": "https://mirrors.cloud.tencent.com/pypi/simple/",
	}
	npmMirrors = map[string]string{
		"":        "https://repo.huaweicloud.com/repository/npm/",
		"aliyun":  "https://registry.npmmirror.com/",
		"tencent": "https://mirrors.cloud.tencent.com/npm/",
	}
)

const (
	officialPyPI = "https://pypi.org/simple/"
	officialNPM  = "https://registry.npmjs.org/"
)

func pickMirror(table map[string]string, cloud string) string {
	if v, ok := table[cloud]; ok {
		return v
	}
	return table[""]
}

// Standalone is the standalone edition's Provider.
type Standalone struct {
	m Machine
}

// NewStandalone is the provider for this machine.
func NewStandalone(m Machine) *Standalone { return &Standalone{m: m} }

var _ Provider = (*Standalone)(nil)

func (s *Standalone) Edition() string              { return catalog.EditionStandalone }
func (s *Standalone) Table() []catalog.PlatformVar { return Table() }

// base is every variable this machine fills before an app exists. A
// variable of the table that is neither here nor about the app itself has no
// value on this machine.
func (s *Standalone) base() map[string]string {
	out := map[string]string{
		catalog.PVRegion:     "global",
		catalog.PVTZ:         "UTC",
		catalog.PVMirrorPyPI: officialPyPI,
		catalog.PVMirrorNPM:  officialNPM,
	}
	if s.m.China {
		out[catalog.PVRegion] = "cn"
		out[catalog.PVMirrorPyPI] = pickMirror(pypiMirrors, s.m.Cloud)
		out[catalog.PVMirrorNPM] = pickMirror(npmMirrors, s.m.Cloud)
	}
	if tz := strings.TrimSpace(s.m.TZ); KnownTZ(tz) {
		out[catalog.PVTZ] = tz
	}
	return out
}

// Machine lists the table with this machine's values; the variables about
// the app itself are there once the app is installed.
func (s *Standalone) Machine(context.Context) []State {
	base := s.base()
	out := make([]State, 0, len(catalog.PlatformVars))
	for _, pv := range Table() {
		st := State{PlatformVar: pv}
		switch val, ok := base[pv.Name]; {
		case perApp(pv.Name) && s.Has(pv.Name):
			st.Available, st.Note = true, NotePerApp
		case ok:
			st.Value, st.Available = val, true
		default:
			st.Note = NoteNoValue
		}
		out = append(out, st)
	}
	return out
}

// Values: this machine's values, the app's address (app.url, app.scheme,
// app.host; empty for an app only on the internal network) once the app
// exists, then the user's changes.
func (s *Standalone) Values(_ context.Context, app App) (map[string]string, error) {
	out := s.base()
	if app.Created {
		withAddress(out, s.Address(app))
	}
	return withSets(out, app.Sets), nil
}

// Address is Host.Address with this machine's host.
func (s *Standalone) Address(app App) Address { return s.m.Host.Address(app) }

// Has: the machine's values, and the app's address (every app gets one
// when it is created; it is empty for an app only on the internal network).
func (s *Standalone) Has(name string) bool {
	if perApp(name) {
		_, inTable := catalog.LookupPlatformVar(name)
		return inTable
	}
	_, ok := s.base()[name]
	return ok
}

// Check applies CheckSets to the table.
func (s *Standalone) Check(sets map[string]string) error {
	return CheckSets(catalog.PlatformVars, sets)
}
