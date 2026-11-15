package catalog

import "sort"

// Platform variables are what the platform knows about the machine and hands
// to every app: the time zone, the download mirrors, the network region, the
// app's own address. A manifest refers to them as {{platform.<name>}} in
// file templates and init bodies, and as "from: platform.<name>" in env, so
// that it never hard-codes them (Asia/Shanghai, a mirror address).
//
// This table is the single source of the variables: catalog lint accepts no
// other names, the install engine fills exactly these, and the docs refer to
// it. The standalone edition fills them from this machine's settings; the
// cloud edition fills the same names from the cluster and the business
// system, so one manifest works on both. What one app offers another (a
// model API, a mail server) is no platform variable but a service (uses,
// provides).
//
// Values come from one interface only (internal/platformvar): its standalone
// implementation fills them from this machine's settings, and the install
// engine, the API (the console's pages) and the command line take them from
// there and nowhere else.
//
// Adding a variable: add it here (with both editions' sources, when it is
// worked out, the rule a user's value must follow, and Since, the platform
// version that first ships it), fill it in internal/platformvar, then use it
// in manifests.

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
	// Since is the first platform version that has the variable. A manifest
	// that refers to it needs at least that version (see features.go).
	Since string `json:"since"`
	// Refresh says when the value is worked out: RefreshInstall, once when
	// the app is installed (and when it is reinstalled on its old data),
	// kept from then on.
	Refresh string `json:"refresh"`
	// Editions lists the editions that give the variable a value
	// (EditionStandalone, EditionCloud).
	Editions []string `json:"editions"`
	// Rule is what a value the user sets must be, besides one non-empty line
	// of at most 500 characters: RuleTZ, RuleURL, or "" (no more). Only for
	// UserCanSet variables; RuleLabel says it in words.
	Rule      string `json:"rule,omitempty"`
	RuleLabel string `json:"rule_label,omitempty"`
}

// When a platform variable's value is worked out (PlatformVar.Refresh).
const (
	// RefreshInstall: when the app is installed or reinstalled on its old
	// data; kept from then on. Every variable is so for now.
	RefreshInstall = "install"
)

// Editions that give platform variables values (PlatformVar.Editions).
const (
	EditionStandalone = "standalone"
	EditionCloud      = "cloud"
)

// Rules a value the user sets must follow (PlatformVar.Rule).
const (
	RuleTZ  = "tz"  // an IANA time zone name the program knows
	RuleURL = "url" // an http:// or https:// address without credentials
)

var bothEditions = []string{EditionStandalone, EditionCloud}

// Names of the platform variables.
const (
	PVTZ         = "tz"
	PVRegion     = "region"
	PVMirrorPyPI = "mirror.pypi"
	PVMirrorNPM  = "mirror.npm"
	PVAppURL     = "app.url"
	PVAppScheme  = "app.scheme"
	PVAppHost    = "app.host"
)

// PlatformVars is the table. Every variable is there from BasePlatform, the
// first platform version that installs the current manifest format.
var PlatformVars = []PlatformVar{
	{Name: PVTZ, Label: "时区",
		Standalone: "本机时区（读不到时为 UTC）", Cloud: "用户账号的时区", UserCanSet: true, Since: BasePlatform,
		Refresh: RefreshInstall, Editions: bothEditions, Rule: RuleTZ, RuleLabel: "标准时区名，例如 Asia/Shanghai（拼写要和 IANA 时区库一致）"},
	{Name: PVRegion, Label: "网络区域（cn 或 global）",
		Standalone: "安装时的选择（install.env 或 network.json）", Cloud: "按集群", Since: BasePlatform,
		Refresh: RefreshInstall, Editions: bothEditions},
	{Name: PVMirrorPyPI, Label: "Python 包下载源",
		Standalone: "国内用国内镜像（在国内云上用该厂商的公开镜像），海外用官方源", Cloud: "云内网源",
		UserCanSet: true, Since: BasePlatform,
		Refresh: RefreshInstall, Editions: bothEditions, Rule: RuleURL, RuleLabel: "http:// 或 https:// 开头的地址，不能带账号密码"},
	{Name: PVMirrorNPM, Label: "Node 包下载源",
		Standalone: "国内用国内镜像（在国内云上用该厂商的公开镜像），海外用官方源", Cloud: "云内网源",
		UserCanSet: true, Since: BasePlatform,
		Refresh: RefreshInstall, Editions: bothEditions, Rule: RuleURL, RuleLabel: "http:// 或 https:// 开头的地址，不能带账号密码"},
	{Name: PVAppURL, Label: "这个应用的访问地址",
		Standalone: "装好时算：绑了域名是 https://第一个域名；没绑按对外方式，只本机是 http://127.0.0.1:端口，局域网、公网是 http://服务器地址:端口（和 acornfox status 里的地址同一个算法）", Cloud: "https://<应用名>.app.acornfox.com", Since: BasePlatform,
		Refresh: RefreshInstall, Editions: bothEditions},
	{Name: PVAppScheme, Label: "这个应用的访问方式（http 或 https）",
		Standalone: "装好时算：绑了域名是 https，否则是 http（和 app.url 一起算）", Cloud: "https", Since: BasePlatform,
		Refresh: RefreshInstall, Editions: bothEditions},
	{Name: PVAppHost, Label: "这个应用访问地址里的主机（不带端口）",
		Standalone: "装好时算：app.url 里的主机，例如域名、服务器地址或 127.0.0.1", Cloud: "<应用名>.app.acornfox.com", Since: BasePlatform,
		Refresh: RefreshInstall, Editions: bothEditions},
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
// "from", file templates, init steps, first_login "from", provided services'
// value templates and steps), sorted, each once.
func PlatformRefs(m *Manifest) []string {
	seen := map[string]bool{}
	for _, r := range manifestRefs(m) {
		if name, ok := cutPlatform(r.Ref); ok {
			seen[name] = true
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

// TemplateRefs and TemplatePlaceholders are in template.go.

// jsonStrings lists the strings of a JSON-like value (the values of maps, by
// key, and the items of lists; never the keys), in a fixed order.
func jsonStrings(v any) []string {
	var out []string
	switch x := v.(type) {
	case string:
		out = append(out, x)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = append(out, jsonStrings(x[k])...)
		}
	case []any:
		for _, e := range x {
			out = append(out, jsonStrings(e)...)
		}
	}
	return out
}
