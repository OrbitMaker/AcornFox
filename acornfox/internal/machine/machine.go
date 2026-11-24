// Package machine is "what this machine can do right now": the answer of
// GET /v1/machine and `acornfox --json machine`, and the Markdown part
// (分册) the command line keeps next to the general Skill so an AI
// workbench reads it before it starts a task.
//
// What is in it: which apps and solutions are installed, how to open each,
// whether each has an operation manual, which services the machine has and
// who uses them, and what the platform itself can do. All of it comes from
// what is installed here (the apps' state, the service registry, the
// catalog's manifests and display content) and is worked out at the moment
// it is asked for; nothing is stored.
//
// What is never in it: a password, a key, a token or any value of a service
// or an app setting. The types below have no field that could carry one, and
// a test of the server answer checks the text of a machine that has secrets
// registered. Keep it so: add a field only for something that is safe to
// show to anyone who can read the Skill directory.
package machine

import (
	"encoding/json"
	"sort"
)

// Schema is the format version of the document.
const Schema = 1

// View is GET /v1/machine.
type View struct {
	Schema      int        `json:"schema"`
	GeneratedAt string     `json:"generated_at"`
	Machine     Machine    `json:"machine"`
	Apps        []App      `json:"apps"`
	Solutions   []Solution `json:"solutions"`
	Services    []Service  `json:"services"`
	Platform    Platform   `json:"platform"`
	// Warnings are the parts that could not be worked out (the rest is
	// still given): the answer is then incomplete, not wrong.
	Warnings []Warning `json:"warnings"`
}

// Machine says which machine this is. Name is the server's own name (its
// host name); the command line replaces it with the name the user
// registered the server under.
type Machine struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Region  string `json:"region"` // cn | global
}

// Manual says whether an operation manual (acornfox catalog skill <id>)
// exists for an app or a solution.
type Manual struct {
	Has     bool   `json:"has"`
	Kind    string `json:"kind,omitempty"`    // app | solution
	Version string `json:"version,omitempty"` // the app version the manual was checked against
}

// Use is one thing an app needs: what it is called in the app, the service
// type, whether the app cannot be installed without it, and the registered
// service it is plugged into ("" when none).
type Use struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Required bool   `json:"required"`
	Service  string `json:"service"`
}

// App is one installed app.
type App struct {
	// ID is the app's id in the catalog (what `acornfox catalog skill`
	// and `install` take); for a project deployed with `deploy` it is the
	// app's name. Instance is the name this machine knows it by (what the
	// other commands take as the app); it is the same unless the app was
	// installed under another name.
	ID       string `json:"id"`
	Instance string `json:"instance"`
	Name     string `json:"name"`
	Tagline  string `json:"tagline"`
	// Source: catalog (installed from the app center) or deploy (the user's
	// own project).
	Source string `json:"source"`
	// State: running | stopped | not_running (should be up, no container is)
	// | unknown (the container state could not be read).
	State string `json:"state"`
	// URL is where to open it; empty for an app only other apps reach.
	URL string `json:"url"`
	// Exposure: local | lan | public | internal.
	Exposure string   `json:"exposure"`
	Version  string   `json:"version,omitempty"`
	Manual   Manual   `json:"manual"`
	Provides []string `json:"provides"`
	Uses     []Use    `json:"uses"`
}

// Solution is a solution (recipe) with at least one app installed from it.
type Solution struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Apps   []string `json:"apps"` // catalog ids of the apps installed from it
	Manual Manual   `json:"manual"`
}

// Service is one entry of the service registry, without its values.
type Service struct {
	// Type is the service type ("llm_gateway"), Name its label for people;
	// Service is the name it is registered under (what `--use` takes).
	Type    string `json:"type"`
	Name    string `json:"name"`
	Service string `json:"service"`
	// Provider is the app that provides it ("" for one the user registered).
	Provider string `json:"provider"`
	// Status: ok | incomplete | provider_stopped | provider_gone | not_ready.
	Status           string   `json:"status"`
	UsedBy           []string `json:"used_by"`
	RegisteredByUser bool     `json:"registered_by_user"`
}

// Platform says which parts of the platform this machine's program has.
type Platform struct {
	Backup  bool `json:"backup"`
	Upgrade bool `json:"upgrade"`
	Reapply bool `json:"reapply"`
}

// Warning is a part that could not be worked out.
type Warning struct {
	Code    string `json:"code"`
	Part    string `json:"part"` // apps | solutions | services | manuals
	Message string `json:"message"`
}

// Fault codes of this feature (internal/faults).
const (
	// CodeIncomplete: a part of the answer could not be worked out.
	CodeIncomplete = "MACHINE-01"
	// CodeCacheWrite: the Markdown part could not be written.
	CodeCacheWrite = "MACHINE-02"
	// CodeNoServer: the part could not be made for a server (not reachable,
	// or its AcornFox is too old to answer).
	CodeNoServer = "MACHINE-03"
)

// Normalize puts the lists in a fixed order and turns every nil list into an
// empty one, so the JSON never has null and the same machine always gives the
// same bytes.
func Normalize(v *View) {
	v.Schema = Schema
	if v.Apps == nil {
		v.Apps = []App{}
	}
	if v.Solutions == nil {
		v.Solutions = []Solution{}
	}
	if v.Services == nil {
		v.Services = []Service{}
	}
	if v.Warnings == nil {
		v.Warnings = []Warning{}
	}
	sort.SliceStable(v.Apps, func(i, j int) bool { return v.Apps[i].Instance < v.Apps[j].Instance })
	for i := range v.Apps {
		a := &v.Apps[i]
		if a.Provides == nil {
			a.Provides = []string{}
		}
		if a.Uses == nil {
			a.Uses = []Use{}
		}
		sort.Strings(a.Provides)
	}
	sort.SliceStable(v.Solutions, func(i, j int) bool { return v.Solutions[i].ID < v.Solutions[j].ID })
	for i := range v.Solutions {
		if v.Solutions[i].Apps == nil {
			v.Solutions[i].Apps = []string{}
		}
		sort.Strings(v.Solutions[i].Apps)
	}
	sort.SliceStable(v.Services, func(i, j int) bool { return v.Services[i].Service < v.Services[j].Service })
	for i := range v.Services {
		s := &v.Services[i]
		if s.UsedBy == nil {
			s.UsedBy = []string{}
		}
		sort.Strings(s.UsedBy)
	}
}

// ManualOf reads the manual a catalog display entry (the "show" of an app or
// a solution) says it has. It takes the entry as it is sent to the console
// and reads its "skill" field through JSON, so it needs no more than the
// field's name: {"skill": {"version": "…"}} means there is a manual, no
// "skill" (or null) means there is none.
func ManualOf(show any, kind string) Manual {
	if show == nil {
		return Manual{}
	}
	raw, err := json.Marshal(show)
	if err != nil {
		return Manual{}
	}
	var probe struct {
		Skill *struct {
			Version string `json:"version"`
		} `json:"skill"`
	}
	if json.Unmarshal(raw, &probe) != nil || probe.Skill == nil {
		return Manual{}
	}
	return Manual{Has: true, Kind: kind, Version: probe.Skill.Version}
}

// StateOf is the app's State from what the server knows: desired is the
// state it should be in ("running" or "stopped"), observed what its
// container is (running, stopped, missing, unavailable).
func StateOf(desired, observed string) string {
	switch {
	case desired == "stopped":
		return "stopped"
	case observed == "running":
		return "running"
	case observed == "stopped":
		return "stopped"
	case observed == "missing":
		return "not_running"
	}
	return "unknown"
}
