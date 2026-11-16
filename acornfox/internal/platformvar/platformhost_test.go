package platformvar

import (
	"strings"
	"testing"
)

// The platform domain step of the address algorithm: own domain first, then
// <app>.<platform domain> for a public app, then the address with its port.
func TestAddressWithPlatformDomain(t *testing.T) {
	server := Host{PublicHost: "203.0.113.10"}
	long := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 56) + ".com"
	cases := []struct {
		name string
		h    Host
		app  App
		want Address
	}{
		{"public gets the subdomain", server, App{Name: "hello", Where: Where{Exposure: "public", Port: 18810, PlatformDomain: "apps.example.com"}},
			Address{"https://hello.apps.example.com", "https", "hello.apps.example.com"}},
		{"own domain first", server, App{Name: "hello", Where: Where{Exposure: "public", Port: 18810, Domains: []string{"hello.mine.org"}, PlatformDomain: "apps.example.com"}},
			Address{"https://hello.mine.org", "https", "hello.mine.org"}},
		{"lan keeps its port", server, App{Name: "hello", Where: Where{Exposure: "lan", Port: 18810, PlatformDomain: "apps.example.com"}},
			Address{"http://203.0.113.10:18810", "http", "203.0.113.10"}},
		{"local keeps loopback", server, App{Name: "hello", Where: Where{Exposure: "local", Port: 18810, PlatformDomain: "apps.example.com"}},
			Address{"http://127.0.0.1:18810", "http", "127.0.0.1"}},
		{"default exposure is not public", server, App{Name: "hello", Where: Where{Port: 18810, PlatformDomain: "apps.example.com"}},
			Address{"http://203.0.113.10:18810", "http", "203.0.113.10"}},
		{"internal has none", server, App{Name: "db", Internal: true, Where: Where{Exposure: "public", Port: 18810, PlatformDomain: "apps.example.com"}},
			Address{}},
		{"no platform domain", server, App{Name: "hello", Where: Where{Exposure: "public", Port: 18810}},
			Address{"http://203.0.113.10:18810", "http", "203.0.113.10"}},
		{"name too long falls back", server, App{Name: "hello", Where: Where{Exposure: "public", Port: 18810, PlatformDomain: long}},
			Address{"http://203.0.113.10:18810", "http", "203.0.113.10"}},
		{"https port", Host{PublicHost: "203.0.113.10", HTTPSPort: 8443}, App{Name: "hello", Where: Where{Exposure: "public", Port: 18810, PlatformDomain: "apps.example.com"}},
			Address{"https://hello.apps.example.com:8443", "https", "hello.apps.example.com"}},
		{"no port yet still has the name", server, App{Name: "hello", Where: Where{Exposure: "public", PlatformDomain: "apps.example.com"}},
			Address{"https://hello.apps.example.com", "https", "hello.apps.example.com"}},
	}
	for _, c := range cases {
		if got := c.h.Address(c.app); got != c.want {
			t.Errorf("%s: got %+v want %+v", c.name, got, c.want)
		}
		// app.url, app.scheme and app.host follow the same step.
		vals, err := NewStandalone(Machine{Host: c.h}).Values(t.Context(), App{Name: c.app.Name, Created: true, Internal: c.app.Internal, Where: c.app.Where})
		if err != nil {
			t.Fatal(err)
		}
		if vals["app.url"] != c.want.URL || vals["app.scheme"] != c.want.Scheme || vals["app.host"] != c.want.Host {
			t.Errorf("%s: values %v want %+v", c.name, vals, c.want)
		}
	}
}

func TestPlatformHost(t *testing.T) {
	if got := PlatformHost("hello", false, "public", "apps.example.com"); got != "hello.apps.example.com" {
		t.Fatalf("got %q", got)
	}
	for _, c := range []struct {
		app      string
		internal bool
		exposure string
		base     string
	}{
		{"hello", false, "lan", "apps.example.com"},
		{"hello", false, "local", "apps.example.com"},
		{"hello", false, "", "apps.example.com"},
		{"hello", true, "public", "apps.example.com"},
		{"hello", false, "public", ""},
		{"", false, "public", "apps.example.com"},
		{"hello", false, "public", strings.Repeat("x", 250) + ".com"},
	} {
		if got := PlatformHost(c.app, c.internal, c.exposure, c.base); got != "" {
			t.Errorf("%+v: got %q, want none", c, got)
		}
	}
}
