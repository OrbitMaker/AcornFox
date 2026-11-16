package main

import (
	"context"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
)

func TestDoctorPlatformDomain(t *testing.T) {
	old := doctorPlatformDomainQuery
	t.Cleanup(func() { doctorPlatformDomainQuery = old })
	run := func(v client.PlatformDomain, ok bool) doctorReport {
		doctorPlatformDomainQuery = func(context.Context, string) (client.PlatformDomain, bool) { return v, ok }
		r := doctorReport{OK: true}
		doctorPlatformDomain(context.Background(), &r, "/nonexistent.sock")
		return r
	}

	// Not set, or the server cannot be asked: no line.
	if r := run(client.PlatformDomain{State: "unset"}, true); len(r.Items) != 0 {
		t.Fatalf("%+v", r.Items)
	}
	if r := run(client.PlatformDomain{}, false); len(r.Items) != 0 {
		t.Fatalf("%+v", r.Items)
	}

	// All three pass.
	r := run(client.PlatformDomain{Domain: "apps.example.com", State: "ok", Supported: true, Checks: []client.PDomainCheck{
		{Key: "dns", OK: true, Text: "域名指过来了"}, {Key: "ports", OK: true, Text: "外面能连上"}, {Key: "cert", OK: true, Text: "证书已自动申请"},
	}}, true)
	if len(r.Items) != 1 || !r.OK || r.Items[0].Level != "ok" || !strings.Contains(r.Items[0].Text, "外面能连上") {
		t.Fatalf("%+v", r.Items)
	}

	// Each failing check is reported with its code, the same as the API.
	for _, code := range []string{"PDOMAIN-01", "PDOMAIN-03", "PDOMAIN-04"} {
		r = run(client.PlatformDomain{Domain: "apps.example.com", State: "problem", Supported: true,
			Problem: &client.PDomainProblem{Code: code, Text: "不通", Detail: "细节", Next: "怎么办"}}, true)
		if len(r.Items) != 1 || r.OK || r.Items[0].Code != code || !strings.Contains(r.Items[0].Text, "细节") || !strings.Contains(r.Items[0].Text, "怎么办") {
			t.Fatalf("%s: %+v", code, r.Items)
		}
	}

	// A machine without a public address: a warning, PDOMAIN-02.
	r = run(client.PlatformDomain{Domain: "apps.example.com", State: "problem",
		Unsupported: &client.PDomainProblem{Code: "PDOMAIN-02", Text: "这台机器在局域网里，没有公网地址，用不了域名"}}, true)
	if len(r.Items) != 1 || !r.OK || r.Items[0].Level != "warn" || r.Items[0].Code != "PDOMAIN-02" {
		t.Fatalf("%+v", r.Items)
	}

	// The real query against a missing socket gives up quietly.
	if _, ok := queryPlatformDomain(context.Background(), "/nonexistent/acornfox.sock"); ok {
		t.Fatal("missing socket must not be ok")
	}
}
