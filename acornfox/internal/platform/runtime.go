package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// BuiltinRuntime records the built-in runtime that "acornfox setup" installed
// (macOS: Colima + Lima; Windows: WSL 2 + Docker Engine), so every acornfox
// command finds its Docker without DOCKER_HOST. Kept in the data directory as
// runtime.json; absent when the user runs their own Docker.
type BuiltinRuntime struct {
	Kind string `json:"kind"` // "builtin"
	Host string `json:"host"` // DOCKER_HOST form, e.g. tcp://127.0.0.1:23750
	Name string `json:"name"` // display name
}

// RuntimeFile is <dataDir>/runtime.json.
func RuntimeFile(dataDir string) string { return filepath.Join(dataDir, "runtime.json") }

// LoadBuiltinRuntime reads runtime.json; ok is false when there is none.
func LoadBuiltinRuntime(dataDir string) (BuiltinRuntime, bool) {
	b, err := os.ReadFile(RuntimeFile(dataDir))
	if err != nil {
		return BuiltinRuntime{}, false
	}
	var r BuiltinRuntime
	if json.Unmarshal(b, &r) != nil || r.Host == "" {
		return BuiltinRuntime{}, false
	}
	return r, true
}

// SaveBuiltinRuntime writes runtime.json (0600).
func SaveBuiltinRuntime(dataDir string, r BuiltinRuntime) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(RuntimeFile(dataDir), append(b, '\n'), 0o600)
}

// RemoveBuiltinRuntime deletes runtime.json (the user switched to their own Docker).
func RemoveBuiltinRuntime(dataDir string) { _ = os.Remove(RuntimeFile(dataDir)) }
