package faults

// Every code written anywhere is in the catalog, and every code in the
// catalog is raised somewhere. The program's Go code and console sources are
// always scanned; the install scripts, the Skill, the README files, the user
// docs, the release notes and the website only when the whole repository is
// here (when only the Go module is, those parts skip with a log line: run the
// tests in a full checkout before merging).

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// anyUpper finds upper-case codes in any text.
var anyUpper = regexp.MustCompile(`\b(?:CAT-(?:INST|CONN|LINT|BUILD|KEY|VER|SYNC|LOAD|PKG)|SETUP-[A-Z]+|LAN-CONSOLE|INSTALL-[A-Z]+|UPGRADE-[A-Z]+|UNINSTALL-[A-Z]+|OPEN-APP|ID-KEY|GW-[A-Z]+)-\d{2}\b`)

// moduleRoot is the Go module (acornfox/).
const moduleRoot = "../.."

// sourceFile reports whether a module file is program source to scan.
func sourceFile(rel string) bool {
	switch {
	case strings.HasPrefix(rel, "internal/faults/"),
		strings.Contains(rel, "/testdata/"),
		strings.Contains(rel, "node_modules/"),
		strings.HasPrefix(rel, "internal/console/ui/"):
		return false
	case strings.HasSuffix(rel, "_test.go"), strings.Contains(rel, ".test."):
		return false
	case strings.HasSuffix(rel, ".go"):
		return true
	case strings.HasPrefix(rel, "web/console/src/") && (strings.HasSuffix(rel, ".ts") || strings.HasSuffix(rel, ".tsx")):
		return true
	}
	return false
}

// programFiles returns the program's source files (relative to the module)
// and their contents.
func programFiles(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(moduleRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(moduleRoot, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !sourceFile(rel) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 50 {
		t.Fatalf("只找到 %d 个源文件，目录不对", len(out))
	}
	return out
}

// repoFiles returns the install scripts (program) and the Skill and user
// docs (docs) when the whole repository is here; ok is false otherwise.
func repoFiles(t *testing.T) (program, docs map[string]string, ok bool) {
	t.Helper()
	root := filepath.Join(moduleRoot, "..")
	if _, err := os.Stat(filepath.Join(root, "scripts", "install", "install.sh")); err != nil {
		return nil, nil, false
	}
	program, docs = map[string]string{}, map[string]string{}
	scripts, _ := filepath.Glob(filepath.Join(root, "scripts", "install", "*.sh"))
	for _, p := range scripts {
		if strings.HasSuffix(p, ".test.sh") {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		program["scripts/install/"+filepath.Base(p)] = string(b)
	}
	for _, rel := range []string{"skills/acornfox/SKILL.md", "docs/capabilities.md", "docs/user-guide.md", "docs/quick-start.md", "README.md", "README.en.md"} {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		docs[rel] = string(b)
	}
	// Release notes, the CLI docs and the website (not every copy of the
	// repository has the website).
	for _, pattern := range []string{"docs/release-notes/*.md", "docs/acornfox/*.md", "website/*.html", "website/*/*.html"} {
		matches, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		for _, p := range matches {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			rel, _ := filepath.Rel(root, p)
			docs[filepath.ToSlash(rel)] = string(b)
		}
	}
	return program, docs, true
}

func inCatalog(code string) bool {
	_, ok := lookup(code)
	return ok
}

// Every upper-case code anywhere is in the catalog.
func TestEveryWrittenCodeInCatalog(t *testing.T) {
	files := programFiles(t)
	if prog, docs, ok := repoFiles(t); ok {
		for k, v := range prog {
			files[k] = v
		}
		for k, v := range docs {
			files[k] = v
		}
	} else {
		t.Log("只有 Go 模块、没有整个仓库：安装脚本、Skill、README、用户文档、发行说明、官网没扫（合并前要在完整的仓库里跑一次）")
	}
	// The Skill built into the program (same text as skills/acornfox).
	if b, err := os.ReadFile(filepath.Join(moduleRoot, "internal", "cli", "skilldata", "SKILL.md")); err == nil {
		files["internal/cli/skilldata/SKILL.md"] = string(b)
	} else {
		t.Fatal(err)
	}
	missing := map[string][]string{}
	for name, text := range files {
		for _, code := range anyUpper.FindAllString(text, -1) {
			if !inCatalog(code) {
				missing[code] = append(missing[code], name)
			}
		}
	}
	for code, where := range missing {
		sort.Strings(where)
		t.Errorf("%s 写在 %s，但不在故障目录里：在 internal/faults 补条目", code, strings.Join(dedupe(where), "、"))
	}
}

// Every upper-case code in the catalog is raised by the program or the
// scripts (docs alone do not count), unless it is reserved or sent by the
// download gateway.
func TestEveryCatalogCodeRaised(t *testing.T) {
	text := strings.Builder{}
	for _, v := range programFiles(t) {
		text.WriteString(v)
	}
	prog, _, haveRepo := repoFiles(t)
	for _, v := range prog {
		text.WriteString(v)
	}
	all := text.String()
	for _, f := range Catalog {
		if f.Reserved || f.Origin == "gateway" || f.Code == FallbackCode || lowerCode.MatchString(f.Code) {
			continue
		}
		if !haveRepo && (f.Group == "INSTALL" || f.Group == "UPGRADE") && !strings.Contains(all, f.Code) {
			continue // raised by the scripts, which are not here
		}
		if !strings.Contains(all, f.Code) {
			t.Errorf("%s 在目录里，但程序和脚本里没有地方报它：删掉条目或标 Reserved", f.Code)
		}
	}
}

// The lower-case codes. Every non-test Go file of the program is scanned for
// the literals that hand one to a user: Code: "…" (client.Diagnosis,
// publicconsole.Error, state.Diagnosis …) and x.Code = "…". The files that
// define the deploy and connect diagnosis codes as constants are scanned for
// those too (Code…, code…, Rollback… = "…"). Codes passed positionally to the
// API's error writers (writeError, lifecycleError) are the API's generic
// error codes; they are not faults and not scanned (see the package comment).
var constFiles = []string{
	"internal/runner/docker.go", "internal/cli/deploy.go", "internal/cli/commands.go", "internal/cli/rollback.go",
	"internal/client/diagnose.go", "internal/client/api.go", "internal/publicconsole/diag.go", "internal/state/rollback.go",
}

var (
	codeField = regexp.MustCompile(`\bCode\s*:\s*"([a-z][a-z_]*)"|\.Code\s*=\s*"([a-z][a-z_]*)"`)
	codeConst = regexp.MustCompile(`(?:\bCode\w*|\bcode\w*|\bRollback\w+)\s*(?::|=)\s*"([a-z][a-z_]*)"|\.Code\s*=\s*"([a-z][a-z_]*)"`)
)

// notFaults are lower-case codes found that are not faults, each with why.
// A user who meets one is handled by the fallback entry ANY-00: pass on the
// message and hint, change the command's own input or retry, ask before
// anything else.
var notFaults = map[string]string{
	"ok":              "一项检查通过了（公网控制台验证的结果）",
	"not_found":       "没有这个应用或部署：用法上的回答，按 message 改名字",
	"usage":           "命令用法不对（退出码 2）：按 message 和 --help 改命令",
	"unknown":         "出错时没有编号（命令行的兜底）：转述 message",
	"internal":        "接口的通用错误：服务端意外出错，message 是原文",
	"fault_unknown":   "acornfox faults 查不到编号时的回答本身",
	"refused":         "执行器只回给后台服务：执行器拒绝越权的请求，用户看到的是后台服务换过的编号",
	"invalid_request": "执行器只回给后台服务：请求格式不对，用户看到的是后台服务换过的编号",
	"network_in_use":  "执行器只回给后台服务：删应用时网络还有容器连着，用户看到的是后台服务的 network_removal_failed",
}

func TestDiagnosisCodesInCatalog(t *testing.T) {
	files := programFiles(t)
	found := map[string]string{}
	note := func(rel, text string, re *regexp.Regexp) {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			c := strings.Join(m[1:], "")
			if c != "" {
				found[c] = rel
			}
		}
	}
	for rel, text := range files {
		if strings.HasSuffix(rel, ".go") {
			note(rel, text, codeField)
		}
	}
	consts := append([]string{}, constFiles...)
	recon, _ := filepath.Glob(filepath.Join(moduleRoot, "internal", "reconcile", "*.go"))
	for _, p := range recon {
		if !strings.HasSuffix(p, "_test.go") {
			rel, _ := filepath.Rel(moduleRoot, p)
			consts = append(consts, filepath.ToSlash(rel))
		}
	}
	for _, rel := range consts {
		text, ok := files[rel]
		if !ok {
			t.Fatalf("%s 不在了：改 constFiles", rel)
		}
		note(rel, text, codeConst)
	}
	if len(found) < 60 {
		t.Fatalf("只找到 %d 个小写编号，扫描的写法不对", len(found))
	}
	for c, rel := range found {
		if _, skip := notFaults[c]; !skip && !inCatalog(c) {
			t.Errorf("诊断码 %s（%s）不在故障目录里：在 internal/faults 补条目；确实不是故障就加进 notFaults 并写明为什么", c, rel)
		}
	}
	for c, why := range notFaults {
		if why == "" {
			t.Errorf("notFaults 里的 %s 要写明为什么不是故障", c)
		}
		if _, ok := found[c]; !ok {
			t.Errorf("notFaults 里的 %s 已经没有地方用了，删掉", c)
		}
		if inCatalog(c) {
			t.Errorf("%s 既在目录里又在 notFaults 里，删掉一处", c)
		}
	}
	// Every lower-case entry is raised somewhere in the program.
	var all strings.Builder
	for _, v := range files {
		all.WriteString(v)
	}
	for _, f := range Catalog {
		if lowerCode.MatchString(f.Code) && !strings.Contains(all.String(), `"`+f.Code+`"`) {
			t.Errorf("诊断码 %s 在目录里，但程序里没有地方报它", f.Code)
		}
	}
}

func dedupe(s []string) []string {
	var out []string
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}
