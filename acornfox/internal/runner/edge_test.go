package runner

import (
	"strings"
	"testing"

	"github.com/moby/moby/api/types/network"
)

func validSpec() EdgeSpec {
	return EdgeSpec{Mode: EdgeModeBridge, AdminPort: 18808, AdminToken: strings.Repeat("ab", 24), Ports: []EdgePort{
		{HostIP: "0.0.0.0", Port: 18810}, {HostIP: "127.0.0.1", Port: 18811}, {HostIP: "192.168.31.46", Port: 18811, ContainerPort: 28811}}}
}

func TestValidateEdgeSpec(t *testing.T) {
	if err := ValidateEdgeSpec(validSpec()); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	bad := map[string]func(*EdgeSpec){
		"mode":           func(s *EdgeSpec) { s.Mode = "privileged" },
		"host with port": func(s *EdgeSpec) { s.Mode = EdgeModeHost },
		"admin port":     func(s *EdgeSpec) { s.AdminPort = 80 },
		"short token":    func(s *EdgeSpec) { s.AdminToken = "abc" },
		"token chars":    func(s *EdgeSpec) { s.AdminToken = strings.Repeat("Z", 40) },
		"public address": func(s *EdgeSpec) { s.Ports[0].HostIP = "203.0.113.5" },
		"duplicate":      func(s *EdgeSpec) { s.Ports[1].HostIP = "0.0.0.0"; s.Ports[1].Port = 18810 },
		"same target":    func(s *EdgeSpec) { s.Ports[1].ContainerPort = 18810 },
		"admin clash":    func(s *EdgeSpec) { s.Ports[0].Port = 18808 },
		"internal port":  func(s *EdgeSpec) { s.Ports[0].Port = 2020 },
	}
	for name, mutate := range bad {
		s := validSpec()
		s.Ports = append([]EdgePort(nil), s.Ports...)
		mutate(&s)
		if err := ValidateEdgeSpec(s); err == nil {
			t.Errorf("%s: accepted %+v", name, s)
		}
	}
}

func TestEdgeCaddyfile(t *testing.T) {
	s := validSpec()
	bridge := EdgeCaddyfile(s)
	for _, want := range []string{"admin unix//tmp/caddy-admin.sock", "origins caddy", "persist_config off", "http://:2020 {", `@authorized header X-AcornFox-Token "` + s.AdminToken + `"`, "reverse_proxy unix//tmp/caddy-admin.sock", "header_up Host caddy", `respond "forbidden" 403`} {
		if !strings.Contains(bridge, want) {
			t.Errorf("bridge Caddyfile missing %q:\n%s", want, bridge)
		}
	}
	if strings.Contains(bridge, "bind 127.0.0.1") {
		t.Error("bridge mode listens on all container interfaces (publishing limits it to 127.0.0.1)")
	}
	s.Mode, s.Ports = EdgeModeHost, nil
	host := EdgeCaddyfile(s)
	if !strings.Contains(host, "http://:18808 {") || !strings.Contains(host, "bind 127.0.0.1") {
		t.Errorf("host Caddyfile must bind the admin gateway to loopback:\n%s", host)
	}
}

func TestEdgeHashFollowsSpec(t *testing.T) {
	a := validSpec()
	b := validSpec()
	b.Ports = []EdgePort{b.Ports[2], b.Ports[1], b.Ports[0]}
	if edgeHash(a, EdgeImageLocal) != edgeHash(b, EdgeImageLocal) {
		t.Error("port order must not matter")
	}
	b.Ports[0].HostIP = "0.0.0.0"
	if edgeHash(a, EdgeImageLocal) == edgeHash(b, EdgeImageLocal) {
		t.Error("a changed bind address must change the hash")
	}
	if edgeHash(a, EdgeImageLocal) == edgeHash(a, EdgeImagePublic) {
		t.Error("a changed image must change the hash")
	}
}

func TestOnlyEdgeAttached(t *testing.T) {
	n := network.Inspect{}
	n.Containers = map[string]network.EndpointResource{"x": {Name: EdgeContainerName}}
	if !onlyEdgeAttached(n) {
		t.Error("edge only")
	}
	n.Containers["y"] = network.EndpointResource{Name: "af-web-aaaaaaaaaaaa"}
	if onlyEdgeAttached(n) {
		t.Error("an app container is still attached")
	}
}
