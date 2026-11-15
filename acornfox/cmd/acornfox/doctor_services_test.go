package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/hostcheck"
)

func TestDoctorAppServices(t *testing.T) {
	old := doctorServices
	t.Cleanup(func() { doctorServices = old })

	// The provider of a service an app uses is stopped: a warning, not a
	// failure, naming the app, its need, the service and what to do.
	doctorServices = func(context.Context, string) (servicesQuery, bool) {
		return servicesQuery{services: []hostcheck.ServiceInUse{{
			Name: "gw", Kind: "llm_gateway", KindLabel: "大模型接口", Status: hostcheck.ServiceStatusProviderStopped, StatusLabel: "提供它的应用停着", Provider: "gw",
			Users: []hostcheck.ServiceUser{{App: "agent", Use: "llm"}},
		}}, installed: func(string) bool { return true }}, true
	}
	r := doctorReport{OK: true}
	doctorAppServices(context.Background(), &r, "/nonexistent.sock")
	if len(r.Items) != 1 || !r.OK {
		t.Fatalf("items %+v ok %v", r.Items, r.OK)
	}
	it := r.Items[0]
	if it.Level != "warn" || it.Code != "CAT-SVC-08" || it.Check != "CAT-SVC-08" ||
		!strings.Contains(it.Text, "应用 agent 的需要 llm 接的服务 gw（大模型接口）") || !strings.Contains(it.Text, "acornfox start gw") {
		t.Fatalf("item %+v", it)
	}

	// The server answered with an error: say so.
	doctorServices = func(context.Context, string) (servicesQuery, bool) {
		return servicesQuery{err: errors.New("boom")}, true
	}
	r = doctorReport{OK: true}
	doctorAppServices(context.Background(), &r, "/nonexistent.sock")
	if len(r.Items) != 1 || r.Items[0].Level != "warn" || !strings.Contains(r.Items[0].Text, "boom") {
		t.Fatalf("items %+v", r.Items)
	}

	// Cannot ask (or the server is older than services): no line.
	doctorServices = func(context.Context, string) (servicesQuery, bool) { return servicesQuery{}, false }
	r = doctorReport{OK: true}
	doctorAppServices(context.Background(), &r, "/nonexistent.sock")
	if len(r.Items) != 0 {
		t.Fatalf("items %+v", r.Items)
	}
	// The real query against a missing socket gives up quietly.
	if _, ok := queryServices(context.Background(), "/nonexistent/acornfox.sock"); ok {
		t.Fatal("missing socket must not be ok")
	}
}
