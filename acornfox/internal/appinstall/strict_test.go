package appinstall

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

func faultOf(t *testing.T, err error) *Fault {
	t.Helper()
	var f *Fault
	if !errors.As(err, &f) {
		t.Fatalf("not a fault: %v", err)
	}
	return f
}

// An app whose index entry asks for a newer platform is refused with
// CAT-INST-08 and the version to upgrade to, before its package is even
// looked for; the list and the detail say so too.
func TestTooOldRefusedBeforeDownload(t *testing.T) {
	future := withMinPlatform(t, publishedManifest(t, "new-api"), "9.9.9")
	b := fixtureBundle(t, 5, "", []fixApp{
		{manifest: future, noPackage: true}, // the package cannot be had at all
		{manifest: publishedManifest(t, "hermes-agent")},
	}, nil, nil)
	v, hits := setupSnapshot(t, bundleFS(b), nil)
	ctx := context.Background()

	_, err := v.e.StartInstall(ctx, Request{App: "new-api"})
	f := faultOf(t, err)
	if f.Code != CodeTooOld || f.NeedsPlatform != "9.9.9" || !strings.Contains(f.Message, "请先升级") || f.HTTPStatus() != 409 {
		t.Fatalf("fault %+v", f)
	}
	if hits.Load() != 0 {
		t.Fatalf("the package was asked for %d times before the version check", hits.Load())
	}
	nothingCreated(t, v)
	if ts, _ := v.e.Tasks(ctx, 10); len(ts) != 0 {
		t.Fatalf("a task was recorded: %+v", ts)
	}

	l, err := v.e.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	needs := map[string]string{}
	for _, a := range l.Apps {
		needs[a.ID] = a.NeedsPlatform
	}
	if needs["new-api"] != "9.9.9" || needs["hermes-agent"] != "" {
		t.Fatalf("list needs_platform %v", needs)
	}
	// The detail cannot read the package either: it says "upgrade first"
	// rather than "the package is broken".
	_, err = v.e.Detail(ctx, "new-api")
	if f := faultOf(t, err); f.Code != CodeTooOld || f.NeedsPlatform != "9.9.9" {
		t.Fatalf("detail fault %+v", f)
	}
	if d, err := v.e.Detail(ctx, "hermes-agent"); err != nil || d.NeedsPlatform != "" {
		t.Fatalf("detail of an installable app: %q %v", d.NeedsPlatform, err)
	}

	// With the package here the detail opens and carries the field.
	b2 := fixtureBundle(t, 5, "", []fixApp{{manifest: future}}, nil, nil)
	v2, _ := setupSnapshot(t, bundleFS(b2), nil)
	d, err := v2.e.Detail(ctx, "new-api")
	if err != nil || d.NeedsPlatform != "9.9.9" {
		t.Fatalf("detail %q %v", d.NeedsPlatform, err)
	}
	// A newer AcornFox installs it.
	v3, _ := setupSnapshot(t, bundleFS(b2), func(c *Config) { c.Version = "9.9.9" })
	if tv := v3.install(t, Request{App: "new-api"}); tv.Status != state.TaskSucceeded {
		t.Fatalf("9.9.9 could not install it: %+v", tv.Fault)
	}
	if l, _ := v3.e.List(ctx); l.Apps[0].NeedsPlatform != "" {
		t.Fatalf("needs_platform on a new enough server: %q", l.Apps[0].NeedsPlatform)
	}
}

// A manifest with something this program does not know is refused with
// CAT-INST-29 before anything is created or pulled. When the manifest asks
// for a newer platform the fault says which (upgrade first); when it does
// not, the catalog is at fault and no version is named.
func TestUnknownManifestRefused(t *testing.T) {
	base := publishedManifest(t, "new-api")
	retyped := strings.Replace(base, "port: 3000", "port: { number: 3000 }", 1)
	if _, err := catalog.ParseManifest([]byte(retyped)); err == nil || retyped == base {
		t.Fatal("the retyped manifest must be one this program cannot decode")
	}
	cases := []struct {
		name     string
		app      fixApp
		code     string
		needs    string
		contains string
	}{
		{"unknown field, catalog at fault", fixApp{manifest: withUnknownField(base)}, CodeBadManifest, "", "这份目录有问题"},
		{"unknown value", fixApp{manifest: strings.Replace(base, "protocol: http", "protocol: quic", 1)}, CodeBadManifest, "", "这份目录有问题"},
		{"unknown format version", fixApp{manifest: strings.Replace(base, "schema: 2", "schema: 3", 1)}, CodeBadManifest, "", "这份目录有问题"},
		// The index says 0.5.0 but the manifest itself asks for more: the
		// version check on the index passes, the manifest check names it.
		{"unknown field, newer platform", fixApp{manifest: withUnknownField(withMinPlatform(t, base, "9.9.9")), indexMin: "0.5.0"}, CodeBadManifest, "9.9.9", "请先升级"},
		// A field this program knows, holding another type than it expects
		// (what a later format might do): the manifest cannot even be
		// decoded here. It is still CAT-INST-29, not "broken package".
		{"known field with a new type, catalog at fault", fixApp{manifest: retyped, indexFrom: base}, CodeBadManifest, "", "这份目录有问题"},
		{"known field with a new type, newer platform", fixApp{manifest: withMinPlatform(t, retyped, "9.9.9"), indexFrom: base, indexMin: "0.5.0"}, CodeBadManifest, "9.9.9", "请先升级"},
		// Nothing unknown, but the manifest asks for more than the index.
		{"manifest min above the index", fixApp{manifest: withMinPlatform(t, base, "9.9.9"), indexMin: "0.5.0"}, CodeTooOld, "9.9.9", "请先升级"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, _ := setupSnapshot(t, bundleFS(fixtureBundle(t, 5, "", []fixApp{c.app}, nil, nil)), nil)
			_, err := v.e.StartInstall(context.Background(), Request{App: "new-api"})
			f := faultOf(t, err)
			if f.Code != c.code || f.NeedsPlatform != c.needs || !strings.Contains(f.Message, c.contains) || f.Hint == "" {
				t.Fatalf("fault %+v", f)
			}
			if c.needs == "" && strings.Contains(f.Message, "升级") {
				t.Fatalf("a broken catalog must not tell the user to upgrade: %s", f.Message)
			}
			// The hint differs with the case, so the two can be told apart
			// without reading needs_platform.
			switch {
			case c.code == CodeBadManifest && c.needs != "":
				if !strings.Contains(f.Hint, "先升级到 "+c.needs) || strings.Contains(f.Hint, "反馈") {
					t.Fatalf("hint of the upgrade case: %s", f.Hint)
				}
			case c.code == CodeBadManifest:
				if !strings.Contains(f.Hint, "目录有问题") || !strings.Contains(f.Hint, "反馈") || strings.Contains(f.Hint, "先升级到") {
					t.Fatalf("hint of the broken-catalog case: %s", f.Hint)
				}
			}
			nothingCreated(t, v)
			if ts, _ := v.e.Tasks(context.Background(), 10); len(ts) != 0 {
				t.Fatalf("a task was recorded: %+v", ts)
			}
		})
	}
	// The untouched manifest still installs from a hand-made catalog.
	v, _ := setupSnapshot(t, bundleFS(fixtureBundle(t, 5, "", []fixApp{{manifest: base}}, nil, nil)), nil)
	if tv := v.install(t, Request{App: "new-api"}); tv.Status != state.TaskSucceeded {
		t.Fatalf("published manifest refused: %+v", tv.Fault)
	}
}

// checkManifest reports "upgrade first" before "this version cannot do X".
func TestTooOldComesBeforeUnsupported(t *testing.T) {
	e := &Engine{cfg: Config{Version: "0.5.0", Arch: "amd64"}}
	m := &catalog.Manifest{ID: "x", Lifecycle: catalog.Lifecycle{MinPlatform: "0.7.0"}, Resources: catalog.Resources{Platforms: []string{"linux/s390x"}},
		Release: catalog.Release{Images: []catalog.Image{{Name: "app"}, {Name: "db"}}},
		Env:     []catalog.EnvVar{{Key: "HF_ENDPOINT", From: "platform.mirror.hf"}}}
	f := faultOf(t, e.checkManifest(&App{Manifest: m}))
	if f.Code != CodeTooOld || f.NeedsPlatform != "0.7.0" {
		t.Fatalf("fault %+v", f)
	}
	m.Lifecycle.MinPlatform = "0.5.0"
	if f := faultOf(t, e.checkManifest(&App{Manifest: m})); f.Code != CodeUnsupported || f.NeedsPlatform != "" {
		t.Fatalf("fault %+v", f)
	}
}

func TestNeedsPlatform(t *testing.T) {
	e := &Engine{cfg: Config{Version: "0.5.1-dev.3"}}
	for _, c := range []struct {
		mins []string
		want string
	}{
		{nil, ""}, {[]string{""}, ""}, {[]string{"0.5.0"}, ""}, {[]string{"0.5.1"}, ""}, {[]string{"0.5.2"}, "0.5.2"},
		{[]string{"0.5.0", "v0.6.0", "0.5.9"}, "0.6.0"}, {[]string{"soon", "0.5.0"}, ""}, {[]string{"soon"}, ""},
	} {
		if got := e.needsPlatform(c.mins...); got != c.want {
			t.Errorf("needsPlatform(%v) = %q, want %q", c.mins, got, c.want)
		}
	}
	// A build without a readable version can install nothing from a catalog.
	if got := (&Engine{}).needsPlatform("0.5.0"); got != "0.5.0" {
		t.Errorf("unreadable own version: %q", got)
	}
}

// Recipes: one listed under recipes_v2 is seen, marked and refused by its own
// min_platform; one whose member app needs more is marked by that app's. A
// recipe is checked whole before anything is created: a bad second app
// leaves the first uninstalled.
func TestRecipesNeedPlatform(t *testing.T) {
	newAPI, hermes := publishedManifest(t, "new-api"), publishedManifest(t, "hermes-agent")
	recipe := func(id string, apps ...string) catalog.Recipe {
		r := catalog.Recipe{ID: id, Schema: 1, Title: id, Summary: id, Delivery: catalog.Delivery{Tier: "auto"}}
		for _, a := range apps {
			r.Apps = append(r.Apps, catalog.RecipeApp{App: a})
		}
		return r
	}
	future := recipe("future-bundle", "new-api")
	future.MinPlatform = "9.9.9"
	b := fixtureBundle(t, 6, "", []fixApp{{manifest: newAPI}, {manifest: withMinPlatform(t, hermes, "0.8.0")}},
		[]catalog.Recipe{recipe("plain", "new-api"), recipe("with-newer-app", "new-api", "hermes-agent")},
		[]catalog.Recipe{future})
	v, _ := setupSnapshot(t, bundleFS(b), nil)
	ctx := context.Background()

	l, err := v.e.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	needs := map[string]string{}
	for _, r := range l.Recipes {
		needs[r.ID] = r.NeedsPlatform
	}
	if len(l.Recipes) != 3 || needs["plain"] != "" || needs["with-newer-app"] != "0.8.0" || needs["future-bundle"] != "9.9.9" {
		t.Fatalf("recipes %v", needs)
	}
	if src := v.e.Catalog().Sources(); src[0].Recipes != 3 {
		t.Fatalf("sources count %d recipes, want both lists", src[0].Recipes)
	}

	_, err = v.e.StartInstall(ctx, Request{Recipe: "future-bundle"})
	if f := faultOf(t, err); f.Code != CodeTooOld || f.NeedsPlatform != "9.9.9" || !strings.Contains(f.Message, "配方 future-bundle") {
		t.Fatalf("future recipe: %+v", f)
	}
	// new-api (first) is fine, hermes-agent (second) needs 0.8.0: nothing of
	// the recipe is installed.
	_, err = v.e.StartInstall(ctx, Request{Recipe: "with-newer-app"})
	if f := faultOf(t, err); f.Code != CodeTooOld || f.NeedsPlatform != "0.8.0" {
		t.Fatalf("recipe with a newer app: %+v", f)
	}
	nothingCreated(t, v)
	if tv := v.install(t, Request{Recipe: "plain"}); tv.Status != state.TaskSucceeded {
		t.Fatalf("plain recipe: %+v", tv.Fault)
	}

	// Same for a manifest this program does not know in second place.
	b2 := fixtureBundle(t, 6, "", []fixApp{{manifest: newAPI}, {manifest: withUnknownField(hermes)}},
		[]catalog.Recipe{recipe("pair", "new-api", "hermes-agent")}, nil)
	v2, _ := setupSnapshot(t, bundleFS(b2), nil)
	_, err = v2.e.StartInstall(ctx, Request{Recipe: "pair"})
	if f := faultOf(t, err); f.Code != CodeBadManifest || f.NeedsPlatform != "" {
		t.Fatalf("recipe with an unknown manifest: %+v", f)
	}
	nothingCreated(t, v2)
}

// A private catalog's manifest goes through the same checks.
func TestPrivateCatalogManifestChecked(t *testing.T) {
	v, g, priv, _ := privateSetup(t)
	ctx := context.Background()
	b := fixtureBundle(t, 2, "cat-demo", []fixApp{
		{manifest: withUnknownField(publishedManifest(t, "new-api"))},
		{manifest: withMinPlatform(t, publishedManifest(t, "hermes-agent"), "9.9.9")},
	}, nil, nil)
	b.Sign(priv)
	idx, _ := catalog.ParseIndex(b.Index)
	g.mu.Lock()
	c := &fakeCat{name: "演示", status: "active", serial: 2, index: string(b.Index), sig: string(b.Sig), files: map[string]bool{}}
	for _, a := range idx.Apps {
		fid := catalog.PrivatePackageFileID(a.ID, a.Version)
		c.files[fid], g.private[fid] = true, true
		g.blobs[fid] = b.Packages[a.Package.Path]
	}
	g.cats["cat-demo"] = c
	g.mu.Unlock()
	if _, err := v.e.Catalog().Connect(ctx, g.makeCode("cat-demo", "AAAABBBBCCCCDDDD")); err != nil {
		t.Fatal(err)
	}
	_, err := v.e.StartInstall(ctx, Request{App: "new-api", Catalog: "cat-demo"})
	if f := faultOf(t, err); f.Code != CodeBadManifest || f.NeedsPlatform != "" {
		t.Fatalf("private app with an unknown field: %+v", f)
	}
	issues := len(g.issueReq)
	_, err = v.e.StartInstall(ctx, Request{App: "hermes-agent", Catalog: "cat-demo"})
	if f := faultOf(t, err); f.Code != CodeTooOld || f.NeedsPlatform != "9.9.9" {
		t.Fatalf("private app for a newer platform: %+v", f)
	}
	if len(g.issueReq) != issues {
		t.Fatal("the gateway was asked for the package of an app this version cannot install")
	}
	nothingCreated(t, v)
	l, err := v.e.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range l.Apps {
		if a.Source.ID == "cat-demo" && a.ID == "hermes-agent" && a.NeedsPlatform != "9.9.9" {
			t.Fatalf("private list entry %+v", a)
		}
	}
}
