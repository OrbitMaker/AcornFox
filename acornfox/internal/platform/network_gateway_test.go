package platform

import (
	"os"
	"testing"
)

func TestEffectiveGateway(t *testing.T) {
	// Not set at all: network.json, then the public gateway.
	t.Setenv("ACORNFOX_GATEWAY", "") // restored after the test
	os.Unsetenv("ACORNFOX_GATEWAY")
	if g := (Network{}).EffectiveGateway(); g != "https://dl.acornfox.com" {
		t.Fatalf("default %q", g)
	}
	if g := (Network{Gateway: " https://gw.example "}).EffectiveGateway(); g != "https://gw.example" {
		t.Fatalf("file %q", g)
	}
	if g := (Network{Gateway: "off"}).EffectiveGateway(); g != "" {
		t.Fatalf("off %q", g)
	}
	// The environment wins; empty turns it off.
	t.Setenv("ACORNFOX_GATEWAY", "https://env.example")
	if g := (Network{Gateway: "https://gw.example"}).EffectiveGateway(); g != "https://env.example" {
		t.Fatalf("env %q", g)
	}
	t.Setenv("ACORNFOX_GATEWAY", "")
	if g := (Network{Gateway: "https://gw.example"}).EffectiveGateway(); g != "" {
		t.Fatalf("env off %q", g)
	}
}

func TestGatewaySurvivesSave(t *testing.T) {
	dir := t.TempDir()
	if err := SaveNetwork(dir, Network{China: true, Gateway: "https://gw.example"}); err != nil {
		t.Fatal(err)
	}
	if n := LoadNetwork(dir); n.Gateway != "https://gw.example" {
		t.Fatalf("%+v", n)
	}
}
