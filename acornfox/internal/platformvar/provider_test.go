package platformvar

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
)

// providers are every implementation the contract runs on.
func providers() map[string]Provider {
	return map[string]Provider{
		"standalone":        NewStandalone(Machine{China: true, Cloud: "aliyun", TZ: "Asia/Tokyo"}),
		"standalone-global": NewStandalone(Machine{}),
		"cloud-fake":        newCloudFake(),
	}
}

// The contract every edition keeps: the table is the whole of
// catalog.PlatformVars; what Has says has a value does get one; nothing
// outside the table is given; the user's changes are applied (trimmed) and
// checked alike; app.url only once the app exists, empty when it is only on
// the internal network.
func TestProviderContract(t *testing.T) {
	ctx := context.Background()
	for name, p := range providers() {
		t.Run(name, func(t *testing.T) {
			if e := p.Edition(); e != catalog.EditionStandalone && e != catalog.EditionCloud {
				t.Fatalf("edition %q", e)
			}
			table := p.Table()
			if len(table) != len(catalog.PlatformVars) {
				t.Fatalf("table %d, want %d", len(table), len(catalog.PlatformVars))
			}
			inTable := map[string]bool{}
			for i, v := range table {
				if v.Name != catalog.PlatformVars[i].Name {
					t.Fatalf("table order: %s at %d", v.Name, i)
				}
				inTable[v.Name] = true
			}
			// A copy: changing it changes nothing.
			table[0].Editions[0] = "x"
			if catalog.PlatformVars[0].Editions[0] == "x" {
				t.Fatal("Table shares its lists with catalog.PlatformVars")
			}

			before, err := p.Values(ctx, App{Name: "demo"})
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := before[catalog.PVAppURL]; ok {
				t.Fatalf("app.url before the app exists: %v", before)
			}
			vals, err := p.Values(ctx, App{Name: "demo", Created: true, Where: Where{Exposure: "local", Port: 18810}})
			if err != nil {
				t.Fatal(err)
			}
			for n := range vals {
				if !inTable[n] {
					t.Fatalf("%s is not in the table", n)
				}
			}
			for _, v := range table {
				if p.Has(v.Name) {
					if _, ok := vals[v.Name]; !ok {
						t.Fatalf("Has(%s) but no value for a created app", v.Name)
					}
				}
			}
			if vals[catalog.PVAppURL] == "" {
				t.Fatalf("no app.url for a created app: %v", vals)
			}
			internal, err := p.Values(ctx, App{Name: "db", Created: true, Internal: true, Where: Where{Port: 18811}})
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range []string{catalog.PVAppURL, catalog.PVAppScheme, catalog.PVAppHost} {
				if u, ok := internal[n]; !ok || u != "" {
					t.Fatalf("internal %s %q %v", n, u, ok)
				}
			}
			// Values and Address agree.
			if a := p.Address(App{Name: "demo", Created: true, Where: Where{Exposure: "local", Port: 18810}}); a.URL != vals[catalog.PVAppURL] ||
				a.Scheme != vals[catalog.PVAppScheme] || a.Host != vals[catalog.PVAppHost] || a.Scheme == "" || a.Host == "" {
				t.Fatalf("address %+v, values %v", a, vals)
			}

			// The machine view agrees with Has.
			states := p.Machine(ctx)
			if len(states) != len(table) {
				t.Fatalf("machine %d", len(states))
			}
			for _, st := range states {
				if st.Available != p.Has(st.Name) {
					t.Fatalf("%s: available %v, Has %v", st.Name, st.Available, p.Has(st.Name))
				}
				if st.Available && st.Value == "" && st.Note == "" {
					t.Fatalf("%s: available with neither a value nor a note", st.Name)
				}
				if st.Value != "" && st.Value != before[st.Name] {
					t.Fatalf("%s: machine %q, values %q", st.Name, st.Value, before[st.Name])
				}
			}

			// The user's changes.
			sets := map[string]string{catalog.PVTZ: " Europe/Berlin ", catalog.PVMirrorNPM: "https://npm.example.com/"}
			if err := p.Check(sets); err != nil {
				t.Fatal(err)
			}
			got, _ := p.Values(ctx, App{Name: "demo", Created: true, Where: Where{Exposure: "lan", Port: 1}, Sets: sets})
			if got[catalog.PVTZ] != "Europe/Berlin" || got[catalog.PVMirrorNPM] != "https://npm.example.com/" {
				t.Fatalf("sets not applied: %v", got)
			}
			for _, bad := range badSets {
				if err := p.Check(bad); err == nil {
					t.Errorf("accepted %v", bad)
				}
			}
		})
	}
}

var badSets = []map[string]string{
	{"timezone": "Asia/Tokyo"},               // not in the table
	{"region": "global"},                     // the platform decides
	{"app.url": "https://evil.example"},      // the platform decides
	{"llm.api_key": "sk-x"},                  // not in the table (a model API is a service)
	{"mirror.pypi": "ftp://m.example/"},      // not http(s)
	{"mirror.pypi": "https://u:p@m.example"}, // credentials in the address
	{"tz": "../../etc/passwd"},               // not a zone name
	{"tz": "Asia/Tokio"},                     // a zone Go does not know
	{"tz": "Asia/Tokyo\nX=1"},                // newline
	{"tz": "Asia/Tokyo\tX"},                  // a control character
	{"tz": "  "},                             // empty
	{"mirror.npm": "https://m.example/" + strings.Repeat("a", MaxSetLen)}, // too long
}

// The standalone edition: in China on Aliyun its public mirrors; in China
// off the known clouds Huawei's; outside China the official registries and
// UTC; a zone name the host gives that Go does not know means UTC; the
// app's address is the one the server gives.
func TestStandaloneValues(t *testing.T) {
	ctx := context.Background()
	v, _ := NewStandalone(Machine{China: true, Cloud: "aliyun", TZ: "Asia/Tokyo"}).Values(ctx, App{Name: "a", Created: true, Where: Where{Exposure: "local", Port: 18810}})
	want := map[string]string{"tz": "Asia/Tokyo", "region": "cn", "mirror.pypi": "https://mirrors.aliyun.com/pypi/simple/",
		"mirror.npm": "https://registry.npmmirror.com/", "app.url": "http://127.0.0.1:18810", "app.scheme": "http", "app.host": "127.0.0.1"}
	if !sameMap(v, want) {
		t.Fatalf("aliyun %v", v)
	}
	g, _ := NewStandalone(Machine{}).Values(ctx, App{})
	if !sameMap(g, map[string]string{"tz": "UTC", "region": "global", "mirror.pypi": officialPyPI, "mirror.npm": officialNPM}) {
		t.Fatalf("global %v", g)
	}
	c, _ := NewStandalone(Machine{China: true, Cloud: "volcengine", TZ: "../../etc/passwd"}).Values(ctx, App{})
	if c["region"] != "cn" || !strings.Contains(c["mirror.pypi"], "huaweicloud") || !strings.Contains(c["mirror.npm"], "huaweicloud") || c["tz"] != "UTC" {
		t.Fatalf("cn %v", c)
	}
	if got := NewStandalone(Machine{TZ: "Asia/Tokio"}).base()["tz"]; got != "UTC" {
		t.Fatalf("unknown host zone kept: %s", got)
	}
	for _, ok := range []string{"Asia/Tokyo", "UTC", "Etc/UTC", "America/Argentina/Buenos_Aires"} {
		if err := NewStandalone(Machine{}).Check(map[string]string{"tz": ok}); err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
	}
	for _, st := range NewStandalone(Machine{}).Machine(ctx) {
		if perApp(st.Name) && (st.Note != NotePerApp || st.Value != "" || !st.Available) {
			t.Fatalf("%s %+v", st.Name, st)
		}
	}
}

// The cloud fake: the values come from the account and the cluster, the
// address is the one the platform assigns, never the server's.
func TestCloudFakeValues(t *testing.T) {
	v, err := newCloudFake().Values(context.Background(), App{Name: "N8n", Created: true, Where: Where{Exposure: "lan", Port: 18810, Domains: []string{"n8n.example.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	if v["app.url"] != "https://n8n.app.acornfox.com" || v["app.scheme"] != "https" || v["app.host"] != "n8n.app.acornfox.com" || v["mirror.pypi"] != "http://pypi.cluster.internal/simple/" || v["tz"] != "Asia/Shanghai" {
		t.Fatalf("cloud %v", v)
	}
	if _, err := newCloudFake().Values(context.Background(), App{Created: true}); err == nil {
		t.Fatal("a created app without a name")
	}
}

// The standalone edition's address, the one algorithm behind app.url,
// app.scheme, app.host and the address the server shows: a domain wins
// (https, the first one); otherwise by exposure over http with the port:
// local on 127.0.0.1 (localhost on a desktop), lan and public on the
// server's address; nothing for an internal app, without a port, or
// without the server's address.
func TestStandaloneAddress(t *testing.T) {
	server := Host{PublicHost: "192.0.2.10"}
	desktop := Host{PublicHost: "localhost", Desktop: true}
	cases := []struct {
		name string
		h    Host
		app  App
		want Address
	}{
		{"local", server, App{Where: Where{Exposure: "local", Port: 18810}}, Address{"http://127.0.0.1:18810", "http", "127.0.0.1"}},
		{"lan", server, App{Where: Where{Exposure: "lan", Port: 18811}}, Address{"http://192.0.2.10:18811", "http", "192.0.2.10"}},
		{"public", server, App{Where: Where{Exposure: "public", Port: 18812}}, Address{"http://192.0.2.10:18812", "http", "192.0.2.10"}},
		{"default exposure", server, App{Where: Where{Port: 18812}}, Address{"http://192.0.2.10:18812", "http", "192.0.2.10"}},
		{"desktop local", desktop, App{Where: Where{Exposure: "local", Port: 18810}}, Address{"http://localhost:18810", "http", "localhost"}},
		{"desktop lan", desktop, App{Where: Where{Exposure: "lan", Port: 18810}}, Address{"http://localhost:18810", "http", "localhost"}},
		{"domain", server, App{Where: Where{Exposure: "local", Port: 18810, Domains: []string{"n8n.example.com", "b.example.com"}}}, Address{"https://n8n.example.com", "https", "n8n.example.com"}},
		{"domain other port", Host{PublicHost: "localhost", HTTPSPort: 8443}, App{Where: Where{Exposure: "lan", Port: 18810, Domains: []string{"n8n.test"}}}, Address{"https://n8n.test:8443", "https", "n8n.test"}},
		{"domain 443", Host{HTTPSPort: 443}, App{Where: Where{Domains: []string{"n8n.test"}}}, Address{"https://n8n.test", "https", "n8n.test"}},
		{"ipv6", Host{PublicHost: "2001:db8::1"}, App{Where: Where{Exposure: "lan", Port: 18810}}, Address{"http://[2001:db8::1]:18810", "http", "2001:db8::1"}},
		{"internal", server, App{Internal: true, Where: Where{Port: 18810, Domains: []string{"x.test"}}}, Address{}},
		{"no port", server, App{Where: Where{Exposure: "lan"}}, Address{}},
		{"no server address", Host{}, App{Where: Where{Exposure: "lan", Port: 18810}}, Address{}},
	}
	for _, c := range cases {
		if got := c.h.Address(c.app); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
		if got := NewStandalone(Machine{Host: c.h}).Address(c.app); got != c.want {
			t.Errorf("%s: provider %+v", c.name, got)
		}
	}
}

// Every row says when it is worked out, which editions fill it and, when
// the user may set it, the rule in words; a rule the check does not know
// would refuse every value.
func TestTableRows(t *testing.T) {
	for _, v := range Table() {
		if v.Refresh != catalog.RefreshInstall {
			t.Errorf("%s: refresh %q", v.Name, v.Refresh)
		}
		if len(v.Editions) == 0 {
			t.Errorf("%s: no edition", v.Name)
		}
		for _, e := range v.Editions {
			if e != catalog.EditionStandalone && e != catalog.EditionCloud {
				t.Errorf("%s: edition %q", v.Name, e)
			}
		}
		switch {
		case !v.UserCanSet && (v.Rule != "" || v.RuleLabel != ""):
			t.Errorf("%s: a rule on a variable the user cannot set", v.Name)
		case v.Rule != "" && v.RuleLabel == "":
			t.Errorf("%s: rule %s without words", v.Name, v.Rule)
		case v.Rule != "" && v.Rule != catalog.RuleTZ && v.Rule != catalog.RuleURL:
			t.Errorf("%s: unknown rule %s", v.Name, v.Rule)
		}
	}
}

// The offline table's shape is pinned: what the business system and the
// cloud edition will read. Adding a key is a deliberate change of this
// test; removing or renaming one breaks the readers.
func TestOfflineShape(t *testing.T) {
	b, err := json.Marshal(Export("0.6.0-test"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if got := keys(doc); got != "offline,platform,vars" {
		t.Fatalf("top keys %s", got)
	}
	if doc["offline"] != true || doc["platform"] != "0.6.0-test" {
		t.Fatalf("doc %s", b)
	}
	vars := doc["vars"].([]any)
	if len(vars) != len(catalog.PlatformVars) {
		t.Fatalf("%d vars", len(vars))
	}
	required := "cloud,editions,label,name,refresh,since,standalone,user_can_set"
	optional := map[string]bool{"secret": true, "rule": true, "rule_label": true}
	for _, x := range vars {
		m := x.(map[string]any)
		var req []string
		for k := range m {
			if !optional[k] {
				req = append(req, k)
			}
		}
		sort.Strings(req)
		if strings.Join(req, ",") != required {
			t.Fatalf("%v: keys %v, want %s (plus optional secret, rule, rule_label)", m["name"], req, required)
		}
		for _, k := range []string{"name", "label", "standalone", "cloud", "since", "refresh"} {
			if _, ok := m[k].(string); !ok {
				t.Fatalf("%v: %s is not a string", m["name"], k)
			}
		}
		if _, ok := m["user_can_set"].(bool); !ok {
			t.Fatalf("%v: user_can_set", m["name"])
		}
		if _, ok := m["editions"].([]any); !ok {
			t.Fatalf("%v: editions", m["name"])
		}
		for _, k := range []string{"value", "available", "note"} {
			if _, ok := m[k]; ok {
				t.Fatalf("%v: the offline table carries a machine's %s", m["name"], k)
			}
		}
	}
}

func keys(m map[string]any) string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func sameMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
