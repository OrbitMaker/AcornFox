package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// The gaps register (gaps.yaml in the catalog root) is where a manifest
// author writes down a variable or a manifest feature the platform does not
// have yet. The app is not held up: the value is hard-coded, left to the user
// as a step, or left out for now. CheckGaps closes the loop: once the
// platform has the variable it says which entries can be closed, so nobody
// has to remember to look. The file format is schema/gaps.schema.json.
//
// gaps.yaml is never published: build packs apps/<id>/ only, and index.json
// lists apps and recipes only.

// GapsFile is the register's name in the catalog root.
const GapsFile = "gaps.yaml"

// Gap is one entry of the register.
type Gap struct {
	ID          string `yaml:"id" json:"id"`
	App         string `yaml:"app" json:"app"`
	UpstreamKey string `yaml:"upstream_key" json:"upstream_key"`
	Want        string `yaml:"want" json:"want"`
	Layer       string `yaml:"layer" json:"layer"`
	Handling    string `yaml:"handling" json:"handling"`
	DocURL      string `yaml:"doc_url" json:"doc_url"`
	Date        string `yaml:"date" json:"date"`
	Status      string `yaml:"status" json:"status"`
	DoneIn      string `yaml:"done_in" json:"done_in,omitempty"`
	Note        string `yaml:"note" json:"note,omitempty"`
}

// Kinds of GapNotice.
const (
	GapCanClose   = "can_close"   // the wanted platform variable exists now and the entry is not done
	GapAppMissing = "app_missing" // the entry's app is no longer in the catalog
)

// GapNotice is something to act on for one entry. It is not an error.
type GapNotice struct {
	ID      string `json:"id"`
	App     string `json:"app"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// GapsReport is the result of CheckGaps.
type GapsReport struct {
	// Registered is false when the catalog has no gaps.yaml.
	Registered bool           `json:"registered"`
	File       string         `json:"file"`
	Gaps       []Gap          `json:"gaps"`
	ByStatus   map[string]int `json:"by_status"` // open, planned, done
	Notices    []GapNotice    `json:"notices"`
	Problems   []Problem      `json:"problems"` // CAT-LINT-19
}

// CheckGaps reads gaps.yaml in the catalog at dir and checks every entry
// against the platform variable table and the apps in the catalog. A missing
// file is not a problem (Registered is false); a dir that does not exist is
// (CAT-LINT-01). A file that is not valid YAML,
// does not match the schema or repeats an id is CAT-LINT-19; then no notices
// are worked out. It only reads.
func CheckGaps(dir string) (GapsReport, error) {
	rep := GapsReport{File: GapsFile, Gaps: []Gap{}, ByStatus: map[string]int{"open": 0, "planned": 0, "done": 0},
		Notices: []GapNotice{}, Problems: []Problem{}}
	add := func(_, level, file, path, format string, args ...any) {
		rep.Problems = append(rep.Problems, Problem{Code: CodeGaps, Level: level, File: file, Path: path, Message: fmt.Sprintf(format, args...)})
	}
	// A catalog directory that is not there is a mistake in the path, not
	// "nothing registered".
	if p, missing := missingCatalogDir(dir); missing {
		rep.Problems = append(rep.Problems, p)
		return rep, nil
	}
	b, err := os.ReadFile(filepath.Join(dir, GapsFile))
	if errors.Is(err, os.ErrNotExist) {
		return rep, nil
	}
	rep.Registered = true
	if err != nil {
		add("", LevelError, GapsFile, "", "读不了：%v", err)
		return rep, nil
	}
	sch, err := compiledSchema(gapsSchemaF)
	if err != nil {
		return rep, err
	}
	if _, _, ok := validateBytes(plainDates(b), GapsFile, sch, add); !ok {
		return rep, nil
	}
	var file struct {
		Gaps []Gap `yaml:"gaps"`
	}
	if err := yaml.Unmarshal(b, &file); err != nil {
		add("", LevelError, GapsFile, "", "字段类型不对：%v", err)
		return rep, nil
	}
	seen := map[string]bool{}
	for _, g := range file.Gaps {
		if seen[g.ID] {
			add("", LevelError, GapsFile, "/gaps/"+g.ID, "编号 %s 重复了，每条要有自己的编号", g.ID)
		}
		seen[g.ID] = true
	}
	if len(rep.Problems) > 0 {
		return rep, nil
	}
	rep.Gaps = append(rep.Gaps, file.Gaps...)
	for _, g := range rep.Gaps {
		rep.ByStatus[g.Status]++
		if name, ok := cutPlatform(strings.TrimSpace(g.Want)); ok && g.Status != "done" {
			if pv, ok := LookupPlatformVar(name); ok {
				rep.Notices = append(rep.Notices, GapNotice{g.ID, g.App, GapCanClose,
					fmt.Sprintf("可以关了：变量已有，请改清单。platform.%s 从 AcornFox %s 起就有；把 %s 的清单里 %s 改成引用它（发过的版本不能改内容，要出新的 release.version），再把这条的 status 改成 done、done_in 写 \"%s\"", name, pv.Since, g.App, g.UpstreamKey, pv.Since)})
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "apps", g.App, manifestName)); err != nil {
			rep.Notices = append(rep.Notices, GapNotice{g.ID, g.App, GapAppMissing,
				fmt.Sprintf("应用 %s 已不在目录里（没有 apps/%s/%s）：确认应用名，或这条不再需要时把 status 改成 done 并在 note 里写明", g.App, g.App, manifestName)})
		}
	}
	return rep, nil
}

// plainDates re-renders the YAML with unquoted dates (date: 2026-01-31, which
// YAML reads as a timestamp) turned back into the text that was written, so
// the schema sees a string whether or not the author quoted it. Anything it
// cannot parse is returned unchanged for the validator to report.
func plainDates(b []byte) []byte {
	var raw any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return b
	}
	out, err := yaml.Marshal(stringifyTimes(raw))
	if err != nil {
		return b
	}
	return out
}

func stringifyTimes(v any) any {
	switch x := v.(type) {
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 && x.Nanosecond() == 0 {
			return x.Format("2006-01-02")
		}
		return x.Format(time.RFC3339)
	case map[string]any:
		for k, e := range x {
			x[k] = stringifyTimes(e)
		}
	case []any:
		for i, e := range x {
			x[i] = stringifyTimes(e)
		}
	}
	return v
}
