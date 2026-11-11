package catalog

import "sort"

// Platform variables are what the platform knows about the machine and hands
// to every app: the model gateway, the time zone, the download mirrors, the
// app's own address. A manifest refers to them as {{platform.<name>}} in
// file templates and init bodies, and as "from: platform.<name>" in env, so
// that it never hard-codes them (Asia/Shanghai, a mirror address, a model).
//
// This table is the single source of the variables: catalog lint accepts no
// other names, the install engine fills exactly these, and the docs refer to
// it. The standalone edition fills them from this machine's settings; the
// cloud edition fills the same names from the cluster and the business
// system, so one manifest works on both.
//
// Adding a variable: add it here (with both editions' sources and Since, the
// platform version that first ships it), fill it in
// internal/appinstall/platform.go, then use it in manifests.

// PlatformVar describes one platform variable.
type PlatformVar struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	// Standalone and Cloud say where each edition takes the value from.
	Standalone string `json:"standalone"`
	Cloud      string `json:"cloud"`
	// UserCanSet: the install form (advanced settings) and
	// `acornfox install --set platform.<name>=…` may change it. Values the
	// platform must control (keys, its own address) cannot be changed.
	UserCanSet bool `json:"user_can_set"`
	Secret     bool `json:"secret,omitempty"`
	// Issued: handed out per app at install time and taken back on
	// uninstall (a token), not read from a setting.
	Issued bool `json:"issued,omitempty"`
	// ViaParam: templates do not use it directly; it is what an
	// llm_provider parameter gets when the user picks the platform as the
	// source, so manifests keep referring to params.<llm param>.*.
	ViaParam bool `json:"via_param,omitempty"`
	// Since is the first platform version that has the variable. A manifest
	// that refers to it needs at least that version (see features.go).
	Since string `json:"since"`
}

// Names of the platform variables (first batch, 2026-11-07).
const (
	PVLLMBaseURL   = "llm.base_url"
	PVLLMAPIKey    = "llm.api_key"
	PVLLMModelChat = "llm.model.chat"
	PVTZ           = "tz"
	PVRegion       = "region"
	PVMirrorPyPI   = "mirror.pypi"
	PVMirrorNPM    = "mirror.npm"
	PVAppURL       = "app.url"
)

// PlatformVars is the table.
var PlatformVars = []PlatformVar{
	{Name: PVLLMBaseURL, Label: "平台模型接口地址",
		Standalone: "平台 Key 开放后为 https://api.acornfox.com/v1；现在不可用", Cloud: "https://api.acornfox.com/v1",
		ViaParam: true, Since: v050},
	{Name: PVLLMAPIKey, Label: "平台为这个应用发的 Key",
		Standalone: "平台 Key 开放后由平台签发；现在不可用", Cloud: "每个应用实例一把，默认每天 ¥20 上限",
		Secret: true, Issued: true, ViaParam: true, Since: v050},
	{Name: PVLLMModelChat, Label: "默认对话模型",
		Standalone: "程序内置的默认模型（用本机 New API 又没指定模型时也用它）", Cloud: "经营系统配置表",
		UserCanSet: true, Since: v050},
	{Name: PVTZ, Label: "时区",
		Standalone: "本机时区（读不到时为 UTC）", Cloud: "用户账号的时区", UserCanSet: true, Since: v050},
	{Name: PVRegion, Label: "网络区域（cn 或 global）",
		Standalone: "安装时的选择（install.env 或 network.json）", Cloud: "按集群", Since: v050},
	{Name: PVMirrorPyPI, Label: "Python 包下载源",
		Standalone: "国内用国内镜像（在国内云上用该厂商的公开镜像），海外用官方源", Cloud: "云内网源",
		UserCanSet: true, Since: v050},
	{Name: PVMirrorNPM, Label: "Node 包下载源",
		Standalone: "国内用国内镜像（在国内云上用该厂商的公开镜像），海外用官方源", Cloud: "云内网源",
		UserCanSet: true, Since: v050},
	{Name: PVAppURL, Label: "这个应用的访问地址",
		Standalone: "按应用装好时的对外方式算（默认只本机）", Cloud: "https://<应用名>.app.acornfox.com", Since: v050},
}

var platformVarByName = func() map[string]PlatformVar {
	m := make(map[string]PlatformVar, len(PlatformVars))
	for _, v := range PlatformVars {
		m[v.Name] = v
	}
	return m
}()

// LookupPlatformVar returns the variable called name.
func LookupPlatformVar(name string) (PlatformVar, bool) {
	v, ok := platformVarByName[name]
	return v, ok
}

// PlatformRefs lists the platform variables the manifest refers to (env
// "from", file templates, init bodies, first_login "from"), sorted, each once.
func PlatformRefs(m *Manifest) []string {
	seen := map[string]bool{}
	add := func(ref string) {
		if name, ok := cutPlatform(ref); ok {
			seen[name] = true
		}
	}
	for _, e := range m.Env {
		add(e.From)
	}
	for _, lf := range m.FirstLogin {
		add(lf.From)
	}
	for _, f := range m.Files {
		for _, r := range TemplateRefs(f.Template) {
			add(r)
		}
	}
	for _, s := range m.Init {
		for _, r := range jsonRefs(s.HTTP.JSON) {
			add(r)
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func cutPlatform(ref string) (string, bool) {
	const p = "platform."
	if len(ref) > len(p) && ref[:len(p)] == p {
		return ref[len(p):], true
	}
	return "", false
}

// TemplateRefs returns the references ("params.x", "platform.mirror.pypi")
// in a template, in order.
func TemplateRefs(tmpl string) []string {
	var out []string
	for _, r := range templateRef.FindAllStringSubmatch(tmpl, -1) {
		out = append(out, r[1]+"."+r[2])
	}
	return out
}

func jsonRefs(v any) []string {
	var out []string
	switch x := v.(type) {
	case string:
		out = append(out, TemplateRefs(x)...)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = append(out, jsonRefs(x[k])...)
		}
	case []any:
		for _, e := range x {
			out = append(out, jsonRefs(e)...)
		}
	}
	return out
}
