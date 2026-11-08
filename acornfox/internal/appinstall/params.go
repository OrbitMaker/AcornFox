package appinstall

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
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
// Values maps a parameter key to its value: a string for every plain type
// (int and bool are kept as their text), or an *LLMChoice for llm_provider.
// Templates and "from" references read them as params.<key> and, for the
// model, params.<key>.base_url / api_key / model / source.

// LLM sources a user can choose (POST /v1/catalog/install params.<key>.source;
// CLI --llm own|local|platform).
const (
	LLMOwn          = "own"
	LLMLocalGateway = "local_gateway"
	LLMPlatform     = "platform"
)

// LLMChoice is the value of an llm_provider parameter.
type LLMChoice struct {
	Source  string `json:"source"`
	BaseURL string `json:"base_url,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model,omitempty"`
	App     string `json:"app,omitempty"` // local_gateway: the provider app on this machine
}

// DefaultGatewayModel is the model name an app is configured with when it uses
// the local gateway and the user named none. The user adds a channel with this
// model in the gateway (or changes the model in the app afterwards).
const DefaultGatewayModel = "deepseek-chat"

// Values are resolved parameters.
type Values map[string]any

const (
	charsetAlnum  = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	charsetHex    = "0123456789abcdef"
	minUserSecret = 8
)

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
		n, err := rand.Int(rand.Reader, max)
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
// llm_provider parameters are only parsed here (the engine resolves the
// source). user may carry nested objects for llm parameters.
func resolveParams(m *catalog.Manifest, user map[string]any) (Values, error) {
	known := map[string]catalog.Param{}
	for _, p := range m.Params {
		known[p.Key] = p
	}
	keys := make([]string, 0, len(user))
	for k := range user {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p, ok := known[k]
		if !ok {
			return nil, fault(CodeBadParams, nil, "应用 %s 没有参数 %s", m.ID, k)
		}
		if p.Hidden {
			return nil, fault(CodeBadParams, nil, "参数 %s 由平台生成，不能填", k)
		}
	}
	vals := Values{}
	for _, p := range m.Params {
		raw, given := user[p.Key]
		if p.Type == "llm_provider" {
			if !given || raw == nil {
				continue
			}
			c, err := parseLLM(p.Key, raw)
			if err != nil {
				return nil, err
			}
			vals[p.Key] = c
			continue
		}
		var s string
		if given && raw != nil {
			v, err := scalarText(raw)
			if err != nil {
				return nil, fault(CodeBadParams, nil, "参数 %s：%v", p.Key, err)
			}
			s = v
		} else if p.Default != nil {
			v, err := scalarText(p.Default)
			if err != nil {
				return nil, fault(CodeBadParams, nil, "参数 %s 的默认值不对", p.Key)
			}
			s = v
		}
		if s == "" && p.Generate != nil {
			g, err := generateSecret(p.Generate.Length, p.Generate.Charset)
			if err != nil {
				return nil, fault(CodeInternal, err, "生成 %s 失败", p.Key)
			}
			vals[p.Key] = g
			continue
		}
		if s == "" {
			if p.Required {
				return nil, fault(CodeBadParams, nil, "缺少必填参数 %s（%s）", p.Key, p.Label)
			}
			vals[p.Key] = ""
			continue
		}
		if err := checkScalar(p, s); err != nil {
			return nil, fault(CodeBadParams, nil, "参数 %s：%v", p.Key, err)
		}
		vals[p.Key] = s
	}
	return vals, nil
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

// parseLLM reads an llm_provider value: an object {source, base_url,
// api_key, model, app} or just the source as text.
func parseLLM(key string, raw any) (*LLMChoice, error) {
	var c LLMChoice
	switch x := raw.(type) {
	case string:
		c.Source = x
	case *LLMChoice:
		c = *x
	case map[string]any:
		b, _ := json.Marshal(x)
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fault(CodeBadParams, err, "参数 %s 的格式不对", key)
		}
	default:
		return nil, fault(CodeBadParams, nil, "参数 %s 要是对象 {source, base_url, api_key, model}", key)
	}
	switch c.Source {
	case "local":
		c.Source = LLMLocalGateway
	case "", LLMOwn, LLMLocalGateway, LLMPlatform:
		// "": only fields such as the model; the source comes from a recipe
		// link or the default choice.
	default:
		return nil, fault(CodeBadParams, nil, "参数 %s 的 source 只能是 own、local_gateway、platform", key)
	}
	for _, v := range []string{c.BaseURL, c.APIKey, c.Model, c.App} {
		if !singleLine(v) || len(v) > 2048 {
			return nil, fault(CodeBadParams, nil, "参数 %s 里不能有换行或控制字符", key)
		}
	}
	if c.Source == LLMOwn {
		if c.BaseURL == "" || c.APIKey == "" || c.Model == "" {
			return nil, fault(CodeBadParams, nil, "自己的 Key 要填 base_url、api_key、model 三项（参数 %s）", key)
		}
		u, err := url.Parse(c.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fault(CodeBadParams, nil, "参数 %s 的 base_url 要是 http:// 或 https:// 开头的地址", key)
		}
	}
	return &c, nil
}

// lookup resolves "params.<key>", "params.<key>.<field>" or
// "platform.<name>".
func (v Values) lookup(ref string) (string, error) {
	if name, ok := strings.CutPrefix(ref, "platform."); ok {
		return v.lookupPlatform(name)
	}
	parts := strings.Split(ref, ".")
	if len(parts) < 2 || parts[0] != "params" {
		return "", fmt.Errorf("不支持的引用 %s", ref)
	}
	val, ok := v[parts[1]]
	if !ok {
		return "", fmt.Errorf("没有参数 %s", parts[1])
	}
	switch x := val.(type) {
	case string:
		if len(parts) != 2 {
			return "", fmt.Errorf("参数 %s 没有字段 %s", parts[1], strings.Join(parts[2:], "."))
		}
		return x, nil
	case *LLMChoice:
		if len(parts) != 3 {
			return "", fmt.Errorf("大模型参数要写 %s.base_url / api_key / model", ref)
		}
		switch parts[2] {
		case "base_url":
			return x.BaseURL, nil
		case "api_key":
			return x.APIKey, nil
		case "model":
			return x.Model, nil
		case "source":
			return x.Source, nil
		}
		return "", fmt.Errorf("大模型参数没有字段 %s", parts[2])
	}
	return "", fmt.Errorf("参数 %s 的值不对", parts[1])
}

var placeholderRe = regexp.MustCompile(`\{\{\s*([^{}\s]+)\s*\}\}`)

// render replaces every {{params.…}} in tmpl; escape (may be nil) is applied
// to each inserted value. An unknown reference is an error.
func (v Values) render(tmpl string, escape func(string) string) (string, error) {
	var firstErr error
	out := placeholderRe.ReplaceAllStringFunc(tmpl, func(m string) string {
		ref := placeholderRe.FindStringSubmatch(m)[1]
		s, err := v.lookup(ref)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return m
		}
		if escape != nil {
			s = escape(s)
		}
		return s
	})
	return out, firstErr
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
