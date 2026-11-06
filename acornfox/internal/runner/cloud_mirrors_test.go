package runner

import (
	"os"
	"strings"
	"testing"
)

func TestCloudRegistryMirrors(t *testing.T) {
	if v, ok := os.LookupEnv("ACORNFOX_REGISTRY_MIRRORS"); ok {
		os.Unsetenv("ACORNFOX_REGISTRY_MIRRORS")
		t.Cleanup(func() { os.Setenv("ACORNFOX_REGISTRY_MIRRORS", v) })
	}
	tencent := strings.Join(append([]string{TencentRegistryMirror}, DefaultRegistryMirrors...), ",")
	for cloud, want := range map[string]string{
		"tencent":    tencent,
		" Tencent ":  tencent,
		"aliyun":     "",
		"huawei":     "",
		"volcengine": "",
		"":           "",
		"someday":    "",
	} {
		got := CloudRegistryMirrors(cloud)
		if strings.Join(got, ",") != want || (want == "" && got != nil) {
			t.Fatalf("CloudRegistryMirrors(%q) = %v, want %q", cloud, got, want)
		}
		// Whatever the vendor, the runner ends up with a usable order.
		d := &Docker{}
		d.SetRegistry(Registry{China: true, Mirrors: got})
		if len(d.mirrors()) == 0 {
			t.Fatalf("%q: no mirrors", cloud)
		}
	}
	// The returned slice must not alias DefaultRegistryMirrors.
	m := CloudRegistryMirrors("tencent")
	m[1] = "changed"
	if DefaultRegistryMirrors[0] == "changed" {
		t.Fatal("aliased the defaults")
	}

	t.Setenv("ACORNFOX_REGISTRY_MIRRORS", "mirror.example.com")
	if got := CloudRegistryMirrors("tencent"); got != nil {
		t.Fatalf("explicit mirrors must win, got %v", got)
	}
	d := &Docker{}
	d.SetRegistry(Registry{China: true, Mirrors: CloudRegistryMirrors("tencent")})
	if strings.Join(d.mirrors(), ",") != "mirror.example.com" {
		t.Fatalf("%v", d.mirrors())
	}
}
