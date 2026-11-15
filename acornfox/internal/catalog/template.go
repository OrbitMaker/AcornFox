package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The template engine: how a value written in a manifest becomes the value
// an app gets. It is the one implementation of {{…}} for the whole platform:
// the install engine renders config files, environment values, the start
// command, first-login values and every step with it, and lint checks
// exactly what it renders. Nothing else may implement its own {{…}}.
//
// Syntax (manifest templates):
//
//	{{ ref }}
//	{{ ref | transform "arg" … | transform … }}
//
// A ref is "<kind>.<name>" with a kind of RefKinds and the rest in lower
// case letters, digits and underscores ("params.admin_password",
// "platform.app.url", "uses.db.host"). Only that shape is replaced. Any
// other {{…}} (an app's own template syntax such as {{ $json.x }} or
// {{ .Values }}) is kept as written; lint gives a warning. \{{ writes a
// literal {{.
//
// Transforms (Transforms) take the value on their left; their arguments are
// always in double quotes (\" and \\ inside). They run in order. A value an
// escape is applied to (a config file's quoting, a URL path segment) is
// escaped after the transforms, except where json or yaml is written: those
// write a complete quoted value themselves.
//
// A condition (ParseCondition) is one of three forms: "ref" (has a value
// that is not false, 0 or empty), "ref == literal" and "ref != literal".
//
// The service type definitions' test templates are another, older format
// ({{host}}: an item of the type, no kind, no transform): FieldRefs and
// FillFields read them with the same scanner.

// RefKinds are the kinds a reference can have. params, platform, uses,
// self, saved, issued, consumer and issue are resolved today; requires and
// components are reserved for later formats (lint refuses them, the engine
// cannot resolve them).
var RefKinds = map[string]bool{
	"params": true, "platform": true, "uses": true, "requires": true, "components": true,
	"self": true, "saved": true, "issued": true, "consumer": true, "issue": true,
}

// ErrNoValue is what a lookup wraps when the reference is valid but has no
// value here (an optional service that is not connected). A default
// transform then applies, a condition counts as false; any other lookup
// error stops the render.
var ErrNoValue = errors.New("没有值")

// TemplateError is a template that cannot be read: Code is CodeTemplate
// (CAT-LINT-46: syntax) or CodeTransform (CAT-LINT-47: an unknown
// transform or wrong arguments). Text is the {{…}} as written.
type TemplateError struct {
	Code string
	Text string
	Msg  string
}

func (e *TemplateError) Error() string { return e.Msg }

// TransformError is a transform that failed while rendering (a map without
// a matching entry and without "*=…"). It never holds the value.
type TransformError struct {
	Text string
	Msg  string
}

func (e *TransformError) Error() string { return e.Msg }

// IsRenderError reports whether err is a template that cannot be read or a
// transform that failed (CAT-INST-39 at install), as opposed to a reference
// that has no value or does not exist.
func IsRenderError(err error) bool {
	var te *TemplateError
	var xe *TransformError
	return errors.As(err, &te) || errors.As(err, &xe)
}

// TemplateExpr is one reference written in a template.
type TemplateExpr struct {
	// Text is the {{…}} as written.
	Text string
	Ref  string
	// Transforms names the transforms, in order.
	Transforms []string
	calls      []tmplCall
}

// HasTransform reports whether the expression writes transform name.
func (e TemplateExpr) HasTransform(name string) bool {
	for _, t := range e.Transforms {
		if t == name {
			return true
		}
	}
	return false
}

// quotes reports whether the expression writes a complete quoted value (no
// escape is applied on top).
func (e TemplateExpr) quotes() bool { return e.HasTransform("json") || e.HasTransform("yaml") }

type tmplCall struct {
	name string
	args []string
}

type tmplPart struct {
	lit  string
	expr *TemplateExpr
}

// Template is a parsed template.
type Template struct {
	parts []tmplPart
	// Others are the {{…}} kept as written: what stands between the braces,
	// trimmed.
	Others []string
	// Bad are the references that could not be read (a lenient scan keeps
	// them as written instead of failing).
	Bad []*TemplateError
}

// Exprs returns the references of the template, in order.
func (t *Template) Exprs() []TemplateExpr {
	var out []TemplateExpr
	for _, p := range t.parts {
		if p.expr != nil {
			out = append(out, *p.expr)
		}
	}
	return out
}

// ParseTemplate reads a manifest template. A reference it cannot read is an
// error (*TemplateError); everything that is no reference is kept.
func ParseTemplate(s string) (*Template, error) {
	t := scanTemplate(s)
	if len(t.Bad) > 0 {
		return nil, t.Bad[0]
	}
	return t, nil
}

// ScanTemplate reads a template leniently: a reference that cannot be read
// is kept as written and listed in Bad. Lint uses it to report every
// problem of a template at once.
func ScanTemplate(s string) *Template { return scanTemplate(s) }

// Render fills the template in. lookup resolves one reference; escape (may
// be nil) is applied to each inserted value after its transforms, unless the
// expression writes json or yaml. On an error the output is what could be
// rendered, with the failing reference kept as written.
func (t *Template) Render(lookup func(ref string) (string, error), escape func(string) string) (string, error) {
	var b strings.Builder
	var firstErr error
	for _, p := range t.parts {
		if p.expr == nil {
			b.WriteString(p.lit)
			continue
		}
		v, err := p.expr.eval(lookup)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			b.WriteString(p.expr.Text)
			continue
		}
		if escape != nil && !p.expr.quotes() {
			v = escape(v)
		}
		b.WriteString(v)
	}
	return b.String(), firstErr
}

// RenderTemplate parses and renders s (see Template.Render).
func RenderTemplate(s string, lookup func(ref string) (string, error), escape func(string) string) (string, error) {
	t, err := ParseTemplate(s)
	if err != nil {
		return s, err
	}
	return t.Render(lookup, escape)
}

// TemplateRefs returns the references ("params.x", "platform.mirror.pypi")
// in a template, in order (those that can be read).
func TemplateRefs(tmpl string) []string {
	var out []string
	for _, e := range scanTemplate(tmpl).Exprs() {
		out = append(out, e.Ref)
	}
	return out
}

// TemplatePlaceholders returns what stands between the braces of every
// {{…}} of a template, trimmed, in order: references (as written, with
// their transforms) and everything else. It is for places that are no
// template at all (a readiness check's say, a health check's command): any
// {{…}} there is a mistake.
func TemplatePlaceholders(tmpl string) []string {
	var out []string
	for i := 0; i+1 < len(tmpl); i++ {
		if tmpl[i] != '{' || tmpl[i+1] != '{' {
			continue
		}
		if inner, ok := plainInner(tmpl, i); ok {
			out = append(out, inner)
		}
	}
	return out
}

// plainInner is what stands between the braces of a {{…}} at i that has no
// brace inside, trimmed, when it is not blank.
func plainInner(s string, i int) (string, bool) {
	j := strings.Index(s[i+2:], "}}")
	if j < 0 {
		return "", false
	}
	inner := s[i+2 : i+2+j]
	if strings.ContainsAny(inner, "{}") {
		return "", false
	}
	inner = strings.TrimSpace(inner)
	return inner, inner != ""
}

var (
	refShapeRe  = regexp.MustCompile(`^[a-z]+\.[a-z0-9_]+(?:\.[a-z0-9_]+)*$`)
	callNameRe  = regexp.MustCompile(`^[a-z0-9]+`)
	fieldNameRe = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)
)

func scanTemplate(s string) *Template {
	t := &Template{}
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			t.parts = append(t.parts, tmplPart{lit: lit.String()})
			lit.Reset()
		}
	}
	for i := 0; i < len(s); {
		if s[i] == '\\' && strings.HasPrefix(s[i+1:], "{{") {
			lit.WriteString("{{")
			i += 3
			continue
		}
		if !strings.HasPrefix(s[i:], "{{") {
			lit.WriteByte(s[i])
			i++
			continue
		}
		end, expr, bad, isRef := scanExpr(s, i)
		switch {
		case isRef && bad != nil:
			t.Bad = append(t.Bad, bad)
			lit.WriteString(s[i:end])
			i = end
		case isRef:
			flush()
			t.parts = append(t.parts, tmplPart{expr: expr})
			i = end
		default:
			if inner, ok := plainInner(s, i); ok {
				t.Others = append(t.Others, inner)
			}
			// Kept as written; the next brace may start a reference
			// ({{{params.x}}} keeps the outer braces, as it always did).
			lit.WriteByte('{')
			i++
		}
	}
	flush()
	return t
}

// scanExpr looks at the {{ at i. isRef is false when what follows is not a
// reference (no known kind and a dot): it is then kept as written. A
// reference runs to the first }} outside double quotes; end is past it.
func scanExpr(s string, i int) (end int, expr *TemplateExpr, bad *TemplateError, isRef bool) {
	j := i + 2
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	k := j
	for k < len(s) && s[k] >= 'a' && s[k] <= 'z' {
		k++
	}
	if k == j || k >= len(s) || s[k] != '.' || !RefKinds[s[j:k]] {
		return 0, nil, nil, false
	}
	inQuote := false
	close := -1
	for p := k; p < len(s); p++ {
		c := s[p]
		if inQuote {
			if c == '\\' {
				p++
			} else if c == '"' {
				inQuote = false
			}
			continue
		}
		if c == '"' {
			inQuote = true
			continue
		}
		if c == '\n' || c == '{' {
			break
		}
		if c == '}' && p+1 < len(s) && s[p+1] == '}' {
			close = p
			break
		}
	}
	if close < 0 {
		// Up to the end of the line, for the message.
		stop := strings.IndexByte(s[i:], '\n')
		if stop < 0 {
			stop = len(s) - i
		}
		text := s[i : i+stop]
		return i + 2, nil, &TemplateError{Code: CodeTemplate, Text: text,
			Msg: fmt.Sprintf("%s 没有配对的 }}：引用写成 {{种类.名字}}，变换的参数要用双引号括起来，引号要成对", shorten(text))}, true
	}
	text := s[i : close+2]
	e, err := parseExpr(text, s[i+2:close])
	if err != nil {
		return close + 2, nil, err, true
	}
	return close + 2, e, nil, true
}

func shorten(s string) string {
	r := []rune(s)
	if len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}

// parseExpr reads what stands between the braces of one reference.
func parseExpr(text, inner string) (*TemplateExpr, *TemplateError) {
	syntax := func(format string, a ...any) *TemplateError {
		return &TemplateError{Code: CodeTemplate, Text: text, Msg: text + " " + fmt.Sprintf(format, a...)}
	}
	segs, ok := splitPipes(inner)
	if !ok {
		return nil, syntax("里的引号没有成对")
	}
	ref := strings.TrimSpace(segs[0])
	if !refShapeRe.MatchString(ref) {
		return nil, syntax("里的引用 %q 写得不对：引用写成 种类.名字，全用小写字母、数字和下划线，例如 params.admin_password、platform.app.url", ref)
	}
	e := &TemplateExpr{Text: text, Ref: ref}
	for _, seg := range segs[1:] {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			return nil, syntax("里有一个空的变换：| 后面要写变换的名字")
		}
		name := callNameRe.FindString(seg)
		if name == "" {
			return nil, syntax("里的变换 %q 写得不对：| 后面先写变换的名字（小写字母），参数用双引号", seg)
		}
		rest := seg[len(name):]
		if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
			return nil, syntax("里的变换 %q 写得不对：名字和参数之间要有空格，参数用双引号，例如 map \"https=true\"", seg)
		}
		args, err := parseArgs(rest)
		if err != "" {
			return nil, syntax("里变换 %s 的参数%s", name, err)
		}
		if msg := checkTransform(name, args); msg != "" {
			return nil, &TemplateError{Code: CodeTransform, Text: text, Msg: text + " " + msg}
		}
		e.calls = append(e.calls, tmplCall{name: name, args: args})
		e.Transforms = append(e.Transforms, name)
	}
	return e, nil
}

// splitPipes splits at | outside double quotes.
func splitPipes(s string) ([]string, bool) {
	var out []string
	inQuote, start := false, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case inQuote && c == '\\':
			i++
		case c == '"':
			inQuote = !inQuote
		case !inQuote && c == '|':
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if inQuote {
		return nil, false
	}
	return append(out, s[start:]), true
}

// parseArgs reads blank-separated double-quoted arguments; it returns why
// not when it cannot.
func parseArgs(s string) ([]string, string) {
	var out []string
	for i := 0; ; {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i == len(s) {
			return out, ""
		}
		if s[i] != '"' {
			return nil, "要用双引号括起来（例如 \"https=true\"）"
		}
		var b strings.Builder
		i++
		closed := false
		for i < len(s) {
			c := s[i]
			if c == '\\' {
				if i+1 >= len(s) || (s[i+1] != '"' && s[i+1] != '\\') {
					return nil, "里的反斜杠只能写成 \\\" 或 \\\\"
				}
				b.WriteByte(s[i+1])
				i += 2
				continue
			}
			if c == '"' {
				closed = true
				i++
				break
			}
			b.WriteByte(c)
			i++
		}
		if !closed {
			return nil, "的引号没有成对"
		}
		if i < len(s) && s[i] != ' ' && s[i] != '\t' {
			return nil, "之间要用空格隔开"
		}
		out = append(out, b.String())
	}
}

// Transforms are the transforms a template may write, with what they do.
// The table is fixed in the program; a manifest cannot add to it.
var Transforms = []TransformDef{
	{"default", "没有值或值是空的时，用参数给的值：default \"值\""},
	{"urlencode", "按网址的规则编码（字母、数字和 - _ . ~ 以外的字符都写成 %XX，空格是 %20），放进网址的任何一段都安全，例如连接串里的密码"},
	{"b64", "Base64 编码（标准字母表，带 = 补齐）"},
	{"json", "写成 JSON 里完整的字符串，带双引号；配置文件里这一处不再另外转义"},
	{"yaml", "写成 YAML 里完整的双引号字符串；配置文件里这一处不再另外转义"},
	{"shell", "写成 sh 里完整的单引号字符串，放进命令行脚本里安全"},
	{"lower", "转成小写"},
	{"upper", "转成大写"},
	{"replace", "把第一个参数换成第二个参数（全部换）：replace \",\" \" \""},
	{"map", "按对照表换值：map \"https=true\" \"http=false\"；\"*=值\" 是对不上时用的值；对不上又没写 * 时安装停下（CAT-INST-39）"},
	{"sha256", "取 SHA-256 摘要，写成 64 位小写十六进制"},
}

// TransformDef is one row of Transforms.
type TransformDef struct {
	Name string `json:"name"`
	Does string `json:"does"`
}

func knownTransform(name string) bool {
	for _, t := range Transforms {
		if t.Name == name {
			return true
		}
	}
	return false
}

func transformNames() string {
	names := make([]string, 0, len(Transforms))
	for _, t := range Transforms {
		names = append(names, t.Name)
	}
	return strings.Join(names, "、")
}

// checkTransform says what is wrong with one transform and its arguments,
// "" when nothing is.
func checkTransform(name string, args []string) string {
	if !knownTransform(name) {
		return fmt.Sprintf("里没有 %s 这种变换。能用的变换：%s", name, transformNames())
	}
	want := map[string]int{"default": 1, "replace": 2}[name]
	switch {
	case name == "map":
		if len(args) == 0 {
			return "里的 map 要写对照表，例如 map \"https=true\" \"http=false\""
		}
		seen := map[string]bool{}
		for _, a := range args {
			k, _, ok := strings.Cut(a, "=")
			if !ok {
				return fmt.Sprintf("里 map 的参数 %q 要写成 \"键=值\"（\"*=值\" 是对不上时用的值）", a)
			}
			if seen[k] {
				return fmt.Sprintf("里 map 的键 %q 写了两次", k)
			}
			seen[k] = true
		}
	case len(args) != want:
		if want == 0 {
			return fmt.Sprintf("里的 %s 不带参数", name)
		}
		return fmt.Sprintf("里的 %s 要 %d 个参数（用双引号），写了 %d 个", name, want, len(args))
	case name == "replace" && args[0] == "":
		return "里 replace 的第一个参数（要换掉的）不能是空的"
	}
	return ""
}

func (e *TemplateExpr) eval(lookup func(string) (string, error)) (string, error) {
	v, err := lookup(e.Ref)
	if err != nil {
		if !errors.Is(err, ErrNoValue) || !e.HasTransform("default") {
			return "", err
		}
		v = ""
	}
	for _, c := range e.calls {
		v, err = c.apply(v)
		if err != nil {
			return "", &TransformError{Text: e.Text, Msg: e.Text + " " + err.Error()}
		}
	}
	return v, nil
}

func (c tmplCall) apply(v string) (string, error) {
	switch c.name {
	case "default":
		if v == "" {
			return c.args[0], nil
		}
		return v, nil
	case "urlencode":
		return percentEncode(v), nil
	case "b64":
		return base64.StdEncoding.EncodeToString([]byte(v)), nil
	case "json", "yaml":
		// A JSON string is a YAML double-quoted scalar as well: every escape
		// JSON writes (\", \\, \n, \uXXXX …) means the same in YAML.
		return jsonQuote(v), nil
	case "shell":
		return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'", nil
	case "lower":
		return strings.ToLower(v), nil
	case "upper":
		return strings.ToUpper(v), nil
	case "replace":
		return strings.ReplaceAll(v, c.args[0], c.args[1]), nil
	case "map":
		fallback, have := "", false
		for _, a := range c.args {
			k, val, _ := strings.Cut(a, "=")
			if k == "*" {
				fallback, have = val, true
				continue
			}
			if k == v {
				return val, nil
			}
		}
		if have {
			return fallback, nil
		}
		keys := make([]string, 0, len(c.args))
		for _, a := range c.args {
			k, _, _ := strings.Cut(a, "=")
			keys = append(keys, k)
		}
		return "", fmt.Errorf("的 map 没有对上的项：取到的值不是 %s 中的任何一个，也没写 \"*=值\"（对不上时用的值）", strings.Join(keys, "、"))
	case "sha256":
		sum := sha256.Sum256([]byte(v))
		return hex.EncodeToString(sum[:]), nil
	}
	return "", fmt.Errorf("没有 %s 这种变换", c.name)
}

// percentEncode writes every byte except the unreserved characters of RFC
// 3986 (letters, digits, - _ . ~) as %XX.
func percentEncode(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&15])
	}
	return b.String()
}

func jsonQuote(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(buf.String(), "\n")
}

// ---- conditions ----

// Condition is an "if" of an env entry or a config file.
type Condition struct {
	Text string `json:"text"`
	Ref  string `json:"ref"`
	// Op is "" (the value is true), "==" or "!=".
	Op  string `json:"op,omitempty"`
	Lit string `json:"literal,omitempty"`
}

var condLiteralRe = regexp.MustCompile(`^[^\s"'{}|=!]+$`)

// ParseCondition reads an if (CAT-LINT-48 when it cannot): "ref",
// "ref == literal" or "ref != literal". The literal is one word, without
// quotes.
func ParseCondition(s string) (Condition, error) {
	c := Condition{Text: s}
	bad := func(why string) (Condition, error) {
		return Condition{}, &TemplateError{Code: CodeIf, Text: s, Msg: fmt.Sprintf("if: %q %s。只认三种写法：'引用'（有值且不是 false、0、空的时候成立）、'引用 == 值'、'引用 != 值'，引用不带 {{ }}，例如 'platform.region == cn'", s, why)}
	}
	ref := strings.TrimSpace(s)
	for _, op := range []string{"==", "!="} {
		if l, r, ok := strings.Cut(ref, op); ok {
			c.Op, ref = op, strings.TrimSpace(l)
			c.Lit = strings.TrimSpace(r)
			if !condLiteralRe.MatchString(c.Lit) {
				return bad(fmt.Sprintf("里 %s 右边的值 %q 写得不对：要是一个词，不带引号和空格", op, c.Lit))
			}
			break
		}
	}
	if strings.Contains(ref, "{{") {
		return bad("里的引用不要写 {{ }}")
	}
	kind, _, _ := strings.Cut(ref, ".")
	if !refShapeRe.MatchString(ref) || !RefKinds[kind] {
		return bad(fmt.Sprintf("里的引用 %q 写得不对：要写成 种类.名字（params.名字、platform.名字、uses.需要名.项）", ref))
	}
	c.Ref = ref
	return c, nil
}

// Eval works the condition out. A reference that has no value here
// (ErrNoValue) makes it false, whatever its form; any other lookup error is
// returned.
func (c Condition) Eval(lookup func(string) (string, error)) (bool, error) {
	v, err := lookup(c.Ref)
	if err != nil {
		if errors.Is(err, ErrNoValue) {
			return false, nil
		}
		return false, err
	}
	switch c.Op {
	case "==":
		return v == c.Lit, nil
	case "!=":
		return v != c.Lit, nil
	}
	return v != "" && v != "false" && v != "0", nil
}

// Exclusive reports whether two conditions can be shown never to hold at
// once: the same reference compared == with two different values, or == and
// != with the same value.
func (c Condition) Exclusive(d Condition) bool {
	if c.Ref != d.Ref || c.Op == "" || d.Op == "" {
		return false
	}
	if c.Op == "==" && d.Op == "==" {
		return c.Lit != d.Lit
	}
	return c.Op != d.Op && c.Lit == d.Lit
}

// ---- the service type definitions' test templates ----

// FieldRefs returns the item names of a service type's test template
// ({{host}}), in order.
func FieldRefs(tmpl string) []string {
	var out []string
	FillFields(tmpl, func(k string) string { out = append(out, k); return "" })
	return out
}

// FillFields fills a service type's test template: every {{name}} (letters,
// digits, _ and ., no kind, no transform) is replaced by value(name).
// Anything else is kept as written.
func FillFields(tmpl string, value func(name string) string) string {
	var b strings.Builder
	for i := 0; i < len(tmpl); {
		if strings.HasPrefix(tmpl[i:], "{{") {
			if j := strings.Index(tmpl[i+2:], "}}"); j >= 0 {
				inner := tmpl[i+2 : i+2+j]
				if name := strings.TrimSpace(inner); fieldNameRe.MatchString(name) {
					b.WriteString(value(name))
					i += j + 4
					continue
				}
			}
		}
		b.WriteByte(tmpl[i])
		i++
	}
	return b.String()
}
