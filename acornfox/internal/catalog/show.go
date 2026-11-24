package catalog

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Display content of the public catalog ("show"): what the app center shows
// besides the manifests — a name and a one-line pitch, a badge, a colour, a
// cover picture, one Markdown document per app and per solution, the
// industries solutions are filed under, which solutions the home page
// shows, and the "find someone to deploy it" offer. It is edited in the
// operations system and published with the catalog:
//
//	show.json       the document (schema 1), same directory as index.json
//	show.json.sig   ed25519 over "acornfox-catalog-show-v1\n" + show.json,
//	                base64url without padding, the same trusted keys as index.json
//
// The prefix keeps a signature of show.json from ever being valid for
// anything else (and an index signature from being valid for show.json).
// show.json carries the serial of the index it was published with; one that
// does not match the index in use is not used. None of this is needed to
// install: a missing, unsigned or malformed show only means the pages fall
// back to the name and summary in the manifests (CAT-SHOW-01..04, doctor).
//
// The cache keeps show.json and show.json.sig inside the same v<serial>
// directory as the index they belong to (cache.go), so a version of the
// catalog and its display content are switched together. The built-in
// snapshot may carry a show.json too (no signature: the program is the trust
// anchor); it has none today.

// Published file names, signature domain and limits of the show.
const (
	ShowFile    = "show.json"
	ShowSigFile = "show.json.sig"

	// ShowSigPrefix is prepended to show.json before it is signed.
	ShowSigPrefix = "acornfox-catalog-show-v1\n"
	// ShowKind and ShowSchema identify the document.
	ShowKind   = "catalog-show"
	ShowSchema = 1

	// MaxShowDocBytes is the longest doc (one Markdown document).
	MaxShowDocBytes = 64 << 10
	maxShowBytes    = 8 << 20

	// CoverPrefix is where cover pictures live; any other address is dropped.
	CoverPrefix = "https://acornfox.com/catalog/assets/"
)

// Show is show.json. Field order is the struct order.
type Show struct {
	Kind       string         `json:"kind"`
	Schema     int            `json:"schema"`
	Serial     int64          `json:"serial"`
	Industries []ShowIndustry `json:"industries"`
	Apps       []ShowApp      `json:"apps"`
	Solutions  []ShowSolution `json:"solutions"`
	Home       ShowHome       `json:"home"`
	Assist     ShowAssist     `json:"assist"`
}

// ShowIndustry is one industry solutions are filed under, in the order the
// page lists them.
type ShowIndustry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ShowApp is the display content of one app. Every field may be empty: the
// page falls back to the manifest's.
type ShowApp struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Tagline  string   `json:"tagline"`
	Flag     string   `json:"flag"` // "", hot, new, pick
	Color    string   `json:"color"`
	Cover    string   `json:"cover"`
	Doc      string   `json:"doc"`
	Order    int      `json:"order"`
	Keywords []string `json:"keywords"`
	// Skill is the app's operating manual (skill.go); nil when it has none.
	Skill *ShowSkill `json:"skill,omitempty"`
}

// ShowSolution is the display content of one solution (recipe).
type ShowSolution struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Tagline    string   `json:"tagline"`
	Industries []string `json:"industries"`
	Flag       string   `json:"flag"`
	Color      string   `json:"color"`
	Cover      string   `json:"cover"`
	Doc        string   `json:"doc"`
	Order      int      `json:"order"`
	// Skill is the solution's operating manual (skill.go); nil when it has none.
	Skill *ShowSkill `json:"skill,omitempty"`
}

// ShowHome says which solutions the home page shows, in this order.
type ShowHome struct {
	Solutions []string `json:"solutions"`
}

// ShowAssist is the "find someone to deploy it" offer.
type ShowAssist struct {
	Visible  bool    `json:"visible"`
	PriceCNY float64 `json:"price_cny"`
	// URL opens the offer; only https:// links are kept, empty hides the button.
	URL string `json:"url,omitempty"`
}

// ShowAppView is what GET /v1/catalog gives for one app: the app's entry
// without its id (the id is on the app).
type ShowAppView struct {
	Name     string   `json:"name"`
	Tagline  string   `json:"tagline"`
	Flag     string   `json:"flag"`
	Color    string   `json:"color"`
	Cover    string   `json:"cover"`
	Doc      string   `json:"doc"`
	Order    int      `json:"order"`
	Keywords []string `json:"keywords"`
	// Skill says the app has an operating manual usable on this machine (the
	// manual itself: GET /v1/catalog/skills/app/{id}). Set by the app center
	// after it has checked the cached file, never by App.
	Skill *SkillInfo `json:"skill,omitempty"`
}

// ShowSolutionView is ShowSolution without its id.
type ShowSolutionView struct {
	Name       string   `json:"name"`
	Tagline    string   `json:"tagline"`
	Industries []string `json:"industries"`
	Flag       string   `json:"flag"`
	Color      string   `json:"color"`
	Cover      string   `json:"cover"`
	Doc        string   `json:"doc"`
	Order      int      `json:"order"`
	// Skill as in ShowAppView.
	Skill *SkillInfo `json:"skill,omitempty"`
}

// App returns the display content of app id, or nil.
func (s *Show) App(id string) *ShowAppView {
	if s == nil {
		return nil
	}
	for _, a := range s.Apps {
		if a.ID == id {
			v := ShowAppView{Name: a.Name, Tagline: a.Tagline, Flag: a.Flag, Color: a.Color, Cover: a.Cover, Doc: a.Doc, Order: a.Order, Keywords: a.Keywords}
			return &v
		}
	}
	return nil
}

// Solution returns the display content of recipe id, or nil.
func (s *Show) Solution(id string) *ShowSolutionView {
	if s == nil {
		return nil
	}
	for _, r := range s.Solutions {
		if r.ID == id {
			v := ShowSolutionView{Name: r.Name, Tagline: r.Tagline, Industries: r.Industries, Flag: r.Flag, Color: r.Color, Cover: r.Cover, Doc: r.Doc, Order: r.Order}
			return &v
		}
	}
	return nil
}

var (
	showColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	// What follows CoverPrefix: one plain file name, no path, query or fragment.
	coverNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

// ValidCover reports whether s is a cover address the program accepts:
// CoverPrefix followed by one plain file name.
func ValidCover(s string) bool {
	rest, ok := strings.CutPrefix(s, CoverPrefix)
	return ok && coverNameRe.MatchString(rest) && !strings.Contains(rest, "..")
}

// showNotes collects what ParseShow dropped.
type showNotes []string

func (n *showNotes) addf(format string, a ...any) { *n = append(*n, fmt.Sprintf(format, a...)) }

func (n *showNotes) cover(where, s string) string {
	if s == "" || ValidCover(s) {
		return s
	}
	n.addf("%s 的封面地址 %q 不在 %s 下面，程序会当成没有封面", where, s, CoverPrefix)
	return ""
}

func (n *showNotes) flag(where, s string) string {
	switch s {
	case "", "hot", "new", "pick":
		return s
	}
	n.addf("%s 的角标 %q 不是 hot、new、pick 之一，程序会当成没有角标", where, s)
	return ""
}

func (n *showNotes) color(where, s string) string {
	if s == "" || showColorRe.MatchString(s) {
		return s
	}
	n.addf("%s 的主色 %q 不是 #rrggbb，程序会当成没有", where, s)
	return ""
}

// ParseShow decodes show.json and checks what a client relies on (CAT-SHOW-03
// otherwise): kind, schema 1, a positive serial, every id once, no doc over
// MaxShowDocBytes. Values it cannot use are dropped, not refused: a flag, a
// colour or a cover address of another shape becomes empty; an industry or
// home solution that is not listed is left out. Missing lists become empty
// ones, so the JSON the program gives out always has them.
func ParseShow(b []byte) (*Show, error) {
	s, _, err := parseShow(b)
	return s, err
}

// ShowNotes parses b like ParseShow and also says what the program will not
// use: a cover address, badge or colour of the wrong shape, an industry or a
// home solution that is not listed. The publisher's check (catalog
// show-sign) prints them; the program itself just drops them.
func ShowNotes(b []byte) (*Show, []string, error) {
	return parseShow(b)
}

func parseShow(b []byte) (*Show, []string, error) {
	var notes showNotes
	var s Show
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, nil, newErr(CodeShowFormat, err, "show.json 读不懂")
	}
	if s.Kind != ShowKind {
		return nil, nil, newErr(CodeShowFormat, nil, fmt.Sprintf("show.json 的 kind 是 %q，要是 %q", s.Kind, ShowKind))
	}
	if s.Schema != ShowSchema {
		return nil, nil, newErr(CodeShowFormat, nil, fmt.Sprintf("show.json 的 schema 是 %d，这个程序只认 %d", s.Schema, ShowSchema))
	}
	if s.Serial < 1 {
		return nil, nil, newErr(CodeShowFormat, nil, fmt.Sprintf("show.json 的 serial 是 %d", s.Serial))
	}
	if s.Industries == nil {
		s.Industries = []ShowIndustry{}
	}
	industry := map[string]bool{}
	for _, in := range s.Industries {
		if in.ID == "" {
			return nil, nil, newErr(CodeShowFormat, nil, "show.json 里有行业没写 id")
		}
		if industry[in.ID] {
			return nil, nil, newErr(CodeShowFormat, nil, "show.json 里行业 "+in.ID+" 写了两次")
		}
		industry[in.ID] = true
	}
	if s.Apps == nil {
		s.Apps = []ShowApp{}
	}
	seen := map[string]bool{}
	for i := range s.Apps {
		a := &s.Apps[i]
		if a.ID == "" || seen[a.ID] {
			return nil, nil, newErr(CodeShowFormat, nil, "show.json 里应用的 id 是空的或重复："+a.ID)
		}
		seen[a.ID] = true
		if len(a.Doc) > MaxShowDocBytes {
			return nil, nil, newErr(CodeShowFormat, nil, fmt.Sprintf("应用 %s 的介绍有 %d 字节，超过 %d", a.ID, len(a.Doc), MaxShowDocBytes))
		}
		where := "应用 " + a.ID
		a.Flag, a.Color, a.Cover = notes.flag(where, a.Flag), notes.color(where, a.Color), notes.cover(where, a.Cover)
		a.Skill = notes.skill(where, SkillKindApp, a.ID, a.Skill)
		if a.Keywords == nil {
			a.Keywords = []string{}
		}
	}
	if s.Solutions == nil {
		s.Solutions = []ShowSolution{}
	}
	sol := map[string]bool{}
	for i := range s.Solutions {
		r := &s.Solutions[i]
		if r.ID == "" || sol[r.ID] {
			return nil, nil, newErr(CodeShowFormat, nil, "show.json 里解决方案的 id 是空的或重复："+r.ID)
		}
		sol[r.ID] = true
		if len(r.Doc) > MaxShowDocBytes {
			return nil, nil, newErr(CodeShowFormat, nil, fmt.Sprintf("解决方案 %s 的介绍有 %d 字节，超过 %d", r.ID, len(r.Doc), MaxShowDocBytes))
		}
		where := "解决方案 " + r.ID
		r.Flag, r.Color, r.Cover = notes.flag(where, r.Flag), notes.color(where, r.Color), notes.cover(where, r.Cover)
		r.Skill = notes.skill(where, SkillKindRecipe, r.ID, r.Skill)
		known := []string{}
		for _, id := range r.Industries {
			if industry[id] {
				known = append(known, id)
			} else {
				notes.addf("解决方案 %s 写的行业 %s 不在行业列表里，程序会去掉它", r.ID, id)
			}
		}
		r.Industries = known
	}
	home := []string{}
	for _, id := range s.Home.Solutions {
		if sol[id] {
			home = append(home, id)
		} else {
			notes.addf("首页写的解决方案 %s 不在解决方案列表里，程序会去掉它", id)
		}
	}
	s.Home.Solutions = home
	if s.Assist.PriceCNY < 0 {
		s.Assist.PriceCNY = 0
	}
	if s.Assist.URL != "" && !strings.HasPrefix(s.Assist.URL, "https://") {
		notes.addf("“找专人部署”的链接 %q 不是 https:// 开头，程序会去掉它", s.Assist.URL)
		s.Assist.URL = ""
	}
	return &s, notes, nil
}

// SignShow signs show.json's bytes (ShowSigPrefix + show) and returns what
// goes into show.json.sig: base64url without padding.
func SignShow(show []byte, key ed25519.PrivateKey) []byte {
	return []byte(b64.EncodeToString(ed25519.Sign(key, showSigned(show))))
}

func showSigned(show []byte) []byte {
	return append([]byte(ShowSigPrefix), show...)
}

// VerifyShow checks sig (the content of show.json.sig) over show against each
// trusted key (CAT-SHOW-01 when none matches).
func VerifyShow(show, sig []byte, trusted []ed25519.PublicKey) error {
	if err := Verify(showSigned(show), sig, trusted); err != nil {
		return newErr(CodeShowSignature, err, "show.json 的签名验不过")
	}
	return nil
}

// ReadShow verifies, decodes and checks a published show against the index it
// goes with: signature (CAT-SHOW-01), format (CAT-SHOW-03), then its serial
// against idxSerial (CAT-SHOW-02). Nothing unsigned is decoded.
func ReadShow(show, sig []byte, idxSerial int64, trusted []ed25519.PublicKey) (*Show, error) {
	if err := VerifyShow(show, sig, trusted); err != nil {
		return nil, err
	}
	s, err := ParseShow(show)
	if err != nil {
		return nil, err
	}
	if err := checkShowSerial(s, idxSerial); err != nil {
		return nil, err
	}
	return s, nil
}

func checkShowSerial(s *Show, idxSerial int64) error {
	if s.Serial != idxSerial {
		return newErr(CodeShowSerial, nil, fmt.Sprintf("展示内容的序号是 %d，目录的序号是 %d，不用", s.Serial, idxSerial))
	}
	return nil
}

// States of the display content, as ShowState.State says them.
const (
	ShowOK     = "ok"     // a verified show matching the index is in use
	ShowNone   = "none"   // the catalog has none (the pages use the manifests' words)
	ShowFailed = "failed" // there is one but it could not be used; Code says why
)

// ShowState is where the display content stands: in use, absent, or unusable.
type ShowState struct {
	State   string `json:"state"`
	Code    string `json:"code,omitempty"`    // CAT-SHOW-* when State is failed
	Message string `json:"message,omitempty"` // for people
	Serial  int64  `json:"serial,omitempty"`  // the show in use
}

func failedShow(err error) ShowState {
	e := asError(err, CodeShowFormat)
	return ShowState{State: ShowFailed, Code: e.Code, Message: e.Error()}
}

// cacheShow reads the show kept in a cache version directory and checks it
// against the index in use. A directory without show.json has none
// (ShowNone); a show.json that does not verify, or does not match, is
// ShowFailed with the code.
func cacheShow(vdir string, idx *Index, keys []ed25519.PublicKey) (*Show, ShowState) {
	if vdir == "" || idx == nil {
		return nil, ShowState{State: ShowNone}
	}
	b, err := os.ReadFile(filepath.Join(vdir, ShowFile))
	if err != nil {
		return nil, ShowState{State: ShowNone}
	}
	sig, err := os.ReadFile(filepath.Join(vdir, ShowSigFile))
	if err != nil {
		return nil, failedShow(newErr(CodeShowSignature, err, "缓存里有 show.json，没有它的签名"))
	}
	s, err := ReadShow(b, sig, idx.Serial, keys)
	if err != nil {
		return nil, failedShow(err)
	}
	return s, ShowState{State: ShowOK, Serial: s.Serial}
}

// snapshotShow reads show.json from the built-in snapshot (trusted without a
// signature, like the snapshot's index). The snapshot has none today.
func snapshotShow(snapshot fs.FS, idx *Index) (*Show, ShowState) {
	if snapshot == nil || idx == nil {
		return nil, ShowState{State: ShowNone}
	}
	b, err := fs.ReadFile(snapshot, ShowFile)
	if err != nil {
		return nil, ShowState{State: ShowNone}
	}
	s, err := ParseShow(b)
	if err == nil {
		err = checkShowSerial(s, idx.Serial)
	}
	if err != nil {
		return nil, failedShow(err)
	}
	return s, ShowState{State: ShowOK, Serial: s.Serial}
}

// LoadShow returns the display content that goes with idx, the index Load
// gave (origin FromCache or FromSnapshot): the cache's show when idx came
// from the cache, else (or when the cache has none) the snapshot's. It never
// fails: with nothing usable it returns nil and says why in the state. The
// check is repeated on every read, like loadCache's.
func LoadShow(cacheDir string, snapshot fs.FS, keys []ed25519.PublicKey, idx *Index, origin string) (*Show, ShowState) {
	if idx == nil {
		return nil, ShowState{State: ShowNone}
	}
	st := ShowState{State: ShowNone}
	if origin == FromCache {
		var s *Show
		if s, st = cacheShow(currentVersionDir(cacheDir), idx, keys); s != nil {
			return s, st
		}
	}
	if s, sst := snapshotShow(snapshot, idx); s != nil || sst.State == ShowFailed {
		return s, sst
	}
	return nil, st
}

// storeShowInCurrent adds show.json and its signature to the cache version in
// use when it is the one for serial and has no show yet (a show that could
// not be fetched with its index, fetched by a later sync). The caller holds
// the cache lock and has verified both.
func storeShowInCurrent(dir string, serial int64, show, sig []byte) error {
	vdir := currentVersionDir(dir)
	if vdir == "" || filepath.Base(vdir) != "v"+strconv.FormatInt(serial, 10) {
		return fmt.Errorf("缓存里在用的版本不是 %d", serial)
	}
	// The signature first: a show.json without it is never used.
	if err := writeCacheFile(filepath.Join(vdir, ShowSigFile), sig); err != nil {
		return err
	}
	return writeCacheFile(filepath.Join(vdir, ShowFile), show)
}

// hasCachedShow reports whether the cache version in use already has a show.
func hasCachedShow(dir string) bool {
	vdir := currentVersionDir(dir)
	if vdir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(vdir, ShowFile))
	return err == nil
}

// VerifyShowDir checks the show of a published directory without changing it
// (acornfox catalog verify): returns nil, ShowNone when the directory has no
// show.json; otherwise the show when signature, format and serial (against
// index.json) all pass, or the CAT-SHOW error.
func VerifyShowDir(dir string, idxSerial int64, trusted []ed25519.PublicKey) (*Show, bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, ShowFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, true, newErr(CodeShowFormat, err, "读不了 show.json")
	}
	sig, err := os.ReadFile(filepath.Join(dir, ShowSigFile))
	if err != nil {
		return nil, true, newErr(CodeShowSignature, err, "有 show.json，缺 show.json.sig")
	}
	s, err := ReadShow(b, bytes.TrimSpace(sig), idxSerial, trusted)
	return s, true, err
}
