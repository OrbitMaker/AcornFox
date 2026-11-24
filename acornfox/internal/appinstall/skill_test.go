package appinstall

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/acornfox/acornfox/internal/catalog"
)

// Operating manuals in the app center's views and in Engine.Skill.

const (
	appManual = "# Hermes\n\n## 装之前\n先问用户有没有大模型的 Key。\n"
	recManual = "# 一套助手\n\n## 顺序\n先装 New API，再装 Hermes。\n"
)

func manualRef(kind, id, body string) *catalog.ShowSkill {
	sum := sha256.Sum256([]byte(body))
	return &catalog.ShowSkill{Path: catalog.SkillPath(kind, id), SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body)), Version: "2.0.1", Checked: "2026-11-24"}
}

func showWithManuals(serial int64) catalog.Show {
	s := showFor(serial)
	s.Apps[0].Skill = manualRef(catalog.SkillKindApp, "hermes-agent", appManual)
	s.Solutions[0].Skill = manualRef(catalog.SkillKindRecipe, "hermes-with-new-api", recManual)
	return s
}

func TestSkillInListDetailAndEngine(t *testing.T) {
	b := goodBundle(t)
	snap := bundleFS(b).(fstest.MapFS)
	raw, _ := json.Marshal(showWithManuals(b.Serial))
	snap[catalog.ShowFile] = &fstest.MapFile{Data: raw}
	snap[catalog.SkillPath(catalog.SkillKindApp, "hermes-agent")] = &fstest.MapFile{Data: []byte(appManual)}
	snap[catalog.SkillPath(catalog.SkillKindRecipe, "hermes-with-new-api")] = &fstest.MapFile{Data: []byte(recManual)}
	v, _ := setupSnapshot(t, snap, nil)
	ctx := context.Background()

	// The list says there is one (version, date), never the text.
	l, err := v.e.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range l.Apps {
		switch {
		case a.ID == "hermes-agent":
			if a.Show == nil || a.Show.Skill == nil || a.Show.Skill.Version != "2.0.1" || a.Show.Skill.Checked != "2026-11-24" {
				t.Fatalf("hermes show.skill %+v", a.Show)
			}
		case a.Show != nil && a.Show.Skill != nil:
			t.Errorf("%s 没有手册，不该有 skill", a.ID)
		}
	}
	for _, r := range l.Recipes {
		if r.ID == "hermes-with-new-api" && (r.Show == nil || r.Show.Skill == nil || r.Show.Skill.Checked != "2026-11-24") {
			t.Fatalf("recipe show.skill %+v", r.Show)
		}
	}
	if l.Sync.Skills == nil || l.Sync.Skills.State != catalog.SkillsOK || l.Sync.Skills.Count != 2 {
		t.Fatalf("sync.skills %+v", l.Sync.Skills)
	}
	out, _ := json.Marshal(l)
	if strings.Contains(string(out), "先问用户有没有大模型") || strings.Contains(string(out), `"sha256":"`+manualRef("app", "hermes-agent", appManual).SHA256) {
		t.Fatal("目录列表里不该有手册全文或它的 sha256")
	}
	d, err := v.e.Detail(ctx, "hermes-agent")
	if err != nil || d.Show == nil || d.Show.Skill == nil || d.Show.Skill.Version != "2.0.1" {
		t.Fatalf("detail %+v %v", d.Show, err)
	}

	// The full text: an app, a solution (recipe or solution as the kind).
	sk, err := v.e.Skill(ctx, "app", "hermes-agent")
	if err != nil || !sk.HasSkill || sk.Text != appManual || sk.Kind != "app" || sk.Version != "2.0.1" || sk.Checked != "2026-11-24" || sk.Code != "" {
		t.Fatalf("app skill %+v %v", sk, err)
	}
	for _, kind := range []string{"recipe", "solution"} {
		sk, err = v.e.Skill(ctx, kind, "hermes-with-new-api")
		if err != nil || !sk.HasSkill || sk.Text != recManual || sk.Kind != "recipe" {
			t.Fatalf("%s skill %+v %v", kind, sk, err)
		}
	}
	// An app that is in the catalog and has none: not an error.
	sk, err = v.e.Skill(ctx, "app", "new-api")
	if err != nil || sk.HasSkill || sk.Text != "" || sk.Note == "" || sk.Code != "" {
		t.Fatalf("no manual %+v %v", sk, err)
	}
	// Not in the catalog: CAT-INST-01.
	for _, c := range [][2]string{{"app", "nothing-here"}, {"recipe", "nothing-here"}, {"solution", "nothing-here"}} {
		if _, err := v.e.Skill(ctx, c[0], c[1]); AsFault(err).Code != CodeUnknown {
			t.Errorf("%v: %v", c, err)
		}
	}
	// A kind that is none: CAT-INST-02.
	if _, err := v.e.Skill(ctx, "apps", "hermes-agent"); AsFault(err).Code != CodeBadParams {
		t.Fatalf("bad kind: %v", err)
	}
}

func TestSkillListedButNotUsable(t *testing.T) {
	b := goodBundle(t)
	snap := bundleFS(b).(fstest.MapFS)
	raw, _ := json.Marshal(showWithManuals(b.Serial))
	snap[catalog.ShowFile] = &fstest.MapFile{Data: raw}
	// The app's file is not there; the solution's has other bytes.
	snap[catalog.SkillPath(catalog.SkillKindRecipe, "hermes-with-new-api")] = &fstest.MapFile{Data: []byte("别的内容")}
	v, _ := setupSnapshot(t, snap, nil)
	ctx := context.Background()

	l, err := v.e.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range l.Apps {
		if a.Show != nil && a.Show.Skill != nil {
			t.Errorf("%s 的手册用不了，列表里不该说有", a.ID)
		}
	}
	if l.Sync.Skills == nil || l.Sync.Skills.State != catalog.SkillsFailed || l.Sync.Skills.Count != 0 || len(l.Sync.Skills.Failed) != 2 {
		t.Fatalf("sync.skills %+v", l.Sync.Skills)
	}
	sk, err := v.e.Skill(ctx, "app", "hermes-agent")
	if err != nil || sk.HasSkill || sk.Code != catalog.CodeSkillFetch || !strings.Contains(sk.Note, "catalog sync") {
		t.Fatalf("missing: %+v %v", sk, err)
	}
	sk, err = v.e.Skill(ctx, "recipe", "hermes-with-new-api")
	if err != nil || sk.HasSkill || sk.Code != catalog.CodeSkillMismatch || sk.Text != "" {
		t.Fatalf("mismatch: %+v %v", sk, err)
	}
}

func TestSkillWithoutShow(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	l, err := v.e.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if l.Sync.Skills == nil || l.Sync.Skills.State != catalog.SkillsNone {
		t.Fatalf("sync.skills %+v", l.Sync.Skills)
	}
	sk, err := v.e.Skill(ctx, "app", "hermes-agent")
	if err != nil || sk.HasSkill || sk.Note == "" {
		t.Fatalf("%+v %v", sk, err)
	}
}

// skillSource is showSource with manuals: a signed catalog directory with
// show.json (listing two manuals) and the files under skills/.
func skillSource(t *testing.T, serial int64, priv ed25519.PrivateKey, appBody string) *httptest.Server {
	t.Helper()
	b, err := catalog.BuildFor(goodDir, serial, catalog.PublicCatalog)
	if err != nil {
		t.Fatal(err)
	}
	b.Sign(priv)
	out := t.TempDir()
	if _, err := b.Write(out); err != nil {
		t.Fatal(err)
	}
	s := showFor(serial)
	s.Apps[0].Skill = manualRef(catalog.SkillKindApp, "hermes-agent", appManual) // always the listed one
	s.Solutions[0].Skill = manualRef(catalog.SkillKindRecipe, "hermes-with-new-api", recManual)
	raw, _ := json.Marshal(s)
	_ = os.WriteFile(filepath.Join(out, catalog.ShowFile), raw, 0o644)
	_ = os.WriteFile(filepath.Join(out, catalog.ShowSigFile), catalog.SignShow(raw, priv), 0o644)
	_ = os.MkdirAll(filepath.Join(out, catalog.SkillDir), 0o755)
	_ = os.WriteFile(filepath.Join(out, filepath.FromSlash(catalog.SkillPath(catalog.SkillKindApp, "hermes-agent"))), []byte(appBody), 0o644)
	_ = os.WriteFile(filepath.Join(out, filepath.FromSlash(catalog.SkillPath(catalog.SkillKindRecipe, "hermes-with-new-api"))), []byte(recManual), 0o644)
	srv := httptest.NewServer(http.StripPrefix("/catalog", http.FileServer(http.Dir(out))))
	t.Cleanup(srv.Close)
	return srv
}

func TestSyncTakesSkillsAndReportsFailure(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	ctx := context.Background()
	newCatalog := func(srv *httptest.Server) *Catalog {
		return NewCatalog(CatalogConfig{CacheDir: t.TempDir(), Snapshot: fstest.MapFS{}, Keys: []ed25519.PublicKey{pub}, Sources: []string{srv.URL + "/catalog"}})
	}

	c := newCatalog(skillSource(t, 7, priv, appManual))
	st := c.Sync(ctx)
	if !st.OK || !st.Updated || st.Skills == nil || st.Skills.State != catalog.SkillsOK || st.Skills.Count != 2 {
		t.Fatalf("sync %+v skills %+v", st, st.Skills)
	}
	infos, now := c.Skills()
	if now.State != catalog.SkillsOK || len(infos) != 2 {
		t.Fatalf("skills %+v %+v", infos, now)
	}
	doc, err := c.Skill(catalog.SkillKindApp, "hermes-agent")
	if err != nil || doc == nil || doc.Text != appManual {
		t.Fatalf("skill %+v %v", doc, err)
	}
	// Offline from here on: it is read from the cache.
	if doc, err := NewCatalog(CatalogConfig{CacheDir: c.cfg.CacheDir, Snapshot: fstest.MapFS{}, Keys: []ed25519.PublicKey{pub}, Sources: []string{"http://127.0.0.1:1"}}).Skill(catalog.SkillKindRecipe, "hermes-with-new-api"); err != nil || doc == nil || doc.Text != recManual {
		t.Fatalf("offline %+v %v", doc, err)
	}

	// The file on the source differs from what the signed show lists: the
	// catalog and show are taken, the manual is not, and Skills() says why.
	c = newCatalog(skillSource(t, 7, priv, appManual+"被人改过\n"))
	st = c.Sync(ctx)
	if !st.OK || !st.Updated || st.Show == nil || st.Show.State != catalog.ShowOK || st.Skills == nil || st.Skills.State != catalog.SkillsFailed || st.Skills.Code != catalog.CodeSkillMismatch {
		t.Fatalf("mismatch sync %+v skills %+v", st, st.Skills)
	}
	if _, now := c.Skills(); now.State != catalog.SkillsFailed || now.Count != 1 || now.Code != catalog.CodeSkillMismatch {
		t.Fatalf("mismatch now %+v", now)
	}
	if doc, err := c.Skill(catalog.SkillKindApp, "hermes-agent"); doc != nil || catalog.CodeOf(err) == "" {
		t.Fatalf("mismatch manual used: %+v %v", doc, err)
	}
	// The failure survives a restart in the saved status.
	again := NewCatalog(CatalogConfig{CacheDir: c.cfg.CacheDir, Snapshot: fstest.MapFS{}, Keys: []ed25519.PublicKey{pub}})
	if s := again.Status(); s.Skills == nil || s.Skills.State != catalog.SkillsFailed || len(s.Skills.Failed) != 1 {
		t.Fatalf("saved status %+v", s.Skills)
	}
}
