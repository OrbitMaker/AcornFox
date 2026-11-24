package catalog

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"unicode/utf8"
)

// Operating manuals ("skill") of apps and solutions. One Markdown file each,
// written and verified in the operations system and published next to the
// signed catalog:
//
//	skills/app-<id>.md        one app
//	skills/solution-<id>.md   one solution (recipe)
//
// show.json (signed, show.go) lists them: each app and solution entry may
// carry a "skill" {path, sha256, size, version, checked}. The manual's
// integrity rests on that sha256, so the files themselves need no signature of
// their own. A sync fetches every listed manual into the same v<serial>
// cache directory as the index and the show (cache.go); one that cannot be
// fetched or does not match is not stored, and neither stops the sync nor an
// install (CAT-SKILL-01..03, doctor). Reading re-checks size and sha256 every
// time, like the index.
//
// The program never interprets the text: it hands it to the user's AI
// workbench (`acornfox catalog skill`), which follows it under the general
// Skill's rules.

// Limits, kinds and where the files live.
const (
	// MaxSkillBytes is the longest manual.
	MaxSkillBytes = 32 << 10
	// SkillDir is the published (and cached) directory of the manuals.
	SkillDir = "skills"

	// SkillKindApp and SkillKindRecipe are the kinds a manual is asked for
	// (the CLI's --recipe; the file name says "solution").
	SkillKindApp    = "app"
	SkillKindRecipe = "recipe"
)

// ShowSkill is the "skill" of a show.json entry.
type ShowSkill struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Version string `json:"version"` // what the manual was verified against (app version, or recipe version/commit)
	Checked string `json:"checked"` // date the sandbox check passed, YYYY-MM-DD
}

// SkillInfo is what the catalog list says about a manual that is usable on
// this machine (verified in the cache): that there is one, which version it
// was verified against and when. Never the text.
type SkillInfo struct {
	Version string `json:"version"`
	Checked string `json:"checked"`
	Size    int64  `json:"size"`
}

// SkillDoc is one manual read back and verified.
type SkillDoc struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"` // app | recipe
	Version string `json:"version"`
	Checked string `json:"checked"`
	Size    int64  `json:"size"`
	Text    string `json:"text"`
}

// SkillRef names one listed manual.
type SkillRef struct {
	Kind  string
	ID    string
	Skill ShowSkill
}

// Key is "app/<id>" or "recipe/<id>", how maps of manuals are keyed.
func (r SkillRef) Key() string { return SkillKey(r.Kind, r.ID) }

// SkillKey is the key of a manual in maps ("app/<id>", "recipe/<id>").
func SkillKey(kind, id string) string { return kind + "/" + id }

// NormalizeSkillKind maps the kind words the interfaces accept (app, recipe,
// solution) to SkillKindApp or SkillKindRecipe; "" when it is none of them.
func NormalizeSkillKind(kind string) string {
	switch kind {
	case SkillKindApp:
		return SkillKindApp
	case SkillKindRecipe, "solution":
		return SkillKindRecipe
	}
	return ""
}

// SkillPath is where the manual of kind and id is published.
func SkillPath(kind, id string) string {
	name := "app-"
	if kind == SkillKindRecipe {
		name = "solution-"
	}
	return SkillDir + "/" + name + id + ".md"
}

var skillPathRe = regexp.MustCompile(`^skills/(app|solution)-[a-z0-9][a-z0-9-]{0,63}\.md$`)

// SkillRefs lists the manuals show.json names: apps first, then solutions,
// each in the order of the document.
func (s *Show) SkillRefs() []SkillRef {
	if s == nil {
		return nil
	}
	var out []SkillRef
	for _, a := range s.Apps {
		if a.Skill != nil {
			out = append(out, SkillRef{Kind: SkillKindApp, ID: a.ID, Skill: *a.Skill})
		}
	}
	for _, r := range s.Solutions {
		if r.Skill != nil {
			out = append(out, SkillRef{Kind: SkillKindRecipe, ID: r.ID, Skill: *r.Skill})
		}
	}
	return out
}

// SkillOf returns the manual show.json lists for kind and id, or nil.
func (s *Show) SkillOf(kind, id string) *ShowSkill {
	for _, r := range s.SkillRefs() {
		if r.Kind == kind && r.ID == id {
			sk := r.Skill
			return &sk
		}
	}
	return nil
}

// skill checks one entry of show.json: the path has to be the one the app or
// solution gets (skills/app-<id>.md, skills/solution-<id>.md, nothing else),
// the sha256 lower-case hex, the size positive. What does not fit is dropped
// with a note (the program then acts as if the app had no manual), not
// refused. A size over MaxSkillBytes is kept: it is reported when the
// manual is fetched or verified (CAT-SKILL-03).
func (n *showNotes) skill(where, kind, id string, sk *ShowSkill) *ShowSkill {
	if sk == nil {
		return nil
	}
	if want := SkillPath(kind, id); sk.Path != want || !skillPathRe.MatchString(sk.Path) {
		n.addf("%s 的操作手册路径 %q 不是 %s，程序会当成没有手册", where, sk.Path, want)
		return nil
	}
	if !sha256Re.MatchString(sk.SHA256) {
		n.addf("%s 的操作手册 sha256 不是 64 位小写十六进制，程序会当成没有手册", where)
		return nil
	}
	if sk.Size < 1 {
		n.addf("%s 的操作手册大小是 %d，程序会当成没有手册", where, sk.Size)
		return nil
	}
	out := *sk
	if len(out.Version) > 64 {
		out.Version = out.Version[:64]
	}
	if len(out.Checked) > 32 {
		out.Checked = out.Checked[:32]
	}
	return &out
}

// skillError says why bytes b are not the manual ref names, nil when they are:
// CAT-SKILL-03 when the listed size is over the limit, CAT-SKILL-02 for the
// wrong size, the wrong sha256 or something that is not text.
func skillError(ref SkillRef, b []byte) *Error {
	sk := ref.Skill
	switch {
	case sk.Size > MaxSkillBytes || int64(len(b)) > MaxSkillBytes:
		return newErr(CodeSkillTooBig, nil, fmt.Sprintf("%s 的操作手册超过 %d KB", ref.ID, MaxSkillBytes>>10))
	case int64(len(b)) != sk.Size:
		return newErr(CodeSkillMismatch, nil, fmt.Sprintf("%s 的操作手册大小是 %d，展示内容里写的是 %d，已丢弃", ref.ID, len(b), sk.Size))
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != sk.SHA256 {
		return newErr(CodeSkillMismatch, nil, ref.ID+" 的操作手册 sha256 对不上展示内容里写的，已丢弃")
	}
	if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return newErr(CodeSkillMismatch, nil, ref.ID+" 的操作手册不是文本，已丢弃")
	}
	return nil
}

// readSkillFile reads and verifies the manual ref names from fsys (a cache
// version directory or the built-in snapshot).
func readSkillFile(fsys fs.FS, ref SkillRef) ([]byte, error) {
	if ref.Skill.Size > MaxSkillBytes {
		return nil, skillError(ref, nil)
	}
	if fsys == nil {
		return nil, newErr(CodeSkillFetch, nil, ref.ID+" 的操作手册不在这台机器的目录缓存里，下次同步目录会再取")
	}
	b, err := fs.ReadFile(fsys, ref.Skill.Path)
	if err != nil {
		return nil, newErr(CodeSkillFetch, err, ref.ID+" 的操作手册不在这台机器的目录缓存里，下次同步目录会再取")
	}
	if e := skillError(ref, b); e != nil {
		return nil, e
	}
	return b, nil
}

// ReadSkill reads the manual of kind and id: nil, nil when show lists none;
// the error is the CAT-SKILL-* of a listed manual that is not usable (missing
// from the cache, wrong size or sha256, over the limit). fsys is where the
// files are (SkillFS).
func ReadSkill(fsys fs.FS, show *Show, kind, id string) (*SkillDoc, error) {
	sk := show.SkillOf(kind, id)
	if sk == nil {
		return nil, nil
	}
	b, err := readSkillFile(fsys, SkillRef{Kind: kind, ID: id, Skill: *sk})
	if err != nil {
		return nil, err
	}
	return &SkillDoc{ID: id, Kind: kind, Version: sk.Version, Checked: sk.Checked, Size: sk.Size, Text: string(b)}, nil
}

// SkillFS is where the manuals of the catalog in use are: the cache version
// directory when the catalog came from the cache (origin FromCache), else the
// built-in snapshot (nil when there is none).
func SkillFS(cacheDir string, snapshot fs.FS, origin string) fs.FS {
	if origin == FromCache {
		if v := currentVersionDir(cacheDir); v != "" {
			return os.DirFS(v)
		}
		return nil
	}
	return snapshot
}

// VerifySkills is a verified manual of each app or solution show lists, keyed by
// SkillKey, read from fsys (nil is an empty directory), and the state of the
// lot: ok when every listed manual is usable, none when show lists none,
// failed (with the first problem's code) when some are not. lastFailed are
// the codes the last sync gave to manuals it could not fetch; a manual that is
// not usable and is not among them is CAT-SKILL-01 (not fetched yet).
func VerifySkills(fsys fs.FS, show *Show, lastFailed []SkillFailure) (map[string]SkillInfo, SkillsState) {
	refs := show.SkillRefs()
	infos := map[string]SkillInfo{}
	if len(refs) == 0 {
		return infos, SkillsState{State: SkillsNone}
	}
	st := SkillsState{State: SkillsOK}
	for _, ref := range refs {
		_, err := readSkillFile(fsys, ref)
		if err == nil {
			infos[ref.Key()] = SkillInfo{Version: ref.Skill.Version, Checked: ref.Skill.Checked, Size: ref.Skill.Size}
			st.Count++
			continue
		}
		e := asError(err, CodeSkillFetch)
		code, msg := e.Code, e.Message
		if e.Code == CodeSkillFetch {
			for _, f := range lastFailed {
				if f.Kind == ref.Kind && f.ID == ref.ID {
					code, msg = f.Code, f.Message
				}
			}
		}
		st.Failed = append(st.Failed, SkillFailure{Kind: ref.Kind, ID: ref.ID, Code: code, Message: msg})
	}
	if len(st.Failed) > 0 {
		st.State = SkillsFailed
		st.Code, st.Message = skillSummary(st.Failed)
	}
	return infos, st
}

// SkillFailure is one listed manual that is not usable.
type SkillFailure struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// States of the manuals (the same words as the display content's).
const (
	SkillsOK     = "ok"     // every manual show.json lists is here and checks out
	SkillsNone   = "none"   // show.json lists none (or there is no show.json)
	SkillsFailed = "failed" // some are missing or do not check out; Failed says which
)

// SkillsState is where the manuals stand: what a sync made of them, or what
// is in use now.
type SkillsState struct {
	State   string         `json:"state"`
	Count   int            `json:"count"`             // usable manuals
	Code    string         `json:"code,omitempty"`    // CAT-SKILL-* of the first failure
	Message string         `json:"message,omitempty"` // for people
	Failed  []SkillFailure `json:"failed,omitempty"`
}

func skillSummary(failed []SkillFailure) (code, msg string) {
	f := failed[0]
	msg = fmt.Sprintf("有 %d 份操作手册没取到或没对上（第一份是 %s：%s）", len(failed), f.ID, f.Message)
	return f.Code, msg
}

// fetchSkills gets the manuals refs list from src, except those have says
// are already here, and returns the verified files (named as they are
// published, skills/…) and the state of the lot. A manual that cannot be
// fetched or does not match is left out and recorded; nothing here is an
// error for the caller.
func fetchSkills(ctx context.Context, cfg SyncConfig, src string, refs []SkillRef, have func(SkillRef) bool) ([]cacheFile, SkillsState) {
	if len(refs) == 0 {
		return nil, SkillsState{State: SkillsNone}
	}
	var files []cacheFile
	st := SkillsState{State: SkillsOK}
	fail := func(ref SkillRef, e *Error) {
		st.Failed = append(st.Failed, SkillFailure{Kind: ref.Kind, ID: ref.ID, Code: e.Code, Message: e.Message})
	}
	for _, ref := range refs {
		if have != nil && have(ref) {
			st.Count++
			continue
		}
		if ref.Skill.Size > MaxSkillBytes {
			fail(ref, skillError(ref, nil))
			continue
		}
		if err := ctx.Err(); err != nil {
			fail(ref, newErr(CodeSkillFetch, err, ref.ID+" 的操作手册没取：同步被取消"))
			continue
		}
		b, found, err := getOptional(ctx, cfg, src, ref.Skill.Path, ref.Skill.Size)
		switch {
		case err != nil:
			e := asError(err, CodeSyncNetwork)
			if e.Code == CodeSyncResponse { // longer than listed
				fail(ref, newErr(CodeSkillMismatch, nil, ref.ID+" 的操作手册比展示内容里写的大，已丢弃"))
			} else {
				fail(ref, newErr(CodeSkillFetch, nil, ref.ID+" 的操作手册没取到："+e.Message))
			}
			continue
		case !found:
			fail(ref, newErr(CodeSkillFetch, nil, ref.ID+" 的操作手册在来源上找不到（HTTP 404）"))
			continue
		}
		if e := skillError(ref, b); e != nil {
			fail(ref, e)
			continue
		}
		files = append(files, cacheFile{ref.Skill.Path, b})
		st.Count++
	}
	if len(st.Failed) > 0 {
		st.State = SkillsFailed
		st.Code, st.Message = skillSummary(st.Failed)
	}
	return files, st
}

// backfillSkills is what a sync that finds nothing newer does about the
// manuals: the cache version in use is this very serial and has a show that
// lists manuals; those that are missing or do not check out (an earlier fetch
// failed, a file was damaged) are fetched again from this source. Manuals
// that are fine cost nothing: no request. It never fails the sync. The
// caller holds the cache lock.
func backfillSkills(ctx context.Context, cfg SyncConfig, src string, remote, local int64, keys []ed25519.PublicKey) SkillsState {
	c := loadCache(cfg.CacheDir, keys)
	if c == nil || c.Serial != remote || remote != local {
		return SkillsState{}
	}
	vdir := currentVersionDir(cfg.CacheDir)
	show, st := cacheShow(vdir, c, keys)
	if show == nil || st.State != ShowOK {
		return SkillsState{}
	}
	fsys := os.DirFS(vdir)
	refs := show.SkillRefs()
	files, res := fetchSkills(ctx, cfg, src, refs, func(ref SkillRef) bool {
		_, err := readSkillFile(fsys, ref)
		return err == nil
	})
	for _, f := range files {
		if err := writeCacheFile(filepath.Join(vdir, filepath.FromSlash(f.name)), f.data); err != nil {
			for _, ref := range refs {
				if ref.Skill.Path == f.name {
					res.Failed = append(res.Failed, SkillFailure{Kind: ref.Kind, ID: ref.ID, Code: CodeSkillFetch, Message: ref.ID + " 的操作手册取到了，写本地缓存失败"})
				}
			}
			res.State = SkillsFailed
			res.Count--
			res.Code, res.Message = skillSummary(res.Failed)
		}
	}
	return res
}

// verifySkillsDir checks the manuals the show of a published directory
// lists: each file is there with the listed size and sha256 (CAT-SKILL-01..03
// otherwise). Returns how many it checked.
func verifySkillsDir(dir string, show *Show) (int, error) {
	n := 0
	for _, ref := range show.SkillRefs() {
		if ref.Skill.Size > MaxSkillBytes {
			return n, skillError(ref, nil)
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(ref.Skill.Path)))
		if err != nil {
			return n, newErr(CodeSkillFetch, err, ref.ID+" 的操作手册缺文件 "+ref.Skill.Path)
		}
		if e := skillError(ref, b); e != nil {
			return n, e
		}
		n++
	}
	return n, nil
}
