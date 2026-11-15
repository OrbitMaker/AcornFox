// Package catalog reads and checks the AcornFox app catalog: one manifest per
// app (apps/<id>/manifest.yaml, schema 3, see ManifestSchema) and one recipe
// per bundle (recipes/<id>/recipe.yaml, schema 1).
//
// The JSON Schemas in schema/ are the single source of the file format; the
// public catalog repository keeps a copy of them for editors and CI. Lint
// checks a catalog directory against the schemas and then applies the rules
// a schema cannot express (references between files, licence obligations,
// safe defaults). Every problem carries a stable code (CAT-LINT-*) so that a
// workbench can branch on it.
//
// Around that sit: the feature table (features.go), from which lint works out
// the lowest platform version a manifest needs; the variable report (vars.go),
// which classifies every variable of a manifest for whoever checks an app
// before it is listed; the gaps register check (gaps.go); and the service
// types (servicetypes.go): what each kind of service an app can need or
// offer consists of, kept in the catalog as data and published inside
// index.json.
package catalog

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed schema/manifest.schema.json schema/recipe.schema.json schema/gaps.schema.json schema/service-type.schema.json
var schemaFS embed.FS

// ManifestSchema is the manifest format this platform installs (the
// manifest's "schema"). A manifest of an earlier format is refused, not
// converted (FormerManifest); one of a later format asks for a newer
// platform.
const ManifestSchema = 3

// Manifest is the part of an app manifest the platform reads. Unknown fields
// are rejected by the schema, not here.
type Manifest struct {
	ID         string       `yaml:"id" json:"id"`
	Schema     int          `yaml:"schema" json:"schema"`
	Source     string       `yaml:"source" json:"source"`
	App        AppInfo      `yaml:"app" json:"app"`
	Lifecycle  Lifecycle    `yaml:"lifecycle" json:"lifecycle"`
	Resources  Resources    `yaml:"resources" json:"resources"`
	Release    Release      `yaml:"release" json:"release"`
	Container  Container    `yaml:"container" json:"container"`
	Endpoints  []Endpoint   `yaml:"endpoints" json:"endpoints"`
	Exposure   Exposure     `yaml:"exposure" json:"exposure"`
	Volumes    []Volume     `yaml:"volumes" json:"volumes"`
	Params     []Param      `yaml:"params" json:"params"`
	Env        []EnvVar     `yaml:"env" json:"env"`
	Files      []File       `yaml:"files" json:"files"`
	Init       []Step       `yaml:"init" json:"init"`
	FirstLogin []LoginField `yaml:"first_login" json:"first_login"`
	Provides   []Provide    `yaml:"provides" json:"provides"`
	Uses       []Use        `yaml:"uses" json:"uses"`
	Delivery   Delivery     `yaml:"delivery" json:"delivery"`
	Uninstall  Uninstall    `yaml:"uninstall" json:"uninstall"`
}

type AppInfo struct {
	Name       string   `yaml:"name" json:"name"`
	Summary    string   `yaml:"summary" json:"summary"`
	Category   string   `yaml:"category" json:"category"`
	Tags       []string `yaml:"tags" json:"tags,omitempty"`
	Keywords   []string `yaml:"keywords" json:"keywords,omitempty"`
	Icon       string   `yaml:"icon" json:"icon,omitempty"`
	Homepage   string   `yaml:"homepage" json:"homepage"`
	License    string   `yaml:"license" json:"license"`
	SourceCode string   `yaml:"source_code" json:"source_code,omitempty"`
}

type Lifecycle struct {
	MinPlatform string `yaml:"min_platform" json:"min_platform"`
	Status      string `yaml:"status" json:"status"`
}

type Resources struct {
	MemoryMB  MinRec   `yaml:"memory_mb" json:"memory_mb"`
	DiskMB    MinRec   `yaml:"disk_mb" json:"disk_mb,omitempty"`
	Platforms []string `yaml:"platforms" json:"platforms"`
}

type MinRec struct {
	Min         int `yaml:"min" json:"min"`
	Recommended int `yaml:"recommended" json:"recommended,omitempty"`
}

type Release struct {
	Version string  `yaml:"version" json:"version"`
	Images  []Image `yaml:"images" json:"images"`
}

type Image struct {
	Name       string `yaml:"name" json:"name"`
	Repository string `yaml:"repository" json:"repository"`
	Tag        string `yaml:"tag" json:"tag,omitempty"`
	Digest     string `yaml:"digest" json:"digest"`
	SizeMB     int    `yaml:"size_mb" json:"size_mb,omitempty"`
	Artifact   string `yaml:"artifact" json:"artifact,omitempty"`
	// ArtifactSHA256 and ArtifactSize pin the image archive the download
	// gateway serves under Artifact. The archive is loaded into Docker only
	// when it matches them; without ArtifactSHA256 the gateway is not used.
	ArtifactSHA256 string `yaml:"artifact_sha256" json:"artifact_sha256,omitempty"`
	ArtifactSize   int64  `yaml:"artifact_size" json:"artifact_size,omitempty"`
	// Artifacts names one gateway archive per CPU architecture (key "amd64"
	// or "arm64"), from platform 0.5.1 on. A manifest writes either this or
	// the single archive above, which stands for amd64 only. Read the image
	// archive through ArtifactFor, never these fields directly (arch.go).
	Artifacts map[string]ArchArtifact `yaml:"artifacts" json:"artifacts,omitempty"`
}

type Container struct {
	Command         []string `yaml:"command" json:"command,omitempty"`
	User            string   `yaml:"user" json:"user,omitempty"`
	ShmMB           int      `yaml:"shm_mb" json:"shm_mb,omitempty"`
	PidsLimit       int      `yaml:"pids_limit" json:"pids_limit,omitempty"`
	NoNewPrivileges *bool    `yaml:"no_new_privileges" json:"no_new_privileges,omitempty"`
}

type Endpoint struct {
	Name string `yaml:"name" json:"name"`
	Port int    `yaml:"port" json:"port"`
	// Protocol is ProtocolHTTP or ProtocolTCP.
	Protocol string  `yaml:"protocol" json:"protocol"`
	Primary  bool    `yaml:"primary" json:"primary,omitempty"`
	Health   *Health `yaml:"health" json:"health,omitempty"`
}

// Endpoint protocols. A tcp endpoint does not speak HTTP (a database, a
// cache): other apps reach it over the app network, the entry proxy does not
// route it.
const (
	ProtocolHTTP = "http"
	ProtocolTCP  = "tcp"
)

// Health says how to tell the app is up. A manifest writes exactly one of
// Path (a GET to the endpoint, http endpoints only; ExpectJSON goes with
// it), Exec (a command run inside the app's own container, exit code 0
// meaning up; an argument list taken as written, no template) and TCP (the
// endpoint's port accepts a connection). ExpectStdout goes with Exec only
// (the schema refuses it elsewhere): when written, the command's standard
// output, with the blanks around it trimmed, must equal it as well (compared
// as written, no template): some services answer a check with exit code 0
// while they are not ready yet.
type Health struct {
	Path         string         `yaml:"path" json:"path"`
	ExpectJSON   map[string]any `yaml:"expect_json" json:"expect_json,omitempty"`
	Exec         []string       `yaml:"exec" json:"exec,omitempty"`
	ExpectStdout string         `yaml:"expect_stdout" json:"expect_stdout,omitempty"`
	TCP          bool           `yaml:"tcp" json:"tcp,omitempty"`
	TimeoutS     int            `yaml:"timeout_s" json:"timeout_s,omitempty"`
}

type Exposure struct {
	Default string `yaml:"default" json:"default,omitempty"`
}

// DefaultExposure is what an app gets when the manifest says nothing.
const DefaultExposure = "local"

// ExposureInternal is the exposure of an app reached only over the app
// network: no host port, no entry route, nothing to open in a browser.
const ExposureInternal = "internal"

type Volume struct {
	Name   string `yaml:"name" json:"name"`
	Path   string `yaml:"path" json:"path"`
	Backup bool   `yaml:"backup" json:"backup,omitempty"`
}

type Param struct {
	Key      string    `yaml:"key" json:"key"`
	Type     string    `yaml:"type" json:"type"`
	Label    string    `yaml:"label" json:"label,omitempty"`
	Hint     string    `yaml:"hint" json:"hint,omitempty"`
	Default  any       `yaml:"default" json:"default,omitempty"`
	Pattern  string    `yaml:"pattern" json:"pattern,omitempty"`
	Options  []any     `yaml:"options" json:"options,omitempty"`
	Min      *float64  `yaml:"min" json:"min,omitempty"`
	Max      *float64  `yaml:"max" json:"max,omitempty"`
	Required bool      `yaml:"required" json:"required,omitempty"`
	Hidden   bool      `yaml:"hidden" json:"hidden,omitempty"`
	Generate *Generate `yaml:"generate" json:"generate,omitempty"`
	Editable string    `yaml:"editable" json:"editable,omitempty"`
}

type Generate struct {
	Length  int    `yaml:"length" json:"length"`
	Charset string `yaml:"charset" json:"charset,omitempty"`
}

// EnvVar is one environment variable of the app. From takes one reference
// as it is; Value is a template (template.go): fixed text, or text with
// references and transforms in it.
type EnvVar struct {
	Key    string `yaml:"key" json:"key"`
	From   string `yaml:"from" json:"from,omitempty"`
	Value  string `yaml:"value" json:"value,omitempty"`
	Secret bool   `yaml:"secret" json:"secret,omitempty"`
	// Optional: when From or a reference of Value has no value (an optional
	// use that is not connected, say), or Value comes out empty, the
	// variable is left out altogether. Without it a missing value stops the
	// install before it starts.
	Optional bool `yaml:"optional" json:"optional,omitempty"`
	// If is a condition (ParseCondition): the variable is written only when
	// it holds. Several entries of one key are written for different cases,
	// each with an if, never two holding at once (CAT-LINT-49).
	If string `yaml:"if" json:"if,omitempty"`
}

// File is a config file written into a data volume; Template is rendered
// with the file's escape (yaml, json: quotes and backslashes), If (a
// condition) says whether the file is written at all.
type File struct {
	Volume    string `yaml:"volume" json:"volume"`
	Path      string `yaml:"path" json:"path"`
	Template  string `yaml:"template" json:"template"`
	Mode      string `yaml:"mode" json:"mode,omitempty"`
	Overwrite string `yaml:"overwrite" json:"overwrite,omitempty"`
	Owner     string `yaml:"owner" json:"owner,omitempty"`
	If        string `yaml:"if" json:"if,omitempty"`
}

// Step is one step of init, or of a provided service's issue, revoke, purge,
// check or ready list. It is either an HTTP request to the app's own
// endpoint or (Exec set) a command run inside the app's own container. The
// steps of one list share
// a session: one cookie jar, and what a step saves the later ones can use
// ({{saved.<key>}}). They run in order and the first failure stops the list.
type Step struct {
	// Name says which step failed; it may be empty.
	Name string `yaml:"name" json:"name"`
	// When is set on init steps only ("after_healthy").
	When string   `yaml:"when" json:"when"`
	HTTP StepHTTP `yaml:"http" json:"http"`
	// Exec is nil for an HTTP step.
	Exec *StepExec `yaml:"exec" json:"exec,omitempty"`
}

// IsExec reports whether the step runs a command instead of sending a request.
func (s Step) IsExec() bool { return s.Exec != nil }

// Saves returns what the step saves: an exec step's save, else the HTTP
// request's.
func (s Step) Saves() map[string]SaveValue {
	if s.Exec != nil {
		return s.Exec.Save
	}
	return s.HTTP.Save
}

// SaveKeys returns the keys of Saves, sorted.
func (s Step) SaveKeys() []string { return sortedSaveKeys(s.Saves()) }

// FinishIfWritten reports whether the step has a finish_if (an HTTP one or
// an exec one).
func (s Step) FinishIfWritten() bool {
	if s.Exec != nil {
		return s.Exec.FinishIf != nil
	}
	return s.HTTP.FinishIf != nil
}

func sortedSaveKeys(save map[string]SaveValue) []string {
	keys := make([]string, 0, len(save))
	for k := range save {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Label names the step inside a path (/init/<label>, /provides/0/issue/
// <label>): its name, else its position i in its list counted from 0, as
// every other list item in these paths is.
func (s Step) Label(i int) string {
	if s.Name != "" {
		return s.Name
	}
	return strconv.Itoa(i)
}

// StepHTTP is the request of an HTTP step. Path, the header values and the
// strings inside JSON, ExpectJSON and the Where of a list save are templates.
type StepHTTP struct {
	Method  string            `yaml:"method" json:"method"`
	Path    string            `yaml:"path" json:"path"`
	Headers map[string]string `yaml:"headers" json:"headers,omitempty"`
	JSON    map[string]any    `yaml:"json" json:"json,omitempty"`
	// ExpectStatus lists the status codes that pass; empty means any 2xx.
	ExpectStatus []int          `yaml:"expect_status" json:"expect_status,omitempty"`
	ExpectJSON   map[string]any `yaml:"expect_json" json:"expect_json,omitempty"`
	// Save takes values out of the JSON answer: key → where to find it.
	Save map[string]SaveValue `yaml:"save" json:"save,omitempty"`
	// Retry sends a GET step again when its answer does not pass or a value
	// to save is not in it yet (an app that lists what it just made a moment
	// later). Only a GET may retry: sending it again changes nothing.
	Retry *StepRetry `yaml:"retry" json:"retry,omitempty"`
	// FinishIf ends the whole list, as a success, when the answer matches
	// it: the steps after it are not done. Written in revoke and purge steps
	// only (lint refuses it elsewhere): taking back or deleting what is gone
	// already is no failure.
	FinishIf *StepFinishIf `yaml:"finish_if" json:"finish_if,omitempty"`
}

// StepFinishIf is what an answer is matched against for a step's finish_if:
// its status is one of Status (when written) and its JSON holds JSON (a
// subset match, templates allowed, like expect_json; when written). At
// least one of the two is written; with both, both must match.
type StepFinishIf struct {
	Status []int          `yaml:"status" json:"status,omitempty"`
	JSON   map[string]any `yaml:"json" json:"json,omitempty"`
}

// Written reports whether anything of it is written.
func (f *StepFinishIf) Written() bool {
	return f != nil && (len(f.Status) > 0 || f.JSON != nil)
}

// StepRetry is a step's retry: at most Times more requests, DelayS seconds
// apart. It is about the answer only; a request that could not connect is
// sent again by the executor's own rule, whatever is written here.
type StepRetry struct {
	Times  int `yaml:"times" json:"times"`
	DelayS int `yaml:"delay_s" json:"delay_s"`
}

// Bounds of StepRetry, as the schema has them.
const (
	MaxStepRetryTimes  = 10
	MaxStepRetryDelayS = 30
)

// Written reports whether anything of the request is written. A step that
// is not an exec step and has nothing written here is no step at all.
func (h StepHTTP) Written() bool {
	return h.Method != "" || h.Path != "" || len(h.Headers) > 0 || h.JSON != nil || len(h.ExpectStatus) > 0 ||
		h.ExpectJSON != nil || len(h.Save) > 0 || h.Retry != nil || h.FinishIf != nil
}

// StatusOK reports whether an answer's status code passes the step.
func (h StepHTTP) StatusOK(code int) bool {
	if len(h.ExpectStatus) == 0 {
		return code >= 200 && code < 300
	}
	for _, c := range h.ExpectStatus {
		if c == code {
			return true
		}
	}
	return false
}

// SaveKeys returns the keys of Save, sorted.
func (h StepHTTP) SaveKeys() []string { return sortedSaveKeys(h.Save) }

// SaveValue says how one saved value is taken out of a JSON answer. A
// manifest writes it in one of three ways:
//
//	token: data.access_token                         a dotted path (Path)
//	key:   { path: data.key, prefix: "sk-", secret: true }
//	id:    { list: data.items, where: { name: x }, sort_by: id, pick: last, field: id }
//
// The last form picks one item of the array at List: the items whose fields
// equal Where (a template) are kept, sorted ascending by SortBy when set
// (numbers by value), the first or last is taken (PickLast) and Field is read
// from it. Prefix is put in front of the value found. A Secret value is
// masked in logs, progress and errors.
//
// An exec step saves from its standard output: the three forms above read it
// as JSON, and a fourth, { stdout: true, prefix, secret } (Stdout), takes the
// whole of it with the blanks around it trimmed. An HTTP step has no fourth
// form (the schema refuses it there).
//
// Decoding is strict: a key the object form does not have is an error, not
// ignored (a misspelt "secret" would otherwise leave a value unprotected),
// and a number or a boolean is no dotted path.
type SaveValue struct {
	Path   string         `yaml:"path" json:"path,omitempty"`
	Prefix string         `yaml:"prefix" json:"prefix,omitempty"`
	Secret bool           `yaml:"secret" json:"secret,omitempty"`
	List   string         `yaml:"list" json:"list,omitempty"`
	Where  map[string]any `yaml:"where" json:"where,omitempty"`
	SortBy string         `yaml:"sort_by" json:"sort_by,omitempty"`
	Pick   string         `yaml:"pick" json:"pick,omitempty"`
	Field  string         `yaml:"field" json:"field,omitempty"`
	Stdout bool           `yaml:"stdout" json:"stdout,omitempty"`
}

// FromList reports whether the value is picked from a list (the third form).
func (s SaveValue) FromList() bool { return s.List != "" }

// FromStdout reports whether the value is the whole standard output of an
// exec step (the fourth form).
func (s SaveValue) FromStdout() bool { return s.Stdout }

// PickLast reports whether the last matching item is taken (the default).
func (s SaveValue) PickLast() bool { return s.Pick != "first" }

// saveValueFields is SaveValue without its decoding methods.
type saveValueFields SaveValue

// saveValueKeys are the keys of the object forms.
var saveValueKeys = map[string]bool{
	"path": true, "prefix": true, "secret": true, "list": true, "where": true, "sort_by": true, "pick": true, "field": true,
	"stdout": true,
}

// UnmarshalYAML accepts the short form (a dotted path) and the two map forms.
func (s *SaveValue) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	switch n.Kind {
	case yaml.ScalarNode:
		if n.ShortTag() != "!!str" {
			return fmt.Errorf("第 %d 行：save 的值要写成点路径（文字）或对象，写的是 %s", n.Line, n.Value)
		}
		*s = SaveValue{Path: n.Value}
		return nil
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if k := n.Content[i]; !saveValueKeys[k.Value] {
				return fmt.Errorf("第 %d 行：save 里没有 %s 这个字段", k.Line, k.Value)
			}
		}
	default:
		return fmt.Errorf("第 %d 行：save 的值要写成点路径（文字）或对象", n.Line)
	}
	var f saveValueFields
	if err := n.Decode(&f); err != nil {
		return err
	}
	*s = SaveValue(f)
	return nil
}

// UnmarshalJSON accepts the same forms as UnmarshalYAML, as strictly.
func (s *SaveValue) UnmarshalJSON(b []byte) error {
	var path string
	if json.Unmarshal(b, &path) == nil {
		*s = SaveValue{Path: path}
		return nil
	}
	var f saveValueFields
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return fmt.Errorf("save 的值要写成点路径（文字）或对象：%w", err)
	}
	*s = SaveValue(f)
	return nil
}

// StepExec is the command of an exec step: an argument list, never passed
// through a shell, run inside the app's own container. Command (each item
// rendered into exactly one argument, no line break in it), User, the values
// of Env and Stdin are templates. A secret value goes in Env or Stdin only,
// never in Command or User: the argument list of a process is visible to
// every user of the host. The step passes when the exit code is one of
// ExpectExit ([0] when not written); Save takes values out of the standard
// output; FinishIf (revoke and purge steps only) ends the list as a success
// on one of its exit codes.
type StepExec struct {
	Command    []string             `yaml:"command" json:"command"`
	User       string               `yaml:"user" json:"user,omitempty"`
	Env        map[string]string    `yaml:"env" json:"env,omitempty"`
	Stdin      string               `yaml:"stdin" json:"stdin,omitempty"`
	TimeoutS   int                  `yaml:"timeout_s" json:"timeout_s,omitempty"`
	ExpectExit []int                `yaml:"expect_exit" json:"expect_exit,omitempty"`
	Save       map[string]SaveValue `yaml:"save" json:"save,omitempty"`
	FinishIf   *StepExecFinishIf    `yaml:"finish_if" json:"finish_if,omitempty"`
}

// StepExecFinishIf is an exec step's finish_if: the exit codes that end a
// revoke or purge list as a success.
type StepExecFinishIf struct {
	Exit []int `yaml:"exit" json:"exit"`
}

// Bounds of an exec step, as the schema has them, and what applies when the
// manifest does not say.
const (
	DefaultExecTimeoutS = 30
	MaxExecTimeoutS     = 300
	// MaxExecOutput is the most standard output and standard error an exec
	// step may produce together; more counts as a failure.
	MaxExecOutput = 64 << 10
)

// Timeout returns the step's timeout in seconds (DefaultExecTimeoutS when
// not written).
func (e StepExec) Timeout() int {
	if e.TimeoutS <= 0 {
		return DefaultExecTimeoutS
	}
	return e.TimeoutS
}

// ExitOK reports whether an exit code passes the step.
func (e StepExec) ExitOK(code int) bool {
	if len(e.ExpectExit) == 0 {
		return code == 0
	}
	for _, c := range e.ExpectExit {
		if c == code {
			return true
		}
	}
	return false
}

// EnvKeys returns the names of Env, sorted.
func (e StepExec) EnvKeys() []string {
	keys := make([]string, 0, len(e.Env))
	for k := range e.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// InitStep and InitHTTP are the former names of Step and StepHTTP.
type (
	InitStep = Step
	InitHTTP = StepHTTP
)

// LoginField is one line of what the user is told after the install: From
// takes one reference, Value is a template.
type LoginField struct {
	Label  string `yaml:"label" json:"label"`
	From   string `yaml:"from" json:"from,omitempty"`
	Value  string `yaml:"value" json:"value,omitempty"`
	Secret bool   `yaml:"secret" json:"secret,omitempty"`
}

// Provide is one service an app offers to other apps once it is installed.
// Kind names a service type (ServiceType). Values says where each item of
// that type comes from; Issue, Revoke, Purge and Check are the steps the
// platform runs on the app's behalf: to hand a credential to one consumer,
// to take it back (the consumer's account no longer works; its data stays),
// to delete what was made for the consumer (only when the consumer is
// deleted together with its data, after Revoke), and to tell whether an old
// credential still works when the app is reinstalled on kept data (all
// steps passing means it does; OnCheckFail says what follows when it does
// not). Endpoint names the endpoint the steps are sent to and self.* in
// Values and in the Revoke, Purge, Check and Ready steps refers to.
//
// The platform carries out MaxConsumers (matching, and writing a binding),
// Purge (deleting a consumer with its data), OnCheckFail and Ready (looking
// at services, a plan, the end of an install, doctor): see
// internal/appinstall (needs.go, connect.go, ready.go).
type Provide struct {
	Kind     string `yaml:"kind" json:"kind"`
	Endpoint string `yaml:"endpoint" json:"endpoint"`
	// MaxConsumers is how many consumers the service may be connected to at
	// once, at least 1 (the schema refuses less); 0 means not written: no
	// limit. A service that can serve one consumer only writes 1.
	MaxConsumers int `yaml:"max_consumers" json:"max_consumers,omitempty"`

	Values map[string]ValueSource `yaml:"values" json:"values,omitempty"`
	// IssueVars are worked out before the Issue steps run, once per issue:
	// the steps refer to them as {{issue.<key>}}, and once the steps have
	// passed they are part of the credential like what the steps save
	// (issued.<key>).
	IssueVars map[string]IssueVar `yaml:"issue_vars" json:"issue_vars,omitempty"`
	Issue     []Step              `yaml:"issue" json:"issue,omitempty"`
	Revoke    []Step              `yaml:"revoke" json:"revoke,omitempty"`
	// Purge deletes the data made for one consumer (its database, say). It
	// runs only when that consumer is deleted together with its data, after
	// Revoke; it refers to the credential as Revoke does (issued.<key>) and
	// may end early as a success (finish_if).
	Purge []Step `yaml:"purge" json:"purge,omitempty"`
	Check []Step `yaml:"check" json:"check,omitempty"`
	// OnCheckFail is OnCheckFailReissue (also when not written) or
	// OnCheckFailStop. Read it through CheckFailAction.
	OnCheckFail string `yaml:"on_check_fail" json:"on_check_fail,omitempty"`
	// Ready tells whether the service can really be used now (a gateway
	// with no upstream configured yet cannot); nil when not written.
	Ready *ProvideReady `yaml:"ready" json:"ready,omitempty"`
}

// What the platform does when an old credential fails the Check steps on a
// reinstall on kept data (Provide.OnCheckFail).
const (
	// OnCheckFailReissue takes the old credential back and hands out a new
	// one; what is not written means this.
	OnCheckFailReissue = "reissue"
	// OnCheckFailStop stops and tells the user, handing out nothing new: for
	// a service that holds the consumer's data, a new credential would be a
	// new, empty database.
	OnCheckFailStop = "stop"
)

// CheckFailAction returns OnCheckFail, OnCheckFailReissue when not written.
func (p Provide) CheckFailAction() string {
	if p.OnCheckFail == "" {
		return OnCheckFailReissue
	}
	return p.OnCheckFail
}

// ProvideReady is a provided service's readiness check. Steps are read-only
// steps (requests or commands) run with no consumer and no credential: they
// may refer to params.*, platform.*, saved.* and, when the entry names its
// endpoint, self.*, never to issued.*, consumer.* or issue.*. All passing
// means ready. A service that is not ready is still matched; it is marked
// "not usable yet" and Say, shown to the user as written (no template), says
// why and what to do.
type ProvideReady struct {
	Steps []Step `yaml:"steps" json:"steps"`
	Say   string `yaml:"say" json:"say"`
}

// ReadySteps returns the steps of the readiness check (nil when there is
// none).
func (p Provide) ReadySteps() []Step {
	if p.Ready == nil {
		return nil
	}
	return p.Ready.Steps
}

// IssueVarKeys returns the keys of IssueVars, sorted.
func (p Provide) IssueVarKeys() []string {
	keys := make([]string, 0, len(p.IssueVars))
	for k := range p.IssueVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// IssuedSecret reports whether the credential key k is secret, by the
// rule of IssuedSecretKeys.
func (p Provide) IssuedSecret(k string, secretRef func(ref string) bool, t *ServiceType) bool {
	return p.IssuedSecretKeys(secretRef, t)[k]
}

// IssuedSecretKeys returns the keys of a credential that are secret (the
// one rule for the lint and the platform):
//   - an issue variable that is generated, or written as a template that
//     refers to a secret value (secretRef says which references are; nil:
//     none);
//   - a value an issue step saves as secret (an http or an exec step);
//   - a key behind an item written { issued: k } that is secret: written
//     secret: true, or secret in the service type t (nil: not known), which
//     counts whatever the manifest says.
//
// A key behind a template item marked secret is not counted: such an item
// is often made of plain parts (an account name) and a secret one.
func (p Provide) IssuedSecretKeys(secretRef func(ref string) bool, t *ServiceType) map[string]bool {
	out := map[string]bool{}
	for k, v := range p.IssueVars {
		if v.Generated() {
			out[k] = true
			continue
		}
		if secretRef != nil {
			for _, r := range TemplateRefs(v.Template) {
				if secretRef(r) {
					out[k] = true
				}
			}
		}
	}
	for _, st := range p.Issue {
		for k, sv := range st.Saves() {
			if sv.Secret {
				out[k] = true
			}
		}
	}
	for item, src := range p.Values {
		if src.Kind() != ValueIssued {
			continue
		}
		if src.Secret {
			out[src.Issued] = true
		} else if t != nil {
			if f, ok := t.Field(item); ok && f.Secret {
				out[src.Issued] = true
			}
		}
	}
	return out
}

// IssueVar is one entry of Provide.IssueVars. A manifest writes it in one
// of two ways:
//
//	database: "af_{{issue.id}}"                              a template (Template)
//	password: { generate: { length: 32, charset: alnum }, secret: true }
//
// A template may refer to issue.id, consumer.name, consumer.id, params.<name>
// and platform.<name>. A generated value is drawn with a secure random
// source each time, under the same rules as a parameter's generate, and is
// always secret (Secret may only be written true).
type IssueVar struct {
	Template string    `yaml:"-" json:"-"`
	Generate *Generate `yaml:"generate" json:"generate,omitempty"`
	Secret   bool      `yaml:"secret" json:"secret,omitempty"`
}

// Generated reports whether the value is drawn at random.
func (v IssueVar) Generated() bool { return v.Generate != nil }

// issueVarFields is IssueVar without its coding methods.
type issueVarFields IssueVar

var issueVarKeys = map[string]bool{"generate": true, "secret": true}

// UnmarshalYAML accepts a template string or the generate form, strictly.
func (v *IssueVar) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	switch n.Kind {
	case yaml.ScalarNode:
		*v = IssueVar{Template: n.Value}
		return nil
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if k := n.Content[i]; !issueVarKeys[k.Value] {
				return fmt.Errorf("第 %d 行：issue_vars 的值里没有 %s 这个字段", k.Line, k.Value)
			}
		}
	default:
		return fmt.Errorf("第 %d 行：issue_vars 的值要写成模板（文字）或 { generate: … }", n.Line)
	}
	var f issueVarFields
	if err := n.Decode(&f); err != nil {
		return err
	}
	*v = IssueVar(f)
	return nil
}

// MarshalJSON writes the form the manifest used: a string for a template.
func (v IssueVar) MarshalJSON() ([]byte, error) {
	if !v.Generated() {
		return json.Marshal(v.Template)
	}
	return json.Marshal(issueVarFields(v))
}

// UnmarshalJSON accepts what MarshalJSON writes, strictly.
func (v *IssueVar) UnmarshalJSON(b []byte) error {
	var tmpl string
	if json.Unmarshal(b, &tmpl) == nil {
		*v = IssueVar{Template: tmpl}
		return nil
	}
	var f issueVarFields
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return fmt.Errorf("issue_vars 的值要写成模板（文字）或 { generate: … }：%w", err)
	}
	*v = IssueVar(f)
	return nil
}

// ValueKeys returns the keys of Values, sorted.
func (p Provide) ValueKeys() []string {
	keys := make([]string, 0, len(p.Values))
	for k := range p.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// The issue number. Each time the platform runs a provided service's Issue
// steps it draws a fresh random number (8 hex digits) first. The steps refer
// to it as {{issue.id}}, so that what they make in the app has a name no
// other issue has ("acornfox-{{consumer.name}}-{{issue.id}}"); it goes into
// the credential under IssueIDKey, where Revoke and Check find it as
// {{issued.issue_id}} and can tell it is still the same thing.
const (
	IssueIDRef = "issue.id"
	IssueIDKey = "issue_id"
)

// IssuedKeys returns every key the Issue steps save and every issue
// variable, sorted, each once. After the steps have run, these keys and their
// values, plus IssueIDKey (which the platform adds, never a step), are what
// a credential handed to the consumer can hold (only the keys something
// reads are kept).
func (p Provide) IssuedKeys() []string {
	seen := map[string]bool{}
	var keys []string
	add := func(k string) {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for k := range p.IssueVars {
		add(k)
	}
	for _, st := range p.Issue {
		for k := range st.Saves() {
			add(k)
		}
	}
	sort.Strings(keys)
	return keys
}

// Where one item of a provided service comes from (ValueSource.Kind).
const (
	ValueTemplate = "template" // the platform works it out from a template
	ValueIssued   = "issued"   // handed out per consumer by the issue steps
	ValueAsk      = "ask"      // the user does a step in the app and fills it in
)

// ValueSource is one item of Provide.Values. A manifest writes it in one of
// four ways:
//
//	base_url: "{{self.internal_url}}/v1"     a template (Template)
//	url:      { template: "…", secret: true }   the same, marked secret
//	api_key:  { issued: api_key }            the key saved by the issue steps
//	api_key:  { ask: 在应用里新建一把 Key，贴进来 }  shown to the user as written
//
// A template may refer to self.internal_url (how another container reaches
// the entry's endpoint: http://<app instance>:<port>, no trailing slash),
// self.host, self.port, params.<name> and platform.<name>; in an entry with
// issue steps also to issued.<key> (a key the steps save or an issue
// variable), which makes it an item handed out per consumer (PerConsumer):
// a connection string of each consumer's own, say. Secret may be set on the
// object forms. An item is secret when it says so or when the service type
// marks it secret; nothing else. A template that refers to a secret value
// must therefore be marked (CAT-LINT-40) unless the service type marks the
// item secret.
type ValueSource struct {
	Template string `yaml:"template" json:"template,omitempty"`
	Issued   string `yaml:"issued" json:"issued,omitempty"`
	Ask      string `yaml:"ask" json:"ask,omitempty"`
	Secret   bool   `yaml:"secret" json:"secret,omitempty"`
}

// IssuedRefs returns the credential keys a template value refers to as
// issued.<key>, in order, each once; nil for the other forms.
func (v ValueSource) IssuedRefs() []string {
	if v.Kind() != ValueTemplate {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, r := range TemplateRefs(v.Template) {
		if k, ok := strings.CutPrefix(r, "issued."); ok && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// PerConsumer reports whether the item is handed out per consumer: written
// { issued: key }, or a template that refers to issued.<key>.
func (v ValueSource) PerConsumer() bool {
	return v.Kind() == ValueIssued || len(v.IssuedRefs()) > 0
}

// Kind is ValueTemplate, ValueIssued or ValueAsk.
func (v ValueSource) Kind() string {
	switch {
	case v.Issued != "":
		return ValueIssued
	case v.Ask != "":
		return ValueAsk
	}
	return ValueTemplate
}

// valueSourceFields is ValueSource without its coding methods.
type valueSourceFields ValueSource

// UnmarshalYAML accepts a template string or one of the two map forms.
func (v *ValueSource) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*v = ValueSource{Template: n.Value}
		return nil
	}
	var f valueSourceFields
	if err := n.Decode(&f); err != nil {
		return err
	}
	*v = ValueSource(f)
	return nil
}

// MarshalJSON writes the form the manifest used: a string for a template
// that is not marked secret.
func (v ValueSource) MarshalJSON() ([]byte, error) {
	if v.Kind() == ValueTemplate && !v.Secret {
		return json.Marshal(v.Template)
	}
	return json.Marshal(valueSourceFields(v))
}

// UnmarshalJSON accepts what MarshalJSON writes.
func (v *ValueSource) UnmarshalJSON(b []byte) error {
	var tmpl string
	if json.Unmarshal(b, &tmpl) == nil {
		*v = ValueSource{Template: tmpl}
		return nil
	}
	var f valueSourceFields
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	*v = ValueSource(f)
	return nil
}

// Use is one service an app needs. Kind names a service type; templates and
// env "from" refer to the connected service's items as uses.<Name>.<item>.
type Use struct {
	Name string `yaml:"name" json:"name"`
	Kind string `yaml:"kind" json:"kind"`
	// Optional false means the app cannot be installed without the service.
	Optional bool `yaml:"optional" json:"optional,omitempty"`
	// Without tells the user what is missing when an optional service is not
	// there (one sentence, shown as written).
	Without string `yaml:"without" json:"without,omitempty"`
	// Dedicated is a matching rule: connect only a service no other consumer
	// is connected to, and once connected no other consumer may connect to
	// it. The platform installs no instance for it: when no such service is
	// there, it is as if the service were missing (an optional one is
	// skipped; a required one stops the install, which only tells the user
	// to register one). The platform checks it when matching and when the
	// binding is written (no two consumers can take it at once).
	Dedicated bool `yaml:"dedicated" json:"dedicated,omitempty"`
}

type Delivery struct {
	Tier      string   `yaml:"tier" json:"tier"`
	Minutes   int      `yaml:"minutes" json:"minutes,omitempty"`
	MemoryMB  int      `yaml:"memory_mb" json:"memory_mb,omitempty"`
	Video     string   `yaml:"video" json:"video,omitempty"`
	UserSteps []string `yaml:"user_steps" json:"user_steps,omitempty"`
}

type Uninstall struct {
	Data []string `yaml:"data" json:"data,omitempty"`
}

// Recipe installs one or more apps in a fixed combination and links them.
type Recipe struct {
	ID       string      `yaml:"id" json:"id"`
	Schema   int         `yaml:"schema" json:"schema"`
	Title    string      `yaml:"title" json:"title"`
	Summary  string      `yaml:"summary" json:"summary"`
	Keywords []string    `yaml:"keywords" json:"keywords,omitempty"`
	Apps     []RecipeApp `yaml:"apps" json:"apps"`
	Links    []Link      `yaml:"links" json:"links,omitempty"`
	Delivery Delivery    `yaml:"delivery" json:"delivery"`
	// MinPlatform is the lowest platform version that can install the recipe.
	// A recipe file may leave it out; build works it out (what the recipe
	// writes and what its apps need) and always fills it in for the recipes
	// it lists under recipes_v2.
	MinPlatform string `yaml:"min_platform" json:"min_platform,omitempty"`
}

type RecipeApp struct {
	App            string         `yaml:"app" json:"app"`
	ReuseInstalled *bool          `yaml:"reuse_installed" json:"reuse_installed,omitempty"`
	Preset         map[string]any `yaml:"preset" json:"preset,omitempty"`
}

// Link wires, in a recipe, the need of app To called Uses (a uses[].name of
// To's manifest, not a service kind) to the service of that need's kind that
// app From provides. From comes before To in Apps: the apps are installed in
// that order and a later one is connected to what an earlier one registers.
type Link struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
	Uses string `yaml:"uses" json:"uses"`
}
