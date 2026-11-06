package setup

import (
	"context"
	"strings"
	"time"

	dockerclient "github.com/moby/moby/client"
)

// pingDocker asks the Docker daemon at host for its version and description.
func pingDocker(ctx context.Context, host string) (version, osName, name string, err error) {
	c, err := dockerclient.New(dockerclient.FromEnv, dockerclient.WithHost(host))
	if err != nil {
		return "", "", "", err
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	info, err := c.Info(ctx, dockerclient.InfoOptions{})
	if err != nil {
		return "", "", "", err
	}
	return info.Info.ServerVersion, info.Info.OperatingSystem, info.Info.Name, nil
}

// waitDocker polls host until Docker answers or d passes.
func waitDocker(ctx context.Context, host string, d time.Duration) (string, error) {
	deadline := time.Now().Add(d)
	var last error
	for {
		v, _, _, err := pingDocker(ctx, host)
		if err == nil {
			return v, nil
		}
		last = err
		if time.Now().After(deadline) {
			return "", last
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// kindOf maps a runtime display name (platform.ResolveDocker /
// RuntimeFromInfo) to a Found kind.
func kindOf(name string) string {
	switch strings.ToLower(name) {
	case "docker desktop":
		return RuntimeDockerDesktop
	case "orbstack":
		return "orbstack"
	case "colima":
		return "colima"
	}
	return "docker"
}

// tail keeps the last n bytes of s (for error messages from tools).
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}
