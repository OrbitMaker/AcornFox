package catalog

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// testShow is a show.json for serial: two apps, a solution, two industries.
func testShow(serial int64) Show {
	return Show{
		Kind: ShowKind, Schema: ShowSchema, Serial: serial,
		Industries: []ShowIndustry{{ID: "office", Name: "通用办公"}, {ID: "retail", Name: "电商零售"}},
		Apps: []ShowApp{
			{ID: "hermes-agent", Name: "Hermes 智能体", Tagline: "一个会聊天、会办事的助手", Flag: "new", Color: "#3b5bdb",
				Cover: CoverPrefix + strings.Repeat("a", 64) + ".png", Doc: "# Hermes\n\n能做什么……", Order: 10, Keywords: []string{"智能体", "聊天"}},
			{ID: "new-api"},
		},
		Solutions: []ShowSolution{
			{ID: "hermes-with-new-api", Name: "一套能聊天的助手", Tagline: "两个应用接好", Industries: []string{"office"}, Color: "#0ca678", Doc: "说明", Order: 1},
		},
		Home:   ShowHome{Solutions: []string{"hermes-with-new-api"}},
		Assist: ShowAssist{Visible: true, PriceCNY: 199},
	}
}

func encodeShow(t testing.TB, s Show) []byte {
	t.Helper()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// publishShow writes show.json and show.json.sig (signed with priv) into a
// published directory, as the operations system's publish does.
func publishShow(t testing.TB, dir string, raw []byte, priv ed25519.PrivateKey) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ShowFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ShowSigFile), SignShow(raw, priv), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestShowGoodAndCleaned(t *testing.T) {
	s := testShow(5)
	s.Apps[0].Cover = "https://evil.example/x.png"
	s.Apps[0].Flag = "super"
	s.Apps[0].Color = "blue"
	s.Solutions[0].Industries = []string{"office", "ghost"}
	s.Home.Solutions = []string{"hermes-with-new-api", "ghost"}
	s.Apps[1].Keywords = nil
	raw := encodeShow(t, s)
	got, notes, err := ShowNotes(raw)
	if err != nil {
		t.Fatal(err)
	}
	a := got.App("hermes-agent")
	if a == nil || a.Cover != "" || a.Flag != "" || a.Color != "" || a.Name != "Hermes 智能体" || a.Order != 10 || len(a.Keywords) != 2 {
		t.Fatalf("app %+v", a)
	}
	r := got.Solution("hermes-with-new-api")
	if r == nil || len(r.Industries) != 1 || r.Industries[0] != "office" || r.Color != "#0ca678" {
		t.Fatalf("solution %+v", r)
	}
	if len(got.Home.Solutions) != 1 || got.Home.Solutions[0] != "hermes-with-new-api" {
		t.Fatalf("home %+v", got.Home)
	}
	if got.Apps[1].Keywords == nil {
		t.Fatal("缺的关键词列表要变成空列表，输出里才有 []")
	}
	if len(notes) != 5 { // cover, flag, color, industry, home
		t.Fatalf("notes %q", notes)
	}
	if got.App("nope") != nil || got.Solution("nope") != nil || (*Show)(nil).App("x") != nil {
		t.Fatal("没有的编号要给 nil")
	}
	// A valid cover and every field survive.
	ok, err := ParseShow(encodeShow(t, testShow(5)))
	if err != nil || ok.App("hermes-agent").Cover != CoverPrefix+strings.Repeat("a", 64)+".png" || ok.App("hermes-agent").Flag != "new" || !ok.Assist.Visible || ok.Assist.PriceCNY != 199 {
		t.Fatalf("%+v %v", ok, err)
	}
	// Minimal document: lists come out empty, not nil.
	min, err := ParseShow([]byte(`{"kind":"catalog-show","schema":1,"serial":2}`))
	if err != nil || min.Industries == nil || min.Apps == nil || min.Solutions == nil || min.Home.Solutions == nil {
		t.Fatalf("%+v %v", min, err)
	}
	// Unknown fields of a later minor change are ignored.
	if _, err := ParseShow([]byte(`{"kind":"catalog-show","schema":1,"serial":2,"future":{"a":1}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestValidCover(t *testing.T) {
	good := CoverPrefix + strings.Repeat("0", 64) + ".webp"
	for s, want := range map[string]bool{
		good:                  true,
		CoverPrefix + "a.png": true,
		"":                    false,
		"http://acornfox.com/catalog/assets/a.png":           false,
		"https://acornfox.com/catalog/assets/":               false,
		"https://acornfox.com/catalog/assets/../x.png":       false,
		"https://acornfox.com/catalog/assets/a/b.png":        false,
		"https://acornfox.com/catalog/assets/a.png?x=1":      false,
		"https://acornfox.com/catalog/assets/a.png#x":        false,
		"https://acornfox.com.evil.example/catalog/assets/a": false,
		"https://acornfox.com/catalog/assetsx/a.png":         false,
		"//acornfox.com/catalog/assets/a.png":                false,
		"javascript:alert(1)":                                false,
		CoverPrefix + "a b.png":                              false,
	} {
		if ValidCover(s) != want {
			t.Errorf("ValidCover(%q) = %v", s, !want)
		}
	}
}

func TestShowFormatErrors(t *testing.T) {
	long := strings.Repeat("字", MaxShowDocBytes/3+1) // 3 bytes each: over 64 KB
	exact := strings.Repeat("a", MaxShowDocBytes)
	bad := func(mod func(*Show)) []byte {
		s := testShow(5)
		mod(&s)
		return encodeShow(t, s)
	}
	cases := map[string][]byte{
		"not json":        []byte("<html>"),
		"wrong kind":      bad(func(s *Show) { s.Kind = "catalog-ops" }),
		"ops overlay":     []byte(`{"kind":"ops-overlay","schema":1,"serial":5}`),
		"schema 2":        bad(func(s *Show) { s.Schema = 2 }),
		"serial 0":        bad(func(s *Show) { s.Serial = 0 }),
		"dup app":         bad(func(s *Show) { s.Apps = append(s.Apps, s.Apps[0]) }),
		"empty app id":    bad(func(s *Show) { s.Apps[1].ID = "" }),
		"dup solution":    bad(func(s *Show) { s.Solutions = append(s.Solutions, s.Solutions[0]) }),
		"dup industry":    bad(func(s *Show) { s.Industries = append(s.Industries, s.Industries[0]) }),
		"long app doc":    bad(func(s *Show) { s.Apps[0].Doc = long }),
		"doc 64KB+1":      bad(func(s *Show) { s.Apps[0].Doc = exact + "a" }),
		"long recipe doc": bad(func(s *Show) { s.Solutions[0].Doc = long }),
	}
	for name, raw := range cases {
		if _, err := ParseShow(raw); CodeOf(err) != CodeShowFormat {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Exactly 64 KB is fine.
	if _, err := ParseShow(bad(func(s *Show) { s.Apps[0].Doc = exact })); err != nil {
		t.Fatalf("64 KB 整: %v", err)
	}
}

func TestReadShowSignatureAndSerial(t *testing.T) {
	pub, priv := testKey(t)
	_, otherPriv := testKey(t)
	keys := []ed25519.PublicKey{pub}
	raw := encodeShow(t, testShow(5))

	if s, err := ReadShow(raw, SignShow(raw, priv), 5, keys); err != nil || s.Serial != 5 {
		t.Fatalf("good: %v", err)
	}
	// Signature trailing newline (the signing tool writes one) is fine.
	if _, err := ReadShow(raw, append(SignShow(raw, priv), '\n'), 5, keys); err != nil {
		t.Fatalf("newline: %v", err)
	}
	if _, err := ReadShow(raw, SignShow(raw, otherPriv), 5, keys); CodeOf(err) != CodeShowSignature {
		t.Fatalf("other key: %v", err)
	}
	if _, err := ReadShow(raw, nil, 5, keys); CodeOf(err) != CodeShowSignature {
		t.Fatalf("no sig: %v", err)
	}
	if _, err := ReadShow(raw, []byte("not base64 !!"), 5, keys); CodeOf(err) != CodeShowSignature {
		t.Fatalf("garbage sig: %v", err)
	}
	if _, err := ReadShow(append(raw, ' '), SignShow(raw, priv), 5, keys); CodeOf(err) != CodeShowSignature {
		t.Fatalf("tampered: %v", err)
	}
	// A signature of the same bytes made the index way (no prefix) is not a
	// show signature: the two cannot be swapped.
	plain := []byte(b64.EncodeToString(ed25519.Sign(priv, raw)))
	if _, err := ReadShow(raw, plain, 5, keys); CodeOf(err) != CodeShowSignature {
		t.Fatalf("no prefix: %v", err)
	}
	// And a show signature does not verify as an index signature.
	if Verify(raw, SignShow(raw, priv), keys) == nil {
		t.Fatal("展示内容的签名不能当目录的签名用")
	}
	// Serial of another publish.
	if _, err := ReadShow(raw, SignShow(raw, priv), 6, keys); CodeOf(err) != CodeShowSerial {
		t.Fatalf("serial: %v", err)
	}
	// Signed but malformed: format, after the signature.
	junk := []byte(`{"kind":"catalog-show","schema":9,"serial":5}`)
	if _, err := ReadShow(junk, SignShow(junk, priv), 5, keys); CodeOf(err) != CodeShowFormat {
		t.Fatalf("schema: %v", err)
	}
	// Unsigned junk is a signature failure, never parsed.
	if _, err := ReadShow(junk, nil, 5, keys); CodeOf(err) != CodeShowSignature {
		t.Fatalf("unsigned junk: %v", err)
	}
	if _, err := ReadShow(raw, SignShow(raw, priv), 5, nil); CodeOf(err) != CodeShowSignature {
		t.Fatalf("no keys: %v", err)
	}
}

func syncShow(t *testing.T, src *source, cache string, pub ed25519.PublicKey) SyncResult {
	t.Helper()
	res, err := Sync(context.Background(), cfgFor(cache, pub, src))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return res
}

func TestSyncFetchesAndStoresShow(t *testing.T) {
	pub, priv := testKey(t)
	out := publish(t, copyTree(t), 5, priv)
	publishShow(t, out, encodeShow(t, testShow(5)), priv)
	src := newSource(t, out)
	cache := filepath.Join(t.TempDir(), "cache")

	res := syncShow(t, src, cache, pub)
	if !res.Updated || res.Show.State != ShowOK || res.Show.Serial != 5 {
		t.Fatalf("res %+v", res)
	}
	keys := []ed25519.PublicKey{pub}
	idx, origin, err := LoadWith(cache, nil, keys)
	if err != nil || origin != FromCache {
		t.Fatal(err)
	}
	show, st := LoadShow(cache, nil, keys, idx, origin)
	if show == nil || st.State != ShowOK || show.App("hermes-agent").Name != "Hermes 智能体" {
		t.Fatalf("load %v %+v", show, st)
	}
	// Same files, nothing fetched again once the cache has the show.
	hits := src.count(ShowFile)
	res = syncShow(t, src, cache, pub)
	if res.Updated || res.Show.State != ShowOK || src.count(ShowFile) != hits {
		t.Fatalf("second %+v show hits %d→%d", res, hits, src.count(ShowFile))
	}

	// The cache edited on disk: the show is not used (the index still is).
	p := filepath.Join(currentVersionDir(cache), ShowFile)
	b, _ := os.ReadFile(p)
	_ = os.WriteFile(p, []byte(strings.Replace(string(b), "Hermes 智能体", "被改过", 1)), 0o644)
	idx, origin, _ = LoadWith(cache, nil, keys)
	if show, st := LoadShow(cache, nil, keys, idx, origin); show != nil || st.State != ShowFailed || st.Code != CodeShowSignature {
		t.Fatalf("tampered cache: %v %+v", show, st)
	}
	// Signature file gone: also failed, never "ok".
	_ = os.Remove(filepath.Join(currentVersionDir(cache), ShowSigFile))
	if show, st := LoadShow(cache, nil, keys, idx, origin); show != nil || st.State != ShowFailed || st.Code != CodeShowSignature {
		t.Fatalf("no sig in cache: %v %+v", show, st)
	}
}

func TestSyncShowAbsent404(t *testing.T) {
	pub, priv := testKey(t)
	src := newSource(t, publish(t, copyTree(t), 3, priv)) // no show.json: the file server says 404
	cache := t.TempDir()
	res := syncShow(t, src, cache, pub)
	if !res.Updated || res.Show.State != ShowNone || res.Show.Code != "" {
		t.Fatalf("res %+v", res)
	}
	keys := []ed25519.PublicKey{pub}
	idx, origin, _ := LoadWith(cache, nil, keys)
	if show, st := LoadShow(cache, nil, keys, idx, origin); show != nil || st.State != ShowNone {
		t.Fatalf("load %v %+v", show, st)
	}
	if src.count(ShowSigFile) != 0 {
		t.Fatal("没有 show.json 就不该去取它的签名")
	}
}

func TestSyncShowProblemsDoNotStopTheCatalog(t *testing.T) {
	pub, priv := testKey(t)
	_, otherPriv := testKey(t)
	keys := []ed25519.PublicKey{pub}
	cases := []struct {
		name string
		prep func(dir string)
		code string
	}{
		{"signed by another key", func(d string) { publishShow(t, d, encodeShow(t, testShow(5)), otherPriv) }, CodeShowSignature},
		{"serial of another publish", func(d string) { publishShow(t, d, encodeShow(t, testShow(4)), priv) }, CodeShowSerial},
		{"bad format", func(d string) {
			s := testShow(5)
			s.Apps[0].Doc = strings.Repeat("a", MaxShowDocBytes+1)
			publishShow(t, d, encodeShow(t, s), priv)
		}, CodeShowFormat},
		{"signature missing", func(d string) {
			publishShow(t, d, encodeShow(t, testShow(5)), priv)
			_ = os.Remove(filepath.Join(d, ShowSigFile))
		}, CodeShowSignature},
	}
	for _, c := range cases {
		out := publish(t, copyTree(t), 5, priv)
		c.prep(out)
		src := newSource(t, out)
		cache := t.TempDir()
		res := syncShow(t, src, cache, pub)
		if !res.Updated || res.Serial != 5 || res.Show.State != ShowFailed || res.Show.Code != c.code {
			t.Errorf("%s: %+v", c.name, res)
			continue
		}
		idx, origin, err := LoadWith(cache, nil, keys)
		if err != nil || idx.Serial != 5 {
			t.Errorf("%s: 目录没同步成：%v", c.name, err)
			continue
		}
		if _, err := os.Stat(filepath.Join(currentVersionDir(cache), ShowFile)); !os.IsNotExist(err) {
			t.Errorf("%s: 验不过的展示内容落盘了", c.name)
		}
		if show, st := LoadShow(cache, nil, keys, idx, origin); show != nil || st.State != ShowNone {
			t.Errorf("%s: %v %+v", c.name, show, st)
		}
	}
}

func TestSyncShowFetchFailureThenBackfill(t *testing.T) {
	pub, priv := testKey(t)
	out := publish(t, copyTree(t), 5, priv)
	publishShow(t, out, encodeShow(t, testShow(5)), priv)
	src := newSource(t, out)
	src.pathStatus[ShowFile] = http.StatusInternalServerError
	cache := t.TempDir()

	res := syncShow(t, src, cache, pub)
	if !res.Updated || res.Show.State != ShowFailed || res.Show.Code != CodeShowFetch {
		t.Fatalf("first %+v", res)
	}
	// Same serial, the source works now: the cache version in use gets the
	// show without another index download.
	delete(src.pathStatus, ShowFile)
	idxHits := src.count(IndexFile)
	res = syncShow(t, src, cache, pub)
	if res.Updated || res.Show.State != ShowOK || res.Show.Serial != 5 || src.count(IndexFile) != idxHits {
		t.Fatalf("backfill %+v", res)
	}
	keys := []ed25519.PublicKey{pub}
	idx, origin, _ := LoadWith(cache, nil, keys)
	if show, st := LoadShow(cache, nil, keys, idx, origin); show == nil || st.State != ShowOK {
		t.Fatalf("after backfill %v %+v", show, st)
	}
	// A backfill that fails verification stores nothing.
	out2 := publish(t, copyTree(t), 6, priv)
	src2 := newSource(t, out2)
	cache2 := t.TempDir()
	syncShow(t, src2, cache2, pub)
	publishShow(t, out2, encodeShow(t, testShow(9)), priv) // wrong serial
	res = syncShow(t, src2, cache2, pub)
	if res.Show.State != ShowFailed || res.Show.Code != CodeShowSerial || hasCachedShow(cache2) {
		t.Fatalf("bad backfill %+v", res)
	}
}

func TestSyncNewIndexDropsOldShow(t *testing.T) {
	pub, priv := testKey(t)
	cache := t.TempDir()
	out5 := publish(t, copyTree(t), 5, priv)
	publishShow(t, out5, encodeShow(t, testShow(5)), priv)
	syncShow(t, newSource(t, out5), cache, pub)
	out6 := publish(t, copyTree(t), 6, priv) // no show
	res := syncShow(t, newSource(t, out6), cache, pub)
	if !res.Updated || res.Show.State != ShowNone {
		t.Fatalf("res %+v", res)
	}
	keys := []ed25519.PublicKey{pub}
	idx, origin, _ := LoadWith(cache, nil, keys)
	if show, st := LoadShow(cache, nil, keys, idx, origin); show != nil || st.State != ShowNone {
		t.Fatalf("旧序号的展示内容不能留给新目录用：%v %+v", show, st)
	}
}

func TestLoadShowFromSnapshot(t *testing.T) {
	keys := []ed25519.PublicKey{}
	idx := &Index{Schema: IndexSchema, Serial: 4}
	raw := encodeShow(t, testShow(4))
	snap := fstest.MapFS{ShowFile: {Data: raw}}
	if s, st := LoadShow("", snap, keys, idx, FromSnapshot); s == nil || st.State != ShowOK || st.Serial != 4 {
		t.Fatalf("snapshot show: %v %+v", s, st)
	}
	// Without one the program still runs: none.
	if s, st := LoadShow("", fstest.MapFS{}, keys, idx, FromSnapshot); s != nil || st.State != ShowNone {
		t.Fatalf("no show: %v %+v", s, st)
	}
	if s, st := LoadShow("", nil, keys, idx, FromSnapshot); s != nil || st.State != ShowNone {
		t.Fatalf("nil snapshot: %v %+v", s, st)
	}
	if s, st := LoadShow("", nil, keys, nil, FromSnapshot); s != nil || st.State != ShowNone {
		t.Fatalf("nil index: %v %+v", s, st)
	}
	// Another serial than the snapshot's index.
	if s, st := LoadShow("", fstest.MapFS{ShowFile: {Data: encodeShow(t, testShow(3))}}, keys, idx, FromSnapshot); s != nil || st.State != ShowFailed || st.Code != CodeShowSerial {
		t.Fatalf("serial: %v %+v", s, st)
	}
	if s, st := LoadShow("", fstest.MapFS{ShowFile: {Data: []byte("{")}}, keys, idx, FromSnapshot); s != nil || st.State != ShowFailed || st.Code != CodeShowFormat {
		t.Fatalf("junk: %v %+v", s, st)
	}
}

func TestVerifyDirChecksShow(t *testing.T) {
	pub, priv := testKey(t)
	_, otherPriv := testKey(t)
	keys := []ed25519.PublicKey{pub}
	out := publish(t, copyTree(t), 5, priv)
	if res, err := VerifyDir(out, keys); err != nil || res.Show != ShowNone {
		t.Fatalf("no show: %+v %v", res, err)
	}
	publishShow(t, out, encodeShow(t, testShow(5)), priv)
	if res, err := VerifyDir(out, keys); err != nil || res.Show != ShowOK {
		t.Fatalf("show: %+v %v", res, err)
	}
	publishShow(t, out, encodeShow(t, testShow(5)), otherPriv)
	if _, err := VerifyDir(out, keys); CodeOf(err) != CodeShowSignature {
		t.Fatalf("signed by another key: %v", err)
	}
	publishShow(t, out, encodeShow(t, testShow(4)), priv)
	if _, err := VerifyDir(out, keys); CodeOf(err) != CodeShowSerial {
		t.Fatalf("serial: %v", err)
	}
	publishShow(t, out, encodeShow(t, testShow(5)), priv)
	_ = os.Remove(filepath.Join(out, ShowSigFile))
	if _, err := VerifyDir(out, keys); CodeOf(err) != CodeShowSignature {
		t.Fatalf("no sig: %v", err)
	}
}

// The index parser still refuses a show.json (it carries a kind): the two
// documents cannot stand in for each other.
func TestShowIsNotAnIndex(t *testing.T) {
	if _, err := ParseIndex(encodeShow(t, testShow(5))); err == nil {
		t.Fatal("show.json 被当成了索引")
	}
}

func TestShowAssistURL(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"https://acornfox.com/assist", "https://acornfox.com/assist"},
		{"http://example.com", ""},
		{"", ""},
	} {
		b := []byte(`{"kind":"catalog-show","schema":1,"serial":3,"industries":[],"apps":[],"solutions":[],"home":{"solutions":[]},"assist":{"visible":true,"price_cny":1,"url":"` + c.in + `"}}`)
		s, err := ParseShow(b)
		if err != nil || s.Assist.URL != c.want {
			t.Fatalf("url %q: got %+v err %v", c.in, s, err)
		}
	}
}
