// Package catalog reads and checks the AcornFox app catalog: one manifest per
// app (apps/<id>/manifest.yaml, schema 2) and one recipe per bundle
// (recipes/<id>/recipe.yaml, schema 1).
//
// The JSON Schemas in schema/ are the single source of the file format; the
// public catalog repository keeps a copy of them for editors and CI. Lint
// checks a catalog directory against the schemas and then applies the rules
// a schema cannot express (references between files, licence obligations,
// safe defaults). Every problem carries a stable code (CAT-LINT-*) so that a
// workbench can branch on it.
package catalog

import (
	"embed"
)

//go:embed schema/manifest.schema.json schema/recipe.schema.json
var schemaFS embed.FS

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
	Init       []InitStep   `yaml:"init" json:"init"`
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
}

type Container struct {
	Command         []string `yaml:"command" json:"command,omitempty"`
	User            string   `yaml:"user" json:"user,omitempty"`
	ShmMB           int      `yaml:"shm_mb" json:"shm_mb,omitempty"`
	PidsLimit       int      `yaml:"pids_limit" json:"pids_limit,omitempty"`
	NoNewPrivileges *bool    `yaml:"no_new_privileges" json:"no_new_privileges,omitempty"`
}

type Endpoint struct {
	Name     string  `yaml:"name" json:"name"`
	Port     int     `yaml:"port" json:"port"`
	Protocol string  `yaml:"protocol" json:"protocol"`
	Primary  bool    `yaml:"primary" json:"primary,omitempty"`
	Health   *Health `yaml:"health" json:"health,omitempty"`
}

type Health struct {
	Path       string         `yaml:"path" json:"path"`
	ExpectJSON map[string]any `yaml:"expect_json" json:"expect_json,omitempty"`
	TimeoutS   int            `yaml:"timeout_s" json:"timeout_s,omitempty"`
}

type Exposure struct {
	Default string `yaml:"default" json:"default,omitempty"`
}

// DefaultExposure is what an app gets when the manifest says nothing.
const DefaultExposure = "local"

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
	LLM      *LLMParam `yaml:"llm" json:"llm,omitempty"`
}

type Generate struct {
	Length  int    `yaml:"length" json:"length"`
	Charset string `yaml:"charset" json:"charset,omitempty"`
}

type LLMParam struct {
	Platform     string   `yaml:"platform" json:"platform,omitempty"`
	LocalGateway bool     `yaml:"local_gateway" json:"local_gateway,omitempty"`
	Needs        []string `yaml:"needs" json:"needs,omitempty"`
	MinContext   int      `yaml:"min_context" json:"min_context,omitempty"`
}

type EnvVar struct {
	Key    string `yaml:"key" json:"key"`
	From   string `yaml:"from" json:"from,omitempty"`
	Value  string `yaml:"value" json:"value,omitempty"`
	Secret bool   `yaml:"secret" json:"secret,omitempty"`
}

type File struct {
	Volume    string `yaml:"volume" json:"volume"`
	Path      string `yaml:"path" json:"path"`
	Template  string `yaml:"template" json:"template"`
	Mode      string `yaml:"mode" json:"mode,omitempty"`
	Overwrite string `yaml:"overwrite" json:"overwrite,omitempty"`
	Owner     string `yaml:"owner" json:"owner,omitempty"`
}

type InitStep struct {
	Name string   `yaml:"name" json:"name"`
	When string   `yaml:"when" json:"when"`
	HTTP InitHTTP `yaml:"http" json:"http"`
}

type InitHTTP struct {
	Method     string         `yaml:"method" json:"method"`
	Path       string         `yaml:"path" json:"path"`
	JSON       map[string]any `yaml:"json" json:"json,omitempty"`
	ExpectJSON map[string]any `yaml:"expect_json" json:"expect_json,omitempty"`
}

type LoginField struct {
	Label  string `yaml:"label" json:"label"`
	From   string `yaml:"from" json:"from,omitempty"`
	Value  string `yaml:"value" json:"value,omitempty"`
	Secret bool   `yaml:"secret" json:"secret,omitempty"`
}

type Provide struct {
	Kind     string `yaml:"kind" json:"kind"`
	Adapter  string `yaml:"adapter" json:"adapter"`
	Endpoint string `yaml:"endpoint" json:"endpoint"`
	BasePath string `yaml:"base_path" json:"base_path,omitempty"`
}

type Use struct {
	Name     string `yaml:"name" json:"name"`
	Kind     string `yaml:"kind" json:"kind"`
	ForParam string `yaml:"for_param" json:"for_param,omitempty"`
	Optional bool   `yaml:"optional" json:"optional,omitempty"`
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
}

type RecipeApp struct {
	App            string         `yaml:"app" json:"app"`
	ReuseInstalled *bool          `yaml:"reuse_installed" json:"reuse_installed,omitempty"`
	Preset         map[string]any `yaml:"preset" json:"preset,omitempty"`
}

type Link struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
	Uses string `yaml:"uses" json:"uses"`
}
