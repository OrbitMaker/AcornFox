package state

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

// The rule of who may be plugged into a service (PlugRule): another app's
// dedicated binding holds it; a dedicated need takes only a service no other
// app uses; MaxConsumers counts other apps (not needs), not the app itself,
// and lets an app already plugged in through another need through.
func TestPlugRule(t *testing.T) {
	b := func(app string, dedicated bool, max int) ServiceBinding {
		return ServiceBinding{App: app, UseName: "db", Service: "db", Dedicated: dedicated, MaxConsumers: max}
	}
	cases := []struct {
		name     string
		existing []PlugBinding
		b        ServiceBinding
		reason   string
		apps     []string
	}{
		{"free", nil, b("a", true, 1), "", nil},
		{"own binding does not count", []PlugBinding{{App: "a", Dedicated: true}}, b("a", true, 1), "", nil},
		{"held by another", []PlugBinding{{App: "x", Dedicated: true}}, b("a", false, 0), TakenHeld, []string{"x"}},
		{"held wins over full", []PlugBinding{{App: "y"}, {App: "x", Dedicated: true}}, b("a", false, 1), TakenHeld, []string{"x"}},
		{"dedicated, others plugged in", []PlugBinding{{App: "y"}, {App: "x"}, {App: "x"}}, b("a", true, 0), TakenInUse, []string{"x", "y"}},
		{"full", []PlugBinding{{App: "x"}, {App: "y"}}, b("a", false, 2), TakenFull, []string{"x", "y"}},
		{"room left (apps, not needs)", []PlugBinding{{App: "x"}, {App: "x"}}, b("a", false, 2), "", nil},
		{"full, but plugged in already", []PlugBinding{{App: "x"}, {App: "a"}}, b("a", false, 1), "", nil},
		{"no limit", []PlugBinding{{App: "x"}, {App: "y"}, {App: "z"}}, b("a", false, 0), "", nil},
	}
	for _, c := range cases {
		err := PlugRule(c.existing, c.b)
		if c.reason == "" {
			if err != nil {
				t.Errorf("%s: %v", c.name, err)
			}
			continue
		}
		var te *ServiceTakenError
		if !errors.As(err, &te) || te.Reason != c.reason || !reflect.DeepEqual(te.Apps, c.apps) || te.Service != "db" || !errors.Is(err, ErrConflict) {
			t.Errorf("%s: %#v", c.name, err)
		}
		if c.reason == TakenFull && te.Max != c.b.MaxConsumers {
			t.Errorf("%s: max %d", c.name, te.Max)
		}
	}
}

// SetServiceBinding stores whether a binding is dedicated and checks the
// rule in the transaction that writes it: of several apps writing a
// dedicated binding to the same service at once, exactly one gets it.
// CheckServicePlug says the same without writing.
func TestSetServiceBindingDedicated(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	mustCreateService(t, s, appService("db", "demo_sql", "db", 0))

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = s.SetServiceBinding(ctx, ServiceBinding{App: "app" + string(rune('a'+i)), UseName: "db", Service: "db", Dedicated: true})
		}(i)
	}
	wg.Wait()
	won := 0
	for _, err := range errs {
		var te *ServiceTakenError
		switch {
		case err == nil:
			won++
		case errors.As(err, &te) && (te.Reason == TakenHeld || te.Reason == TakenInUse):
		default:
			t.Fatalf("unexpected: %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("%d apps got the dedicated service", won)
	}
	bs, err := s.ListServiceBindings(ctx, BindingFilter{Service: "db"})
	if err != nil || len(bs) != 1 || !bs[0].Dedicated {
		t.Fatalf("%+v %v", bs, err)
	}
	holder := bs[0].App
	// Read back as stored; the holder may write its own binding again.
	if got, err := s.GetServiceBinding(ctx, holder, "db"); err != nil || !got.Dedicated {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.SetServiceBinding(ctx, ServiceBinding{App: holder, UseName: "db", Service: "db", Dedicated: true, IssuedEnc: "v1:again"}); err != nil {
		t.Fatalf("the holder writing its binding again: %v", err)
	}
	// Nobody else, dedicated or not; CheckServicePlug agrees and writes
	// nothing.
	other := ServiceBinding{App: "late", UseName: "db", Service: "db"}
	var te *ServiceTakenError
	if err := s.CheckServicePlug(ctx, other); !errors.As(err, &te) || te.Reason != TakenHeld || te.Apps[0] != holder {
		t.Fatalf("check: %v", err)
	}
	if _, err := s.SetServiceBinding(ctx, other); !errors.As(err, &te) || te.Reason != TakenHeld {
		t.Fatalf("set: %v", err)
	}
	if bs, _ := s.ListServiceBindings(ctx, BindingFilter{Service: "db"}); len(bs) != 1 {
		t.Fatalf("written anyway: %+v", bs)
	}
	// A limit counts apps; a parked binding is its app's and moves with it.
	mustCreateService(t, s, appService("cache", "demo_sql", "cache", 0))
	if _, err := s.SetServiceBinding(ctx, ServiceBinding{App: "app-x", UseName: "c", Service: "cache", MaxConsumers: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveServiceBinding(ctx, "app-x", "c", ParkedUseName("c")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetServiceBinding(ctx, ServiceBinding{App: "app-x", UseName: "c", Service: "cache", MaxConsumers: 1}); err != nil {
		t.Fatalf("its own parked binding does not count: %v", err)
	}
	if _, err := s.SetServiceBinding(ctx, ServiceBinding{App: "app-y", UseName: "c", Service: "cache", MaxConsumers: 1}); !errors.As(err, &te) || te.Reason != TakenFull || te.Max != 1 {
		t.Fatalf("full: %v", err)
	}
	if err := s.CheckServicePlug(ctx, ServiceBinding{App: "app-y", UseName: "c", Service: "cache"}); err != nil {
		t.Fatalf("no limit given, not dedicated: %v", err)
	}
}
