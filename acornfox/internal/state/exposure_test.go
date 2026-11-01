package state

import (
	"context"
	"errors"
	"testing"
)

func TestAppExposure(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, _, err := s.EnsureApp(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	a, _ := s.GetApp(ctx, "web")
	if a.Exposure != "" {
		t.Fatalf("new app exposure = %q, want platform default \"\"", a.Exposure)
	}
	for _, e := range []string{ExposureLocal, ExposureLAN, ExposurePublic, ExposureAFXCloud, ""} {
		got, err := s.UpdateApp(ctx, "web", func(a *App) error { a.Exposure = e; return nil })
		if err != nil || got.Exposure != e {
			t.Fatalf("set %q: %+v %v", e, got, err)
		}
	}
	if _, err := s.UpdateApp(ctx, "web", func(a *App) error { a.Exposure = "internet"; return nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid exposure err = %v", err)
	}
	apps, _ := s.ListApps(ctx)
	if len(apps) != 1 || apps[0].Exposure != "" {
		t.Fatalf("list = %+v", apps)
	}
}
