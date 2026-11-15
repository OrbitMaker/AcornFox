package edge

import (
	"strings"
	"testing"
)

func TestVersionIgnoresOrderAndDefaults(t *testing.T) {
	a := Config{
		Routes: []Route{
			{App: "web", PublicPort: 18810, Upstream: "127.0.0.1:32768", Domains: []string{"b.example.com", "a.example.com"}},
			{App: "api", PublicPort: 18811, Listen: "127.0.0.1", Upstream: "127.0.0.1:32769"},
		},
	}
	b := Config{
		Routes: []Route{
			{App: "api", PublicPort: 18811, Listen: "127.0.0.1", Upstream: "127.0.0.1:32769", Domains: []string{}},
			{App: "web", PublicPort: 18810, Upstream: "127.0.0.1:32768", Domains: []string{"a.example.com", "b.example.com", "a.example.com", ""}},
		},
		HTTPS: HTTPS{HTTPPort: 80, HTTPSPort: 443, Issuer: "acme"},
	}
	if a.Version() != b.Version() {
		t.Fatalf("same meaning, different versions: %s %s", a.Version(), b.Version())
	}
	if !strings.HasPrefix(a.Version(), versionPrefix) {
		t.Fatalf("version %q lacks prefix", a.Version())
	}
}

func TestVersionSeesEveryField(t *testing.T) {
	base := Config{Routes: []Route{{App: "web", PublicPort: 18810, Upstream: "127.0.0.1:1"}}}
	v := base.Version()
	changes := map[string]Config{
		"port":     {Routes: []Route{{App: "web", PublicPort: 18811, Upstream: "127.0.0.1:1"}}},
		"listen":   {Routes: []Route{{App: "web", PublicPort: 18810, Listen: "127.0.0.1", Upstream: "127.0.0.1:1"}}},
		"lan":      {Routes: []Route{{App: "web", PublicPort: 18810, LANPort: 28810, Upstream: "127.0.0.1:1"}}},
		"upstream": {Routes: []Route{{App: "web", PublicPort: 18810, Upstream: "127.0.0.1:2"}}},
		"domain":   {Routes: []Route{{App: "web", PublicPort: 18810, Upstream: "127.0.0.1:1", Domains: []string{"x.example.com"}}}},
		"app":      {Routes: []Route{{App: "web2", PublicPort: 18810, Upstream: "127.0.0.1:1"}}},
		"extra":    {Routes: []Route{{App: "web", PublicPort: 18810, Upstream: "127.0.0.1:1"}, {App: "_c", Upstream: "127.0.0.1:3", Domains: []string{"c.example.com"}}}},
		"none":     {},
		"httpport": {Routes: base.Routes, HTTPS: HTTPS{HTTPPort: 8080}},
		"issuer":   {Routes: base.Routes, HTTPS: HTTPS{Issuer: IssuerInternal}},
		"email":    {Routes: base.Routes, HTTPS: HTTPS{Email: "ops@example.com"}},
	}
	for name, c := range changes {
		if c.Version() == v {
			t.Errorf("%s: version did not change", name)
		}
	}
}

func TestNormalizedDoesNotTouchInput(t *testing.T) {
	c := Config{Routes: []Route{{App: "b", Domains: []string{"z", "y"}}, {App: "a"}}}
	_ = c.Normalized()
	if c.Routes[0].App != "b" || c.Routes[0].Domains[0] != "z" {
		t.Fatalf("input changed: %+v", c.Routes)
	}
}

func TestHealthOK(t *testing.T) {
	if !(Health{State: StateRunning}).OK() || (Health{State: StateStarting}).OK() || (Health{State: StateError}).OK() {
		t.Fatal("OK must be true for running only")
	}
}
