package appinstall

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // zone names are checked even on hosts without tzdata

	"github.com/acornfox/acornfox/internal/catalog"
)

// Platform variables on the standalone edition: where each value in
// catalog.PlatformVars comes from on this machine, and how the user may
// change the ones marked UserCanSet. The table itself (names, which ones can
// be changed) is catalog.PlatformVars; this file only fills it.

// PlatformEnv is this machine's settings the platform variables are filled
// from. The server builds it from install.env / network.json at start.
type PlatformEnv struct {
	China bool   // region: cn (true) or global
	Cloud string // cloud vendor install.sh detected (aliyun, tencent, huawei, volcengine) or ""
	TZ    string // IANA time zone of this machine; "" => UTC
	// ChatModel is the default chat model; "" => DefaultGatewayModel.
	ChatModel string
}

// platformKey is where Values keep the platform variables. "@" can never
// start a parameter key (the schema allows [a-z][a-z0-9_]*).
const platformKey = "@platform"

// Mirror addresses. Public mirrors only: every one answers from anywhere
// (the vendors' internal-network addresses are faster on their own clouds
// but are not verified yet). Outside China the official registries.
var (
	pypiMirrors = map[string]string{
		"":        "https://repo.huaweicloud.com/repository/pypi/simple/",
		"aliyun":  "https://mirrors.aliyun.com/pypi/simple/",
		"tencent": "https://mirrors.cloud.tencent.com/pypi/simple/",
	}
	npmMirrors = map[string]string{
		"":        "https://repo.huaweicloud.com/repository/npm/",
		"aliyun":  "https://registry.npmmirror.com/",
		"tencent": "https://mirrors.cloud.tencent.com/npm/",
	}
)

const (
	officialPyPI = "https://pypi.org/simple/"
	officialNPM  = "https://registry.npmjs.org/"
)

func pickMirror(table map[string]string, cloud string) string {
	if v, ok := table[cloud]; ok {
		return v
	}
	return table[""]
}

// platformBase is every variable this machine can fill before an app
// exists (app.url is added when the app is created; the platform model key
// is not available on the standalone edition yet).
func (e *Engine) platformBase() map[string]string {
	p := e.cfg.Platform
	out := map[string]string{
		catalog.PVRegion:       "global",
		catalog.PVTZ:           "UTC",
		catalog.PVLLMModelChat: DefaultGatewayModel,
		catalog.PVMirrorPyPI:   officialPyPI,
		catalog.PVMirrorNPM:    officialNPM,
	}
	if p.China {
		out[catalog.PVRegion] = "cn"
		out[catalog.PVMirrorPyPI] = pickMirror(pypiMirrors, p.Cloud)
		out[catalog.PVMirrorNPM] = pickMirror(npmMirrors, p.Cloud)
	}
	if tz := strings.TrimSpace(p.TZ); knownTZ(tz) {
		out[catalog.PVTZ] = tz
	}
	if m := strings.TrimSpace(p.ChatModel); m != "" {
		out[catalog.PVLLMModelChat] = m
	}
	return out
}

// knownTZ reports whether tz is an IANA zone name Go knows (the apps get it
// as TZ; a name the app does not know silently means UTC there).
func knownTZ(tz string) bool {
	if tz == "" || tz == "Local" || !validTZ.MatchString(tz) {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}

var (
	validTZ    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+){0,2}$`)
	validModel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,99}$`)
)

// checkPlatformSets validates the user's changes (CAT-INST-28): the name
// must be in the table and marked UserCanSet, and the value must fit it.
func checkPlatformSets(sets map[string]string) error {
	names := make([]string, 0, len(sets))
	for k := range sets {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, name := range names {
		v := strings.TrimSpace(sets[name])
		pv, ok := catalog.LookupPlatformVar(name)
		if !ok {
			return fault(CodeBadPlatform, nil, "没有平台变量 %s（acornfox catalog platform-vars 看有哪些）", name)
		}
		if !pv.UserCanSet {
			return fault(CodeBadPlatform, nil, "平台变量 %s（%s）由平台决定，不能改", name, pv.Label)
		}
		if v == "" || len(v) > 500 || !singleLine(v) {
			return fault(CodeBadPlatform, nil, "平台变量 %s 的值不能为空、不能换行、不超过 500 个字符", name)
		}
		switch name {
		case catalog.PVTZ:
			if !knownTZ(v) {
				return fault(CodeBadPlatform, nil, "时区要写成 Asia/Shanghai 这样的标准名字，%q 不是（拼写要和 IANA 时区库一致）", v)
			}
		case catalog.PVLLMModelChat:
			if !validModel.MatchString(v) {
				return fault(CodeBadPlatform, nil, "模型名 %q 不对：只用字母、数字和 . _ : / @ + -，不超过 100 个字符", v)
			}
		case catalog.PVMirrorPyPI, catalog.PVMirrorNPM:
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
				return fault(CodeBadPlatform, nil, "下载源 %s 要是 http:// 或 https:// 开头的地址，不能带账号密码", name)
			}
		}
	}
	return nil
}

// platformUserKey is where Values keep only the platform variables the user
// changed in the install request (--set platform.<name>=…), next to the
// merged values under platformKey. An app installed before this key existed
// has none: its platform values cannot be told apart and count as "the
// value at install time".
const platformUserKey = "@platform_user"

// platformUserSets is the user's changes as they are saved (already
// checked); never nil, so a saved install with no change is told from an old
// record without the key.
func platformUserSets(sets map[string]string) map[string]string {
	out := make(map[string]string, len(sets))
	for k, v := range sets {
		out[k] = strings.TrimSpace(v)
	}
	return out
}

// platformValues is base plus the user's changes (already checked).
func (e *Engine) platformValues(sets map[string]string) map[string]string {
	out := e.platformBase()
	for k, v := range sets {
		out[k] = strings.TrimSpace(v)
	}
	return out
}

// platformMap returns the platform variables kept in v (a map[string]any
// after the saved parameters were read back from JSON).
func (v Values) platformMap() map[string]string {
	switch m := v[platformKey].(type) {
	case map[string]string:
		return m
	case map[string]any:
		out := make(map[string]string, len(m))
		for k, x := range m {
			if s, ok := x.(string); ok {
				out[k] = s
			}
		}
		return out
	}
	return nil
}

// setPlatform stores one platform variable in v.
func (v Values) setPlatform(name, value string) {
	m, ok := v[platformKey].(map[string]string)
	if !ok {
		m = v.platformMap()
		if m == nil {
			m = map[string]string{}
		}
		v[platformKey] = m
	}
	m[name] = value
}

// lookupPlatform resolves platform.<name>.
func (v Values) lookupPlatform(name string) (string, error) {
	pv, ok := catalog.LookupPlatformVar(name)
	if !ok {
		return "", fmt.Errorf("没有平台变量 %s", name)
	}
	if pv.ViaParam {
		return "", fmt.Errorf("平台变量 %s 只能经大模型参数使用", name)
	}
	s, ok := v.platformMap()[name]
	if !ok {
		return "", fmt.Errorf("平台变量 %s（%s）在这台机器上还没有值", name, pv.Label)
	}
	return s, nil
}

// PlatformVarView is one platform variable as GET /v1/catalog/platform-vars
// and the install form show it: the table entry plus this machine's value.
type PlatformVarView struct {
	catalog.PlatformVar
	Value     string `json:"value,omitempty"`
	Available bool   `json:"available"`
	Note      string `json:"note,omitempty"`
}

// PlatformVars lists the whole table with this machine's values.
func (e *Engine) PlatformVars(ctx context.Context) []PlatformVarView {
	base := e.platformBase()
	out := make([]PlatformVarView, 0, len(catalog.PlatformVars))
	for _, pv := range catalog.PlatformVars {
		out = append(out, platformView(pv, base))
	}
	return out
}

func platformView(pv catalog.PlatformVar, base map[string]string) PlatformVarView {
	v := PlatformVarView{PlatformVar: pv}
	switch {
	case pv.Name == catalog.PVAppURL:
		v.Available, v.Note = true, "每个应用装好时生成"
	case pv.ViaParam:
		v.Note = "平台 Key 即将开放"
	default:
		v.Value, v.Available = base[pv.Name], true
	}
	return v
}

// appPlatformViews is what the install form shows for one manifest: the
// variables it uses, plus the default chat model when it has a model
// parameter that can use the local gateway (the model falls back to it).
func (e *Engine) appPlatformViews(m *catalog.Manifest) []PlatformVarView {
	names := catalog.PlatformRefs(m)
	for _, p := range m.Params {
		if p.Type == "llm_provider" && p.LLM != nil && p.LLM.LocalGateway {
			names = append(names, catalog.PVLLMModelChat)
		}
	}
	sort.Strings(names)
	base := e.platformBase()
	out := []PlatformVarView{}
	for i, n := range names {
		if i > 0 && names[i-1] == n {
			continue
		}
		if pv, ok := catalog.LookupPlatformVar(n); ok {
			out = append(out, platformView(pv, base))
		}
	}
	return out
}
