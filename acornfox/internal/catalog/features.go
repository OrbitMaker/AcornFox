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
//	manifest:/params/*/type          the field params[].type
//	manifest:/params/*/type=secret   that field holding secret
//	recipe:/apps/*/preset            a recipe's apps[].preset
//
// "*" stands for "each item of the list" and for "each entry of a map"
// (provides[].values, a step's headers and save). Platform variables carry
// their own version in the PlatformVars table (Since).
//
// Lint walks a manifest, looks up what it actually writes and works out the
// lowest platform version that can install it (ManifestMinPlatform). A
// manifest that declares less is refused (CAT-LINT-17): the declared version
// is the only thing an older platform checks before installing, so it must
// never be lower than what the manifest uses.
//
// The table starts at manifest format 3 (ManifestSchema): every row of it
// is since BasePlatform, the first platform version that installs that
// format. Earlier formats are not installed at all, so what earlier
// platforms understood does not matter here any more. A row added before
// the platform carries it out is listed in PendingFeatures (a manifest
// writing it is refused at install, not given a higher version); none is
// at present.
//
// Adding to the format: add the field or value to the schema AND a row here
// with the version that first ships it. A test fails when a schema path has
// no row. Something the walk along the schema cannot see (a new kind of
// reference in a template, a value that used to be one of a fixed few, a
// field that used to be required and may now be left out) needs a detector
// in manifestMinPlatform and a row of its own, with a test that a manifest
// writing it reaches the row; there is none of that kind today. The
// template writings (template.go: the transforms, \{{, a {{…}} kept as
// written, templates in env values, the start command and first-login
// values) all arrive with BasePlatform, so none of them needs a detector;
// a transform added later does.

// BasePlatform is the first platform version that installs manifests of the
// current format. Everything it understands is "since BasePlatform"; nothing
// can require less.
const BasePlatform = "0.6.0"

const v060 = BasePlatform

// Feature is one row of the table.
type Feature struct {
	Key   string `json:"key"`
	Since string `json:"since"`
}

var featureTable = withExecRows(withStepRows([]Feature{
	// Manifest (schema/manifest.schema.json).
	{"manifest:/id", v060},
	{"manifest:/schema", v060},
	{"manifest:/schema=3", v060},
	{"manifest:/source", v060},
	{"manifest:/source=upstream", v060},
	{"manifest:/source=community", v060},
	{"manifest:/source=official", v060},
	{"manifest:/maintainer", v060},
	{"manifest:/app", v060},
	{"manifest:/app/name", v060},
	{"manifest:/app/summary", v060},
	{"manifest:/app/category", v060},
	{"manifest:/app/category=ai", v060},
	{"manifest:/app/category=ai-agent", v060},
	{"manifest:/app/category=ai-gateway", v060},
	{"manifest:/app/category=automation", v060},
	{"manifest:/app/category=site", v060},
	{"manifest:/app/category=database", v060},
	{"manifest:/app/category=monitor", v060},
	{"manifest:/app/category=dev", v060},
	{"manifest:/app/category=files", v060},
	{"manifest:/app/tags", v060},
	{"manifest:/app/tags/*", v060},
	{"manifest:/app/keywords", v060},
	{"manifest:/app/keywords/*", v060},
	{"manifest:/app/icon", v060},
	{"manifest:/app/homepage", v060},
	{"manifest:/app/license", v060},
	{"manifest:/app/source_code", v060},
	{"manifest:/lifecycle", v060},
	{"manifest:/lifecycle/min_platform", v060},
	{"manifest:/lifecycle/status", v060},
	{"manifest:/lifecycle/status=active", v060},
	{"manifest:/lifecycle/status=deprecated", v060},
	{"manifest:/resources", v060},
	{"manifest:/resources/memory_mb", v060},
	{"manifest:/resources/memory_mb/min", v060},
	{"manifest:/resources/memory_mb/recommended", v060},
	{"manifest:/resources/cpu", v060},
	{"manifest:/resources/cpu/min", v060},
	{"manifest:/resources/disk_mb", v060},
	{"manifest:/resources/disk_mb/min", v060},
	{"manifest:/resources/platforms", v060},
	{"manifest:/resources/platforms/*", v060},
	{"manifest:/resources/platforms/*=linux/amd64", v060},
	{"manifest:/resources/platforms/*=linux/arm64", v060},
	{"manifest:/resources/gpu", v060},
	{"manifest:/resources/gpu=none", v060},
	{"manifest:/resources/gpu=optional", v060},
	{"manifest:/resources/gpu=required", v060},
	{"manifest:/release", v060},
	{"manifest:/release/version", v060},
	{"manifest:/release/images", v060},
	{"manifest:/release/images/*", v060},
	{"manifest:/release/images/*/name", v060},
	{"manifest:/release/images/*/repository", v060},
	{"manifest:/release/images/*/tag", v060},
	{"manifest:/release/images/*/digest", v060},
	{"manifest:/release/images/*/size_mb", v060},
	{"manifest:/release/images/*/artifact", v060},
	{"manifest:/release/images/*/artifact_sha256", v060},
	{"manifest:/release/images/*/artifact_size", v060},
	{"manifest:/release/images/*/artifacts", v060},
	{"manifest:/release/images/*/artifacts/amd64", v060},
	{"manifest:/release/images/*/artifacts/amd64/file", v060},
	{"manifest:/release/images/*/artifacts/amd64/sha256", v060},
	{"manifest:/release/images/*/artifacts/amd64/size", v060},
	{"manifest:/release/images/*/artifacts/arm64", v060},
	{"manifest:/release/images/*/artifacts/arm64/file", v060},
	{"manifest:/release/images/*/artifacts/arm64/sha256", v060},
	{"manifest:/release/images/*/artifacts/arm64/size", v060},
	{"manifest:/container", v060},
	{"manifest:/container/command", v060},
	{"manifest:/container/command/*", v060},
	{"manifest:/container/user", v060},
	{"manifest:/container/shm_mb", v060},
	{"manifest:/container/pids_limit", v060},
	{"manifest:/container/no_new_privileges", v060},
	{"manifest:/endpoints", v060},
	{"manifest:/endpoints/*", v060},
	{"manifest:/endpoints/*/name", v060},
	{"manifest:/endpoints/*/port", v060},
	{"manifest:/endpoints/*/protocol", v060},
	{"manifest:/endpoints/*/protocol=http", v060},
	{"manifest:/endpoints/*/protocol=tcp", v060},
	{"manifest:/endpoints/*/primary", v060},
	{"manifest:/endpoints/*/health", v060},
	{"manifest:/endpoints/*/health/path", v060},
	{"manifest:/endpoints/*/health/expect_json", v060},
	{"manifest:/endpoints/*/health/exec", v060},
	{"manifest:/endpoints/*/health/exec/*", v060},
	{"manifest:/endpoints/*/health/expect_stdout", v060},
	{"manifest:/endpoints/*/health/tcp", v060},
	{"manifest:/endpoints/*/health/tcp=true", v060},
	{"manifest:/endpoints/*/health/timeout_s", v060},
	{"manifest:/exposure", v060},
	{"manifest:/exposure/default", v060},
	{"manifest:/exposure/default=local", v060},
	{"manifest:/exposure/default=lan", v060},
	{"manifest:/exposure/default=public", v060},
	{"manifest:/exposure/default=internal", v060},
	{"manifest:/volumes", v060},
	{"manifest:/volumes/*", v060},
	{"manifest:/volumes/*/name", v060},
	{"manifest:/volumes/*/path", v060},
	{"manifest:/volumes/*/backup", v060},
	{"manifest:/params", v060},
	{"manifest:/params/*", v060},
	{"manifest:/params/*/key", v060},
	{"manifest:/params/*/type", v060},
	{"manifest:/params/*/type=string", v060},
	{"manifest:/params/*/type=int", v060},
	{"manifest:/params/*/type=bool", v060},
	{"manifest:/params/*/type=enum", v060},
	{"manifest:/params/*/type=secret", v060},
	{"manifest:/params/*/type=url", v060},
	{"manifest:/params/*/type=domain", v060},
	{"manifest:/params/*/type=email", v060},
	{"manifest:/params/*/label", v060},
	{"manifest:/params/*/hint", v060},
	{"manifest:/params/*/default", v060},
	{"manifest:/params/*/options", v060},
	{"manifest:/params/*/pattern", v060},
	{"manifest:/params/*/min", v060},
	{"manifest:/params/*/max", v060},
	{"manifest:/params/*/required", v060},
	{"manifest:/params/*/hidden", v060},
	{"manifest:/params/*/generate", v060},
	{"manifest:/params/*/generate/length", v060},
	{"manifest:/params/*/generate/charset", v060},
	{"manifest:/params/*/generate/charset=alnum", v060},
	{"manifest:/params/*/generate/charset=hex", v060},
	{"manifest:/params/*/editable", v060},
	{"manifest:/params/*/editable=never", v060},
	{"manifest:/params/*/editable=always", v060},
	{"manifest:/params/*/editable=reinstall", v060},
	{"manifest:/params/*/scope", v060},
	{"manifest:/params/*/scope=all", v060},
	{"manifest:/params/*/scope=standalone", v060},
	{"manifest:/params/*/scope=cloud", v060},
	{"manifest:/env", v060},
	{"manifest:/env/*", v060},
	{"manifest:/env/*/key", v060},
	{"manifest:/env/*/from", v060},
	{"manifest:/env/*/value", v060},
	{"manifest:/env/*/secret", v060},
	{"manifest:/env/*/optional", v060},
	{"manifest:/env/*/if", v060},
	{"manifest:/files", v060},
	{"manifest:/files/*", v060},
	{"manifest:/files/*/volume", v060},
	{"manifest:/files/*/path", v060},
	{"manifest:/files/*/template", v060},
	{"manifest:/files/*/mode", v060},
	{"manifest:/files/*/owner", v060},
	{"manifest:/files/*/overwrite", v060},
	{"manifest:/files/*/overwrite=never", v060},
	{"manifest:/files/*/overwrite=install", v060},
	{"manifest:/files/*/if", v060},
	{"manifest:/init", v060},
	{"manifest:/init/*", v060},
	{"manifest:/init/*/name", v060},
	{"manifest:/init/*/when", v060},
	{"manifest:/init/*/when=after_healthy", v060},
	{"manifest:/init/*/http", v060},
	{"manifest:/init/*/http/method", v060},
	{"manifest:/init/*/http/method=GET", v060},
	{"manifest:/init/*/http/method=POST", v060},
	{"manifest:/init/*/http/method=PUT", v060},
	{"manifest:/init/*/http/method=PATCH", v060},
	{"manifest:/init/*/http/method=DELETE", v060},
	{"manifest:/init/*/http/path", v060},
	{"manifest:/init/*/http/headers", v060},
	{"manifest:/init/*/http/headers/*", v060},
	{"manifest:/init/*/http/json", v060},
	{"manifest:/init/*/http/expect_status", v060},
	{"manifest:/init/*/http/expect_status/*", v060},
	{"manifest:/init/*/http/expect_json", v060},
	{"manifest:/init/*/http/save", v060},
	{"manifest:/init/*/http/save/*", v060},
	{"manifest:/init/*/http/save/*/path", v060},
	{"manifest:/init/*/http/save/*/prefix", v060},
	{"manifest:/init/*/http/save/*/secret", v060},
	{"manifest:/init/*/http/save/*/list", v060},
	{"manifest:/init/*/http/save/*/where", v060},
	{"manifest:/init/*/http/save/*/sort_by", v060},
	{"manifest:/init/*/http/save/*/pick", v060},
	{"manifest:/init/*/http/save/*/pick=first", v060},
	{"manifest:/init/*/http/save/*/pick=last", v060},
	{"manifest:/init/*/http/save/*/field", v060},
	{"manifest:/init/*/http/retry", v060},
	{"manifest:/init/*/http/retry/times", v060},
	{"manifest:/init/*/http/retry/delay_s", v060},
	{"manifest:/init/*/http/finish_if", v060}, // refused by lint outside revoke
	{"manifest:/init/*/http/finish_if/status", v060},
	{"manifest:/init/*/http/finish_if/status/*", v060},
	{"manifest:/init/*/http/finish_if/json", v060},
	// The exec rows of init steps: withExecRows.
	{"manifest:/first_login", v060},
	{"manifest:/first_login/*", v060},
	{"manifest:/first_login/*/label", v060},
	{"manifest:/first_login/*/from", v060},
	{"manifest:/first_login/*/value", v060},
	{"manifest:/first_login/*/secret", v060},
	{"manifest:/provides", v060},
	{"manifest:/provides/*", v060},
	{"manifest:/provides/*/kind", v060},
	{"manifest:/provides/*/endpoint", v060},
	{"manifest:/provides/*/max_consumers", v060},
	{"manifest:/provides/*/values", v060},
	{"manifest:/provides/*/values/*", v060},
	{"manifest:/provides/*/values/*/template", v060},
	{"manifest:/provides/*/values/*/issued", v060},
	{"manifest:/provides/*/values/*/ask", v060},
	{"manifest:/provides/*/values/*/secret", v060},
	{"manifest:/provides/*/issue_vars", v060},
	{"manifest:/provides/*/issue_vars/*", v060},
	{"manifest:/provides/*/issue_vars/*/generate", v060},
	{"manifest:/provides/*/issue_vars/*/generate/length", v060},
	{"manifest:/provides/*/issue_vars/*/generate/charset", v060},
	{"manifest:/provides/*/issue_vars/*/generate/charset=alnum", v060},
	{"manifest:/provides/*/issue_vars/*/generate/charset=hex", v060},
	{"manifest:/provides/*/issue_vars/*/secret", v060},
	{"manifest:/provides/*/issue_vars/*/secret=true", v060},
	{"manifest:/provides/*/issue", v060}, // the steps themselves: withStepRows
	{"manifest:/provides/*/revoke", v060},
	{"manifest:/provides/*/purge", v060},
	{"manifest:/provides/*/check", v060},
	{"manifest:/provides/*/on_check_fail", v060},
	{"manifest:/provides/*/on_check_fail=reissue", v060},
	{"manifest:/provides/*/on_check_fail=stop", v060},
	{"manifest:/provides/*/ready", v060},
	{"manifest:/provides/*/ready/steps", v060},
	{"manifest:/provides/*/ready/say", v060},
	{"manifest:/uses", v060},
	{"manifest:/uses/*", v060},
	{"manifest:/uses/*/name", v060},
	{"manifest:/uses/*/kind", v060},
	{"manifest:/uses/*/optional", v060},
	{"manifest:/uses/*/without", v060},
	{"manifest:/uses/*/dedicated", v060},
	{"manifest:/delivery", v060},
	{"manifest:/delivery/tier", v060},
	{"manifest:/delivery/tier=auto", v060},
	{"manifest:/delivery/tier=steps", v060},
	{"manifest:/delivery/minutes", v060},
	{"manifest:/delivery/video", v060},
	{"manifest:/delivery/user_steps", v060},
	{"manifest:/delivery/user_steps/*", v060},
	{"manifest:/upgrade", v060},
	{"manifest:/upgrade/from", v060},
	{"manifest:/upgrade/breaking", v060},
	{"manifest:/upgrade/notes", v060},
	{"manifest:/uninstall", v060},
	{"manifest:/uninstall/data", v060},
	{"manifest:/uninstall/data/*", v060},

	// Recipe (schema/recipe.schema.json).
	{"recipe:/id", v060},
	{"recipe:/schema", v060},
	{"recipe:/schema=1", v060},
	{"recipe:/title", v060},
	{"recipe:/summary", v060},
	{"recipe:/keywords", v060},
	{"recipe:/keywords/*", v060},
	{"recipe:/apps", v060},
	{"recipe:/apps/*", v060},
	{"recipe:/apps/*/app", v060},
	{"recipe:/apps/*/reuse_installed", v060},
	{"recipe:/apps/*/preset", v060},
	{"recipe:/links", v060},
	{"recipe:/links/*", v060},
	{"recipe:/links/*/from", v060},
	{"recipe:/links/*/to", v060},
	{"recipe:/links/*/uses", v060},
	{"recipe:/delivery", v060},
	{"recipe:/delivery/tier", v060},
	{"recipe:/delivery/tier=auto", v060},
	{"recipe:/delivery/tier=steps", v060},
	{"recipe:/delivery/minutes", v060},
	{"recipe:/delivery/memory_mb", v060},
	{"recipe:/delivery/video", v060},
	{"recipe:/delivery/user_steps", v060},
	{"recipe:/delivery/user_steps/*", v060},
	{"recipe:/min_platform", v060},
}, v060, "manifest:/provides/*/issue", "manifest:/provides/*/revoke", "manifest:/provides/*/purge", "manifest:/provides/*/check",
	"manifest:/provides/*/ready/steps"),
	v060, "manifest:/init", "manifest:/provides/*/issue", "manifest:/provides/*/revoke", "manifest:/provides/*/purge", "manifest:/provides/*/check",
	"manifest:/provides/*/ready/steps")

// stepFields are the paths and values of one step below the list that holds
// it (schema $defs step, stepHTTP, saveValue, stepExec). The steps of a
// provided service all arrived with one platform version, so their rows are
// generated from this list; init steps are older and written out row by row
// in featureTable.
var stepFields = []string{
	"", "/name",
	"/http", "/http/method", "/http/method=GET", "/http/method=POST", "/http/method=PUT", "/http/method=PATCH", "/http/method=DELETE",
	"/http/path", "/http/headers", "/http/headers/*", "/http/json",
	"/http/expect_status", "/http/expect_status/*", "/http/expect_json",
	"/http/save", "/http/save/*", "/http/save/*/path", "/http/save/*/prefix", "/http/save/*/secret",
	"/http/save/*/list", "/http/save/*/where", "/http/save/*/sort_by",
	"/http/save/*/pick", "/http/save/*/pick=first", "/http/save/*/pick=last", "/http/save/*/field",
	"/http/retry", "/http/retry/times", "/http/retry/delay_s",
	"/http/finish_if", "/http/finish_if/status", "/http/finish_if/status/*", "/http/finish_if/json",
}

// stepExecFields are the rows of an exec step below the list that holds it
// (schema $defs stepExec, execSaveValue), init steps' included: exec steps
// arrived with one platform version wherever they are written.
var stepExecFields = []string{
	"/exec", "/exec/command", "/exec/command/*", "/exec/user",
	"/exec/env", "/exec/env/*", "/exec/stdin", "/exec/timeout_s",
	"/exec/expect_exit", "/exec/expect_exit/*",
	"/exec/save", "/exec/save/*", "/exec/save/*/path", "/exec/save/*/prefix", "/exec/save/*/secret",
	"/exec/save/*/list", "/exec/save/*/where", "/exec/save/*/sort_by",
	"/exec/save/*/pick", "/exec/save/*/pick=first", "/exec/save/*/pick=last", "/exec/save/*/field",
	"/exec/save/*/stdout", "/exec/save/*/stdout=true",
	"/exec/finish_if", "/exec/finish_if/exit", "/exec/finish_if/exit/*",
}

// withStepRows appends, for each step list, one row per stepFields entry.
func withStepRows(rows []Feature, since string, lists ...string) []Feature {
	for _, list := range lists {
		for _, f := range stepFields {
			rows = append(rows, Feature{list + "/*" + f, since})
		}
	}
	return rows
}

// withExecRows appends, for each step list, one row per stepExecFields entry.
func withExecRows(rows []Feature, since string, lists ...string) []Feature {
	for _, list := range lists {
		for _, f := range stepExecFields {
			rows = append(rows, Feature{list + "/*" + f, since})
		}
	}
	return rows
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

// Features returns the table, sorted by key (a copy).
func Features() []Feature {
	out := make([]Feature, 0, len(featureSince))
	for k, v := range featureSince {
		out = append(out, Feature{Key: k, Since: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Requirement is one thing a manifest or recipe writes that needs a platform
// version: a format feature ("manifest:/init/*/exec"), a
// platform variable ("platform.tz") or, for a recipe, a member app
// ("app:demo-app"). Where points at it inside the file.
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
// check's expect_json, a recipe's preset) count as one field. A map whose
// values the schema does describe ("additionalProperties" holding a schema)
// is followed like a list: each value under "/*".
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
	if each, ok := node["additionalProperties"].(map[string]any); ok {
		if obj, ok := inst.(map[string]any); ok {
			props, _ := node["properties"].(map[string]any)
			names := make([]string, 0, len(obj))
			for k := range obj {
				if _, named := props[k]; !named {
					names = append(names, k)
				}
			}
			sort.Strings(names)
			for _, k := range names {
				if err := walkFeatures(kind, root, each, obj[k], key+"/*", where+"/"+k, out); err != nil {
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
