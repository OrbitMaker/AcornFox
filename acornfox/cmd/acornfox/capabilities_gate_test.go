package main

// The gate of `acornfox capabilities`: the command table in
// internal/capabilities must say what the program can really run.
//
//   - every command in the two dispatch tables (internal/cli/cli.go, main.go)
//     and every subcommand a command's own switch takes is in the table;
//   - the flags of a row are the ones its FlagSet declares (read from the
//     source, so a flag added to a command without a row fails here);
//   - every row is a command that exists;
//   - the fault codes the document lists are the fault catalog's.
//
// A command is added by adding its row in internal/capabilities/commands.go.

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/capabilities"
	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/cli"
	"github.com/acornfox/acornfox/internal/faults"
	"github.com/acornfox/acornfox/internal/platformvar"
)

const cliSrc = "../../internal/cli"

// parseDir parses the non-test Go files of dir.
func parseDir(t *testing.T, dir string) (*token.FileSet, []*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	return fset, files
}

func strLit(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	return s, err == nil
}

// dispatch is one command of a dispatch table and the function it calls.
type dispatch struct{ name, fn string }

// switchCommands reads the cases of the first `switch cmd` in function fn of
// files (the dispatch tables).
func switchCommands(files []*ast.File, fn, tag string) []dispatch {
	var out []dispatch
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name != fn {
				continue
			}
			ast.Inspect(fd, func(n ast.Node) bool {
				sw, ok := n.(*ast.SwitchStmt)
				if !ok || len(out) > 0 {
					return len(out) == 0
				}
				id, ok := sw.Tag.(*ast.Ident)
				if !ok || id.Name != tag {
					return true
				}
				for _, st := range sw.Body.List {
					cc := st.(*ast.CaseClause)
					callee := ""
					ast.Inspect(cc, func(m ast.Node) bool {
						if call, ok := m.(*ast.CallExpr); ok && callee == "" {
							switch fun := call.Fun.(type) {
							case *ast.SelectorExpr:
								callee = fun.Sel.Name
							case *ast.Ident:
								callee = fun.Name
							}
						}
						return true
					})
					for _, e := range cc.List {
						if s, ok := strLit(e); ok {
							out = append(out, dispatch{s, callee})
						}
					}
				}
				return false
			})
		}
	}
	return out
}

// subCommands reads the words a command function takes as its subcommand:
// the cases of a `switch sub` or `switch args[0]` and the comparisons
// `sub == "x"`, `args[0] == "x"`, `args[0] != "x"`.
// dynamicPrefixed: FlagSets named "<prefix>" + <variable> where the variable
// only takes these values (function:variable → the table rows it covers).
var dynamicPrefixed = map[string][]string{
	"macFlags:name":     {"mac install", "mac upgrade", "mac uninstall"},
	"macStartStop:verb": {"mac start", "mac stop"},
}

func subCommands(files []*ast.File, fn string) []string {
	isSubExpr := func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.Ident:
			return x.Name == "sub"
		case *ast.IndexExpr:
			id, ok := x.X.(*ast.Ident)
			return ok && id.Name == "args"
		}
		return false
	}
	seen := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name != fn {
				continue
			}
			ast.Inspect(fd, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.SwitchStmt:
					if x.Tag != nil && isSubExpr(x.Tag) {
						for _, st := range x.Body.List {
							for _, e := range st.(*ast.CaseClause).List {
								if s, ok := strLit(e); ok {
									seen[s] = true
								}
							}
						}
					}
				case *ast.BinaryExpr:
					if x.Op == token.EQL || x.Op == token.NEQ {
						if s, ok := strLit(x.Y); ok && isSubExpr(x.X) {
							seen[s] = true
						}
					}
				}
				return true
			})
		}
	}
	var out []string
	for s := range seen {
		if s == "" || strings.HasPrefix(s, "-") || s == "help" {
			continue
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// topFunctions maps each command to the function that handles it (and its
// subcommands). cmd/acornfox/main.go's own commands come first: the cli
// switch only names a stub that answers "this runs on this computer" for
// them.
func topFunctions(cliFiles, mainFiles []*ast.File) map[string]string {
	out := map[string]string{}
	for _, list := range [][]dispatch{switchCommands(mainFiles, "run", "cmd"), switchCommands(cliFiles, "mainWith", "cmd")} {
		for _, d := range list {
			if d.name == "help" || strings.HasPrefix(d.name, "-") || d.fn == "" {
				continue
			}
			if out[d.name] == "" {
				out[d.name] = d.fn
			}
		}
	}
	return out
}

// tablePaths is every path of the table and its aliases.
func tablePaths() map[string]bool {
	m := map[string]bool{}
	for _, c := range capabilities.Commands {
		m[c.Path] = true
		if i := strings.LastIndex(c.Path, " "); i >= 0 {
			for _, a := range c.Aliases {
				m[c.Path[:i+1]+a] = true
			}
		} else {
			for _, a := range c.Aliases {
				m[a] = true
			}
		}
	}
	return m
}

func TestCapabilityTableHasNoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range capabilities.Commands {
		if seen[c.Path] {
			t.Errorf("%s：表里写了两次", c.Path)
		}
		seen[c.Path] = true
		if c.Path != strings.TrimSpace(c.Path) || strings.Contains(c.Path, "  ") {
			t.Errorf("%q：路径里的空格不对", c.Path)
		}
		fl := map[string]bool{}
		for _, f := range c.Flags {
			if !strings.HasPrefix(f, "-") || fl[f] {
				t.Errorf("%s：参数 %q 写得不对或重复", c.Path, f)
			}
			fl[f] = true
		}
	}
}

// Every command of both dispatch tables, and every subcommand of a command,
// is in the table.
func TestCapabilitiesCoverEveryCommand(t *testing.T) {
	_, cliFiles := parseDir(t, cliSrc)
	_, mainFiles := parseDir(t, ".")
	have := tablePaths()

	// Top-level commands: the switch of mainWith, and the commands of main.go.
	cliCmds := switchCommands(cliFiles, "mainWith", "cmd")
	if len(cliCmds) < 30 {
		t.Fatalf("internal/cli/cli.go 的 switch cmd 只读到 %d 条，读法要改了", len(cliCmds))
	}
	mainCmds := switchCommands(mainFiles, "run", "cmd")
	if len(mainCmds) < 10 {
		t.Fatalf("cmd/acornfox/main.go 的 switch cmd 只读到 %d 条，读法要改了", len(mainCmds))
	}
	top := topFunctions(cliFiles, mainFiles) // command → function that takes its subcommands
	for _, d := range append(append([]dispatch{}, cliCmds...), mainCmds...) {
		if d.name == "help" || strings.HasPrefix(d.name, "-") {
			continue
		}
		hasRow := false
		for p := range have {
			if p == d.name || strings.HasPrefix(p, d.name+" ") {
				hasRow = true
			}
		}
		if !hasRow {
			t.Errorf("命令 %q 能运行（分发表里有），但 internal/capabilities/commands.go 的表里没有：加一行", d.name)
		}
	}
	// main.go's mainCommands map is the same set as its switch.
	for name := range mainCommands {
		found := false
		for p := range have {
			if p == name || strings.HasPrefix(p, name+" ") {
				found = true
			}
		}
		if !found {
			t.Errorf("命令 %q 在 main.go 的 mainCommands 里，表里没有", name)
		}
	}

	// Subcommands: what each command's own function takes.
	all := append(append([]*ast.File{}, cliFiles...), mainFiles...)
	for name, fn := range top {
		if fn == "" {
			continue
		}
		for _, sub := range subCommands(all, fn) {
			path := name + " " + sub
			if !have[path] {
				t.Errorf("子命令 %q 能运行（%s 里处理它），但表里没有：加一行 %q", path, fn, path)
			}
		}
	}
	// A second level that a function hands on to: `domain base …`.
	for _, d := range []struct{ top, fn string }{{"domain base", "cmdDomainBase"}} {
		if !have[d.top] {
			t.Errorf("表里没有 %q", d.top)
		}
		_ = d.fn
	}
}

// Every row is a command that exists.
func TestCapabilityRowsAreRealCommands(t *testing.T) {
	_, cliFiles := parseDir(t, cliSrc)
	_, mainFiles := parseDir(t, ".")
	all := append(append([]*ast.File{}, cliFiles...), mainFiles...)
	cmds := topFunctions(cliFiles, mainFiles)
	for _, c := range capabilities.Commands {
		words := strings.Fields(c.Path)
		fn, ok := cmds[words[0]]
		if !ok {
			t.Errorf("%s：不是能运行的命令（分发表里没有 %q）", c.Path, words[0])
			continue
		}
		if len(words) == 1 {
			continue
		}
		subs := subCommands(all, fn)
		// A second word that is a subcommand of its own function, or one
		// the first level hands on (domain base set → domain base).
		okSub := false
		for _, s := range subs {
			if s == words[1] {
				okSub = true
			}
		}
		if !okSub && !(words[0] == "install" && words[1] == "status") && !(words[0] == "apps" && len(subs) == 0) {
			t.Errorf("%s：%s 里找不到子命令 %q", c.Path, fn, words[1])
		}
	}
}

// flagSetDecl is one FlagSet a function builds and the flags it declares.
type flagSetDecl struct {
	fn     string
	name   string // the literal name, or the literal prefix
	prefix bool   // name is a prefix ("lan-console "+sub)
	dyn    string // the name is this variable (action, name)
	flags  map[string]bool
}

var flagMethods = map[string]int{ // method → index of the argument that names the flag
	"Bool": 0, "String": 0, "Int": 0, "Int64": 0, "Uint": 0, "Float64": 0, "Duration": 0,
	"Var": 1, "BoolVar": 1, "StringVar": 1, "IntVar": 1, "Int64Var": 1, "DurationVar": 1, "Float64Var": 1,
}

func flagSetsOf(files []*ast.File) []flagSetDecl {
	var out []flagSetDecl
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Name.Name == "catalogFlags" {
				continue
			}
			sets := map[string]*flagSetDecl{}
			var order []string
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok || len(as.Rhs) != 1 || len(as.Lhs) < 1 {
					return true
				}
				call, ok := as.Rhs[0].(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				callee := ""
				switch fun := call.Fun.(type) {
				case *ast.SelectorExpr:
					callee = fun.Sel.Name
					if x, ok := fun.X.(*ast.Ident); ok && x.Name == "flag" && callee != "NewFlagSet" {
						return true
					}
				}
				if callee != "NewFlagSet" && callee != "catalogFlags" {
					return true
				}
				id, ok := as.Lhs[0].(*ast.Ident)
				if !ok {
					return true
				}
				decl := &flagSetDecl{fn: fd.Name.Name, flags: map[string]bool{}}
				switch a := call.Args[0].(type) {
				case *ast.BasicLit:
					decl.name, _ = strLit(a)
				case *ast.BinaryExpr:
					decl.name, _ = strLit(a.X)
					decl.prefix = true
					// "mac " + verb: a name built from a known set of subcommands
					// (dynamicPrefixed) is checked against exactly those rows.
					if y, ok := a.Y.(*ast.Ident); ok {
						if _, known := dynamicPrefixed[fd.Name.Name+":"+y.Name]; known {
							decl.dyn = fd.Name.Name + ":" + y.Name
							decl.prefix = false
						}
					}
				case *ast.Ident:
					decl.dyn = a.Name
				}
				sets[id.Name] = decl
				order = append(order, id.Name)
				return true
			})
			if len(sets) == 0 {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				recv, ok := sel.X.(*ast.Ident)
				if !ok || sets[recv.Name] == nil {
					return true
				}
				idx, ok := flagMethods[sel.Sel.Name]
				if !ok || len(call.Args) <= idx {
					return true
				}
				if name, ok := strLit(call.Args[idx]); ok {
					if len(name) == 1 {
						name = "-" + name
					} else {
						name = "--" + name
					}
					if name != "--json" { // the global one: the table lists it per command
						sets[recv.Name].flags[name] = true
					}
				}
				return true
			})
			for _, k := range order {
				out = append(out, *sets[k])
			}
		}
	}
	return out
}

func flagsOfRow(c capabilities.Command) map[string]bool {
	m := map[string]bool{}
	for _, f := range c.Flags {
		if f != "--json" {
			m[f] = true
		}
	}
	return m
}

func setText(m map[string]bool) string {
	var s []string
	for k := range m {
		s = append(s, k)
	}
	sort.Strings(s)
	return strings.Join(s, " ")
}

// handParsed are the rows whose flags the command reads by hand, not with a
// FlagSet (so the source does not declare them): the gate can only check that
// the row exists. Each says why.
var handParsed = map[string]string{
	"faults":        "faults.go 自己逐个读参数",
	"sources":       "sources.go 自己逐个读参数",
	"domain add":    "domain.go 在循环里读 --yes、--no-reapply",
	"domain remove": "domain.go 在循环里读 --yes、--no-reapply",
}

// The flags of a row are the ones the command's FlagSet declares.
func TestCapabilityFlagsMatchTheCode(t *testing.T) {
	_, cliFiles := parseDir(t, cliSrc)
	_, mainFiles := parseDir(t, ".")
	sets := append(flagSetsOf(cliFiles), flagSetsOf(mainFiles)...)

	exact := map[string]map[string]bool{} // path → declared flags (union over FlagSets of that name)
	var families []flagSetDecl
	dynamic := map[string][]string{"action": {"stop", "start", "restart"}}
	for k, v := range dynamicPrefixed {
		dynamic[k] = v
	}
	for _, s := range sets {
		switch {
		case s.dyn != "":
			if paths, ok := dynamic[s.dyn]; ok {
				families = append(families, flagSetDecl{name: strings.Join(paths, "|"), prefix: false, dyn: s.dyn, flags: s.flags, fn: s.fn})
			}
		case s.prefix:
			families = append(families, s)
		default:
			if exact[s.name] == nil {
				exact[s.name] = map[string]bool{}
			}
			for f := range s.flags {
				exact[s.name][f] = true
			}
		}
	}

	checked := map[string]bool{}
	for _, c := range capabilities.Commands {
		row := flagsOfRow(c)
		if want, ok := exact[c.Path]; ok {
			checked[c.Path] = true
			if setText(want) != setText(row) {
				t.Errorf("%s：表里写的参数 [%s] 和代码里的 FlagSet 声明的 [%s] 不一样", c.Path, setText(row), setText(want))
			}
			continue
		}
		// A family: "lan-console <sub>", "backup <sub>", "stop|start|restart".
		matched := false
		for _, fam := range families {
			in := false
			if fam.dyn != "" {
				for _, p := range strings.Split(fam.name, "|") {
					in = in || p == c.Path
				}
			} else {
				in = strings.HasPrefix(c.Path, fam.name)
			}
			if !in {
				continue
			}
			matched = true
			for f := range row {
				if !fam.flags[f] {
					t.Errorf("%s：表里写了参数 %s，代码里 %s 的 FlagSet 没有声明它", c.Path, f, fam.fn)
				}
			}
		}
		if matched {
			continue
		}
		if len(row) > 0 {
			if _, ok := handParsed[c.Path]; !ok && !c.Internal {
				t.Errorf("%s：表里写了参数 [%s]，但源码里找不到它的 FlagSet；是手工读参数的就写进测试里的 handParsed 并说明", c.Path, setText(row))
			}
		}
	}

	// A family declares nothing the table does not know.
	for _, fam := range families {
		union := map[string]bool{}
		for _, c := range capabilities.Commands {
			in := false
			if fam.dyn != "" {
				for _, p := range strings.Split(fam.name, "|") {
					in = in || p == c.Path
				}
			} else {
				in = strings.HasPrefix(c.Path, fam.name)
			}
			if in {
				for f := range flagsOfRow(c) {
					union[f] = true
				}
			}
		}
		for f := range fam.flags {
			if !union[f] {
				t.Errorf("%s 的 FlagSet 声明了参数 %s，表里 %q 开头的行都没写它", fam.fn, f, fam.name)
			}
		}
	}
	// A FlagSet with the name of a row but no row of that name.
	have := tablePaths()
	for name := range exact {
		if !have[name] {
			t.Errorf("代码里有名叫 %q 的 FlagSet，表里没有这一行", name)
		}
	}
}

func TestCapabilityDocFaultsMatchCatalog(t *testing.T) {
	doc := capabilities.Build(cli.Version, cli.SkillCorpus())
	want := map[string]bool{}
	for _, f := range faults.Catalog {
		want[f.Code] = true
	}
	if len(doc.Faults) != len(want) {
		t.Fatalf("清单里 %d 个故障编号，故障目录里 %d 个", len(doc.Faults), len(want))
	}
	last := ""
	for _, c := range doc.Faults {
		if !want[c] {
			t.Errorf("%s 在清单里，不在故障目录里", c)
		}
		if c <= last {
			t.Errorf("故障编号没排序或重复：%s 在 %s 后面", c, last)
		}
		last = c
	}
	// Every code a row's command can raise is one the catalog knows is
	// covered by the faults tests (internal/faults); here: the machine ones.
	for _, c := range []string{"MACHINE-01", "MACHINE-02", "MACHINE-03"} {
		if !want[c] {
			t.Errorf("%s 不在故障目录里", c)
		}
	}
}

func TestCapabilityDocParts(t *testing.T) {
	doc := capabilities.Build("9.9.9", "x <!-- 套路: llm-key --> y <!-- 套路: llm-key -->")
	if doc.Schema != 1 || doc.Version != "9.9.9" {
		t.Fatalf("schema/version: %+v", doc)
	}
	if len(doc.SkillPatterns) != 1 || doc.SkillPatterns[0] != "llm-key" {
		t.Errorf("skill_patterns 读成 %v", doc.SkillPatterns)
	}
	// platform_vars is the catalog's table.
	if len(doc.PlatformVars) != len(platformvar.Table()) || !contains(doc.PlatformVars, "platform.tz") {
		t.Errorf("platform_vars：%v", doc.PlatformVars)
	}
	// service_types are the built-in snapshot's.
	if !contains(doc.ServiceTypes, "llm_gateway") {
		t.Errorf("service_types：%v", doc.ServiceTypes)
	}
	// manifest_features is the catalog's table.
	if len(doc.ManifestFeatures) != len(catalog.Features()) {
		t.Errorf("manifest_features %d 项，catalog.Features %d 项", len(doc.ManifestFeatures), len(catalog.Features()))
	}
	// Every "since" of a command is empty, the first release's (SinceNext) or a later explicit one.
	maxFeature := "0.0.0"
	for _, f := range catalog.Features() {
		if catalog.CompareVersions(f.Since, maxFeature) > 0 {
			maxFeature = f.Since
		}
	}
	if catalog.CompareVersions(capabilities.SinceNext, maxFeature) < 0 {
		t.Errorf("SinceNext %s 比清单格式功能里最高的版本 %s 还低：要一起改", capabilities.SinceNext, maxFeature)
	}
	for _, c := range doc.Commands {
		// 0.7.0 之后新加的命令写明自己的版本（c(…, "since:0.7.1")）：不能早于 SinceNext。
		if c.Since != "" && catalog.CompareVersions(c.Since, capabilities.SinceNext) < 0 {
			t.Errorf("%s 的 since 是 %s，只能是空，或不早于 %s", c.Path, c.Since, capabilities.SinceNext)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// The general Skill registers the pattern the capability list names.
func TestCapabilityGeneralSkillRegistersLLMKey(t *testing.T) {
	doc := capabilities.Build(cli.Version, cli.SkillCorpus())
	if !contains(doc.SkillPatterns, "llm-key") {
		t.Fatalf("通用 Skill 里要登记示范套路 llm-key（<!-- 套路: llm-key -->），现在读到 %v", doc.SkillPatterns)
	}
}

// The command's output: ok first, the same document, no server contacted.
func TestCapabilitiesCommandOutput(t *testing.T) {
	var out, errb bytes.Buffer
	if rc := run([]string{"--json", "capabilities"}, strings.NewReader(""), &out, &errb); rc != 0 {
		t.Fatalf("rc %d: %s %s", rc, out.String(), errb.String())
	}
	if !strings.HasPrefix(out.String(), `{"ok":true,"schema":1,"version":`) {
		t.Fatalf("开头不对：%.80s", out.String())
	}
	var got struct {
		OK       bool                   `json:"ok"`
		Commands []capabilities.Command `json:"commands"`
		Faults   []string               `json:"faults"`
		Features []capabilities.Feature `json:"manifest_features"`
		Patterns []string               `json:"skill_patterns"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || len(got.Commands) != len(capabilities.Commands) || len(got.Faults) == 0 || len(got.Features) == 0 {
		t.Fatalf("内容不全：%d 条命令 %d 个编号 %d 项功能", len(got.Commands), len(got.Faults), len(got.Features))
	}
	// Written after the command too.
	out.Reset()
	if rc := run([]string{"capabilities", "--json"}, strings.NewReader(""), &out, &errb); rc != 0 || !strings.HasPrefix(out.String(), `{"ok":true`) {
		t.Fatalf("命令后写 --json：%d %.80s", rc, out.String())
	}
	// A word it does not know is a usage error in JSON.
	out.Reset()
	if rc := run([]string{"--json", "capabilities", "--nope"}, strings.NewReader(""), &out, &errb); rc != 2 || !strings.Contains(out.String(), `"code":"usage"`) {
		t.Fatalf("多余参数：%d %s", rc, out.String())
	}
	// Plain text lists commands.
	out.Reset()
	if rc := run([]string{"capabilities"}, strings.NewReader(""), &out, &errb); rc != 0 || !strings.Contains(out.String(), "apps upgrade") {
		t.Fatalf("文字输出：%d %.200s", rc, out.String())
	}
}
