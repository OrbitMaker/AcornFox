// Package platformvar is the one place the values of platform variables come
// from.
//
// What it does: given one app (its name, whether it is only on the internal
// network, where it can be reached: its exposure, its port and the domains
// bound to it; the user's changes), it works out every platform variable
// that app gets, and the address the app opens at (Address: what
// app.url, app.scheme and app.host are and what `acornfox status` shows,
// worked out in one place); it lists the whole table with this machine's
// values; it checks the changes a user asks for. The table itself (names,
// labels, which ones can be changed, when they are worked out) is
// catalog.PlatformVars; this package fills it.
//
// Who uses it: the install engine (install, plan, reinstall on old data,
// `apps vars`), the API behind the console's pages (GET
// /v1/catalog/platform-vars, an app's detail and its address in every app
// view), and the command line (`acornfox catalog platform-vars`,
// online through the API, or --offline straight from Table). None of them
// works a value out by itself; a test (guard_test.go) fails when one does.
//
// Implementations: Standalone (this machine's settings). The cloud edition
// gets its own implementation behind the same interface later; a fake one
// in the tests (cloud_fake_test.go) runs the same contract.
//
// Side effects: none. Nothing here writes, connects or reads files; the
// machine's settings are given to NewStandalone by the server at start.
//
// Errors: Check says in words why a user's change is refused (the install
// engine reports it as CAT-INST-28); Values of the standalone edition never
// fails. Checking it: go test ./internal/platformvar/ runs the contract on
// every implementation, the pinned shape of the offline table, and the
// dependency check. Adding a variable: see catalog/platformvars.go.
package platformvar

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // zone names are checked even on hosts without tzdata

	"github.com/acornfox/acornfox/internal/catalog"
)

// Provider gives the platform variables. Every method is safe to call at
// any time and has no side effect.
type Provider interface {
	// Edition is catalog.EditionStandalone or catalog.EditionCloud.
	Edition() string
	// Table is the whole table (a copy of catalog.PlatformVars, in order).
	Table() []catalog.PlatformVar
	// Machine is every variable of the table with the value it has here
	// before any app exists (what GET /v1/catalog/platform-vars shows).
	Machine(ctx context.Context) []State
	// Values works out the platform variables of one app, with the user's
	// changes (app.Sets, checked with Check first) put over them. A
	// variable without a value here is left out of the map; the ones about
	// the app itself (app.url, app.scheme, app.host: Address) only when
	// app.Created.
	Values(ctx context.Context, app App) (map[string]string, error)
	// Address is the address app opens at, by where it can be reached
	// (app.Where): what app.url, app.scheme and app.host are and what the
	// server shows the user as the app's address. Empty for an app only on
	// the internal network, or without a port and a domain.
	Address(app App) Address
	// Has reports whether variable name gets a value here once the app
	// exists. A manifest that refers to a variable without one is refused
	// before anything is created.
	Has(name string) bool
	// Check validates the user's changes: each name is in the table and
	// marked UserCanSet, each value fits the variable's rule. The error
	// says which and why, in words for the user (the install engine
	// reports it as CAT-INST-28).
	Check(sets map[string]string) error
}

// Addresser works out the address an app opens at. Every Provider is one;
// so is Host, the standalone edition's algorithm on its own.
type Addresser interface {
	Address(app App) Address
}

// App is the app the variables are worked out for.
type App struct {
	Name string
	// Created: the app's record exists (it has its port), so the
	// variables about the app itself are worked out too.
	Created bool
	// Internal: only on the internal network (no address to open).
	Internal bool
	// Where the app can be reached, what its address is worked out from.
	Where
	// Sets is the user's changes (--set platform.<name>=…), already checked.
	Sets map[string]string
}

// Where is where an app can be reached: the server gives it from the app's
// record (its exposure with the server's default filled in, its port) and
// the domains bound to it.
type Where struct {
	// Exposure is local (this machine only), lan or public; "" for an app
	// only on the internal network.
	Exposure string
	// Port is the port the app is opened at on this machine (0: none yet).
	Port int
	// Domains are the domain names bound to the app, the first one bound
	// first. With one the app opens at https://<the first>.
	Domains []string
	// PlatformDomain is this machine's platform domain (one for the whole
	// machine, "" when none is set). A public app that has no domain of its
	// own opens at https://<app name>.<PlatformDomain> (PlatformHost). The
	// server leaves it "" for an app whose name under it is already bound
	// to another app or to the public console.
	PlatformDomain string
}

// Address is the address an app opens at, and its parts as the platform
// variables give them.
type Address struct {
	URL    string `json:"url"`    // app.url, e.g. http://192.0.2.10:18810 or https://app.example.com
	Scheme string `json:"scheme"` // app.scheme: http, or https only with a domain
	Host   string `json:"host"`   // app.host: the host of URL, without the port
}

// State is one variable of the table with the value it has here.
type State struct {
	catalog.PlatformVar
	Value     string `json:"value,omitempty"`
	Available bool   `json:"available"`
	Note      string `json:"note,omitempty"`
}

// Notes of State.
const (
	NotePerApp  = "每个应用装好时生成"
	NoteNoValue = "这台机器上没有值，用到它的应用这个版本装不了"
)

// Table is a copy of catalog.PlatformVars (the lists in it copied too).
func Table() []catalog.PlatformVar {
	out := make([]catalog.PlatformVar, len(catalog.PlatformVars))
	for i, v := range catalog.PlatformVars {
		v.Editions = append([]string(nil), v.Editions...)
		out[i] = v
	}
	return out
}

// perApp reports whether a variable is about the app itself: it gets its
// value when the app is created, never before (app.url, app.scheme,
// app.host).
func perApp(name string) bool {
	return strings.HasPrefix(name, "app.")
}

// MaxSetLen is the longest value a user may set.
const MaxSetLen = 500

// CheckSets is the check every edition applies to the user's changes
// (Provider.Check): the name is in table and marked UserCanSet; the value is
// one non-empty line of at most MaxSetLen characters and fits the
// variable's Rule.
func CheckSets(table []catalog.PlatformVar, sets map[string]string) error {
	byName := make(map[string]catalog.PlatformVar, len(table))
	for _, v := range table {
		byName[v.Name] = v
	}
	names := make([]string, 0, len(sets))
	for k := range sets {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, name := range names {
		v := strings.TrimSpace(sets[name])
		pv, ok := byName[name]
		if !ok {
			return fmt.Errorf("没有平台变量 %s（acornfox catalog platform-vars 看有哪些）", name)
		}
		if !pv.UserCanSet {
			return fmt.Errorf("平台变量 %s（%s）由平台决定，不能改", name, pv.Label)
		}
		if v == "" || len(v) > MaxSetLen || !singleLine(v) {
			return fmt.Errorf("平台变量 %s 的值不能为空、不能换行、不超过 %d 个字符", name, MaxSetLen)
		}
		switch pv.Rule {
		case catalog.RuleTZ:
			if !KnownTZ(v) {
				return fmt.Errorf("时区要写成 Asia/Shanghai 这样的标准名字，%q 不是（拼写要和 IANA 时区库一致）", v)
			}
		case catalog.RuleURL:
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
				return fmt.Errorf("下载源 %s 要是 http:// 或 https:// 开头的地址，不能带账号密码", name)
			}
		case "":
		default:
			// A rule this version does not know: refuse rather than pass a
			// value nobody checked.
			return fmt.Errorf("平台变量 %s 的格式要求（%s）这个版本不认识，不能改", name, pv.Rule)
		}
	}
	return nil
}

// singleLine reports whether s has no control character (no newline, no tab).
func singleLine(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// KnownTZ reports whether tz is an IANA zone name Go knows (the apps get it
// as TZ; a name the app does not know silently means UTC there).
func KnownTZ(tz string) bool {
	if tz == "" || tz == "Local" || !validTZ.MatchString(tz) {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}

var validTZ = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+){0,2}$`)

// withAddress puts the variables about the app's address into values: all
// three, empty when there is no address (an app only on the internal
// network).
func withAddress(values map[string]string, a Address) {
	values[catalog.PVAppURL] = a.URL
	values[catalog.PVAppScheme] = a.Scheme
	values[catalog.PVAppHost] = a.Host
}

// withSets puts the user's changes over values (trimmed, as they are saved).
func withSets(values, sets map[string]string) map[string]string {
	for k, v := range sets {
		values[k] = strings.TrimSpace(v)
	}
	return values
}

// Offline is the table as `acornfox catalog platform-vars --offline --json`
// prints it, straight from this program with no server: no machine's
// values, only what each variable is. Its shape is pinned by a test; the
// business system and the cloud edition are to read this, not the code.
type Offline struct {
	Offline bool `json:"offline"` // always true
	// Platform is the version of the program the table comes from.
	Platform string                `json:"platform"`
	Vars     []catalog.PlatformVar `json:"vars"`
}

// Export is the Offline table of the program at version.
func Export(version string) Offline {
	return Offline{Offline: true, Platform: version, Vars: Table()}
}
