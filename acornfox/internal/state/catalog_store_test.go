package state

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestAppCatalogFieldsRoundTrip(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, _, err := s.EnsureApp(ctx, "hermes-agent"); err != nil {
		t.Fatal(err)
	}
	a, err := s.UpdateApp(ctx, "hermes-agent", func(a *App) error {
		a.Command = []string{"gateway", "run"}
		a.PidsLimit = 512
		a.Links = []string{"new-api"}
		a.CatalogID, a.CatalogVersion, a.CatalogSource = "hermes-agent", "v0.23.0", "recipe:hermes-with-new-api"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetApp(ctx, "hermes-agent")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Command, []string{"gateway", "run"}) || got.PidsLimit != 512 || !reflect.DeepEqual(got.Links, []string{"new-api"}) ||
		got.CatalogID != "hermes-agent" || got.CatalogVersion != "v0.23.0" || got.CatalogSource != "recipe:hermes-with-new-api" || a.CatalogID != got.CatalogID {
		t.Fatalf("round trip: %+v", got)
	}
	// A link to itself or a malformed name is refused.
	if _, err := s.UpdateApp(ctx, "hermes-agent", func(a *App) error { a.Links = []string{"hermes-agent"}; return nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("self link: %v", err)
	}
	// Clearing works and stores the empty form.
	got, err = s.UpdateApp(ctx, "hermes-agent", func(a *App) error { a.Command, a.Links = nil, nil; return nil })
	if err != nil || got.Command != nil || got.Links != nil {
		t.Fatalf("clear: %+v %v", got, err)
	}
}

func TestInstallTasks(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, err := s.CreateInstallTask(ctx, InstallTask{ID: "0123456789ab", Kind: "app", Target: "new-api", Status: TaskRunning}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateInstallTask(ctx, InstallTask{ID: "0123456789ab", Kind: "app", Target: "new-api", Status: TaskRunning}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := s.CreateInstallTask(ctx, InstallTask{ID: "bad", Kind: "app", Status: TaskRunning}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad id: %v", err)
	}
	u, err := s.UpdateInstallTask(ctx, "0123456789ab", func(it *InstallTask) error {
		it.Status, it.FaultCode, it.Data = TaskFailed, "CAT-INST-17", `{"steps":[]}`
		it.Target = "changed"
		return nil
	})
	if err != nil || u.Target != "new-api" || u.Status != TaskFailed {
		t.Fatalf("update: %+v %v", u, err)
	}
	if _, err := s.UpdateInstallTask(ctx, "0123456789ab", func(it *InstallTask) error { it.Data = "not json"; return nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad data: %v", err)
	}
	if _, err := s.CreateInstallTask(ctx, InstallTask{ID: "0123456789ac", Kind: "recipe", Target: "r", Status: TaskRunning}); err != nil {
		t.Fatal(err)
	}
	running, err := s.ListInstallTasks(ctx, TaskRunning, 10)
	if err != nil || len(running) != 1 || running[0].ID != "0123456789ac" {
		t.Fatalf("running: %+v %v", running, err)
	}
	all, _ := s.ListInstallTasks(ctx, "", 10)
	if len(all) != 2 {
		t.Fatalf("all: %d", len(all))
	}
	if _, err := s.GetInstallTask(ctx, "ffffffffffff"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestCatalogSecretsGoWithApp(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if err := s.SetCatalogSecrets(ctx, CatalogSecrets{App: "nope", ParamsEnc: "v1:x", FirstLoginEnc: "v1:y"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no app: %v", err)
	}
	if _, _, err := s.EnsureApp(ctx, "new-api"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCatalogSecrets(ctx, CatalogSecrets{App: "new-api", ParamsEnc: "v1:x", FirstLoginEnc: "v1:y"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCatalogSecrets(ctx, CatalogSecrets{App: "new-api", ParamsEnc: "v1:x2", FirstLoginEnc: "v1:y2"}); err != nil {
		t.Fatal(err)
	}
	cs, err := s.GetCatalogSecrets(ctx, "new-api")
	if err != nil || cs.ParamsEnc != "v1:x2" || cs.FirstLoginEnc != "v1:y2" {
		t.Fatalf("get: %+v %v", cs, err)
	}
	if err := s.DeleteApp(ctx, "new-api", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCatalogSecrets(ctx, "new-api"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}
