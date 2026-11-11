package catalog

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func testKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// publish builds dir with serial, signs it with priv and writes it to a new
// temp directory.
func publish(t *testing.T, dir string, serial int64, priv ed25519.PrivateKey) string {
	t.Helper()
	b, err := Build(dir, serial)
	if err != nil {
		t.Fatal(err)
	}
	b.Sign(priv)
	out := t.TempDir()
	if _, err := b.Write(out); err != nil {
		t.Fatal(err)
	}
	return out
}

func readTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	m := map[string][]byte{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		b, err := os.ReadFile(p)
		m[filepath.ToSlash(rel)] = b
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBuildDeterministic(t *testing.T) {
	_, priv := testKey(t)
	src := copyTree(t)
	a := readTree(t, publish(t, src, 3, priv))
	// Touch every source file: mtimes must not leak into the output.
	_ = filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			_ = os.Chtimes(p, info.ModTime().Add(7777e9), info.ModTime().Add(7777e9))
			_ = os.Chmod(p, 0o600)
		}
		return nil
	})
	b := readTree(t, publish(t, src, 3, priv))
	if len(a) != len(b) {
		t.Fatalf("文件数不同：%d %d", len(a), len(b))
	}
	for k, v := range a {
		if !bytes.Equal(v, b[k]) {
			t.Fatalf("%s 两次打包字节不同", k)
		}
	}
	for _, want := range []string{"version.txt", "index.json", "index.json.sig", "apps/new-api/v1.0.0-rc.48.tar.gz"} {
		if _, ok := a[want]; !ok {
			t.Fatalf("缺 %s；有 %v", want, keys(a))
		}
	}
	if string(a["version.txt"]) != "3\n" {
		t.Fatalf("version.txt = %q", a["version.txt"])
	}

	// index.json content.
	idx, err := ParseIndex(a["index.json"])
	if err != nil {
		t.Fatal(err)
	}
	if idx.Serial != 3 || len(idx.Apps) != 2 || len(idx.Recipes) != 3 || idx.Apps[0].ID != "hermes-agent" {
		t.Fatalf("index = %+v", idx)
	}
	var na IndexApp
	for _, ap := range idx.Apps {
		if ap.ID == "new-api" {
			na = ap
		}
	}
	if na.SizeMB != 78 || na.Package.Path != "apps/new-api/v1.0.0-rc.48.tar.gz" || na.Package.Size != int64(len(a[na.Package.Path])) || na.Package.SHA256 != sha256Hex(a[na.Package.Path]) {
		t.Fatalf("new-api = %+v", na)
	}
	var raw map[string]any
	_ = json.Unmarshal(a["index.json"], &raw)
	app0 := raw["apps"].([]any)[0].(map[string]any)
	for _, f := range []string{"id", "version", "name", "summary", "category", "keywords", "tags", "min_platform", "status", "memory_mb", "platforms", "size_mb", "delivery", "uses", "provides", "package"} {
		if _, ok := app0[f]; !ok {
			t.Fatalf("index.json 的应用缺字段 %s", f)
		}
	}

	// Package content: sorted names under <id>/, fixed metadata.
	zr, err := gzip.NewReader(bytes.NewReader(a[na.Package.Path]))
	if err != nil {
		t.Fatal(err)
	}
	if !zr.ModTime.IsZero() || zr.Name != "" {
		t.Fatalf("gzip 头带了时间或文件名：%v %q", zr.ModTime, zr.Name)
	}
	tr := tar.NewReader(zr)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.ModTime.Unix() != 0 || h.Uid != 0 || h.Gid != 0 || h.Mode != 0o644 || h.Uname != "" {
			t.Fatalf("%s 的元数据不固定：%+v", h.Name, h)
		}
		names = append(names, h.Name)
	}
	if strings.Join(names, ",") != "new-api/icon.svg,new-api/manifest.yaml" || !sort.StringsAreSorted(names) {
		t.Fatalf("包里的文件 = %v", names)
	}
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestBuildRefusesLintErrors(t *testing.T) {
	dir := copyTree(t)
	edit(t, filepath.Join(dir, "apps/new-api/manifest.yaml"), "id: new-api", "id: newapi")
	b, err := Build(dir, 1)
	if CodeOf(err) != CodeBuildLint || b != nil {
		t.Fatalf("err = %v", err)
	}
	var e *Error
	if !(errorsAs(err, &e) && len(e.Problems) > 0) {
		t.Fatalf("没带上 lint 问题：%v", err)
	}
	if _, err := Build(copyTree(t), 0); CodeOf(err) != CodeBuildInput {
		t.Fatalf("serial 0: %v", err)
	}
}

func errorsAs(err error, e **Error) bool {
	x, ok := err.(*Error)
	if ok {
		*e = x
	}
	return ok
}

func TestWriteIdempotentAndPackagesImmutable(t *testing.T) {
	_, priv := testKey(t)
	dir := copyTree(t)
	b, err := Build(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	b.Sign(priv)
	out := t.TempDir()
	if _, err := b.Write(out); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write(out); err != nil {
		t.Fatalf("再写一次应该没事：%v", err)
	}
	edit(t, filepath.Join(dir, "apps/new-api/manifest.yaml"), "统一管理 Key", "统一管理密钥")
	b2, err := Build(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	b2.Sign(priv)
	if _, err := b2.Write(out); CodeOf(err) != CodeBuildPackage {
		t.Fatalf("同版本换内容要拒绝：%v", err)
	}
	if v, _ := os.ReadFile(filepath.Join(out, VersionFile)); string(v) != "1\n" {
		t.Fatalf("拒绝后 version.txt 不该变：%q", v)
	}
}

func TestWriteSerialMustIncrease(t *testing.T) {
	_, priv := testKey(t)
	dir := copyTree(t)
	build := func(serial int64) *Bundle {
		t.Helper()
		b, err := Build(dir, serial)
		if err != nil {
			t.Fatal(err)
		}
		b.Sign(priv)
		return b
	}
	out := t.TempDir()
	if _, err := build(5).Write(out); err != nil {
		t.Fatal(err)
	}
	before := readTree(t, out)

	// Same serial, same content: a rerun, allowed.
	if _, err := build(5).Write(out); err != nil {
		t.Fatalf("同序号同内容重跑应该没事：%v", err)
	}
	// Same serial, other content (a recipe title only): refused.
	edit(t, filepath.Join(dir, "recipes/new-api/recipe.yaml"), "title: ", "title: 新")
	if _, err := build(5).Write(out); CodeOf(err) != CodeBuildSerial {
		t.Fatalf("同序号换内容要拒绝：%v", err)
	}
	// Lower serial: refused.
	if _, err := build(3).Write(out); CodeOf(err) != CodeBuildSerial {
		t.Fatalf("序号变小要拒绝：%v", err)
	}
	after := readTree(t, out)
	if len(after) != len(before) {
		t.Fatal("拒绝后输出目录变了")
	}
	for k, v := range before {
		if !bytes.Equal(after[k], v) {
			t.Fatalf("拒绝后 %s 变了", k)
		}
	}
	// Higher serial: fine.
	if _, err := build(6).Write(out); err != nil {
		t.Fatal(err)
	}
	if v, _ := os.ReadFile(filepath.Join(out, VersionFile)); string(v) != "6\n" {
		t.Fatalf("version.txt = %q", v)
	}
}

// A clean output directory checked against a previous publish (--prev) gets
// the same protection as building on top of it.
func TestCheckPrevCleanDir(t *testing.T) {
	_, priv := testKey(t)
	dir := copyTree(t)
	last := publish(t, dir, 8, priv)
	// Only the small files of the last publish, as downloaded from the site.
	prev := t.TempDir()
	for _, n := range []string{VersionFile, IndexFile} {
		b, _ := os.ReadFile(filepath.Join(last, n))
		if err := os.WriteFile(filepath.Join(prev, n), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := Build(dir, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.CheckPrev(prev); CodeOf(err) != CodeBuildSerial {
		t.Fatalf("序号小于上次：%v", err)
	}
	edit(t, filepath.Join(dir, "apps/new-api/manifest.yaml"), "统一管理 Key", "统一管理密钥")
	b, err = Build(dir, 9)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.CheckPrev(prev); CodeOf(err) != CodeBuildPackage {
		t.Fatalf("同版本换内容（只有上次的 index.json）：%v", err)
	}
	if err := b.CheckPrev(filepath.Join(t.TempDir(), "none")); err != nil {
		t.Fatalf("没有上次发布时不拦：%v", err)
	}
	if err := os.WriteFile(filepath.Join(prev, IndexFile), []byte("<html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := b.CheckPrev(prev); CodeOf(err) != CodeBuildSerial {
		t.Fatalf("读不懂上次的 index.json 要停下：%v", err)
	}
}

func TestSignVerify(t *testing.T) {
	pub, priv := testKey(t)
	other, _ := testKey(t)
	b, err := Build(copyTree(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	b.Sign(priv)
	if err := Verify(b.Index, b.Sig, []ed25519.PublicKey{pub}); err != nil {
		t.Fatal(err)
	}
	if err := Verify(b.Index, append(append([]byte{}, b.Sig...), '\n'), []ed25519.PublicKey{pub}); err != nil {
		t.Fatalf("签名后面带换行也要认：%v", err)
	}
	if err := Verify(b.Index, b.Sig, []ed25519.PublicKey{other, pub}); err != nil {
		t.Fatalf("多把公钥（轮换）里有一把对就行：%v", err)
	}
	if Verify(b.Index, b.Sig, []ed25519.PublicKey{other}) == nil {
		t.Fatal("错钥匙也通过了")
	}
	if Verify(b.Index, b.Sig, nil) == nil {
		t.Fatal("没有公钥也通过了")
	}
	for i := range b.Index {
		mod := append([]byte{}, b.Index...)
		mod[i] ^= 0x01
		if Verify(mod, b.Sig, []ed25519.PublicKey{pub}) == nil {
			t.Fatalf("改了第 %d 个字节还通过", i)
		}
	}
	badSig := append([]byte{}, b.Sig...)
	if badSig[3] == 'A' {
		badSig[3] = 'B'
	} else {
		badSig[3] = 'A'
	}
	if Verify(b.Index, badSig, []ed25519.PublicKey{pub}) == nil {
		t.Fatal("改了签名还通过")
	}
}

func TestVerifyDir(t *testing.T) {
	pub, priv := testKey(t)
	other, _ := testKey(t)
	out := publish(t, copyTree(t), 4, priv)
	res, err := VerifyDir(out, []ed25519.PublicKey{pub})
	if err != nil || res.Serial != 4 || res.Apps != 2 || res.Packages != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, err := VerifyDir(out, []ed25519.PublicKey{other}); CodeOf(err) != CodeVerifySig {
		t.Fatalf("错钥匙：%v", err)
	}
	ver := filepath.Join(out, VersionFile)
	_ = os.WriteFile(ver, []byte("5\n"), 0o644)
	if _, err := VerifyDir(out, []ed25519.PublicKey{pub}); CodeOf(err) != CodeVerifySerial {
		t.Fatalf("序号不一致：%v", err)
	}
	_ = os.WriteFile(ver, []byte("4\n"), 0o644)
	pkg := filepath.Join(out, "apps/new-api/v1.0.0-rc.48.tar.gz")
	b, _ := os.ReadFile(pkg)
	b[len(b)-1] ^= 1
	_ = os.WriteFile(pkg, b, 0o644)
	if _, err := VerifyDir(out, []ed25519.PublicKey{pub}); CodeOf(err) != CodeVerifyPackage {
		t.Fatalf("包被改：%v", err)
	}
	_ = os.Remove(filepath.Join(out, SigFile))
	if _, err := VerifyDir(out, []ed25519.PublicKey{pub}); CodeOf(err) != CodeVerifyFiles {
		t.Fatalf("缺签名：%v", err)
	}
}

func TestKeyFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key.pem")
	pub, err := GenerateKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("权限 = %v", st.Mode().Perm())
	}
	before, _ := os.ReadFile(path)
	if _, err := GenerateKeyFile(path); CodeOf(err) != CodeKeyExists {
		t.Fatalf("已存在要拒绝：%v", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("已存在的私钥被改了")
	}
	priv, err := LoadSigningKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if !pub.Equal(priv.Public()) {
		t.Fatal("读回的私钥和公钥对不上")
	}
	if p, err := ParsePublicKey(EncodePublicKey(pub)); err != nil || !p.Equal(pub) {
		t.Fatalf("公钥编码来回：%v", err)
	}
	if _, err := ParsePublicKey("abc"); CodeOf(err) != CodeKeyPublic {
		t.Fatalf("坏公钥：%v", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(path, 0o644)
		if _, err := LoadSigningKey(path); CodeOf(err) != CodeKeyPerm {
			t.Fatalf("权限过宽要拒绝：%v", err)
		}
	}
	junk := filepath.Join(t.TempDir(), "junk.pem")
	_ = os.WriteFile(junk, []byte("not a key"), 0o600)
	if _, err := LoadSigningKey(junk); CodeOf(err) != CodeKeyRead {
		t.Fatalf("坏私钥：%v", err)
	}
}

func TestFaultsCoverEveryCode(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range Faults {
		if seen[f.Code] || f.Meaning == "" || f.Action == "" {
			t.Fatalf("故障表有重复或空项：%+v", f)
		}
		seen[f.Code] = true
	}
	for _, c := range []string{CodeBuildLint, CodeBuildInput, CodeBuildWrite, CodeBuildSerial, CodeBuildPackage, CodeKeyRead, CodeKeyPerm, CodeKeyExists, CodeKeyPublic,
		CodeVerifyFiles, CodeVerifySig, CodeVerifySerial, CodeVerifyPackage, CodeSyncNoKeys, CodeSyncNetwork, CodeSyncResponse,
		CodeSyncSignature, CodeSyncMismatch, CodeSyncRollback, CodeSyncCache, CodeSyncAllFailed, CodeSyncBusy, CodeSyncGateway, CodeSyncDisconnected, CodeLoad,
		CodePkgAllFailed, CodePkgMismatch, CodePkgWrite, CodePkgPath} {
		if !seen[c] {
			t.Fatalf("%s 没写进 Faults", c)
		}
	}
}

// TestBuildMatchesPublishedIndex rebuilds the catalog that was published as
// serial 3 and compares index.json byte for byte with what was published
// then. The index carries each package's sha256 and size, so the packages
// are covered too. A catalog that uses nothing new must keep building to
// the same bytes: published packages never change and older clients read
// this exact format.
func TestBuildMatchesPublishedIndex(t *testing.T) {
	root := filepath.Join("testdata", "published-3")
	want, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(filepath.Join(root, "src"), 3)
	if err != nil {
		var e *Error
		if errorsAs(err, &e) {
			t.Fatalf("已发布的目录现在打不了包：%v %+v", err, e.Problems)
		}
		t.Fatal(err)
	}
	if !bytes.Equal(b.Index, want) {
		t.Fatalf("index.json 和序号 3 发布的不一样了：\n%s", b.Index)
	}
	if bytes.Contains(b.Index, []byte("recipes_v2")) || bytes.Contains(b.Index, []byte(`"min_platform": ""`)) {
		t.Fatal("没有新写法的目录，index.json 里不该出现新字段")
	}
	// The embedded snapshot holds the very packages that were published.
	idx, err := ParseIndex(want)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range idx.Apps {
		if sha256Hex(b.Packages[a.Package.Path]) != a.Package.SHA256 || int64(len(b.Packages[a.Package.Path])) != a.Package.Size {
			t.Fatalf("%s 打出来的包和发布的不一样", a.Package.Path)
		}
	}
}
