package appinstall

import (
	"context"
	"testing"

	"github.com/acornfox/acornfox/internal/state"
)

// A catalog app whose manifest gives first-login credentials has its own
// login; an app that is not from the catalog, or an unknown name, has not.
func TestHasLogin(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	if tv := v.install(t, Request{App: "new-api"}); tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	if !v.e.HasLogin(ctx, "new-api") {
		t.Fatal("new-api has an admin account: want HasLogin true")
	}
	if v.e.HasLogin(ctx, "not-installed") {
		t.Fatal("unknown app: want false")
	}
	if v.e.HasLogin(ctx, "../bad") {
		t.Fatal("invalid name: want false")
	}
}

// Only an address the machine itself answers on gets the first-login note:
// the loopback range and ::1, not localhost (a desktop install, where the
// user sits at that machine) and not a LAN or public address.
func TestLoopbackURL(t *testing.T) {
	for u, want := range map[string]bool{
		"http://127.0.0.1:18811":    true,
		"http://127.0.0.2:80/x":     true,
		"http://[::1]:18811/":       true,
		"http://localhost:18811":    false,
		"http://192.168.1.20:18811": false,
		"https://app.example.com/":  false,
		"":                          false,
		"http://%zz":                false,
	} {
		if got := loopbackURL(u); got != want {
			t.Errorf("%q: %v", u, got)
		}
	}
}
