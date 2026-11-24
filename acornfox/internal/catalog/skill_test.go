package catalog

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	hermesManual   = "# Hermes 操作手册\n\n## 装之前\n先问用户有没有大模型的 Key。\n"
	solutionManual = "# 一套能聊天的助手\n\n## 顺序\n先装 New API，再装 Hermes。\n"
)

// skillOf is the show.json entry for a manual body.
func skillOf(kind, id, body string) *ShowSkill {
	sum := sha256.Sum256([]byte(body))
	return &ShowSkill{Path: SkillPath(kind, id), SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body)), Version: "1.2.3", Checked: "2026-11-24"}
}

// showWithSkills is testShow with manuals on Hermes and on the solution.
func showWithSkills(serial int64) Show {
	s := testShow(serial)
	s.Apps[0].Skill = skillOf(SkillKindApp, "hermes-agent", hermesManual)
	s.Solutions[0].Skill = skillOf(SkillKindRecipe, "hermes-with-new-api", solutionManual)
	return s
}

// publishSkills writes the manuals into a published directory.
func publishSkills(t testing.TB, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, SkillDir), 0o755); err != nil {
		t.Fatal(err)
	}
	for p, body := range map[string]string{
		SkillPath(SkillKindApp, "hermes-agent"):           hermesManual,
		SkillPath(SkillKindRecipe, "hermes-with-new-api"): solutionManual,
	} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(p)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// skillRelease is a published directory (serial 5) with a signed show listing
// the two manuals and the manual files.
func skillRelease(t *testing.T, priv ed25519.PrivateKey) string {
	out := publish(t, copyTree(t), 5, priv)
	publishShow(t, out, encodeShow(t, showWithSkills(5)), priv)
	publishSkills(t, out)
	return out
}

func loadedShow(t *testing.T, cache string, pub ed25519.PublicKey) (*Show, string, ShowState) {
	t.Helper()
	keys := []ed25519.PublicKey{pub}
	idx, origin, err := LoadWith(cache, nil, keys)
	if err != nil {
		t.Fatal(err)
	}
	s, st := LoadShow(cache, nil, keys, idx, origin)
	return s, origin, st
}

func TestSkillParseKeepsGoodEntriesAndDropsBadOnes(t *testing.T) {
	s := showWithSkills(5)
	s.Apps[1].Skill = &ShowSkill{Path: "skills/app-other.md", SHA256: strings.Repeat("a", 64), Size: 10} // another app's path
	raw := encodeShow(t, s)
	got, notes, err := ShowNotes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if sk := got.SkillOf(SkillKindApp, "hermes-agent"); sk == nil || sk.Version != "1.2.3" || sk.Checked != "2026-11-24" {
		t.Fatalf("hermes %+v", sk)
	}
	if got.SkillOf(SkillKindRecipe, "hermes-with-new-api") == nil {
		t.Fatal("the solution's manual is gone")
	}
	if got.SkillOf(SkillKindApp, "new-api") != nil || len(notes) != 1 || !strings.Contains(notes[0], "new-api") {
		t.Fatalf("a manual with another app's path must be dropped with a note: %v", notes)
	}
	if len(got.SkillRefs()) != 2 || got.SkillRefs()[0].Kind != SkillKindApp || got.SkillRefs()[1].Kind != SkillKindRecipe {
		t.Fatalf("refs %+v", got.SkillRefs())
	}

	for name, sk := range map[string]*ShowSkill{
		"path traversal": {Path: "skills/app-../x.md", SHA256: strings.Repeat("a", 64), Size: 1},
		"upper sha":      {Path: SkillPath(SkillKindApp, "hermes-agent"), SHA256: strings.Repeat("A", 64), Size: 1},
		"short sha":      {Path: SkillPath(SkillKindApp, "hermes-agent"), SHA256: "abc", Size: 1},
		"zero size":      {Path: SkillPath(SkillKindApp, "hermes-agent"), SHA256: strings.Repeat("a", 64), Size: 0},
		"solution path":  {Path: SkillPath(SkillKindRecipe, "hermes-agent"), SHA256: strings.Repeat("a", 64), Size: 1},
	} {
		s := testShow(5)
		s.Apps[0].Skill = sk
		got, notes, err := ShowNotes(encodeShow(t, s))
		if err != nil || got.SkillOf(SkillKindApp, "hermes-agent") != nil || len(notes) == 0 {
			t.Errorf("%s: %v %v %v", name, got.SkillOf(SkillKindApp, "hermes-agent"), notes, err)
		}
	}
	// An oversize listing is kept: it is reported when fetched or verified.
	s = testShow(5)
	s.Apps[0].Skill = &ShowSkill{Path: SkillPath(SkillKindApp, "hermes-agent"), SHA256: strings.Repeat("a", 64), Size: MaxSkillBytes + 1}
	if got, _, err := ShowNotes(encodeShow(t, s)); err != nil || got.SkillOf(SkillKindApp, "hermes-agent") == nil {
		t.Fatalf("oversize listing: %v", err)
	}
}

func TestSyncFetchesAndStoresSkills(t *testing.T) {
	pub, priv := testKey(t)
	src := newSource(t, skillRelease(t, priv))
	cache := filepath.Join(t.TempDir(), "cache")

	res := syncShow(t, src, cache, pub)
	if !res.Updated || res.Show.State != ShowOK || res.Skills.State != SkillsOK || res.Skills.Count != 2 || len(res.Skills.Failed) != 0 {
		t.Fatalf("res %+v", res)
	}
	show, origin, _ := loadedShow(t, cache, pub)
	fsys := SkillFS(cache, nil, origin)
	doc, err := ReadSkill(fsys, show, SkillKindApp, "hermes-agent")
	if err != nil || doc == nil || doc.Text != hermesManual || doc.Version != "1.2.3" || doc.Checked != "2026-11-24" || doc.Kind != SkillKindApp {
		t.Fatalf("app manual %+v %v", doc, err)
	}
	doc, err = ReadSkill(fsys, show, SkillKindRecipe, "hermes-with-new-api")
	if err != nil || doc == nil || doc.Text != solutionManual {
		t.Fatalf("solution manual %+v %v", doc, err)
	}
	// No manual listed: nil, nil (not an error).
	if doc, err := ReadSkill(fsys, show, SkillKindApp, "new-api"); doc != nil || err != nil {
		t.Fatalf("no manual: %+v %v", doc, err)
	}
	infos, st := VerifySkills(fsys, show, nil)
	if st.State != SkillsOK || st.Count != 2 || infos[SkillKey(SkillKindApp, "hermes-agent")].Checked != "2026-11-24" {
		t.Fatalf("verify %+v %+v", infos, st)
	}

	// Same files, nothing fetched again.
	hits := src.count(SkillPath(SkillKindApp, "hermes-agent"))
	res = syncShow(t, src, cache, pub)
	if res.Updated || res.Skills.State != SkillsOK || res.Skills.Count != 2 || src.count(SkillPath(SkillKindApp, "hermes-agent")) != hits {
		t.Fatalf("second %+v hits %d→%d", res, hits, src.count(SkillPath(SkillKindApp, "hermes-agent")))
	}
}

func TestSyncSkillProblemsDoNotStopTheCatalogOrTheShow(t *testing.T) {
	pub, priv := testKey(t)
	big := strings.Repeat("字", MaxSkillBytes/3+1) // over 32 KB in bytes
	appPath := SkillPath(SkillKindApp, "hermes-agent")
	cases := []struct {
		name   string
		prep   func(dir string)
		status int // the source answers this for the app's manual
		code   string
	}{
		{"file missing (404)", func(d string) {
			_ = os.Remove(filepath.Join(d, filepath.FromSlash(SkillPath(SkillKindApp, "hermes-agent"))))
		}, 0, CodeSkillFetch},
		{"server error", func(d string) {}, http.StatusInternalServerError, CodeSkillFetch},
		{"sha256 differs", func(d string) {
			_ = os.WriteFile(filepath.Join(d, filepath.FromSlash(SkillPath(SkillKindApp, "hermes-agent"))), []byte(strings.Replace(hermesManual, "Key", "key", 1)), 0o644)
		}, 0, CodeSkillMismatch},
		{"longer than listed", func(d string) {
			_ = os.WriteFile(filepath.Join(d, filepath.FromSlash(SkillPath(SkillKindApp, "hermes-agent"))), []byte(hermesManual+"多出来的一行\n"), 0o644)
		}, 0, CodeSkillMismatch},
		{"shorter than listed", func(d string) {
			_ = os.WriteFile(filepath.Join(d, filepath.FromSlash(SkillPath(SkillKindApp, "hermes-agent"))), []byte(hermesManual[:20]), 0o644)
		}, 0, CodeSkillMismatch},
		{"listed over the limit", func(d string) {
			// The signed show lists a size over 32 KB; the file is not even asked for.
			s := showWithSkills(5)
			s.Apps[0].Skill = skillOf(SkillKindApp, "hermes-agent", big)
			publishShow(t, d, encodeShow(t, s), priv)
			_ = os.WriteFile(filepath.Join(d, filepath.FromSlash(SkillPath(SkillKindApp, "hermes-agent"))), []byte(big), 0o644)
		}, 0, CodeSkillTooBig},
	}
	for _, c := range cases {
		out := skillRelease(t, priv)
		c.prep(out)
		src := newSource(t, out)
		if c.status != 0 {
			src.pathStatus[appPath] = c.status
		}
		cache := t.TempDir()
		res := syncShow(t, src, cache, pub)
		if !res.Updated || res.Serial != 5 || res.Show.State != ShowOK {
			t.Errorf("%s: 目录和展示内容不该受影响 %+v", c.name, res)
			continue
		}
		if res.Skills.State != SkillsFailed || res.Skills.Code != c.code || len(res.Skills.Failed) != 1 || res.Skills.Failed[0].ID != "hermes-agent" || res.Skills.Count != 1 {
			t.Errorf("%s: %+v", c.name, res.Skills)
			continue
		}
		// The good manual (the solution's) is stored and readable; the bad one is not on disk.
		show, origin, _ := loadedShow(t, cache, pub)
		fsys := SkillFS(cache, nil, origin)
		if doc, err := ReadSkill(fsys, show, SkillKindRecipe, "hermes-with-new-api"); err != nil || doc == nil {
			t.Errorf("%s: 好的那份读不到 %v", c.name, err)
		}
		doc, err := ReadSkill(fsys, show, SkillKindApp, "hermes-agent")
		if doc != nil || CodeOf(err) == "" {
			t.Errorf("%s: 坏的那份不该读得到 %+v %v", c.name, doc, err)
		}
		if _, statErr := os.Stat(filepath.Join(currentVersionDir(cache), filepath.FromSlash(SkillPath(SkillKindApp, "hermes-agent")))); !os.IsNotExist(statErr) {
			t.Errorf("%s: 对不上的手册落盘了", c.name)
		}
		// What the page and doctor see: not usable, with the sync's code.
		infos, st := VerifySkills(fsys, show, res.Skills.Failed)
		if st.State != SkillsFailed || st.Code != c.code || len(infos) != 1 {
			t.Errorf("%s: verify %+v %+v", c.name, infos, st)
		}
	}
}

func TestSyncSkillBackfillRetriesOnlyWhatIsMissing(t *testing.T) {
	pub, priv := testKey(t)
	out := skillRelease(t, priv)
	appPath := SkillPath(SkillKindApp, "hermes-agent")
	solPath := SkillPath(SkillKindRecipe, "hermes-with-new-api")
	src := newSource(t, out)
	src.pathStatus[appPath] = http.StatusServiceUnavailable
	cache := t.TempDir()

	res := syncShow(t, src, cache, pub)
	if res.Skills.State != SkillsFailed || res.Skills.Count != 1 {
		t.Fatalf("first %+v", res.Skills)
	}
	// The source works now: the same serial, nothing newer, yet the missing
	// manual is fetched; the one already here is not asked for again.
	delete(src.pathStatus, appPath)
	solHits, idxHits := src.count(solPath), src.count(IndexFile)
	res = syncShow(t, src, cache, pub)
	if res.Updated || res.Skills.State != SkillsOK || res.Skills.Count != 2 {
		t.Fatalf("backfill %+v", res)
	}
	if src.count(solPath) != solHits || src.count(IndexFile) != idxHits {
		t.Fatal("已经在本地的手册和目录不该再下载")
	}
	show, origin, _ := loadedShow(t, cache, pub)
	if doc, err := ReadSkill(SkillFS(cache, nil, origin), show, SkillKindApp, "hermes-agent"); err != nil || doc == nil || doc.Text != hermesManual {
		t.Fatalf("after backfill %+v %v", doc, err)
	}

	// A file damaged on disk is noticed and fetched again.
	p := filepath.Join(currentVersionDir(cache), filepath.FromSlash(appPath))
	_ = os.WriteFile(p, []byte("被改过"), 0o644)
	if _, err := ReadSkill(SkillFS(cache, nil, origin), show, SkillKindApp, "hermes-agent"); CodeOf(err) != CodeSkillMismatch {
		t.Fatalf("damaged file: %v", err)
	}
	res = syncShow(t, src, cache, pub)
	if res.Skills.State != SkillsOK || res.Skills.Count != 2 {
		t.Fatalf("repair %+v", res.Skills)
	}
	if b, _ := os.ReadFile(p); string(b) != hermesManual {
		t.Fatalf("not repaired: %q", b)
	}
}

func TestSyncWithoutShowOrManualsHasNoSkills(t *testing.T) {
	pub, priv := testKey(t)
	// No show.json at all.
	src := newSource(t, publish(t, copyTree(t), 3, priv))
	res := syncShow(t, src, t.TempDir(), pub)
	if res.Skills.State != "" || res.Skills.Count != 0 {
		t.Fatalf("no show: %+v", res.Skills)
	}
	// A show without manuals.
	out := publish(t, copyTree(t), 5, priv)
	publishShow(t, out, encodeShow(t, testShow(5)), priv)
	cache := t.TempDir()
	res = syncShow(t, newSource(t, out), cache, pub)
	if res.Skills.State != SkillsNone {
		t.Fatalf("show without manuals: %+v", res.Skills)
	}
	show, origin, _ := loadedShow(t, cache, pub)
	if _, st := VerifySkills(SkillFS(cache, nil, origin), show, nil); st.State != SkillsNone {
		t.Fatalf("verify none: %+v", st)
	}
	// No show at all: nil show is fine.
	if doc, err := ReadSkill(nil, nil, SkillKindApp, "x"); doc != nil || err != nil {
		t.Fatalf("nil show: %+v %v", doc, err)
	}
}

func TestNewIndexDropsOldManuals(t *testing.T) {
	pub, priv := testKey(t)
	cache := t.TempDir()
	syncShow(t, newSource(t, skillRelease(t, priv)), cache, pub)
	old := currentVersionDir(cache)
	out6 := publish(t, copyTree(t), 6, priv)
	publishShow(t, out6, encodeShow(t, testShow(6)), priv) // no manuals
	res := syncShow(t, newSource(t, out6), cache, pub)
	if !res.Updated || res.Skills.State != SkillsNone {
		t.Fatalf("res %+v", res)
	}
	show, origin, _ := loadedShow(t, cache, pub)
	if doc, err := ReadSkill(SkillFS(cache, nil, origin), show, SkillKindApp, "hermes-agent"); doc != nil || err != nil {
		t.Fatalf("旧序号的手册不能留给新目录用：%+v %v", doc, err)
	}
	if _, err := os.Stat(filepath.Join(currentVersionDir(cache), SkillDir)); !os.IsNotExist(err) {
		t.Fatal("新版本里不该有 skills 目录")
	}
	if old == currentVersionDir(cache) {
		t.Fatal("没有换版本")
	}
}

func TestReadSkillFromSnapshotAndNilFS(t *testing.T) {
	s := showWithSkills(4)
	raw := encodeShow(t, s)
	snap := fstest.MapFS{
		ShowFile:                                {Data: raw},
		SkillPath(SkillKindApp, "hermes-agent"): {Data: []byte(hermesManual)},
	}
	idx := &Index{Schema: IndexSchema, Serial: 4}
	show, st := LoadShow("", snap, nil, idx, FromSnapshot)
	if show == nil || st.State != ShowOK {
		t.Fatalf("show %v %+v", show, st)
	}
	fsys := SkillFS("", snap, FromSnapshot)
	if doc, err := ReadSkill(fsys, show, SkillKindApp, "hermes-agent"); err != nil || doc == nil || doc.Text != hermesManual {
		t.Fatalf("snapshot manual %+v %v", doc, err)
	}
	// The solution's file is not in the snapshot: CAT-SKILL-01, not a panic.
	if _, err := ReadSkill(fsys, show, SkillKindRecipe, "hermes-with-new-api"); CodeOf(err) != CodeSkillFetch {
		t.Fatalf("missing in snapshot: %v", err)
	}
	if _, err := ReadSkill(nil, show, SkillKindApp, "hermes-agent"); CodeOf(err) != CodeSkillFetch {
		t.Fatalf("nil fs: %v", err)
	}
	if _, st := VerifySkills(nil, show, nil); st.State != SkillsFailed || st.Code != CodeSkillFetch || st.Count != 0 || len(st.Failed) != 2 {
		t.Fatalf("verify nil fs: %+v", st)
	}
}

func TestSkillRejectsNonText(t *testing.T) {
	body := "ok\x00binary"
	ref := SkillRef{Kind: SkillKindApp, ID: "x", Skill: *skillOf(SkillKindApp, "x", body)}
	if e := skillError(ref, []byte(body)); e == nil || e.Code != CodeSkillMismatch {
		t.Fatalf("NUL: %v", e)
	}
	bad := string([]byte{0xff, 0xfe, 'a'})
	ref = SkillRef{Kind: SkillKindApp, ID: "x", Skill: *skillOf(SkillKindApp, "x", bad)}
	if e := skillError(ref, []byte(bad)); e == nil || e.Code != CodeSkillMismatch {
		t.Fatalf("not utf-8: %v", e)
	}
	ok := SkillRef{Kind: SkillKindApp, ID: "x", Skill: *skillOf(SkillKindApp, "x", hermesManual)}
	if e := skillError(ok, []byte(hermesManual)); e != nil {
		t.Fatalf("good text refused: %v", e)
	}
	// A 32 KB manual is allowed, one byte more is not.
	edge := strings.Repeat("a", MaxSkillBytes)
	ref = SkillRef{Kind: SkillKindApp, ID: "x", Skill: *skillOf(SkillKindApp, "x", edge)}
	if e := skillError(ref, []byte(edge)); e != nil {
		t.Fatalf("exactly 32 KB: %v", e)
	}
	ref = SkillRef{Kind: SkillKindApp, ID: "x", Skill: *skillOf(SkillKindApp, "x", edge+"a")}
	if e := skillError(ref, []byte(edge+"a")); e == nil || e.Code != CodeSkillTooBig {
		t.Fatalf("32 KB + 1: %v", e)
	}
}

func TestNormalizeSkillKind(t *testing.T) {
	for in, want := range map[string]string{"app": SkillKindApp, "recipe": SkillKindRecipe, "solution": SkillKindRecipe, "": "", "apps": "", "x": ""} {
		if got := NormalizeSkillKind(in); got != want {
			t.Errorf("%q → %q，要 %q", in, got, want)
		}
	}
	if SkillPath(SkillKindApp, "memos") != "skills/app-memos.md" || SkillPath(SkillKindRecipe, "office") != "skills/solution-office.md" {
		t.Fatal("手册路径写法变了")
	}
}

func TestVerifyDirChecksSkills(t *testing.T) {
	pub, priv := testKey(t)
	keys := []ed25519.PublicKey{pub}
	out := skillRelease(t, priv)
	res, err := VerifyDir(out, keys)
	if err != nil || res.Show != ShowOK || res.Skills != 2 {
		t.Fatalf("good: %+v %v", res, err)
	}
	app := filepath.Join(out, filepath.FromSlash(SkillPath(SkillKindApp, "hermes-agent")))
	_ = os.WriteFile(app, []byte("改过"), 0o644)
	if _, err := VerifyDir(out, keys); CodeOf(err) != CodeSkillMismatch {
		t.Fatalf("changed: %v", err)
	}
	_ = os.Remove(app)
	if _, err := VerifyDir(out, keys); CodeOf(err) != CodeSkillFetch {
		t.Fatalf("missing: %v", err)
	}
	// A directory with a show but no manuals listed: zero.
	out2 := publish(t, copyTree(t), 5, priv)
	publishShow(t, out2, encodeShow(t, testShow(5)), priv)
	if res, err := VerifyDir(out2, keys); err != nil || res.Skills != 0 {
		t.Fatalf("no manuals: %+v %v", res, err)
	}
}
