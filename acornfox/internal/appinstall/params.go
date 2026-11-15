package appinstall

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/acornfox/acornfox/internal/catalog"
)

// Parameters of one app install.
//
// Values maps a parameter key to its value, as text (int and bool are kept
// as their text). Templates and "from" references read them as
// params.<key>. Besides the parameters, Values carry the engine's own
// entries under keys starting with "@" (platform variables, origins, the
// items of the services the app's needs are plugged into), which no
// parameter key can start with.

// Values are resolved parameters.
type Values map[string]any

const (
	charsetAlnum  = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	charsetHex    = "0123456789abcdef"
	minUserSecret = 8
)

// secretRand is where generated passwords and keys take their randomness
// from: crypto/rand. A test that pins the engine's output byte for byte
// swaps it for a fixed stream; nothing else may.
var secretRand io.Reader = rand.Reader

// generateSecret returns length characters from charset ("alnum" default,
// "hex") using crypto/rand.
func generateSecret(length int, charset string) (string, error) {
	cs := charsetAlnum
	if charset == "hex" {
		cs = charsetHex
	}
	if length < 1 || length > 256 {
		return "", fmt.Errorf("长度 %d 不对", length)
	}
	out := make([]byte, length)
	max := big.NewInt(int64(len(cs)))
	for i := range out {
		n, err := rand.Int(secretRand, max)
		if err != nil {
			return "", err
		}
		out[i] = cs[n.Int64()]
	}
	return string(out), nil
}

// singleLine rejects control characters (newlines included): every value goes
// into an environment variable, a config file or a JSON body.
func singleLine(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// resolveParams merges defaults, the user's values and generated secrets.
func resolveParams(m *catalog.Manifest, user map[string]any) (Values, error) {
	vals, _, err := resolveParamsOpt(m, user, false)
	return vals, err
}

// ParamMissing is a required parameter that has no value yet (the plan
// before an install lists them; the install itself refuses, CAT-INST-02).
type ParamMissing struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// resolveParamsOpt is resolveParams; with lenient a required parameter that
// has no value is not an error but listed in missing (and left empty), so
// that a plan can be shown before the form is filled in. With lenient a key
// that is no parameter of the app is still an error (CAT-INST-02, returned),
// but the values and what is missing are worked out as well, without it: a
// plan then lists both, the wrong key and the required parameter it was
// probably meant for.
func resolveParamsOpt(m *catalog.Manifest, user map[string]any, lenient bool) (Values, []ParamMissing, error) {
	var missing []ParamMissing
	known := map[string]catalog.Param{}
	for _, p := range m.Params {
		known[p.Key] = p
	}
	keys := make([]string, 0, len(user))
	for k := range user {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var unknown error
	for _, k := range keys {
		p, ok := known[k]
		if !ok {
			f := unknownParam(m, k)
			if !lenient {
				return nil, nil, f
			}
			if unknown == nil {
				unknown = f
			}
			continue
		}
		if p.Hidden {
			return nil, nil, fault(CodeBadParams, nil, "参数 %s 由平台生成，不能填", k)
		}
	}
	vals, missing, err := resolveKnownParams(m, user, lenient, missing)
	if err != nil {
		return nil, nil, err
	}
	return vals, missing, unknown
}

// unknownParam is CAT-INST-02 for key k that is no parameter of m. It names
// the parameters the app has (key and label); a key written as a
// parameter's label (the words a form shows, e.g. 模型名) is pointed to that
// parameter's key.
func unknownParam(m *catalog.Manifest, k string) *Fault {
	var list []string
	for _, p := range m.Params {
		if p.Hidden {
			continue
		}
		if p.Label != "" && strings.TrimSpace(p.Label) == strings.TrimSpace(k) {
			return fault(CodeBadParams, nil, "应用 %s 没有参数 %s：参数名是 %s（%s），写成 --set %s=值", m.ID, k, p.Key, p.Label, p.Key)
		}
		if p.Label != "" && p.Label != p.Key {
			list = append(list, p.Key+"（"+p.Label+"）")
		} else {
			list = append(list, p.Key)
		}
	}
	if len(list) == 0 {
		return fault(CodeBadParams, nil, "应用 %s 没有参数 %s：这个应用没有能填的参数", m.ID, k)
	}
	return fault(CodeBadParams, nil, "应用 %s 没有参数 %s。它的参数有：%s（--set 左边写参数名，不写括号里的说明）", m.ID, k, strings.Join(list, "、"))
}

// resolveKnownParams merges defaults, the values given for the app's
// parameters (keys that are none of them are left out) and generated
// secrets; see resolveParamsOpt.
func resolveKnownParams(m *catalog.Manifest, user map[string]any, lenient bool, missing []ParamMissing) (Values, []ParamMissing, error) {
	vals := Values{}
	for _, p := range m.Params {
		raw, given := user[p.Key]
		var s string
		if given && raw != nil {
			v, err := scalarText(raw)
			if err != nil {
				return nil, nil, fault(CodeBadParams, nil, "参数 %s：%v", p.Key, err)
			}
			s = v
		} else if p.Default != nil {
			v, err := scalarText(p.Default)
			if err != nil {
				return nil, nil, fault(CodeBadParams, nil, "参数 %s 的默认值不对", p.Key)
			}
			s = v
		}
		if s == "" && p.Generate != nil {
			g, err := generateSecret(p.Generate.Length, p.Generate.Charset)
			if err != nil {
				return nil, nil, fault(CodeInternal, err, "生成 %s 失败", p.Key)
			}
			vals[p.Key] = g
			continue
		}
		if s == "" {
			if p.Required && lenient {
				missing = append(missing, ParamMissing{Key: p.Key, Label: p.Label})
			} else if p.Required {
				return nil, nil, fault(CodeBadParams, nil, "缺少必填参数 %s（%s）", p.Key, p.Label)
			}
			vals[p.Key] = ""
			continue
		}
		if err := checkScalar(p, s); err != nil {
			return nil, nil, fault(CodeBadParams, nil, "参数 %s：%v", p.Key, err)
		}
		vals[p.Key] = s
	}
	return vals, missing, nil
}

func scalarText(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case json.Number:
		return x.String(), nil
	}
	return "", fmt.Errorf("要是文字、数字或真假值")
}

func checkScalar(p catalog.Param, s string) error {
	if !singleLine(s) {
		return fmt.Errorf("不能有换行或控制字符")
	}
	if len(s) > 4096 {
		return fmt.Errorf("太长")
	}
	switch p.Type {
	case "secret":
		if s == "-" {
			// What a command line writes for "read it from standard
			// input": never installed as the password itself.
			return fmt.Errorf("值是“-”：命令行里写 --set %s=- 时，值要从标准输入给（acornfox install 会读一行）；页面和接口里直接填它本身", p.Key)
		}
		if len(s) < minUserSecret {
			return fmt.Errorf("至少 %d 个字符", minUserSecret)
		}
	case "int":
		n, err := strconv.ParseFloat(s, 64)
		if err != nil || n != float64(int64(n)) {
			return fmt.Errorf("要是整数")
		}
		if p.Min != nil && n < *p.Min || p.Max != nil && n > *p.Max {
			return fmt.Errorf("超出范围")
		}
	case "bool":
		if s != "true" && s != "false" {
			return fmt.Errorf("要是 true 或 false")
		}
	case "url":
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("要是 http:// 或 https:// 开头的地址")
		}
	case "enum":
		ok := false
		for _, o := range p.Options {
			if t, err := scalarText(o); err == nil && t == s {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("只能是给定的选项之一")
		}
	}
	if p.Pattern != "" {
		re, err := regexp.Compile(p.Pattern)
		if err != nil {
			return fmt.Errorf("清单里的格式要求写错了")
		}
		if !re.MatchString(s) {
			return fmt.Errorf("格式不符合 %s", p.Pattern)
		}
	}
	return nil
}

// lookup resolves "params.<key>", "platform.<name>" or
// "uses.<name>.<item>" (an item of the service a need of the app is
// connected to; see usesKey).
func (v Values) lookup(ref string) (string, error) {
	if name, ok := strings.CutPrefix(ref, "platform."); ok {
		return v.lookupPlatform(name)
	}
	if rest, ok := strings.CutPrefix(ref, "uses."); ok {
		return v.lookupUse(rest)
	}
	parts := strings.Split(ref, ".")
	if len(parts) < 2 || parts[0] != "params" {
		return "", fmt.Errorf("不支持的引用 %s", ref)
	}
	val, ok := v[parts[1]]
	if !ok {
		return "", fmt.Errorf("没有参数 %s", parts[1])
	}
	x, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("参数 %s 的值不对", parts[1])
	}
	if len(parts) != 2 {
		return "", fmt.Errorf("参数 %s 没有字段 %s", parts[1], strings.Join(parts[2:], "."))
	}
	return x, nil
}

// render fills in the references of tmpl (catalog.ParseTemplate: params.*,
// platform.*, uses.* with their transforms; any other {{…}} is kept as
// written); escape (may be nil) is applied to each inserted value. A
// reference that cannot be resolved is an error; so is one that cannot be
// read or a transform that fails (catalog.IsRenderError).
func (v Values) render(tmpl string, escape func(string) string) (string, error) {
	return catalog.RenderTemplate(tmpl, v.lookup, escape)
}

// holds works out a condition (an env entry's or a config file's if) with
// these values. A reference with no value (an optional use that is not
// connected) makes it false.
func (v Values) holds(cond string) (bool, error) {
	c, err := catalog.ParseCondition(cond)
	if err != nil {
		return false, err
	}
	return c.Eval(v.lookup)
}

// quotedEscape makes a value safe inside a double-quoted YAML or JSON string.
func quotedEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

// fileEscape picks the escape for a config file by its extension.
func fileEscape(p string) func(string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".yaml", ".yml", ".json":
		return quotedEscape
	}
	return nil
}

// renderJSON renders every string in a JSON-like value (maps, slices,
// strings); other types are kept.
func (v Values) renderJSON(in any) (any, error) {
	switch x := in.(type) {
	case string:
		return v.render(x, nil)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			r, err := v.renderJSON(e)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			r, err := v.renderJSON(e)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	}
	return in, nil
}

// matchJSON reports whether every key of want has the same value in got
// (nested objects compared the same way; numbers compared as float64).
func matchJSON(want map[string]any, got any) bool {
	g, ok := got.(map[string]any)
	if !ok {
		return false
	}
	for k, w := range want {
		gv, ok := g[k]
		if !ok {
			return false
		}
		switch wv := w.(type) {
		case map[string]any:
			if !matchJSON(wv, gv) {
				return false
			}
		default:
			if !scalarEqual(wv, gv) {
				return false
			}
		}
	}
	return true
}

func scalarEqual(a, b any) bool {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		return af == bf
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}
