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
