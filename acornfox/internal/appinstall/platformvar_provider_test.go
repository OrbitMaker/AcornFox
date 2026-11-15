package appinstall

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/platformvar"
	"github.com/acornfox/acornfox/internal/state"
)

// otherEdition is a provider that is not this machine (a stand-in for the
// cloud edition, only in the tests): every value is its own, and the app's
// address is the one it assigns. An engine given it must show and use these
// values and none of this machine's.
type otherEdition struct {
	mu    sync.Mutex
	calls []platformvar.App
}

func (o *otherEdition) Edition() string              { return catalog.EditionCloud }
func (o *otherEdition) Table() []catalog.PlatformVar { return platformvar.Table() }

func (o *otherEdition) base() map[string]string {
	return map[string]string{"tz": "Pacific/Auckland", "region": "global",
		"mirror.pypi": "http://pypi.other.example/simple/", "mirror.npm": "http://npm.other.example/"}
}

func (o *otherEdition) Machine(context.Context) []platformvar.State {
	base := o.base()
	var out []platformvar.State
	for _, pv := range platformvar.Table() {
		st := platformvar.State{PlatformVar: pv, Available: true}
		st.Value = base[pv.Name]
		if st.Value == "" {
			st.Note = "per app"
		}
		out = append(out, st)
	}
	return out
}

func (o *otherEdition) Values(_ context.Context, app platformvar.App) (map[string]string, error) {
	o.mu.Lock()
	o.calls = append(o.calls, app)
	o.mu.Unlock()
	out := o.base()
	if app.Created {
		a := o.Address(app)
		out["app.url"], out["app.scheme"], out["app.host"] = a.URL, a.Scheme, a.Host
	}
	for k, v := range app.Sets {
		out[k] = strings.TrimSpace(v)
	}
	return out, nil
}

// Address is the one this edition assigns, whatever the server would say.
func (o *otherEdition) Address(app platformvar.App) platformvar.Address {
	if app.Internal {
		return platformvar.Address{}
	}
	host := app.Name + ".apps.other.example"
	return platformvar.Address{URL: "https://" + host, Scheme: "https", Host: host}
}

func (o *otherEdition) Has(name string) bool { _, ok := catalog.LookupPlatformVar(name); return ok }

func (o *otherEdition) Check(sets map[string]string) error {
	return platformvar.CheckSets(platformvar.Table(), sets)
}

// The install engine takes every platform value from the provider it is
// given, in the install, the saved values, the views and the check of the
// user's changes; this machine's settings (Config.Platform) are not used.
func TestEngineUsesGivenProvider(t *testing.T) {
	other := &otherEdition{}
	v := setupWith(t, func(c *Config) {
		c.Platform = PlatformEnv{China: true, Cloud: "aliyun", TZ: "Asia/Shanghai"} // must be ignored
		c.PlatformVars = other
	})
	ctx := context.Background()
	if tv := v.install(t, Request{App: "new-api", Platform: map[string]string{"mirror.npm": "https://npm.example.com/"}}); tv.Status != state.TaskSucceeded {
		t.Fatalf("new-api %+v", tv.Fault)
	}
	if got := envMap(t, v, "new-api")["TZ"]; got != "Pacific/Auckland" {
		t.Fatalf("TZ %q: not from the provider", got)
	}
	saved, err := v.e.savedParams(ctx, "new-api")
	if err != nil {
		t.Fatal(err)
	}
	p := Values(saved).platformMap()
	if p["app.url"] != "https://new-api.apps.other.example" || p["mirror.pypi"] != "http://pypi.other.example/simple/" ||
		p["mirror.npm"] != "https://npm.example.com/" || p["region"] != "global" {
		t.Fatalf("saved %v", p)
	}
	// It was asked once before the app existed and once with its address.
	var created *platformvar.App
	other.mu.Lock()
	for i, c := range other.calls {
		if c.Created {
			created = &other.calls[i]
		}
	}
	other.mu.Unlock()
	if created == nil || created.Name != "new-api" || created.Exposure != state.ExposureLocal || created.Port == 0 || created.Sets["mirror.npm"] != "https://npm.example.com/" {
		t.Fatalf("calls %+v", other.calls)
	}

	all := v.e.PlatformVars(ctx)
	if len(all) != len(catalog.PlatformVars) {
		t.Fatalf("%d vars", len(all))
	}
	for _, st := range all {
		if st.Name == "tz" && st.Value != "Pacific/Auckland" {
			t.Fatalf("view tz %q", st.Value)
		}
	}
	d, err := v.e.Detail(ctx, "hermes-agent")
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range d.Platform {
		if st.Name == "mirror.pypi" && st.Value != "http://pypi.other.example/simple/" {
			t.Fatalf("detail %+v", st)
		}
	}
	if _, err := v.e.StartInstall(ctx, Request{App: "new-api", Name: "x", Platform: map[string]string{"region": "cn"}}); code(err) != CodeBadPlatform {
		t.Fatalf("check: %v", err)
	}

	// Reinstalled on its old data: the user's earlier change comes back
	// through the provider (it is asked again with it), everything else is
	// the provider's, none of this machine's.
	keepData(t, v, "new-api")
	other.mu.Lock()
	other.calls = nil
	other.mu.Unlock()
	if tv := v.install(t, Request{App: "new-api", ReuseData: true}); tv.Status != state.TaskSucceeded {
		t.Fatalf("reuse %+v", tv.Fault)
	}
	if got := envMap(t, v, "new-api")["TZ"]; got != "Pacific/Auckland" {
		t.Fatalf("reuse TZ %q: not from the provider", got)
	}
	saved, err = v.e.savedParams(ctx, "new-api")
	if err != nil {
		t.Fatal(err)
	}
	p = Values(saved).platformMap()
	if p["mirror.npm"] != "https://npm.example.com/" || p["app.url"] != "https://new-api.apps.other.example" || p["mirror.pypi"] != "http://pypi.other.example/simple/" {
		t.Fatalf("reuse saved %v", p)
	}
	created = nil
	other.mu.Lock()
	for i, c := range other.calls {
		if c.Created {
			created = &other.calls[i]
		}
	}
	other.mu.Unlock()
	if created == nil || created.Sets["mirror.npm"] != "https://npm.example.com/" {
		t.Fatalf("reuse calls %+v", other.calls)
	}
}
