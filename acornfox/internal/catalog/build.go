package catalog

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Published layout. A publish uploads packages first, then index.json.sig and
// index.json, and version.txt last, so a client never sees a version.txt
// that points at files that are not there yet.
const (
	IndexSchema  = 1
	VersionFile  = "version.txt"
	IndexFile    = "index.json"
	SigFile      = "index.json.sig"
	packagesRoot = "apps"
)

// Index is index.json. Field order is the struct order, so the same catalog
// always produces the same bytes.
//
// Catalog names the catalog the index belongs to ("public" or a private
// catalog id). It is inside the signed bytes, so a signed index of one
// catalog cannot be passed off as another's: the public sync takes only
// "public", a private catalog only its own id. It is left out when the
// builder was not told (older publishes); such an index is still accepted
// for either, see CheckCatalog.
//
// Recipes holds the recipes every platform version can install. RecipesV2
// holds the ones that need more than BasePlatform, each with its MinPlatform
// filled in. The first platform release reads Recipes only and ignores keys
// it does not know, so it never shows a recipe it could not install (it has
// no version check for recipes). Later releases read both: use AllRecipes.
// RecipesV2 is left out of the file when it is empty, so a catalog without
// such recipes builds to the same bytes as before the key existed.
type Index struct {
	Schema    int        `json:"schema"`
	Serial    int64      `json:"serial"`
	Catalog   string     `json:"catalog,omitempty"`
	Apps      []IndexApp `json:"apps"`
	Recipes   []Recipe   `json:"recipes"`
	RecipesV2 []Recipe   `json:"recipes_v2,omitempty"`
}

// AllRecipes returns Recipes and RecipesV2 together, sorted by id. Check a
// recipe's MinPlatform (empty means BasePlatform) before installing it. When
// an id is in both lists the one in Recipes is kept.
func (i *Index) AllRecipes() []Recipe {
	out := make([]Recipe, 0, len(i.Recipes)+len(i.RecipesV2))
	seen := map[string]bool{}
	for _, list := range [][]Recipe{i.Recipes, i.RecipesV2} {
		for _, r := range list {
			if !seen[r.ID] {
				seen[r.ID] = true
				out = append(out, r)
			}
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// PublicCatalog is the Catalog value of the public catalog's index.
const PublicCatalog = "public"

// IndexApp is the summary of one app plus where its package is.
type IndexApp struct {
	ID          string     `json:"id"`
	Version     string     `json:"version"`
	Name        string     `json:"name"`
	Summary     string     `json:"summary"`
	Category    string     `json:"category"`
	Keywords    []string   `json:"keywords"`
	Tags        []string   `json:"tags"`
	MinPlatform string     `json:"min_platform"`
	Status      string     `json:"status"`
	MemoryMB    MinRec     `json:"memory_mb"`
	Platforms   []string   `json:"platforms"`
	SizeMB      int        `json:"size_mb"`
	Delivery    Delivery   `json:"delivery"`
	Uses        []Use      `json:"uses"`
	Provides    []string   `json:"provides"`
	Package     PackageRef `json:"package"`
}

// PackageRef says where an app package is (relative to the catalog base URL)
// and what it must hash to.
type PackageRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Bundle is a built catalog held in memory. Sign it, then Write it.
type Bundle struct {
	Serial   int64
	Index    []byte            // index.json, exactly the bytes that get signed
	Sig      []byte            // index.json.sig (base64url, no padding); nil until Sign
	Packages map[string][]byte // published path → apps/<id>/<version>.tar.gz bytes
}

// OutFile is one published file.
type OutFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	data   []byte
}

var versionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

// packageEpoch is the mtime of every entry in an app package.
var packageEpoch = time.Unix(0, 0).UTC()

// Build lints the catalog at dir and packs it with the given serial. It
// refuses (CAT-BUILD-01, problems attached) when Lint reports errors. It
// writes nothing; call Sign and Write on the result. The index names no
// catalog; BuildFor binds it to one.
func Build(dir string, serial int64) (*Bundle, error) {
	return BuildFor(dir, serial, "")
}

// BuildFor is Build with the index bound to catalogID ("public" or a private
// catalog id; "" for none), which is then part of the signed bytes.
func BuildFor(dir string, serial int64, catalogID string) (*Bundle, error) {
	if serial < 1 {
		return nil, newErr(CodeBuildInput, nil, fmt.Sprintf("发布序号要是正整数，现在是 %d", serial))
	}
	if catalogID != "" && catalogID != PublicCatalog && !ValidCatalogID(catalogID) {
		return nil, newErr(CodeBuildInput, nil, fmt.Sprintf("目录编号 %q 不对：要么是 public，要么是 cat- 加 1～64 个字母、数字、_ 或 -", catalogID))
	}
	rep, err := Lint(dir)
	if err != nil {
		return nil, newErr(CodeBuildInput, err, "检查目录失败")
	}
	if rep.Errors() > 0 {
		return nil, &Error{Code: CodeBuildLint, Message: fmt.Sprintf("目录有 %d 个错误，没有打包", rep.Errors()), Problems: rep.Problems}
	}

	idx := Index{Schema: IndexSchema, Serial: serial, Catalog: catalogID, Apps: []IndexApp{}, Recipes: []Recipe{}}
	b := &Bundle{Serial: serial, Packages: map[string][]byte{}}
	for _, id := range rep.Apps {
		var m Manifest
		if err := readYAML(filepath.Join(dir, "apps", id, manifestName), &m); err != nil {
			return nil, newErr(CodeBuildInput, err, "读清单失败："+id)
		}
		if !versionRe.MatchString(m.Release.Version) {
			return nil, newErr(CodeBuildInput, nil, fmt.Sprintf("应用 %s 的 release.version %q 不能用作文件名（只允许字母、数字和 . _ + -）", id, m.Release.Version))
		}
		pkg, err := packApp(filepath.Join(dir, "apps", id), id)
		if err != nil {
			return nil, err
		}
		path := packagesRoot + "/" + id + "/" + m.Release.Version + ".tar.gz"
		b.Packages[path] = pkg
		idx.Apps = append(idx.Apps, indexApp(m, PackageRef{Path: path, SHA256: sha256Hex(pkg), Size: int64(len(pkg))}))
	}
	for _, id := range rep.Recipes {
		var r Recipe
		if err := readYAML(filepath.Join(dir, "recipes", id, recipeName), &r); err != nil {
			return nil, newErr(CodeBuildInput, err, "读配方失败："+id)
		}
		// A recipe that needs more than the first platform release goes
		// where that release does not look. The others stay as written, so
		// their index entries do not change.
		if need := rep.MinPlatform.Recipes[id].Required(); CompareVersions(need, BasePlatform) > 0 {
			r.MinPlatform = need
			idx.RecipesV2 = append(idx.RecipesV2, r)
		} else {
			idx.Recipes = append(idx.Recipes, r)
		}
	}
	b.Index, err = encodeIndex(idx)
	if err != nil {
		return nil, newErr(CodeBuildInput, err, "生成 index.json 失败")
	}
	return b, nil
}

func indexApp(m Manifest, pkg PackageRef) IndexApp {
	size := 0
	for _, im := range m.Release.Images {
		size += im.SizeMB
	}
	provides := []string{}
	for _, p := range m.Provides {
		provides = append(provides, p.Kind)
	}
	return IndexApp{
		ID:          m.ID,
		Version:     m.Release.Version,
		Name:        m.App.Name,
		Summary:     m.App.Summary,
		Category:    m.App.Category,
		Keywords:    nonNil(m.App.Keywords),
		Tags:        nonNil(m.App.Tags),
		MinPlatform: m.Lifecycle.MinPlatform,
		Status:      m.Lifecycle.Status,
		MemoryMB:    m.Resources.MemoryMB,
		Platforms:   nonNil(m.Resources.Platforms),
		SizeMB:      size,
		Delivery:    m.Delivery,
		Uses:        nonNilUses(m.Uses),
		Provides:    provides,
		Package:     pkg,
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilUses(s []Use) []Use {
	if s == nil {
		return []Use{}
	}
	return s
}

// encodeIndex renders index.json: two-space indent, no HTML escaping, one
// trailing newline. Maps (recipe presets) are written with sorted keys.
func encodeIndex(idx Index) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(idx); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func readYAML(path string, out any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(b, out)
}

// packApp makes a deterministic tar.gz of every regular file under appDir
// (dotfiles skipped), stored as <id>/<relative path>: sorted names, mtime
// 1970-01-01, uid/gid 0, mode 0644, USTAR format, gzip without name or time.
func packApp(appDir, id string) ([]byte, error) {
	var files []string
	err := filepath.WalkDir(appDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && p != appDir {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s 不是普通文件（符号链接等不打包）", p)
		}
		rel, err := filepath.Rel(appDir, p)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, newErr(CodeBuildInput, err, "读应用目录失败："+id)
	}
	sort.Strings(files)

	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	tw := tar.NewWriter(zw)
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(appDir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, newErr(CodeBuildInput, err, "读文件失败："+rel)
		}
		hdr := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     id + "/" + rel,
			Mode:     0o644,
			Size:     int64(len(data)),
			ModTime:  packageEpoch,
			Format:   tar.FormatUSTAR,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, newErr(CodeBuildInput, err, "打包失败："+rel)
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Sign signs Index with key (ed25519 over the exact bytes).
func (b *Bundle) Sign(key ed25519.PrivateKey) {
	b.Sig = []byte(b64.EncodeToString(ed25519.Sign(key, b.Index)))
}

// Files lists the published files in upload order: packages (sorted), then
// index.json.sig (when signed), index.json, and version.txt last.
func (b *Bundle) Files() []OutFile {
	var paths []string
	for p := range b.Packages {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var out []OutFile
	add := func(path string, data []byte) {
		out = append(out, OutFile{Path: path, SHA256: sha256Hex(data), Size: int64(len(data)), data: data})
	}
	for _, p := range paths {
		add(p, b.Packages[p])
	}
	if b.Sig != nil {
		add(SigFile, b.Sig)
	}
	add(IndexFile, b.Index)
	add(VersionFile, []byte(strconv.FormatInt(b.Serial, 10)+"\n"))
	return out
}

// CheckPrev compares the bundle with a previous publish in prevDir (a
// directory holding what was uploaded last time; missing files mean "no
// previous publish"). It refuses:
//   - CAT-BUILD-04: a serial lower than the previous one, or the same serial
//     with a different index.json (clients would ignore or reject it);
//   - CAT-BUILD-05: a package path listed in the previous index.json, or
//     present on disk, that would get other bytes.
//
// Rebuilding the exact same index with the same serial is allowed, so build
// can be rerun. It only reads.
func (b *Bundle) CheckPrev(prevDir string) error {
	prev, err := readPrev(prevDir)
	if err != nil {
		return err
	}
	if prev.serial > b.Serial {
		return newErr(CodeBuildSerial, nil, fmt.Sprintf("上次发布的序号是 %d，这次的 %d 更小，客户端会当回退拒收；--serial 要大于 %d", prev.serial, b.Serial, prev.serial))
	}
	if prev.serial == b.Serial && prev.index != nil && !bytes.Equal(prev.index, b.Index) {
		return newErr(CodeBuildSerial, nil, fmt.Sprintf("序号 %d 已经发布过，内容和这次不同；已同步过的客户端看不到新内容，--serial 要大于 %d", b.Serial, b.Serial))
	}
	if prev.serial == b.Serial && prev.index == nil && prev.hasVersion {
		return newErr(CodeBuildSerial, nil, fmt.Sprintf("上次发布的 version.txt 已经是 %d，但读不到它的 index.json，无法确认内容相同；--serial 要大于 %d", b.Serial, b.Serial))
	}
	paths := make([]string, 0, len(b.Packages))
	for path := range b.Packages {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		data := b.Packages[path]
		sum := sha256Hex(data)
		if old, ok := prev.packages[path]; ok && old != sum {
			return newErr(CodeBuildPackage, nil, fmt.Sprintf("%s 上次发布的 sha256 是 %s，这次是 %s：改了应用目录就要升 release.version，已发布的包不能换内容", path, old, sum))
		}
		old, err := os.ReadFile(filepath.Join(prevDir, filepath.FromSlash(path)))
		if err == nil && !bytes.Equal(old, data) {
			return newErr(CodeBuildPackage, nil, fmt.Sprintf("%s 已存在且内容不同：改了应用目录就要升 release.version，已发布的包不能换内容", path))
		}
	}
	return nil
}

type prevPublish struct {
	serial     int64             // highest of version.txt and index.json (0: none)
	hasVersion bool              // version.txt was there
	index      []byte            // index.json bytes (nil: none)
	packages   map[string]string // package path → sha256 from index.json
}

// readPrev reads version.txt and index.json of a previous publish. A missing
// directory or file is not an error; an unreadable one is (CAT-BUILD-04),
// because guessing "no previous publish" could let a lower serial through.
func readPrev(dir string) (prevPublish, error) {
	p := prevPublish{packages: map[string]string{}}
	if dir == "" {
		return p, nil
	}
	if v, err := os.ReadFile(filepath.Join(dir, VersionFile)); err == nil {
		n, err := parseSerial(v)
		if err != nil {
			return p, newErr(CodeBuildSerial, err, "上次发布的 version.txt 读不懂："+filepath.Join(dir, VersionFile))
		}
		p.serial, p.hasVersion = n, true
	} else if !os.IsNotExist(err) {
		return p, newErr(CodeBuildSerial, err, "读不了上次发布的 version.txt")
	}
	if b, err := os.ReadFile(filepath.Join(dir, IndexFile)); err == nil {
		idx, err := ParseIndex(b)
		if err != nil {
			return p, newErr(CodeBuildSerial, err, "上次发布的 index.json 读不懂："+filepath.Join(dir, IndexFile))
		}
		p.index = b
		if idx.Serial > p.serial {
			p.serial = idx.Serial
		}
		for _, a := range idx.Apps {
			p.packages[a.Package.Path] = a.Package.SHA256
		}
	} else if !os.IsNotExist(err) {
		return p, newErr(CodeBuildSerial, err, "读不了上次发布的 index.json")
	}
	return p, nil
}

// Write puts the bundle under outDir in upload order, each file via a
// temporary file and rename. It first runs CheckPrev(outDir): build on top of
// the previous publish directory so serials only go up and published packages
// never change (CAT-BUILD-04/05). Rewriting identical bytes is a no-op, so
// running it twice is safe. When the bundle is unsigned a stale
// index.json.sig is removed.
func (b *Bundle) Write(outDir string) ([]OutFile, error) {
	if err := b.CheckPrev(outDir); err != nil {
		return nil, err
	}
	files := b.Files()
	for _, f := range files {
		dst := filepath.Join(outDir, filepath.FromSlash(f.Path))
		if old, err := os.ReadFile(dst); err == nil && bytes.Equal(old, f.data) {
			continue
		}
		if err := writeAtomic(dst, f.data, 0o644); err != nil {
			return nil, newErr(CodeBuildWrite, err, "写文件失败："+f.Path)
		}
	}
	if b.Sig == nil {
		if err := os.Remove(filepath.Join(outDir, SigFile)); err != nil && !os.IsNotExist(err) {
			return nil, newErr(CodeBuildWrite, err, "删旧签名失败")
		}
	}
	return files, nil
}

// writeAtomic writes data to a temporary file next to dst and renames it.
func writeAtomic(dst string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		cleanup()
		return err
	}
	return nil
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
