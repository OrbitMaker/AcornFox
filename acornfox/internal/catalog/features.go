package catalog

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// The feature table says, for everything a manifest or a recipe can write,
// from which platform version on it is understood: one row per field and one
// per enum value of the format, named after the schema.
//
//	manifest:/params/*/type               the field params[].type
//	manifest:/params/*/type=llm_provider  that field holding llm_provider
//	recipe:/apps/*/preset                 a recipe's apps[].preset
//
// "*" stands for "each item of the list". Platform variables carry their own
// version in the PlatformVars table (Since).
//
// Lint walks a manifest, looks up what it actually writes and works out the
// lowest platform version that can install it (ManifestMinPlatform). A
// manifest that declares less is refused (CAT-LINT-17): the declared version
// is the only thing an older platform checks before installing, so it must
// never be lower than what the manifest uses.
//
// Adding to the format: add the field or value to the schema AND a row here
// with the version that first ships it. A test fails when a schema path has
// no row. Something a schema cannot express (a new template syntax, a new
// kind of reference) needs a row here plus a detector in manifestMinPlatform.

// BasePlatform is the first platform version that installs from a catalog.
// Everything it understands is "since BasePlatform"; nothing can require less.
const BasePlatform = "0.5.0"

const v050 = BasePlatform

// v051 is the platform version that first understands the per-architecture
// image archives (release.images[].artifacts).
const v051 = "0.5.1"

// Feature is one row of the table.
type Feature struct {
	Key   string `json:"key"`
	Since string `json:"since"`
}

var featureTable = []Feature{
	// Manifest (schema/manifest.schema.json).
	{"manifest:/id", v050},
	{"manifest:/schema", v050},
	{"manifest:/schema=2", v050},
	{"manifest:/source", v050},
	{"manifest:/source=upstream", v050},
	{"manifest:/source=community", v050},
	{"manifest:/source=official", v050},
	{"manifest:/maintainer", v050},
	{"manifest:/app", v050},
	{"manifest:/app/name", v050},
	{"manifest:/app/summary", v050},
	{"manifest:/app/category", v050},
	{"manifest:/app/category=ai", v050},
	{"manifest:/app/category=ai-agent", v050},
	{"manifest:/app/category=ai-gateway", v050},
	{"manifest:/app/category=automation", v050},
	{"manifest:/app/category=site", v050},
	{"manifest:/app/category=database", v050},
	{"manifest:/app/category=monitor", v050},
	{"manifest:/app/category=dev", v050},
	{"manifest:/app/category=files", v050},
	{"manifest:/app/tags", v050},
	{"manifest:/app/tags/*", v050},
	{"manifest:/app/keywords", v050},
	{"manifest:/app/keywords/*", v050},
	{"manifest:/app/icon", v050},
	{"manifest:/app/homepage", v050},
	{"manifest:/app/license", v050},
	{"manifest:/app/source_code", v050},
	{"manifest:/lifecycle", v050},
	{"manifest:/lifecycle/min_platform", v050},
	{"manifest:/lifecycle/status", v050},
	{"manifest:/lifecycle/status=active", v050},
	{"manifest:/lifecycle/status=deprecated", v050},
	{"manifest:/resources", v050},
	{"manifest:/resources/memory_mb", v050},
	{"manifest:/resources/memory_mb/min", v050},
	{"manifest:/resources/memory_mb/recommended", v050},
	{"manifest:/resources/cpu", v050},
	{"manifest:/resources/cpu/min", v050},
	{"manifest:/resources/disk_mb", v050},
	{"manifest:/resources/disk_mb/min", v050},
	{"manifest:/resources/platforms", v050},
	{"manifest:/resources/platforms/*", v050},
	{"manifest:/resources/platforms/*=linux/amd64", v050},
	{"manifest:/resources/platforms/*=linux/arm64", v050},
	{"manifest:/resources/gpu", v050},
	{"manifest:/resources/gpu=none", v050},
	{"manifest:/resources/gpu=optional", v050},
	{"manifest:/resources/gpu=required", v050},
	{"manifest:/release", v050},
	{"manifest:/release/version", v050},
	{"manifest:/release/images", v050},
	{"manifest:/release/images/*", v050},
	{"manifest:/release/images/*/name", v050},
	{"manifest:/release/images/*/repository", v050},
	{"manifest:/release/images/*/tag", v050},
	{"manifest:/release/images/*/digest", v050},
	{"manifest:/release/images/*/size_mb", v050},
	{"manifest:/release/images/*/artifact", v050},
	{"manifest:/release/images/*/artifact_sha256", v050},
	{"manifest:/release/images/*/artifact_size", v050},
	{"manifest:/release/images/*/artifacts", v051},
	{"manifest:/release/images/*/artifacts/amd64", v051},
	{"manifest:/release/images/*/artifacts/amd64/file", v051},
	{"manifest:/release/images/*/artifacts/amd64/sha256", v051},
	{"manifest:/release/images/*/artifacts/amd64/size", v051},
	{"manifest:/release/images/*/artifacts/arm64", v051},
	{"manifest:/release/images/*/artifacts/arm64/file", v051},
	{"manifest:/release/images/*/artifacts/arm64/sha256", v051},
	{"manifest:/release/images/*/artifacts/arm64/size", v051},
	{"manifest:/container", v050},
	{"manifest:/container/command", v050},
	{"manifest:/container/command/*", v050},
	{"manifest:/container/user", v050},
	{"manifest:/container/shm_mb", v050},
	{"manifest:/container/pids_limit", v050},
	{"manifest:/container/no_new_privileges", v050},
	{"manifest:/endpoints", v050},
	{"manifest:/endpoints/*", v050},
	{"manifest:/endpoints/*/name", v050},
	{"manifest:/endpoints/*/port", v050},
	{"manifest:/endpoints/*/protocol", v050},
	{"manifest:/endpoints/*/protocol=http", v050},
	{"manifest:/endpoints/*/primary", v050},
	{"manifest:/endpoints/*/health", v050},
	{"manifest:/endpoints/*/health/path", v050},
	{"manifest:/endpoints/*/health/expect_json", v050},
	{"manifest:/endpoints/*/health/timeout_s", v050},
	{"manifest:/exposure", v050},
	{"manifest:/exposure/default", v050},
	{"manifest:/exposure/default=local", v050},
	{"manifest:/exposure/default=lan", v050},
	{"manifest:/exposure/default=public", v050},
	{"manifest:/volumes", v050},
	{"manifest:/volumes/*", v050},
	{"manifest:/volumes/*/name", v050},
	{"manifest:/volumes/*/path", v050},
	{"manifest:/volumes/*/backup", v050},
	{"manifest:/params", v050},
	{"manifest:/params/*", v050},
	{"manifest:/params/*/key", v050},
	{"manifest:/params/*/type", v050},
	{"manifest:/params/*/type=string", v050},
	{"manifest:/params/*/type=int", v050},
	{"manifest:/params/*/type=bool", v050},
	{"manifest:/params/*/type=enum", v050},
	{"manifest:/params/*/type=secret", v050},
	{"manifest:/params/*/type=url", v050},
	{"manifest:/params/*/type=domain", v050},
	{"manifest:/params/*/type=email", v050},
	{"manifest:/params/*/type=llm_provider", v050},
	{"manifest:/params/*/label", v050},
	{"manifest:/params/*/hint", v050},
	{"manifest:/params/*/default", v050},
	{"manifest:/params/*/options", v050},
	{"manifest:/params/*/pattern", v050},
	{"manifest:/params/*/min", v050},
	{"manifest:/params/*/max", v050},
	{"manifest:/params/*/required", v050},
	{"manifest:/params/*/hidden", v050},
	{"manifest:/params/*/generate", v050},
	{"manifest:/params/*/generate/length", v050},
	{"manifest:/params/*/generate/charset", v050},
	{"manifest:/params/*/generate/charset=alnum", v050},
	{"manifest:/params/*/generate/charset=hex", v050},
	{"manifest:/params/*/editable", v050},
	{"manifest:/params/*/editable=never", v050},
	{"manifest:/params/*/editable=always", v050},
	{"manifest:/params/*/editable=reinstall", v050},
	{"manifest:/params/*/scope", v050},
	{"manifest:/params/*/scope=all", v050},
	{"manifest:/params/*/scope=standalone", v050},
	{"manifest:/params/*/scope=cloud", v050},
	{"manifest:/params/*/llm", v050},
	{"manifest:/params/*/llm/platform", v050},
	{"manifest:/params/*/llm/platform=default", v050},
	{"manifest:/params/*/llm/platform=optional", v050},
	{"manifest:/params/*/llm/platform=off", v050},
	{"manifest:/params/*/llm/local_gateway", v050},
	{"manifest:/params/*/llm/needs", v050},
	{"manifest:/params/*/llm/needs/*", v050},
	{"manifest:/params/*/llm/needs/*=chat", v050},
	{"manifest:/params/*/llm/needs/*=embedding", v050},
	{"manifest:/params/*/llm/needs/*=rerank", v050},
	{"manifest:/params/*/llm/needs/*=image", v050},
	{"manifest:/params/*/llm/needs/*=tts", v050},
	{"manifest:/params/*/llm/min_context", v050},
	{"manifest:/env", v050},
	{"manifest:/env/*", v050},
	{"manifest:/env/*/key", v050},
	{"manifest:/env/*/from", v050},
	{"manifest:/env/*/value", v050},
	{"manifest:/env/*/secret", v050},
	{"manifest:/files", v050},
	{"manifest:/files/*", v050},
	{"manifest:/files/*/volume", v050},
	{"manifest:/files/*/path", v050},
	{"manifest:/files/*/template", v050},
	{"manifest:/files/*/mode", v050},
	{"manifest:/files/*/owner", v050},
	{"manifest:/files/*/overwrite", v050},
	{"manifest:/files/*/overwrite=never", v050},
	{"manifest:/files/*/overwrite=install", v050},
	{"manifest:/init", v050},
	{"manifest:/init/*", v050},
	{"manifest:/init/*/name", v050},
	{"manifest:/init/*/when", v050},
	{"manifest:/init/*/when=after_healthy", v050},
	{"manifest:/init/*/http", v050},
	{"manifest:/init/*/http/method", v050},
	{"manifest:/init/*/http/method=GET", v050},
	{"manifest:/init/*/http/method=POST", v050},
	{"manifest:/init/*/http/method=PUT", v050},
	{"manifest:/init/*/http/path", v050},
	{"manifest:/init/*/http/json", v050},
	{"manifest:/init/*/http/expect_json", v050},
	{"manifest:/first_login", v050},
	{"manifest:/first_login/*", v050},
	{"manifest:/first_login/*/label", v050},
	{"manifest:/first_login/*/from", v050},
	{"manifest:/first_login/*/value", v050},
	{"manifest:/first_login/*/secret", v050},
	{"manifest:/provides", v050},
	{"manifest:/provides/*", v050},
	{"manifest:/provides/*/kind", v050},
	{"manifest:/provides/*/kind=llm_gateway", v050},
	{"manifest:/provides/*/adapter", v050},
	{"manifest:/provides/*/adapter=new-api", v050},
	{"manifest:/provides/*/endpoint", v050},
	{"manifest:/provides/*/base_path", v050},
	{"manifest:/uses", v050},
	{"manifest:/uses/*", v050},
	{"manifest:/uses/*/name", v050},
	{"manifest:/uses/*/kind", v050},
	{"manifest:/uses/*/kind=llm_gateway", v050},
	{"manifest:/uses/*/for_param", v050},
	{"manifest:/uses/*/optional", v050},
	{"manifest:/cn", v050},
	{"manifest:/cn/env", v050},
	{"manifest:/cn/env/*", v050},
	{"manifest:/cn/env/*/key", v050},
	{"manifest:/cn/env/*/value", v050},
	{"manifest:/cn/verified", v050},
	{"manifest:/delivery", v050},
	{"manifest:/delivery/tier", v050},
	{"manifest:/delivery/tier=auto", v050},
	{"manifest:/delivery/tier=steps", v050},
	{"manifest:/delivery/minutes", v050},
	{"manifest:/delivery/video", v050},
	{"manifest:/delivery/user_steps", v050},
	{"manifest:/delivery/user_steps/*", v050},
	{"manifest:/upgrade", v050},
	{"manifest:/upgrade/from", v050},
	{"manifest:/upgrade/breaking", v050},
	{"manifest:/upgrade/notes", v050},
	{"manifest:/uninstall", v050},
	{"manifest:/uninstall/data", v050},
	{"manifest:/uninstall/data/*", v050},

	// Recipe (schema/recipe.schema.json).
	{"recipe:/id", v050},
	{"recipe:/schema", v050},
	{"recipe:/schema=1", v050},
	{"recipe:/title", v050},
	{"recipe:/summary", v050},
	{"recipe:/keywords", v050},
	{"recipe:/keywords/*", v050},
	{"recipe:/apps", v050},
	{"recipe:/apps/*", v050},
	{"recipe:/apps/*/app", v050},
	{"recipe:/apps/*/reuse_installed", v050},
	{"recipe:/apps/*/preset", v050},
	{"recipe:/links", v050},
	{"recipe:/links/*", v050},
	{"recipe:/links/*/from", v050},
	{"recipe:/links/*/to", v050},
	{"recipe:/links/*/uses", v050},
	{"recipe:/delivery", v050},
	{"recipe:/delivery/tier", v050},
	{"recipe:/delivery/tier=auto", v050},
	{"recipe:/delivery/tier=steps", v050},
	{"recipe:/delivery/minutes", v050},
	{"recipe:/delivery/memory_mb", v050},
	{"recipe:/delivery/video", v050},
	{"recipe:/delivery/user_steps", v050},
	{"recipe:/delivery/user_steps/*", v050},
	{"recipe:/min_platform", v050}, // older platforms ignore it, so writing it costs nothing
}

// featureSince is featureTable by key. Tests add rows to it to stand in for
// a future platform version.
var featureSince = func() map[string]string {
	m := make(map[string]string, len(featureTable))
	for _, f := range featureTable {
		m[f.Key] = f.Since
	}
	return m
}()

// Features returns the table sorted by key (a copy).
func Features() []Feature {
	out := make([]Feature, 0, len(featureSince))
	for k, v := range featureSince {
		out = append(out, Feature{Key: k, Since: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Requirement is one thing a manifest or recipe writes that needs a platform
// version: a format feature ("manifest:/params/*/type=llm_provider"), a
// platform variable ("platform.tz") or, for a recipe, a member app
// ("app:new-api"). Where points at it inside the file.
type Requirement struct {
	Feature string `json:"feature"`
	Where   string `json:"where"`
	Since   string `json:"since"`
}

// MinPlatform is the lowest platform version that understands a manifest or
// recipe. Reasons lists what raised it above BasePlatform (each feature once,
// at its first use); it is empty when Version is BasePlatform.
type MinPlatform struct {
	Version string        `json:"version"`
	Reasons []Requirement `json:"reasons"`
}

// ManifestMinPlatform works out the lowest platform version that understands
// the manifest in raw (manifest.yaml bytes). It needs the raw file, not a
// decoded Manifest, to see which fields are written. It does not validate:
// fields the schema does not know are skipped (schema validation reports
// them), as are platform variables that are not in the table (CAT-LINT-16).
func ManifestMinPlatform(raw []byte) (MinPlatform, error) {
	doc, err := decodeYAMLDoc(raw)
	if err != nil {
		return MinPlatform{}, err
	}
	m, err := ParseManifest(raw)
	if err != nil {
		return MinPlatform{}, err
	}
	return manifestMinPlatform(doc, m)
}

// RecipeMinPlatform is ManifestMinPlatform for recipe.yaml bytes. appMin
// gives the lowest platform version of each app in the catalog (by id); a
// recipe needs at least what each of its apps needs. Apps missing from
// appMin are skipped.
func RecipeMinPlatform(raw []byte, appMin map[string]string) (MinPlatform, error) {
	doc, err := decodeYAMLDoc(raw)
	if err != nil {
		return MinPlatform{}, err
	}
	var r Recipe
	if err := yaml.Unmarshal(raw, &r); err != nil {
		return MinPlatform{}, err
	}
	return recipeMinPlatform(doc, &r, appMin)
}

// manifestMinPlatform fails only when the built-in schema itself cannot be
// walked (a bug in the program, not in the manifest).
func manifestMinPlatform(doc any, m *Manifest) (MinPlatform, error) {
	uses, err := usedFeatures("manifest", manifestSchemaF, doc)
	if err != nil {
		return MinPlatform{}, err
	}
	for _, r := range manifestRefs(m) {
		name, ok := cutPlatform(r.Ref)
		if !ok {
			continue
		}
		if pv, ok := LookupPlatformVar(name); ok {
			uses = append(uses, Requirement{Feature: r.Ref, Where: r.Where, Since: pv.Since})
		}
	}
	return minPlatformOf(uses), nil
}

func recipeMinPlatform(doc any, r *Recipe, appMin map[string]string) (MinPlatform, error) {
	uses, err := usedFeatures("recipe", recipeSchemaF, doc)
	if err != nil {
		return MinPlatform{}, err
	}
	for _, a := range r.Apps {
		if v, ok := appMin[a.App]; ok {
			uses = append(uses, Requirement{Feature: "app:" + a.App, Where: "/apps/" + a.App, Since: v})
		}
	}
	return minPlatformOf(uses), nil
}

// minPlatformOf takes the highest Since and keeps what reached it.
func minPlatformOf(uses []Requirement) MinPlatform {
	out := MinPlatform{Version: BasePlatform, Reasons: []Requirement{}}
	for _, u := range uses {
		if CompareVersions(u.Since, out.Version) > 0 {
			out.Version = u.Since
		}
	}
	if out.Version == BasePlatform {
		return out
	}
	seen := map[string]bool{}
	for _, u := range uses {
		if CompareVersions(u.Since, out.Version) == 0 && !seen[u.Feature] {
			seen[u.Feature] = true
			out.Reasons = append(out.Reasons, u)
		}
	}
	return out
}

// usedFeatures walks doc (a decoded YAML document, JSON types) along the
// schema and returns every table row it uses, in document order. Paths the
// schema does not describe are not followed: free-form objects (a health
// check's expect_json, a recipe's preset) count as one field.
//
// It fails when the schema cannot be read or one of its references cannot be
// followed: a field behind such a reference would silently count for
// nothing, and a manifest using it would be given too low a version.
func usedFeatures(kind, schemaFile string, doc any) ([]Requirement, error) {
	root, err := schemaDoc(schemaFile)
	if err != nil {
		return nil, err
	}
	var out []Requirement
	if err := walkFeatures(kind, root, root, doc, "", "", &out); err != nil {
		return nil, fmt.Errorf("内置格式定义 %s 走不通：%w", schemaFile, err)
	}
	return out, nil
}

func walkFeatures(kind string, root, node map[string]any, inst any, key, where string, out *[]Requirement) error {
	node, err := resolveRef(root, node)
	if err != nil {
		return err
	}
	add := func(k string) {
		if since, ok := featureSince[kind+":"+k]; ok {
			*out = append(*out, Requirement{Feature: kind + ":" + k, Where: where, Since: since})
		}
	}
	if key != "" {
		add(key)
	}
	if s, ok := scalarText(inst); ok {
		_, isEnum := node["enum"]
		_, isConst := node["const"]
		if isEnum || isConst {
			add(key + "=" + s)
		}
	}
	if props, ok := node["properties"].(map[string]any); ok {
		if obj, ok := inst.(map[string]any); ok {
			names := make([]string, 0, len(obj))
			for k := range obj {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				if sub, ok := props[k].(map[string]any); ok {
					if err := walkFeatures(kind, root, sub, obj[k], key+"/"+k, where+"/"+k, out); err != nil {
						return err
					}
				}
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		if arr, ok := inst.([]any); ok {
			for i, e := range arr {
				if err := walkFeatures(kind, root, items, e, key+"/*", where+"/"+itemName(e, i), out); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// resolveRef follows a local reference ("$ref": "#/$defs/x") to the node it
// names. A node without "$ref" is returned as it is. A reference of any other
// shape, or to a definition that is not there, is an error.
func resolveRef(root, node map[string]any) (map[string]any, error) {
	raw, has := node["$ref"]
	if !has {
		return node, nil
	}
	const prefix = "#/$defs/"
	ref, _ := raw.(string)
	if !strings.HasPrefix(ref, prefix) || len(ref) == len(prefix) {
		return nil, fmt.Errorf("引用 %v 不是 %s<名字> 这种写法", raw, prefix)
	}
	defs, _ := root["$defs"].(map[string]any)
	def, ok := defs[ref[len(prefix):]].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("引用 %s 指向的定义不存在", ref)
	}
	return def, nil
}

// itemName names a list item the way lint paths do: by its key, name, path,
// label or app when it has one, else by its index.
func itemName(e any, i int) string {
	if obj, ok := e.(map[string]any); ok {
		for _, f := range []string{"key", "name", "path", "label", "app"} {
			if s, ok := obj[f].(string); ok && s != "" {
				return s
			}
		}
	}
	return strconv.Itoa(i)
}

// scalarText renders a scalar the way enum values are written in keys.
func scalarText(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return strconv.FormatBool(x), true
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10), true
		}
		return strconv.FormatFloat(x, 'g', -1, 64), true
	case json.Number:
		return x.String(), true
	}
	return "", false
}

var schemaDocs sync.Map // schema file name → map[string]any

// schemaDoc returns an embedded schema as plain JSON, for walking.
func schemaDoc(name string) (map[string]any, error) {
	if v, ok := schemaDocs.Load(name); ok {
		return v.(map[string]any), nil
	}
	b, err := schemaFS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("内置格式定义 %s 读不出来：%w", name, err)
	}
	schemaDocs.Store(name, doc)
	return doc, nil
}

var platformVersionRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

// CompareVersions compares the numeric x.y.z part of two platform versions
// ("0.5.1-rc.1" counts as 0.5.1): -1, 0 or 1. A version it cannot read
// counts as 0.0.0.
func CompareVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionParts(v string) [3]int {
	var out [3]int
	if m := platformVersionRe.FindStringSubmatch(v); m != nil {
		for i := range out {
			out[i], _ = strconv.Atoi(m[i+1])
		}
	}
	return out
}

// DeclaredMinPlatform returns lifecycle.min_platform as written in
// manifest.yaml bytes, or "" when it cannot be found. It reads only that one
// field and tolerates anything else in the file, so it also works on a
// manifest this program cannot decode (a later format, a field that changed
// type): the caller wants to know whether an upgrade would help.
func DeclaredMinPlatform(raw []byte) string {
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	lc, _ := doc["lifecycle"].(map[string]any)
	v, _ := lc["min_platform"].(string)
	return v
}

// maxVersion returns the higher of two platform versions (a when equal).
func maxVersion(a, b string) string {
	if CompareVersions(b, a) > 0 {
		return b
	}
	return a
}
