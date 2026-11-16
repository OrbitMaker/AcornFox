package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readManifest(t *testing.T, dir, id string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "apps", id, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rowText(rows []VarRow) string {
	var parts []string
	for _, r := range rows {
		parts = append(parts, r.Name+"="+r.Layer+"/"+r.Class+"@"+strings.Join(r.UsedIn, "+"))
	}
	return strings.Join(parts, "\n")
}

// The two manifests of format 3 (the drafts in testdata/good, the same as
// the catalog repository's), row by row, with their fingerprints spelled out.
func TestVarReportDraftManifests(t *testing.T) {
	good := filepath.Join("testdata", "good")

	rep, err := ManifestVarReport(readManifest(t, good, "new-api"))
	if err != nil {
		t.Fatal(err)
	}
	everywhere := "/init/setup-admin+/init/login+/first_login/%s+/provides/0/issue/login+/provides/0/revoke/login+/provides/0/check/login"
	want := strings.Join([]string{
		"params.admin_password=custom/generated@" + strings.Replace(everywhere, "%s", "密码", 1),
		"params.admin_username=custom/default@" + strings.Replace(everywhere, "%s", "账号", 1),
		"params.session_secret=custom/generated@/env/SESSION_SECRET",
		"platform.tz=system/system@/env/TZ",
	}, "\n")
	if got := rowText(rep.Vars); got != want {
		t.Fatalf("new-api 的变量：\n%s\n要：\n%s", got, want)
	}
	if rep.App != "new-api" || rep.Version != "v1.0.0-rc.48" || rep.DeliveryTier != "auto" ||
		rep.MinPlatform.Declared != BasePlatform || rep.MinPlatform.Computed != BasePlatform || len(rep.MinPlatform.Reasons) != 0 || len(rep.Hardcoded) != 0 {
		t.Fatalf("%+v", rep)
	}
	if len(rep.Counts) != 5 || rep.Counts[ClassSystem] != 1 || rep.Counts[ClassGenerated] != 2 || rep.Counts[ClassDefault] != 1 ||
		rep.Counts[ClassOptional] != 0 || rep.Counts[ClassUserRequired] != 0 {
		t.Fatalf("counts %+v", rep.Counts)
	}
	// The fingerprint is the SHA-256 of a fixed text. Both are spelled out
	// here: fingerprints get stored outside this program, so neither may drift.
	const text = `{"v":2,"vars":[{"name":"params.admin_password","layer":"custom","class":"generated"},{"name":"params.admin_username","layer":"custom","class":"default"},{"name":"params.session_secret","layer":"custom","class":"generated"},{"name":"platform.tz","layer":"system","class":"system"}],"uses":[],"provides":[{"kind":"llm_gateway","values":[{"key":"api_key","how":"issued"},{"key":"base_url","how":"computed"},{"key":"model","how":"ask"}],"has_issue":true,"has_revoke":true,"has_check":true}]}`
	if got := string(varsFingerprintText(rep.Vars, rep.Uses, rep.Provides)); got != text {
		t.Fatalf("指纹的原文变了：\n%s", got)
	}
	if rep.Fingerprint != "33a0170464fcfa372c3e774386d62da8d057e6cb720659b304aba23e9e87517a" || rep.FingerprintVersion != 2 {
		t.Fatalf("new-api 指纹 %s（第 %d 版）", rep.Fingerprint, rep.FingerprintVersion)
	}
	if len(rep.Uses) != 0 || len(rep.Provides) != 1 || rep.Provides[0].Values[2].Ask == "" {
		t.Fatalf("new-api 的服务 %+v %+v", rep.Uses, rep.Provides)
	}
	tz := rep.Vars[3]
	if tz.Label != "时区" || tz.Since != BasePlatform || tz.Known == nil || !*tz.Known || !tz.UserCanSet {
		t.Fatalf("platform.tz 行 %+v", tz)
	}
	if pw := rep.Vars[0]; pw.Type != "secret" || !pw.Secret || pw.Hidden || pw.Label != "管理员密码" {
		t.Fatalf("admin_password 行 %+v", pw)
	}

	// Hermes takes its model API's address and key from the service it
	// needs (system rows); which model it uses is its own setting, for the
	// user to fill in.
	rep, err = ManifestVarReport(readManifest(t, good, "hermes-agent"))
	if err != nil {
		t.Fatal(err)
	}
	want = strings.Join([]string{
		"params.dashboard_password=custom/generated@/env/HERMES_DASHBOARD_BASIC_AUTH_PASSWORD+/first_login/密码",
		"params.dashboard_session_secret=custom/generated@/env/HERMES_DASHBOARD_BASIC_AUTH_SECRET",
		"params.model=custom/user_required@/files/config.yaml",
		"platform.mirror.npm=system/system@/env/npm_config_registry",
		"platform.mirror.pypi=system/system@/env/PIP_INDEX_URL+/env/UV_DEFAULT_INDEX",
		"platform.tz=system/system@/env/TZ",
		"uses.llm.api_key=system/system@/files/config.yaml",
		"uses.llm.base_url=system/system@/files/config.yaml",
	}, "\n")
	if got := rowText(rep.Vars); got != want {
		t.Fatalf("hermes-agent 的变量：\n%s\n要：\n%s", got, want)
	}
	if rep.Counts[ClassUserRequired] != 1 || rep.Counts[ClassSystem] != 5 || rep.Counts[ClassGenerated] != 2 {
		t.Fatalf("counts %+v", rep.Counts)
	}
	const hermesText = `{"v":2,"vars":[{"name":"params.dashboard_password","layer":"custom","class":"generated"},{"name":"params.dashboard_session_secret","layer":"custom","class":"generated"},{"name":"params.model","layer":"custom","class":"user_required"},{"name":"platform.mirror.npm","layer":"system","class":"system"},{"name":"platform.mirror.pypi","layer":"system","class":"system"},{"name":"platform.tz","layer":"system","class":"system"},{"name":"uses.llm.api_key","layer":"system","class":"system"},{"name":"uses.llm.base_url","layer":"system","class":"system"}],"uses":[{"name":"llm","kind":"llm_gateway","optional":false,"without":"","dedicated":false}],"provides":[]}`
	if got := string(varsFingerprintText(rep.Vars, rep.Uses, rep.Provides)); got != hermesText {
		t.Fatalf("hermes-agent 指纹的原文变了：\n%s", got)
	}
	if rep.Fingerprint != "993efab5344ede8eac44b3cbc465d2c7c230125e16c8bc63bbccad489e6a930c" {
		t.Fatalf("hermes-agent 指纹 %s", rep.Fingerprint)
	}
	if len(rep.Uses) != 1 || rep.Uses[0] != (VarUse{Name: "llm", Kind: "llm_gateway"}) || len(rep.Provides) != 0 {
		t.Fatalf("hermes-agent 的服务 %+v %+v", rep.Uses, rep.Provides)
	}
}

func TestVarsFingerprintChangesOnlyWithVariables(t *testing.T) {
	base := string(readManifest(t, filepath.Join("testdata", "good"), "new-api"))
	fp := func(s string) string {
		t.Helper()
		rep, err := ManifestVarReport([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return rep.Fingerprint
	}
	change := func(old, new string) string {
		t.Helper()
		if !strings.Contains(base, old) {
			t.Fatalf("清单里没有 %q", old)
		}
		return strings.Replace(base, old, new, 1)
	}
	want := fp(base)
	if fp(base) != want {
		t.Fatal("同一份清单两次算出来不一样")
	}
	same := map[string]string{
		"介绍文字":     change("统一管理 Key、令牌和额度", "统一管理密钥"),
		"图标":       change("icon: icon.svg", "icon: logo.png"),
		"参数的标签":    change("label: 管理员用户名", "label: 管理员账号"),
		"默认值换一个":   change("default: admin\n", "default: root\n"),
		"版本":       change("version: v1.0.0-rc.48", "version: v1.0.1"),
		"交付方式":     change("tier: auto", "tier: steps"),
		"最低平台版本":   change(`min_platform: "0.6.0"`, `min_platform: "0.6.1"`),
		"同一变量多用一处": change("  - { key: TZ, from: platform.tz }\n", "  - { key: TZ, from: platform.tz }\n  - { key: TIMEZONE, from: platform.tz }\n"),
	}
	// Move the first parameter to the end of params (less of its details).
	same["参数换个顺序"] = strings.Replace(change("  - key: admin_username\n    type: string\n    default: admin\n    pattern: \"^[A-Za-z0-9_]{1,12}$\"\n    label: 管理员用户名\n    editable: never\n", ""),
		"\nenv:\n", "  - key: admin_username\n    type: string\n    default: admin\n\nenv:\n", 1)
	for name, m := range same {
		if got := fp(m); got != want {
			t.Errorf("只改了%s，指纹不该变：%s", name, got)
		}
	}
	differ := map[string]string{
		"参数改名":        strings.ReplaceAll(base, "admin_username", "root_username"),
		"类别变了（默认值没了）": change("    default: admin\n", ""),
		"类别变了（改成必填）":  change("    default: admin\n", "    required: true\n"),
		"多了一个参数":      change("\nenv:\n", "  - key: site_name\n    type: string\n\nenv:\n"),
		"少了一个平台变量":    change("  - { key: TZ, from: platform.tz }\n", ""),
		"多了一个平台变量":    change("  - { key: TZ, from: platform.tz }\n", "  - { key: TZ, from: platform.tz }\n  - { key: PIP_INDEX_URL, from: platform.mirror.pypi }\n"),
		"平台变量换了一个":    change("from: platform.tz", "from: platform.region"),
	}
	seen := map[string]string{}
	for name, m := range differ {
		got := fp(m)
		if got == want {
			t.Errorf("%s，指纹应该变", name)
		}
		if other, dup := seen[got]; dup && !(strings.HasPrefix(name, "类别变了") && strings.HasPrefix(other, "类别变了")) {
			t.Errorf("%s 和 %s 的指纹相同", name, other)
		}
		seen[got] = name
	}
}

func TestClassifyVars(t *testing.T) {
	m := &Manifest{
		Params: []Param{
			{Key: "a_url", Type: "url", Required: true, Default: "x"},
			{Key: "b_gen", Type: "secret", Generate: &Generate{Length: 32}, Required: true, Default: "x"},
			{Key: "c_default", Type: "string", Default: "x", Required: true},
			{Key: "c_empty_must", Type: "string", Default: "", Required: true},
			{Key: "c_empty_opt", Type: "string", Default: ""},
			{Key: "d_zero", Type: "int", Default: 0},
			{Key: "e_false", Type: "bool", Default: false},
			{Key: "f_must", Type: "string", Required: true},
			{Key: "g_opt", Type: "string"},
			{Key: "g_opt", Type: "string", Required: true},
		},
		Env: []EnvVar{
			{Key: "A", From: "uses.gateway.base_url"},
			{Key: "B", From: "requires.postgres.url"},
			{Key: "C", From: "components.web.host"},
			{Key: "D", From: "platform.no.such"},
			{Key: "E", From: "volumes.data"},
			{Key: "F", From: "params.f_must"},
		},
		Files: []File{{Path: "x.conf", Template: "{{params.a_url}} {{params.a_url}} {{platform.app.url}}"}},
	}
	got := map[string]string{}
	rows := ClassifyVars(m)
	for _, r := range rows {
		got[r.Name] = r.Layer + "/" + r.Class
	}
	want := map[string]string{
		"params.a_url": "custom/default", "params.b_gen": "custom/generated", "params.c_default": "custom/default",
		// An empty-string default is no default: the install engine treats an
		// empty value as "not filled in" and refuses a required one.
		"params.c_empty_must": "custom/user_required", "params.c_empty_opt": "custom/optional",
		"params.d_zero": "custom/default", "params.e_false": "custom/default", "params.f_must": "custom/user_required",
		"params.g_opt": "custom/optional", "uses.gateway.base_url": "system/system",
		"platform.no.such": "system/system", "platform.app.url": "system/system",
		// requires.* and components.* are no references format 3 has (lint
		// refuses them): no row.
	}
	if len(got) != len(want) || len(rows) != len(want) {
		t.Fatalf("行数 %d，要 %d：%v", len(rows), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %s，要 %s", k, got[k], v)
		}
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1].Name >= rows[i].Name {
			t.Fatalf("没按名字排序：%s %s", rows[i-1].Name, rows[i].Name)
		}
	}
	for _, r := range rows {
		switch r.Name {
		case "platform.no.such":
			if r.Known == nil || *r.Known {
				t.Errorf("表里没有的平台变量要标 known=false：%+v", r)
			}
		case "params.a_url":
			if strings.Join(r.UsedIn, ",") != "/files/x.conf" {
				t.Errorf("同一处引用两次只记一次：%v", r.UsedIn)
			}
		case "params.g_opt":
			if r.UsedIn == nil || len(r.UsedIn) != 0 {
				t.Errorf("没引用的参数 used_in 要是空列表：%#v", r.UsedIn)
			}
		}
	}
	if VarsFingerprint(nil, nil, nil) != VarsFingerprint([]VarRow{}, []VarUse{}, []VarProvide{}) ||
		string(varsFingerprintText(nil, nil, nil)) != `{"v":2,"vars":[],"uses":[],"provides":[]}` {
		t.Fatalf("没有变量时的指纹原文：%s", varsFingerprintText(nil, nil, nil))
	}
	// Order of the input does not matter.
	rev := make([]VarRow, len(rows))
	for i, r := range rows {
		rev[len(rows)-1-i] = r
	}
	if VarsFingerprint(rev, nil, nil) != VarsFingerprint(rows, nil, nil) {
		t.Fatal("行的顺序不该影响指纹")
	}
}

// CAT-LINT-18: automatic delivery with something the user must fill in.
func TestLintAutoTierNeedsNoUserInput(t *testing.T) {
	dir := copyTree(t)
	file := filepath.Join(dir, "apps", "new-api", manifestName)
	edit(t, file, "\nenv:\n", "  - key: site_name\n    type: string\n    required: true\n  - key: license_key\n    type: secret\n    required: true\n\nenv:\n")
	r, err := Lint(dir)
	if err != nil {
		t.Fatal(err)
	}
	var hit *Problem
	for i := range r.Problems {
		if r.Problems[i].Code == CodeAutoTier {
			hit = &r.Problems[i]
		}
	}
	if hit == nil || codes(r)[CodeAutoTier] != 1 || hit.Level != LevelError || hit.File != "apps/new-api/manifest.yaml" || hit.Path != "/delivery/tier" ||
		!strings.Contains(hit.Message, "2 个") || !strings.Contains(hit.Message, "site_name") || !strings.Contains(hit.Message, "license_key") {
		t.Fatalf("要报一个 %s 并列出两个参数：%+v", CodeAutoTier, r.Problems)
	}
	if _, err := Build(dir, 1); CodeOf(err) != CodeBuildLint {
		t.Fatalf("有 %s 不能打包：%v", CodeAutoTier, err)
	}
	// "steps" delivery may ask the user for things.
	edit(t, file, "delivery:\n  tier: auto\n", "delivery:\n  tier: steps\n")
	if r, _ = Lint(dir); codes(r)[CodeAutoTier] != 0 || r.Errors() != 0 {
		t.Fatalf("交付方式是几步时不查：%+v", r.Problems)
	}
	// A default or generate settles it.
	dir = copyTree(t)
	file = filepath.Join(dir, "apps", "new-api", manifestName)
	edit(t, file, "\nenv:\n", "  - key: site_name\n    type: string\n    required: true\n    default: 我的网关\n  - key: license_key\n    type: secret\n    required: true\n    generate: { length: 32 }\n\nenv:\n")
	if r, _ = Lint(dir); r.Errors() != 0 {
		t.Fatalf("有默认值或能生成就不算必须用户填：%+v", r.Problems)
	}
	// An empty default does not: installing would stop at "missing required
	// parameter", so it still counts as something the user must fill in.
	dir = copyTree(t)
	edit(t, filepath.Join(dir, "apps", "new-api", manifestName), "\nenv:\n", "  - key: site_name\n    type: string\n    required: true\n    default: \"\"\n\nenv:\n")
	if r, _ = Lint(dir); codes(r)[CodeAutoTier] != 1 {
		t.Fatalf("必填又只给了空的默认值，要报 %s：%+v", CodeAutoTier, r.Problems)
	}
	// An optional one is fine too.
	dir = copyTree(t)
	edit(t, filepath.Join(dir, "apps", "new-api", manifestName), "\nenv:\n", "  - key: site_name\n    type: string\n\nenv:\n")
	if r, _ = Lint(dir); r.Errors() != 0 {
		t.Fatalf("可选参数不算：%+v", r.Problems)
	}
}

func TestHardcodedHints(t *testing.T) {
	m := &Manifest{Env: []EnvVar{
		{Key: "TZ", Value: "Asia/Shanghai"},
		{Key: "TZ2", Value: "America/Argentina/Buenos_Aires"},
		{Key: "HF_ENDPOINT", Value: "https://hf-mirror.com"},
		{Key: "DB", Value: "postgres://db:5432/app"},
		{Key: "API_KEY", Value: "sk-abcdef0123456789abcdef0123456789"},
		{Key: "PLAIN", Value: "1"},
		{Key: "PATH_LIKE", Value: "/data/one-api.db"},
		{Key: "WORDS", Value: "abcdefghijklmnopqrstuvwxyz"},
		{Key: "DIGITS", Value: "12345678901234567890123"},
		{Key: "RATIO", Value: "Input/Output"},
		{Key: "FROM", From: "platform.tz"},
	}}
	var got []string
	for _, h := range hardcodedHints(m) {
		got = append(got, h.Where+":"+h.Kind+":"+h.Value)
		if h.Message == "" {
			t.Errorf("%s 没有说明", h.Where)
		}
	}
	want := "/env/TZ:timezone:Asia/Shanghai,/env/TZ2:timezone:America/Argentina/Buenos_Aires,/env/HF_ENDPOINT:url:https://hf-mirror.com,/env/DB:url:postgres://db:5432/app,/env/API_KEY:secret:sk-a…"
	if strings.Join(got, ",") != want {
		t.Fatalf("提醒：\n%s\n要：\n%s", strings.Join(got, ","), want)
	}
	if h := hardcodedHints(&Manifest{}); h == nil || len(h) != 0 {
		t.Fatalf("没有提醒时要给空列表：%#v", h)
	}
	// Hints never make lint fail.
	dir := copyTree(t)
	edit(t, filepath.Join(dir, "apps", "new-api", manifestName), "  - { key: TZ, from: platform.tz }\n", "  - { key: TZ, value: Asia/Shanghai }\n")
	if r, _ := Lint(dir); len(r.Problems) != 0 {
		t.Fatalf("写死的值只在变量报告里提醒：%+v", r.Problems)
	}
	res, err := CatalogVars(dir, "new-api")
	if err != nil || len(res.Apps) != 1 || len(res.Apps[0].Hardcoded) != 1 || res.Apps[0].Hardcoded[0].Kind != "timezone" {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestCatalogVars(t *testing.T) {
	good := filepath.Join("testdata", "good")
	res, err := CatalogVars(good, "")
	if err != nil || len(res.Problems) != 0 || len(res.Apps) != 2 || res.Apps[0].App != "hermes-agent" || res.Apps[1].App != "new-api" {
		t.Fatalf("%+v %v", res, err)
	}
	if res.Apps[1].File != "apps/new-api/manifest.yaml" {
		t.Fatalf("file = %s", res.Apps[1].File)
	}
	one, err := CatalogVars(good, "new-api")
	if err != nil || len(one.Apps) != 1 || one.Apps[0].Fingerprint != res.Apps[1].Fingerprint {
		t.Fatalf("%+v %v", one, err)
	}
	if res, err := CatalogVars(good, "no-such-app"); err != nil || len(res.Apps) != 0 || len(res.Problems) != 1 ||
		res.Problems[0].Code != CodeRead || res.Problems[0].File != "apps/no-such-app/manifest.yaml" || !strings.Contains(res.Problems[0].Message, "no-such-app") {
		t.Fatalf("没有的应用要算问题：%+v %v", res, err)
	}
	// A manifest that does not match the schema gets no report.
	dir := copyTree(t)
	edit(t, filepath.Join(dir, "apps", "new-api", manifestName), "maintainer: acornfox", "maintainer: acornfox\nsidecars: []")
	res, err = CatalogVars(dir, "")
	if err != nil || len(res.Apps) != 1 || res.Apps[0].App != "hermes-agent" || len(res.Problems) != 1 || res.Problems[0].Code != CodeSchema {
		t.Fatalf("%+v %v", res, err)
	}
	// An empty directory: nothing to report, no error.
	if res, err = CatalogVars(t.TempDir(), ""); err != nil || res.Apps == nil || len(res.Apps) != 0 || res.Problems == nil || len(res.Problems) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	// A directory that is not there (or a file) is a problem, not an empty
	// catalog.
	notDir := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(notDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(t.TempDir(), "no-such-dir"), notDir} {
		for _, app := range []string{"", "new-api"} {
			res, err = CatalogVars(dir, app)
			if err != nil || len(res.Apps) != 0 || len(res.Problems) != 1 || res.Problems[0].Code != CodeRead ||
				res.Problems[0].Level != LevelError || !strings.Contains(res.Problems[0].Message, dir) {
				t.Fatalf("目录 %s 不存在要算问题：%+v %v", dir, res, err)
			}
		}
	}
}

// The services an app needs and offers, read from the services test catalog
// (one provider, one consumer, three service types).
func TestVarReportServices(t *testing.T) {
	res, err := CatalogVars(servicesDir, "")
	if err != nil || len(res.Problems) != 0 || len(res.Apps) != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if res.KindLabels["demo_token"] != "样例令牌" || res.KindLabels["smtp"] != "邮箱（发信）" || res.KindLabels["llm_gateway"] != "大模型接口" || len(res.KindLabels) != 3 {
		t.Fatalf("服务类型的中文名 %v", res.KindLabels)
	}
	consumer, provider := res.Apps[0], res.Apps[1]
	if consumer.App != "demo-consumer" || provider.App != "demo-provider" {
		t.Fatalf("%s %s", consumer.App, provider.App)
	}

	// The whole report as JSON: both lists are always written, every key of
	// an entry too (false and "" included), and the fingerprint version.
	shape := func(r VarReport) map[string]any {
		t.Helper()
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	jsonOf := func(v any) string {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	c := shape(consumer)
	if got := jsonOf(c["uses"]); got != `[{"dedicated":false,"kind":"demo_token","name":"demo","optional":false,"without":""},{"dedicated":false,"kind":"smtp","name":"mail","optional":true,"without":"能用，但发不了通知邮件"}]` {
		t.Fatalf("使用方的 uses：%s", got)
	}
	if got := jsonOf(c["provides"]); got != `[]` || c["fingerprint_version"] != float64(2) {
		t.Fatalf("使用方的 provides 要是空数组：%s，版本 %v", got, c["fingerprint_version"])
	}
	p := shape(provider)
	if got := jsonOf(p["provides"]); got != `[{"has_check":true,"has_issue":true,"has_revoke":true,"kind":"demo_token","values":[{"how":"issued","key":"token"},{"how":"computed","key":"url"}]}]` {
		t.Fatalf("提供方的 provides：%s", got)
	}
	if got := jsonOf(p["uses"]); got != `[]` {
		t.Fatalf("提供方的 uses 要是空数组：%s", got)
	}
	// A needed service's items are system variables, as before.
	var names []string
	for _, r := range consumer.Vars {
		names = append(names, r.Name+"="+r.Class)
	}
	if strings.Join(names, ",") != "uses.demo.token=system,uses.demo.url=system,uses.mail.host=system,uses.mail.port=system" {
		t.Fatalf("使用方的变量 %v", names)
	}

	// An app with neither: empty lists, never nil.
	rep, err := ManifestVarReport([]byte(strings.Replace(string(readServicesManifest(t, "demo-consumer")),
		"uses:\n  - { name: demo, kind: demo_token }\n  - { name: mail, kind: smtp, optional: true, without: 能用，但发不了通知邮件 }\n", "", 1)))
	if err != nil || rep.Uses == nil || len(rep.Uses) != 0 || rep.Provides == nil || len(rep.Provides) != 0 {
		t.Fatalf("没有服务时要是空数组：%+v %v", rep, err)
	}
	if m := shape(rep); jsonOf(m["uses"]) != "[]" || jsonOf(m["provides"]) != "[]" {
		t.Fatalf("没有服务时 JSON 里也要写空数组：%v %v", m["uses"], m["provides"])
	}
	rep, err = ManifestVarReport(readManifest(t, filepath.Join("testdata", "good"), "new-api"))
	if err != nil {
		t.Fatal(err)
	}
	if got := jsonOf(shape(rep)["provides"]); got != `[{"has_check":true,"has_issue":true,"has_revoke":true,"kind":"llm_gateway","values":[{"how":"issued","key":"api_key"},{"how":"computed","key":"base_url"},{"how":"ask","key":"model"}]}]` {
		t.Fatalf("New API 草稿的 provides：%s", got)
	}

	// The three ways an item is obtained; an ask item keeps its sentence for
	// people.
	prov := string(readServicesManifest(t, "demo-provider"))
	asked := strings.Replace(prov, "      token: { issued: token }\n", "      token: { ask: 在应用里新建一个令牌，贴进来, secret: true }\n", 1)
	if asked == prov {
		t.Fatal("清单里没有 token: { issued: token }")
	}
	rep, err = ManifestVarReport([]byte(asked))
	if err != nil {
		t.Fatal(err)
	}
	if v := rep.Provides[0].Values; len(v) != 2 || v[0] != (VarProvideValue{Key: "token", How: HowAsk, Ask: "在应用里新建一个令牌，贴进来"}) || v[1] != (VarProvideValue{Key: "url", How: HowComputed}) {
		t.Fatalf("三种来法：%+v", v)
	}
	for _, how := range []string{HowComputed, HowIssued, HowAsk} {
		if HowLabels[how] == "" {
			t.Errorf("%s 没有中文名", how)
		}
	}
}

// Sorting: needed services by name, offered services by type, items by key,
// whatever order the manifest writes them in.
func TestVarReportServicesSorted(t *testing.T) {
	m := &Manifest{
		Uses: []Use{{Name: "zeta", Kind: "a"}, {Name: "alpha", Kind: "z"}, {Name: "mid", Kind: "m"}},
		Provides: []Provide{
			{Kind: "smtp", Values: map[string]ValueSource{"port": {Template: "25"}, "host": {Template: "{{self.host}}"}}},
			{Kind: "demo_token", Values: map[string]ValueSource{"url": {Template: "x"}, "token": {Issued: "token"}}},
		},
	}
	uses, provides := ServiceUses(m), ServiceProvides(m)
	if uses[0].Name != "alpha" || uses[1].Name != "mid" || uses[2].Name != "zeta" {
		t.Fatalf("需要的服务没按名字排：%+v", uses)
	}
	if provides[0].Kind != "demo_token" || provides[1].Kind != "smtp" || provides[0].Values[0].Key != "token" || provides[1].Values[0].Key != "host" {
		t.Fatalf("提供的服务没按类型、项没按名字排：%+v", provides)
	}
	// The fingerprint does not depend on the order of its inputs.
	rev := []VarUse{uses[2], uses[1], uses[0]}
	revP := []VarProvide{provides[1], {Kind: provides[0].Kind, Values: []VarProvideValue{provides[0].Values[1], provides[0].Values[0]}}}
	if VarsFingerprint(nil, rev, revP) != VarsFingerprint(nil, uses, provides) {
		t.Fatal("输入的顺序不该影响指纹")
	}
	if ServiceUses(&Manifest{}) == nil || ServiceProvides(&Manifest{}) == nil {
		t.Fatal("没有时要是空列表，不是 nil")
	}
}

// The fingerprint changes when what the report shows about services changes,
// and only then. The "without" sentence is in it: it is what the user is told
// they lose without an optional service, and the reviewer confirms it.
func TestVarsFingerprintServices(t *testing.T) {
	consumer := string(readServicesManifest(t, "demo-consumer"))
	provider := string(readServicesManifest(t, "demo-provider"))
	fp := func(s string) string {
		t.Helper()
		rep, err := ManifestVarReport([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return rep.Fingerprint
	}
	change := func(base, old, new string) string {
		t.Helper()
		if !strings.Contains(base, old) {
			t.Fatalf("清单里没有 %q", old)
		}
		return strings.Replace(base, old, new, 1)
	}
	const mailLine = "  - { name: mail, kind: smtp, optional: true, without: 能用，但发不了通知邮件 }\n"
	const demoLine = "  - { name: demo, kind: demo_token }\n"
	const urlLine = "      url: \"{{self.internal_url}}/api/{{params.region_hint}}\"\n"
	const tokenLine = "      token: { issued: token }\n"
	const checkSteps = "    check:\n      - http:\n          method: GET\n          path: /api/whoami\n          headers: { Authorization: \"Bearer {{issued.token}}\" }\n          expect_status: [200]\n"
	asked := change(provider, tokenLine, "      token: { ask: 在应用里新建一个令牌，贴进来 }\n")

	for _, tc := range []struct{ base, name, changed string }{
		{consumer, "需要的服务换个顺序", change(change(consumer, mailLine, ""), demoLine, demoLine+mailLine)},
		{consumer, "介绍文字", change(consumer, "只为测试清单格式", "只为测试")},
		{provider, "提供的项换个顺序", change(change(provider, urlLine, ""), tokenLine, tokenLine+urlLine)},
		{provider, "步骤的内容", change(provider, "path: /api/whoami", "path: /api/me")},
		{provider, "算出来的值的写法", change(provider, "/api/{{params.region_hint}}", "/v2/{{params.region_hint}}")},
		{asked, "要用户做的那一步的说法", change(asked, "在应用里新建一个令牌，贴进来", "去应用的设置页建一个令牌")},
	} {
		if fp(tc.changed) != fp(tc.base) {
			t.Errorf("只改了%s，指纹不该变", tc.name)
		}
	}
	for _, tc := range []struct{ base, name, changed string }{
		{consumer, "没有会怎样的说法", change(consumer, "能用，但发不了通知邮件", "能用，但收不到提醒")},
		{consumer, "可选改成必须", change(consumer, mailLine, "  - { name: mail, kind: smtp }\n")},
		{consumer, "只接没有别的使用方在用的服务", change(consumer, demoLine, "  - { name: demo, kind: demo_token, dedicated: true }\n")},
		{consumer, "需要的服务类型换了", change(consumer, "kind: smtp,", "kind: smtp_relay,")},
		{consumer, "少了一个需要的服务", change(consumer, mailLine, "")},
		{provider, "一项改成要用户做一步", asked},
		{provider, "一项改成平台算", change(provider, tokenLine, "      token: \"{{params.admin_password}}\"\n")},
		{provider, "少了检查步骤", change(provider, checkSteps, "")},
		{provider, "多了一项", change(provider, tokenLine, tokenLine+"      region: \"{{params.region_hint}}\"\n")},
		{provider, "提供的服务类型换了", change(provider, "  - kind: demo_token\n", "  - kind: other_token\n")},
	} {
		if fp(tc.changed) == fp(tc.base) {
			t.Errorf("%s，指纹应该变", tc.name)
		}
	}
}
