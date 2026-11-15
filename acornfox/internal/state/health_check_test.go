package state

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

// An app reached over the app network only, with a health check that is no
// GET, is stored and read back; an app written before (and every app by
// default) is not internal and keeps the HTTP check.
func TestAppInternalAndHealthCheck(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	plain, _, err := s.EnsureApp(ctx, "web")
	if err != nil {
		t.Fatal(err)
	}
	if plain.Internal || plain.HealthCheck != nil {
		t.Fatalf("default: %+v", plain)
	}
	if _, _, err := s.EnsureApp(ctx, "db"); err != nil {
		t.Fatal(err)
	}
	want := &HealthCheck{Exec: []string{"demo-ping", "--host", "127.0.0.1"}, ExpectStdout: "PONG", TimeoutS: 120}
	got, err := s.UpdateApp(ctx, "db", func(a *App) error {
		a.Internal = true
		a.HealthCheck = want
		return nil
	})
	if err != nil || !got.Internal || !reflect.DeepEqual(got.HealthCheck, want) {
		t.Fatalf("update: %+v %v", got, err)
	}
	again, err := s.GetApp(ctx, "db")
	if err != nil || !again.Internal || !reflect.DeepEqual(again.HealthCheck, want) {
		t.Fatalf("read back: %+v %v", again, err)
	}
	// Other changes keep them.
	again, err = s.UpdateApp(ctx, "db", func(a *App) error { a.MemoryMB = 300; return nil })
	if err != nil || !again.Internal || !reflect.DeepEqual(again.HealthCheck, want) {
		t.Fatalf("after another change: %+v %v", again, err)
	}
	tcp := &HealthCheck{TCP: true}
	if again, err = s.UpdateApp(ctx, "db", func(a *App) error { a.HealthCheck = tcp; return nil }); err != nil || !reflect.DeepEqual(again.HealthCheck, tcp) {
		t.Fatalf("tcp: %+v %v", again, err)
	}
	if again, err = s.UpdateApp(ctx, "db", func(a *App) error { a.HealthCheck = nil; return nil }); err != nil || again.HealthCheck != nil {
		t.Fatalf("back to the HTTP check: %+v %v", again, err)
	}
	for name, bad := range map[string]*HealthCheck{
		"neither":              {},
		"both":                 {Exec: []string{"true"}, TCP: true},
		"expect_stdout on tcp": {TCP: true, ExpectStdout: "x"},
		"negative time-out":    {TCP: true, TimeoutS: -1},
		"time-out too long":    {TCP: true, TimeoutS: MaxHealthTimeoutS + 1},
	} {
		if _, err := s.UpdateApp(ctx, "db", func(a *App) error { a.HealthCheck = bad; return nil }); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	list, err := s.ListApps(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "db" || !list[0].Internal || list[1].Internal {
		t.Fatalf("list: %+v %v", list, err)
	}
}

// The columns come in with 0015 on a database that has apps: they are not
// internal and keep the HTTP check.
func TestMigration0015OnExistingApps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acornfox.db")
	s, err := Open(Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, _, err := s.EnsureApp(ctx, "old-app"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`ALTER TABLE service_bindings DROP COLUMN dedicated`,
		`DELETE FROM _schema_migrations WHERE version='0016_binding_dedicated'`,
		`ALTER TABLE apps DROP COLUMN health_check`,
		`ALTER TABLE apps DROP COLUMN internal`,
		`DELETE FROM _schema_migrations WHERE version='0015_app_internal_health'`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s.Close()
	s, err = Open(Config{Path: path})
	if err != nil {
		t.Fatalf("open a 0014 database: %v", err)
	}
	defer s.Close()
	a, err := s.GetApp(ctx, "old-app")
	if err != nil || a.Internal || a.HealthCheck != nil {
		t.Fatalf("%+v %v", a, err)
	}
}
