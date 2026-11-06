package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// DockerEndpoint is where acornfox reaches the Docker API and why.
type DockerEndpoint struct {
	Host    string // unix:///..., npipe:////./pipe/..., tcp://...
	Source  string // "DOCKER_HOST" | "context <name>" | "default"
	Runtime string // best guess from the address: "Docker Desktop" | "OrbStack" | "Colima" | "Docker" (refined later from docker info)
}

// StatFile is os.Stat; tests override it.
var StatFile = os.Stat

// ResolveDocker finds the Docker API in this order (the order the docker CLI
// itself uses, plus acornfox's own runtime):
//
//  1. DOCKER_HOST;
//     1b. on macOS and Windows, the built-in runtime "acornfox setup" installed
//     (runtime.json in the data directory);
//  2. the current docker context (DOCKER_CONTEXT, else currentContext in
//     ~/.docker/config.json, DOCKER_CONFIG overrides ~/.docker), when it is
//     not "default";
//  3. the platform default: /var/run/docker.sock on Linux; on macOS the first
//     socket that exists among Docker Desktop, OrbStack, Colima and
//     /var/run/docker.sock; on Windows the docker_engine named pipe.
func ResolveDocker() DockerEndpoint {
	if h := Env("DOCKER_HOST"); h != "" {
		return DockerEndpoint{Host: h, Source: "DOCKER_HOST", Runtime: guessRuntime(h)}
	}
	if DesktopMode() {
		if r, ok := LoadBuiltinRuntime(DefaultPaths().DataDir); ok {
			return DockerEndpoint{Host: r.Host, Source: "AcornFox 自带运行环境", Runtime: r.Name}
		}
	}
	if name, host := currentContext(); host != "" {
		return DockerEndpoint{Host: host, Source: "context " + name, Runtime: guessRuntime(host)}
	}
	h := defaultDockerHost()
	return DockerEndpoint{Host: h, Source: "default", Runtime: guessRuntime(h)}
}

func dockerConfigDir() string {
	if d := Env("DOCKER_CONFIG"); d != "" {
		return d
	}
	home, err := UserHome()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".docker")
}

// currentContext returns the active context name and its docker endpoint, or
// empty strings for the "default" context or any read error.
func currentContext() (string, string) {
	dir := dockerConfigDir()
	if dir == "" {
		return "", ""
	}
	name := Env("DOCKER_CONTEXT")
	if name == "" {
		b, err := os.ReadFile(filepath.Join(dir, "config.json"))
		if err != nil {
			return "", ""
		}
		var cfg struct {
			CurrentContext string `json:"currentContext"`
		}
		if json.Unmarshal(b, &cfg) != nil {
			return "", ""
		}
		name = cfg.CurrentContext
	}
	if name == "" || name == "default" {
		return "", ""
	}
	sum := sha256.Sum256([]byte(name))
	b, err := os.ReadFile(filepath.Join(dir, "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json"))
	if err != nil {
		return "", ""
	}
	var meta struct {
		Endpoints map[string]struct {
			Host string `json:"Host"`
		} `json:"Endpoints"`
	}
	if json.Unmarshal(b, &meta) != nil {
		return "", ""
	}
	return name, meta.Endpoints["docker"].Host
}

// DesktopSockets lists the macOS Docker sockets in the order they are tried.
func DesktopSockets() []string {
	home, _ := UserHome()
	return []string{
		filepath.Join(home, ".docker", "run", "docker.sock"),     // Docker Desktop 4.13+
		filepath.Join(home, ".orbstack", "run", "docker.sock"),   // OrbStack
		filepath.Join(home, ".colima", "default", "docker.sock"), // Colima (default profile)
		filepath.Join(home, ".colima", "docker.sock"),            // Colima (older layout)
		"/var/run/docker.sock",                                   // Docker Desktop with the privileged helper, or a symlink
	}
}

func defaultDockerHost() string {
	switch GOOS {
	case "windows":
		return "npipe:////./pipe/docker_engine"
	case "darwin":
		for _, p := range DesktopSockets() {
			if _, err := StatFile(p); err == nil {
				return "unix://" + p
			}
		}
		// Nothing found: report the Docker Desktop location so the error names
		// the runtime we recommend.
		return "unix://" + DesktopSockets()[0]
	default:
		return "unix:///var/run/docker.sock"
	}
}

func guessRuntime(host string) string {
	h := strings.ToLower(host)
	switch {
	case strings.Contains(h, ".orbstack"):
		return "OrbStack"
	case strings.Contains(h, ".colima"):
		return "Colima"
	case strings.Contains(h, "/.docker/run/"), strings.Contains(h, "docker_engine") && GOOS == "windows", strings.Contains(h, "dockerdesktop"):
		return "Docker Desktop"
	default:
		return "Docker"
	}
}

// RuntimeFromInfo refines the runtime name from Docker's own description
// (docker info: OperatingSystem and Name).
func RuntimeFromInfo(guess, operatingSystem, name string) string {
	os := strings.ToLower(operatingSystem)
	switch {
	case strings.Contains(os, "docker desktop"):
		return "Docker Desktop"
	case strings.Contains(os, "orbstack"):
		return "OrbStack"
	case strings.Contains(strings.ToLower(name), "colima"):
		return "Colima"
	}
	return guess
}
