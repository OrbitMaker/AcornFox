package capabilities

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"sort"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/faults"
	"github.com/acornfox/acornfox/internal/platformvar"
)

// Schema is the format version of the document.
const Schema = 1

// Feature is one row of the manifest feature table: what a manifest or a
// recipe may write and the first platform version that understands it.
type Feature struct {
	Name  string `json:"name"`
	Since string `json:"since"`
}

// Doc is `acornfox capabilities --json` (without the "ok" the command adds).
// Field order is the order of the output.
type Doc struct {
	Schema           int       `json:"schema"`
	Version          string    `json:"version"`
	Commands         []Command `json:"commands"`
	Faults           []string  `json:"faults"`
	PlatformVars     []string  `json:"platform_vars"`
	ServiceTypes     []string  `json:"service_types"`
	ManifestFeatures []Feature `json:"manifest_features"`
	SkillPatterns    []string  `json:"skill_patterns"`
}

// Build works the document out of the program. version is this program's
// version; skillText is the general Skill's text (the patterns it
// registers).
func Build(version, skillText string) Doc {
	d := Doc{
		Schema:           Schema,
		Version:          version,
		Commands:         append([]Command{}, Commands...),
		Faults:           FaultCodes(),
		PlatformVars:     PlatformVarNames(),
		ServiceTypes:     BuiltinServiceTypes(),
		ManifestFeatures: []Feature{},
		SkillPatterns:    SkillPatterns(skillText),
	}
	for _, f := range catalog.Features() {
		d.ManifestFeatures = append(d.ManifestFeatures, Feature{Name: f.Key, Since: f.Since})
	}
	return d
}

// FaultCodes returns every code of the fault catalog, sorted.
func FaultCodes() []string {
	out := make([]string, 0, len(faults.Catalog))
	for _, f := range faults.Catalog {
		out = append(out, f.Code)
	}
	sort.Strings(out)
	return out
}

// PlatformVarNames returns the platform variables as an app writes them
// ("platform.tz").
func PlatformVarNames() []string {
	table := platformvar.Table()
	out := make([]string, 0, len(table))
	for _, v := range table {
		out = append(out, "platform."+v.Name)
	}
	sort.Strings(out)
	return out
}

// BuiltinServiceTypes returns the service types the built-in catalog
// snapshot defines. The catalogs a machine syncs may define more; this is
// what the program knows without any network.
func BuiltinServiceTypes() []string {
	out := []string{}
	raw, err := readSnapshotIndex()
	if err != nil {
		return out
	}
	var idx catalog.Index
	if json.Unmarshal(raw, &idx) != nil {
		return out
	}
	for _, t := range catalog.KnownServiceTypes(&idx) {
		out = append(out, t.Kind)
	}
	return out
}

func readSnapshotIndex() ([]byte, error) {
	return fs.ReadFile(catalog.Snapshot(), "index.json")
}

// patternMark is a pattern's marker, in the general Skill and in a manual:
// a line "<!-- 套路: 名字 -->" in front of the part that can be reused. The
// name is lower-case English with hyphens.
var patternMark = regexp.MustCompile(`<!--\s*套路:\s*([a-z][a-z0-9]*(?:-[a-z0-9]+)*)\s*-->`)

// SkillPatterns returns the pattern names registered in text, sorted, each
// once.
func SkillPatterns(text string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range patternMark.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}
