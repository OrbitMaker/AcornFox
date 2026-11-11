package appinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
)

// Hand-made catalogs for tests. `catalog build` refuses a manifest its own
// schema does not know, so a catalog "from a newer AcornFox" (unknown fields,
// a min_platform the index and the manifest disagree on, recipes under
// recipes_v2) cannot be built with it: these helpers pack the bundle
// directly, the way a publish looks to a client.

// fixApp is one app of a hand-made catalog: its manifest.yaml text and,
// when not "", the min_platform the index claims instead of the manifest's.
type fixApp struct {
	manifest string
	indexMin string
	// noPackage leaves the package out of the published files: any attempt
	// to read it fails, which shows a check ran before the download.
	noPackage bool
	// indexFrom is the manifest the index entry is made from when the
	// package's own manifest cannot be decoded by this program (a field that
	// changed type). Empty: the package's manifest.
	indexFrom string
}

// publishedManifest reads a manifest of the published catalog (serial 3).
func publishedManifest(t testing.TB, id string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "catalog", "testdata", "published-3", "src", "apps", id, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// withMinPlatform rewrites the manifest's lifecycle.min_platform.
func withMinPlatform(t testing.TB, manifest, min string) string {
	t.Helper()
	const old = `min_platform: "0.5.0"`
	if !strings.Contains(manifest, old) {
		t.Fatal("manifest has no min_platform 0.5.0 to rewrite")
	}
	return strings.Replace(manifest, old, `min_platform: "`+min+`"`, 1)
}

// withUnknownField adds a top-level field no schema of this program has.
func withUnknownField(manifest string) string {
	return manifest + "\n# a field from a later format\nfuture_feature:\n  mode: fast\n"
}

func packOne(t testing.TB, id string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, name := range []string{"icon.svg", "manifest.yaml"} {
		data, ok := files[name]
		if !ok {
			continue
		}
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: id + "/" + name, Mode: 0o644, Size: int64(len(data)),
			ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fixtureBundle packs apps and recipes into an unsigned bundle. catalogID is
// the catalog the index is bound to ("" none). recipes go under "recipes",
// recipesV2 under "recipes_v2".
func fixtureBundle(t testing.TB, serial int64, catalogID string, apps []fixApp, recipes, recipesV2 []catalog.Recipe) *catalog.Bundle {
	t.Helper()
	idx := catalog.Index{Schema: catalog.IndexSchema, Serial: serial, Catalog: catalogID, Apps: []catalog.IndexApp{}, Recipes: []catalog.Recipe{}, RecipesV2: recipesV2}
	if recipes != nil {
		idx.Recipes = recipes
	}
	b := &catalog.Bundle{Serial: serial, Packages: map[string][]byte{}}
	for _, fa := range apps {
		from := fa.manifest
		if fa.indexFrom != "" {
			from = fa.indexFrom
		}
		m, err := catalog.ParseManifest([]byte(from))
		if err != nil {
			t.Fatal(err)
		}
		pkg := packOne(t, m.ID, map[string]string{"manifest.yaml": fa.manifest, "icon.svg": `<svg xmlns="http://www.w3.org/2000/svg"/>`})
		sum := sha256.Sum256(pkg)
		path := "apps/" + m.ID + "/" + m.Release.Version + ".tar.gz"
		min := m.Lifecycle.MinPlatform
		if fa.indexMin != "" {
			min = fa.indexMin
		}
		provides := []string{}
		for _, p := range m.Provides {
			provides = append(provides, p.Kind)
		}
		idx.Apps = append(idx.Apps, catalog.IndexApp{
			ID: m.ID, Version: m.Release.Version, Name: m.App.Name, Summary: m.App.Summary, Category: m.App.Category,
			Keywords: m.App.Keywords, Tags: m.App.Tags, MinPlatform: min, Status: m.Lifecycle.Status, MemoryMB: m.Resources.MemoryMB,
			Platforms: m.Resources.Platforms, Delivery: m.Delivery, Uses: m.Uses, Provides: provides,
			Package: catalog.PackageRef{Path: path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(pkg))},
		})
		if !fa.noPackage {
			b.Packages[path] = pkg
		}
	}
	raw, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b.Index = append(raw, '\n')
	if _, err := catalog.ParseIndex(b.Index); err != nil {
		t.Fatalf("the hand-made index does not parse: %v", err)
	}
	return b
}

// bundleFS serves a bundle as a built-in snapshot (trusted unsigned).
func bundleFS(b *catalog.Bundle) fs.FS {
	m := fstest.MapFS{
		catalog.IndexFile:   {Data: b.Index},
		catalog.VersionFile: {Data: []byte(strconv.FormatInt(b.Serial, 10) + "\n")},
	}
	for p, data := range b.Packages {
		m[p] = &fstest.MapFile{Data: data}
	}
	return m
}

// setupSnapshot is setup() with the catalog taken from snap. The catalog's
// only source is a local server that counts requests and serves nothing, so
// a test can assert that nothing was asked for over the network.
func setupSnapshot(t *testing.T, snap fs.FS, mod func(*Config)) (*env, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(src.Close)
	v := setupWith(t, func(c *Config) {
		c.Catalog = NewCatalog(CatalogConfig{CacheDir: filepath.Join(t.TempDir(), "catalog"), Snapshot: snap, Sources: []string{src.URL}})
		if mod != nil {
			mod(c)
		}
	})
	return v, &hits
}

// nothingCreated fails the test when an install left anything behind: an
// app record, a volume, a pulled or loaded image, a written file.
func nothingCreated(t *testing.T, v *env) {
	t.Helper()
	apps, err := v.w.st.ListApps(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	v.w.mu.Lock()
	defer v.w.mu.Unlock()
	vols := 0
	for _, l := range v.w.volumes {
		vols += len(l)
	}
	if len(apps) != 0 || vols != 0 || len(v.w.pulled) != 0 || len(v.w.loads) != 0 || len(v.w.wrote) != 0 {
		t.Fatalf("a refused install left something: apps %d, volumes %d, pulled %v, loads %d, files %d", len(apps), vols, v.w.pulled, len(v.w.loads), len(v.w.wrote))
	}
}

// TestWriteAcceptanceCatalog is not a test of the engine: it writes a signed
// catalog for trying a real server by hand against the cases `catalog build`
// cannot produce. It is skipped unless both variables are set:
//
//	ACORNFOX_FIXTURE_OUT     directory to write the publish files to (new or
//	                         a previous output of this same test)
//	ACORNFOX_FIXTURE_KEY     signing key made by `acornfox catalog keygen`
//	ACORNFOX_FIXTURE_SERIAL  serial of the index (default 100; a client only
//	                         takes a serial above the one it has)
//
// What the catalog holds (index bound to "public"):
//
//	new-api         the published manifest: installs
//	hermes-agent    the published manifest but min_platform 9.9.9: needs_platform, CAT-INST-08 before any download
//	needs-next      a copy of new-api with min_platform 0.5.1 (what an older AcornFox meets first)
//	odd-app         a copy of new-api with a field no schema has, min_platform 0.5.0: CAT-INST-29, catalog at fault
//	odd-newer       the same field, the manifest says 9.9.9 but the index 0.5.0: CAT-INST-29 with needs_platform
//	recipes         "new-api" (installs)
//	recipes_v2      "future-bundle" (new-api, min_platform 9.9.9): unseen by the first release, marked by later ones
//
// Serve the directory over HTTP and point a server built with the
// acornfox_testkeys tag at it (ACORNFOX_CATALOG_SOURCES, and the key's public
// half in ACORNFOX_TEST_CATALOG_PUBKEYS).
func TestWriteAcceptanceCatalog(t *testing.T) {
	out, keyFile := os.Getenv("ACORNFOX_FIXTURE_OUT"), os.Getenv("ACORNFOX_FIXTURE_KEY")
	if out == "" || keyFile == "" {
		t.Skip("set ACORNFOX_FIXTURE_OUT and ACORNFOX_FIXTURE_KEY to write the acceptance catalog")
	}
	serial := int64(100)
	if s := os.Getenv("ACORNFOX_FIXTURE_SERIAL"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 1 {
			t.Fatalf("ACORNFOX_FIXTURE_SERIAL %q", s)
		}
		serial = n
	}
	key, err := catalog.LoadSigningKey(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	newAPI, hermes := publishedManifest(t, "new-api"), publishedManifest(t, "hermes-agent")
	as := func(id, manifest string) string {
		if !strings.Contains(manifest, "id: new-api\n") {
			t.Fatal("manifest has no id line to rewrite")
		}
		return strings.Replace(manifest, "id: new-api\n", "id: "+id+"\n", 1)
	}
	recipe := func(id, min string) catalog.Recipe {
		return catalog.Recipe{ID: id, Schema: 1, Title: id, Summary: "验收用配方 " + id, Apps: []catalog.RecipeApp{{App: "new-api"}},
			Delivery: catalog.Delivery{Tier: "auto"}, MinPlatform: min}
	}
	b := fixtureBundle(t, serial, catalog.PublicCatalog, []fixApp{
		{manifest: newAPI},
		{manifest: withMinPlatform(t, hermes, "9.9.9")},
		{manifest: as("needs-next", withMinPlatform(t, newAPI, "0.5.1"))},
		{manifest: as("odd-app", withUnknownField(newAPI))},
		{manifest: as("odd-newer", withUnknownField(withMinPlatform(t, newAPI, "9.9.9"))), indexMin: "0.5.0"},
	}, []catalog.Recipe{recipe("new-api", "")}, []catalog.Recipe{recipe("future-bundle", "9.9.9")})
	b.Sign(key)
	files, err := b.Write(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		t.Logf("wrote %s (%d bytes)", filepath.Join(out, f.Path), f.Size)
	}
}
