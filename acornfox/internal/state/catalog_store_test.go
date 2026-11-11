package state

import (
	"context"
	"errors"
	"path/filepath"
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

func testManifest(app, text, source string) CatalogManifest {
	return CatalogManifest{App: app, CatalogID: "new-api", CatalogVersion: "v1", Manifest: text, SHA256: ManifestSHA256(text), Source: source}
}

// The manifest an app was installed with is saved with the app, replaced by
// an install, never by a backfill, and removed with the app.
func TestCatalogManifestGoesWithApp(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if err := s.SetCatalogManifest(ctx, testManifest("nope", "id: new-api\n", ManifestFromInstall)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no app: %v", err)
	}
	if _, _, err := s.EnsureApp(ctx, "new-api"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCatalogManifest(ctx, "new-api"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("before save: %v", err)
	}
	bad := testManifest("new-api", "id: new-api\n", ManifestFromInstall)
	bad.SHA256 = ManifestSHA256("something else")
	if err := s.SetCatalogManifest(ctx, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong checksum accepted: %v", err)
	}
	if err := s.SetCatalogManifest(ctx, testManifest("new-api", "id: new-api\n", "guess")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown source accepted: %v", err)
	}
	// A backfill fills an empty slot once and is a no-op after that.
	added, err := s.AddCatalogManifest(ctx, testManifest("new-api", "id: new-api\n# old\n", ManifestFromBackfill))
	if err != nil || !added {
		t.Fatalf("backfill: %v %v", added, err)
	}
	added, err = s.AddCatalogManifest(ctx, testManifest("new-api", "id: new-api\n# other\n", ManifestFromBackfill))
	if err != nil || added {
		t.Fatalf("second backfill: %v %v", added, err)
	}
	m, err := s.GetCatalogManifest(ctx, "new-api")
	if err != nil || m.Manifest != "id: new-api\n# old\n" || m.Source != ManifestFromBackfill || m.CatalogID != "new-api" || m.CatalogVersion != "v1" || m.SavedAt.IsZero() {
		t.Fatalf("get: %+v %v", m, err)
	}
	// An install replaces it; a backfill after that changes nothing.
	inst := testManifest("new-api", "id: new-api\n# installed\n", ManifestFromInstall)
	inst.CatalogOrigin = "cat-demo"
	if err := s.SetCatalogManifest(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if added, _ := s.AddCatalogManifest(ctx, testManifest("new-api", "id: new-api\n# late\n", ManifestFromBackfill)); added {
		t.Fatal("backfill replaced an installed manifest")
	}
	m, _ = s.GetCatalogManifest(ctx, "new-api")
	if m.Manifest != inst.Manifest || m.Source != ManifestFromInstall || m.CatalogOrigin != "cat-demo" || m.SHA256 != inst.SHA256 {
		t.Fatalf("after install: %+v", m)
	}
	if err := s.DeleteApp(ctx, "new-api", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCatalogManifest(ctx, "new-api"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

// Deleting an app with its data kept carries the manifest to the kept record
// (empty when the app had none); deleting with the data drops both.
func TestKeptCatalogSecretsCarryManifest(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	for _, name := range []string{"with", "without"} {
		if _, _, err := s.EnsureApp(ctx, name); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpdateApp(ctx, name, func(a *App) error { a.CatalogID = "new-api"; return nil }); err != nil {
			t.Fatal(err)
		}
		if err := s.SetCatalogSecrets(ctx, CatalogSecrets{App: name, ParamsEnc: "v1:x", FirstLoginEnc: "v1:y"}); err != nil {
			t.Fatal(err)
		}
	}
	text := "id: new-api\nschema: 2\n"
	if err := s.SetCatalogManifest(ctx, testManifest("with", text, ManifestFromInstall)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"with", "without"} {
		if err := s.DeleteApp(ctx, name, true); err != nil {
			t.Fatal(err)
		}
	}
	k, err := s.GetKeptCatalogSecrets(ctx, "with")
	if err != nil || k.Manifest != text || k.ManifestSHA256 != ManifestSHA256(text) || k.ParamsEnc != "v1:x" {
		t.Fatalf("kept with manifest: %+v %v", k, err)
	}
	k, err = s.GetKeptCatalogSecrets(ctx, "without")
	if err != nil || k.Manifest != "" || k.ManifestSHA256 != "" || k.ParamsEnc != "v1:x" {
		t.Fatalf("kept without manifest: %+v %v", k, err)
	}
	// The same name deleted again, this time with its data: nothing is kept.
	if _, _, err := s.EnsureApp(ctx, "with"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteApp(ctx, "with", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetKeptCatalogSecrets(ctx, "with"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("kept after delete with data: %v", err)
	}
}

// A database from before migration 0013 (an app deleted with its data kept,
// no manifests table) opens with this version: the kept row survives with an
// empty manifest and manifests can be saved.
func TestMigration0013OnExistingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acornfox.db")
	s, err := Open(Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Put the schema back to what 0012 left, with a row in it.
	for _, q := range []string{
		`DROP TABLE app_catalog_manifests`,
		`ALTER TABLE kept_catalog_secrets DROP COLUMN manifest`,
		`ALTER TABLE kept_catalog_secrets DROP COLUMN manifest_sha256`,
		`DELETE FROM _schema_migrations WHERE version='0013_app_catalog_manifests'`,
		`INSERT INTO kept_catalog_secrets(app, catalog_id, catalog_origin, params_enc, kept_at) VALUES ('old-app', 'new-api', '', 'v1:x', '2026-01-02T03:04:05.000000000Z')`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s.Close()

	s, err = Open(Config{Path: path})
	if err != nil {
		t.Fatalf("open a 0012 database: %v", err)
	}
	defer s.Close()
	k, err := s.GetKeptCatalogSecrets(ctx, "old-app")
	if err != nil || k.ParamsEnc != "v1:x" || k.Manifest != "" || k.ManifestSHA256 != "" {
		t.Fatalf("kept row after migration: %+v %v", k, err)
	}
	if _, _, err := s.EnsureApp(ctx, "new-api"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCatalogManifest(ctx, testManifest("new-api", "id: new-api\n", ManifestFromInstall)); err != nil {
		t.Fatal(err)
	}
}
