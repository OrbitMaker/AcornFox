package catalog

import (
	"sort"
	"strconv"
	"strings"
)

// What Lint checks about services (CAT-LINT-22 … 44, and 08 for the
// endpoint a service's steps go to).
//
// Against the catalog's service type definitions (service-types/):
//   - every uses[].kind and provides[].kind is defined (22); a catalog
//     without service-types/ defines none, so a manifest that needs or
//     offers a service is refused there;
//   - provides[].values gives only items the type has, and every required
//     item of the type has a source that is not empty (23);
//   - uses.<name>.<item> names an item of the type (23);
//   - the definition files themselves are good (25, servicetypes.go).
//
// Whatever the catalog has:
//   - a step uses saved.<key> only after an earlier step of the same list
//     saved it; revoke and check use issued.<key> only for keys the issue
//     steps save; a value written { issued: key } names such a key; self,
//     saved, issued, consumer and issue.id are written only where they
//     exist (issue.id: the issue steps; issued.issue_id, which the platform
//     stores with every credential, is there in revoke, purge and check;
//     self: values and the revoke, purge, check and readiness check steps
//     of an entry that names its endpoint); no issue
//     step saves a value under issue_id itself; and every {{…}} in a step or
//     a value template is a reference that exists there, because the engine
//     stops at one it cannot resolve (24);
//   - what the format accepts and this platform does not carry out yet
//     (PendingFeatures; nothing at present) gets a warning, the engine
//     refuses it at install (26);
//   - an exec step keeps secrets out of its argument list: command and user
//     refer to no secret value (35), and no item of command, nor of a
//     health check's exec, holds a line break (36);
//   - issue_vars go with issue steps, are not named id or issue_id and do
//     not share a key with what an issue step saves (37); the steps refer
//     to them as issue.<key>, values, revoke and check as issued.<key> (24);
//   - endpoints, health checks and the exposure fit (38): a path health
//     check only on an http endpoint, a tcp primary endpoint only on an
//     internal app, self.internal_url (http://) not for a tcp endpoint, no
//     template in a health check's exec;
//   - nothing is written that needs reaching an app it cannot reach (39):
//     no http step when the app is internal or its primary endpoint is tcp,
//     no first_login for an internal app;
//   - an optional use says what the user loses without it (27), and its
//     items are taken by env entries that are optional as well, never by a
//     file template, an init step or first_login, which cannot do without
//     a value (28);
//   - a step retries only when it is a GET (29);
//   - finish_if is written in revoke and purge steps only (30);
//   - a config file written only when it is not there yet (overwrite:
//     never) refers to no uses.<name>.<item>: what a service hands out may
//     change on a reinstall, and such a file would keep the old value (31);
//   - an entry that issues a credential per consumer can take it back: it
//     has revoke steps (32);
//   - purge and on_check_fail are written only on an entry with issue steps,
//     on_check_fail only with check steps too (41);
//   - the readiness check's steps refer to no issued.*, consumer.* or
//     issue.*: they run with no consumer and no credential (42); its say is
//     a sentence shown as written, not blank, no {{…}} (43);
//   - a health check's expect_stdout has no blanks around it and no {{…}}
//     (44; written without exec, the schema refuses it);
//   - an entry with http steps names no endpoint but the primary one, the
//     only one the platform reaches from this server (08, lint.go).
//
// Where a step is pointed at (Problem.Path, the reasons of the lowest
// platform version, the variable report) it is by its name, else by its
// position counted from 0 like every other list in these paths.

// serviceTypeSet is the catalog's service types as Lint uses them.
type serviceTypeSet struct {
	types  map[string]ServiceType // the definitions that could be read, by kind
	broken map[string]bool        // kinds whose file has a problem (reported there, so not again per manifest)
}

func loadServiceTypeSet(dir string) (*serviceTypeSet, []Problem) {
	types, _, problems := ReadServiceTypes(dir)
	s := &serviceTypeSet{types: map[string]ServiceType{}, broken: map[string]bool{}}
	for _, t := range types {
		s.types[t.Kind] = t
	}
	for _, p := range problems {
		name := strings.TrimPrefix(p.File, ServiceTypesDir+"/")
		s.broken[strings.TrimSuffix(name, serviceTypeExt)] = true
	}
	return s, problems
}

// kinds returns the kinds that could be read, sorted (never nil).
func (s *serviceTypeSet) kinds() []string {
	out := make([]string, 0, len(s.types))
	for k := range s.types {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *serviceTypeSet) kindList() string {
	if len(s.types) == 0 {
		return "一个都没有"
	}
	return strings.Join(s.kinds(), "、")
}

// lookup returns the definition of kind when items can be checked against it.
func (s *serviceTypeSet) lookup(kind string) (ServiceType, bool) {
	t, ok := s.types[kind]
	return t, ok
}

// checkKind is CAT-LINT-22 for one uses[].kind or provides[].kind.
func (s *serviceTypeSet) checkKind(rel, path, kind string, add addFunc) {
	if s.broken[kind] {
		return
	}
	if _, ok := s.types[kind]; !ok {
		add(CodeServiceKind, LevelError, rel, path, "服务类型 %s 没有定义：目录的 %s/ 下没有 %s%s（现有：%s）。类型名写错了就改；确实是新类型，先加它的定义",
			kind, ServiceTypesDir, kind, serviceTypeExt, s.kindList())
	}
}

// checkField is CAT-LINT-23 for a reference uses.<name>.<item>.
func (s *serviceTypeSet) checkField(rel, path string, u Use, item string, add addFunc) {
	t, ok := s.lookup(u.Kind)
	if !ok {
		return
	}
	if _, ok := t.Field(item); !ok {
		add(CodeServiceFields, LevelError, rel, path, "uses.%s.%s：服务类型 %s 没有 %s 这一项（有：%s）", u.Name, item, u.Kind, item, strings.Join(t.FieldKeys(), "、"))
	}
}

// checkProvide is CAT-LINT-23 for one provides entry at path at.
func (s *serviceTypeSet) checkProvide(rel, at string, p Provide, add addFunc) {
	t, ok := s.lookup(p.Kind)
	if !ok {
		return
	}
	for _, k := range p.ValueKeys() {
		if _, ok := t.Field(k); !ok {
			add(CodeServiceFields, LevelError, rel, at+"/values/"+k, "服务类型 %s 没有 %s 这一项（有：%s）", p.Kind, k, strings.Join(t.FieldKeys(), "、"))
		}
	}
	var missing []string
	for _, k := range t.RequiredKeys() {
		// An empty template is no source (the schema refuses it as well).
		if v, ok := p.Values[k]; !ok || (v.Kind() == ValueTemplate && strings.TrimSpace(v.Template) == "") {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		add(CodeServiceFields, LevelError, rel, at+"/values", "服务类型 %s 必须有的项没有来路：%s。在 values 里给每一项写怎么来：一个模板、{ issued: 键 }（发给每个使用方）或 { ask: 一句话 }（让用户填）",
			p.Kind, strings.Join(missing, "、"))
	}
}

// refScope says which temporary values exist where a reference is written.
// The zero value is "none of them": env, files, first_login.
type refScope struct {
	steps    string          // "init", "issue", "revoke", "purge", "check" or "ready" inside a step; else ""
	values   bool            // inside a provides[].values template
	issueVar bool            // inside a provides[].issue_vars template
	self     bool            // values, revoke, purge, check or ready steps of an entry that names its endpoint: self.* exists
	consumer bool            // steps of a provided service: consumer.* exists
	saved    map[string]bool // keys saved by the earlier steps of the list; nil outside steps
	issued   map[string]bool // keys of a credential (issue saves, issue_vars, issue_id); nil outside revoke, purge, check and values
	noUses   bool            // uses.* cannot be referred to here
	env      bool            // an env entry's "from": the one place that can do without an optional use's value
	data     bool            // an entry of uninstall.data: volumes.<name> is what is written here
	// optionalOK: a reference whose missing value is covered (a default
	// transform, an if): an optional service's item may stand there.
	optionalOK bool
	// guard is the need an if of the config file names: its items may be
	// used in the file (no service, no file).
	guard string

	issueVars    map[string]bool // in issue steps: the keys of issue_vars (issue.<key>)
	savedSecret  map[string]bool // which saved keys are secret
	issuedSecret map[string]bool // which issued keys are secret
	issueSecret  map[string]bool // which issue_vars are secret
}

// unknownCode is the code for something that is no reference at all, or a
// kind of reference that does not exist: CAT-LINT-24 inside steps and
// provided values, CAT-LINT-06 elsewhere.
func (sc refScope) unknownCode() string {
	if sc.steps != "" || sc.values || sc.issueVar {
		return CodeStepRef
	}
	return CodeReference
}

// allowed says what may be referred to at this place.
func (sc refScope) allowed() string {
	switch {
	case sc.values:
		return "values 的模板里能引用的是 self.internal_url、self.host、self.port、params.<名>、platform.<名>，这条 provides 写了 issue 时还有 issued.<键>"
	case sc.issueVar:
		return "issue_vars 的模板里能引用的是 issue.id、consumer.name、consumer.id、params.<名>、platform.<名>"
	case sc.steps == "revoke" || sc.steps == "purge" || sc.steps == "check":
		return sc.steps + " 的步骤里能引用的是 params.<名>、platform.<名>、saved.<键>、issued.<键>、consumer.name、consumer.id，这条 provides 写了 endpoint 时还有 self.internal_url、self.host、self.port"
	case sc.steps == "ready":
		return "ready（就绪检查）的步骤里能引用的是 params.<名>、platform.<名>、saved.<键>，这条 provides 写了 endpoint 时还有 self.internal_url、self.host、self.port；它没有使用方、不带凭据，不能引用 issued、consumer、issue"
	case sc.steps == "issue":
		return "issue 的步骤里能引用的是 params.<名>、platform.<名>、saved.<键>、consumer.name、consumer.id、issue.id、issue.<issue_vars 里的键>"
	case sc.steps == "init":
		return "init 的步骤里能引用的是 params.<名>、platform.<名>、uses.<名>.<项>、saved.<键>"
	case sc.data:
		return "uninstall.data 里写的是 volumes.<数据卷名>"
	}
	return "这里能引用的是 params.<名>、platform.<名>、uses.<名>.<项>"
}

var (
	selfNames     = map[string]bool{"internal_url": true, "host": true, "port": true}
	consumerNames = map[string]bool{"name": true, "id": true}
)

// check returns what is wrong with a reference kind.rest (kind is self,
// saved, issued, consumer or issue) at this place, or "".
func (sc refScope) check(kind, rest string) string {
	ref := kind + "." + rest
	switch kind {
	case "self":
		switch {
		case !sc.values && sc.steps != "ready" && sc.steps != "revoke" && sc.steps != "purge" && sc.steps != "check":
			return ref + " 只能写在 provides 的 values 和 revoke、purge、check、ready 的步骤里。" + sc.allowed()
		case !sc.self:
			return "用了 " + ref + "，但这条 provides 没写 endpoint：写上它指的是哪个入口"
		case !selfNames[rest]:
			return "没有 " + ref + "：只有 self.internal_url、self.host、self.port"
		}
	case "saved":
		switch {
		case sc.saved == nil:
			return ref + " 只在步骤里有（前面的步骤 save 下来的值）。" + sc.allowed()
		case !sc.saved[rest]:
			return "引用了 " + ref + "，但这一组前面的步骤没有 save 过 " + rest + savedHint(sc.saved)
		}
	case "issued":
		switch {
		case sc.steps != "revoke" && sc.steps != "purge" && sc.steps != "check" && !sc.values:
			return ref + " 只在 provides 的 revoke、purge、check 步骤和 values 的模板里有（当初发给使用方的值）。" + sc.allowed()
		case !sc.issued[rest]:
			return "引用了 " + ref + "，但 issue 的步骤里没有 save 过 " + rest + "，issue_vars 里也没有" + savedHint(sc.issued)
		}
	case "consumer":
		switch {
		case !sc.consumer:
			return ref + " 只在 provides 的 issue、revoke、purge、check 步骤里有（正在接它的那个应用）。" + sc.allowed()
		case !consumerNames[rest]:
			return "没有 " + ref + "：只有 consumer.name、consumer.id"
		}
	case "issue":
		switch {
		case sc.steps != "issue" && !sc.issueVar:
			return ref + " 只在 provides 的 issue 步骤和 issue_vars 的模板里有（这次发凭据的随机编号和先定好的值）；revoke、purge、check、values 里用 issued.<键> 取回。" + sc.allowed()
		case ref == IssueIDRef:
		case sc.issueVar:
			return "issue_vars 的模板里只能引用 " + IssueIDRef + "，不能引用别的 issue_vars（" + ref + "）：它们生成的先后没有规定"
		case !sc.issueVars[rest]:
			defined := "（这条 provides 没写 issue_vars）"
			if len(sc.issueVars) > 0 {
				defined = "（issue_vars 里有：" + strings.Join(sortedKeys(sc.issueVars), "、") + "）"
			}
			return "没有 " + ref + "：issue 的步骤里能写 " + IssueIDRef + " 和 issue.<issue_vars 里的键>" + defined
		}
	}
	return ""
}

// stepsName names the list of steps sc is in, for messages.
func (sc refScope) stepsName() string {
	if sc.steps == "init" {
		return "init"
	}
	return "provides 的 " + sc.steps
}

// savedHint lists the keys that are there, for the end of a message.
func savedHint(keys map[string]bool) string {
	if len(keys) == 0 {
		return "（现在一个键都没存）"
	}
	return "（已存下的键：" + strings.Join(sortedKeys(keys), "、") + "）"
}

// sortedKeys returns the keys of a set, sorted.
func sortedKeys(set map[string]bool) []string {
	list := make([]string, 0, len(set))
	for k := range set {
		list = append(list, k)
	}
	sort.Strings(list)
	return list
}

// stepTemplates lists the texts of a step that are templates, in a fixed
// order: the path, the header values (by header name), the strings of the
// body, of the expected answer, of the "where" of its list saves (by key)
// and of its finish_if; for an exec step its command, user, env values (by
// name), stdin and the "where" of its list saves (by key).
func stepTemplates(st Step) []string {
	var out []string
	if e := st.Exec; e != nil {
		out = append(out, e.Command...)
		out = append(out, e.User)
		for _, k := range e.EnvKeys() {
			out = append(out, e.Env[k])
		}
		out = append(out, e.Stdin)
		for _, k := range sortedSaveKeys(e.Save) {
			out = append(out, jsonStrings(e.Save[k].Where)...)
		}
	}
	h := st.HTTP
	out = append(out, h.Path)
	names := make([]string, 0, len(h.Headers))
	for k := range h.Headers {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		out = append(out, h.Headers[k])
	}
	out = append(out, jsonStrings(h.JSON)...)
	out = append(out, jsonStrings(h.ExpectJSON)...)
	for _, k := range h.SaveKeys() {
		out = append(out, jsonStrings(h.Save[k].Where)...)
	}
	if h.FinishIf != nil {
		out = append(out, jsonStrings(h.FinishIf.JSON)...)
	}
	return out
}

// stepRefs lists the references a step makes, in the order of stepTemplates.
func stepRefs(st Step) []string {
	var out []string
	for _, tmpl := range stepTemplates(st) {
		out = append(out, TemplateRefs(tmpl)...)
	}
	return out
}

// refUse returns the name of the use a reference uses.<name>.<item> is to.
func refUse(ref string) (string, bool) {
	rest, ok := strings.CutPrefix(ref, "uses.")
	if !ok {
		return "", false
	}
	name, _, _ := strings.Cut(rest, ".")
	return name, name != ""
}

// provideRefs lists the references of a provided service with where they
// are: its value templates (by item), its issue_vars templates (by key),
// then its issue, revoke, purge and check steps and the steps of its
// readiness check (/ready/steps/<step>).
func provideRefs(i int, p Provide) []RefUse {
	at := "/provides/" + strconv.Itoa(i)
	var out []RefUse
	for _, k := range p.ValueKeys() {
		if v := p.Values[k]; v.Kind() == ValueTemplate {
			for _, r := range TemplateRefs(v.Template) {
				out = append(out, RefUse{r, at + "/values/" + k})
			}
		}
	}
	for _, k := range p.IssueVarKeys() {
		for _, r := range TemplateRefs(p.IssueVars[k].Template) {
			out = append(out, RefUse{r, at + "/issue_vars/" + k})
		}
	}
	for _, list := range []struct {
		name  string
		steps []Step
	}{{"issue", p.Issue}, {"revoke", p.Revoke}, {"purge", p.Purge}, {"check", p.Check}, {"ready/steps", p.ReadySteps()}} {
		for j, st := range list.steps {
			for _, r := range stepRefs(st) {
				out = append(out, RefUse{r, at + "/" + list.name + "/" + st.Label(j)})
			}
		}
	}
	return out
}
