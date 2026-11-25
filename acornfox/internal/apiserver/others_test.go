package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/acornfox/acornfox/internal/runner"
)

// othersRunner is the fake runner that can also list the containers AcornFox
// does not manage (the runner does the filtering; the server adds them up).
type othersRunner struct {
	*fakeRunner
	res   runner.OtherContainersResponse
	err   error
	calls atomic.Int32
	stats atomic.Int32 // calls that asked for usage
}

func (o *othersRunner) ListOtherContainers(_ context.Context, req runner.OtherContainersRequest) (runner.OtherContainersResponse, error) {
	o.calls.Add(1)
	if req.Stats {
		o.stats.Add(1)
	}
	return o.res, o.err
}

func othersServer(t *testing.T, rn Runner) http.Handler {
	t.Helper()
	return New(Config{Store: newFakeStore(), Kicker: &fakeKicker{}, Runner: rn, UploadDir: t.TempDir(), HostMetrics: fakeHostSized(8192, 4)})
}

func sampleOthers() runner.OtherContainersResponse {
	return runner.OtherContainersResponse{Containers: []runner.OtherContainer{
		{ID: "a1", Name: "redis", Image: "redis:7", State: "running", Running: true, Ports: []runner.OtherPort{{Host: 6379, Container: 6379, Proto: "tcp"}, {Host: 6379, Container: 6379, Proto: "udp"}}, StatsAvailable: true, CPUAvailable: true, CPUPercent: 0.5, MemoryUsageMB: 12.34},
		{ID: "b2", Name: "grafana", Image: "grafana/grafana", State: "running", Running: true, Ports: []runner.OtherPort{{Host: 3000, Container: 3000, Proto: "tcp"}}, StatsAvailable: true, CPUAvailable: false, MemoryUsageMB: 80},
		{ID: "c3", Name: "old-db", Image: "mysql:5.7", State: "exited", Ports: []runner.OtherPort{}},
	}}
}

func TestOtherContainersView(t *testing.T) {
	rn := &othersRunner{fakeRunner: newFakeRunner(), res: sampleOthers()}
	h := othersServer(t, rn)
	w := do(t, h, "GET", "/v1/host/containers", nil, nil)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var v struct {
		ObservedAt string  `json:"observed_at"`
		Running    int     `json:"running"`
		Stopped    int     `json:"stopped"`
		MemoryMB   float64 `json:"memory_mb"`
		CPUPercent float64 `json:"cpu_percent"`
		Containers []struct {
			Name           string `json:"name"`
			State          string `json:"state"`
			Running        bool   `json:"running"`
			StatsAvailable bool   `json:"stats_available"`
			Ports          []struct{ Host, Container int }
		} `json:"containers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.ObservedAt == "" || len(v.Containers) != 3 || v.Running != 2 || v.Stopped != 1 || v.MemoryMB != 92.3 || v.CPUPercent != 0.5 {
		t.Fatalf("view: %+v", v)
	}
	if v.Containers[0].Name != "redis" || v.Containers[2].State != "exited" || v.Containers[2].Running {
		t.Fatalf("containers: %+v", v.Containers)
	}
	if rn.stats.Load() != 1 {
		t.Fatalf("the page asks for usage: %d", rn.stats.Load())
	}
	// Asked again at once: the same listing, no second Docker call.
	do(t, h, "GET", "/v1/host/containers", nil, nil)
	if rn.calls.Load() != 1 {
		t.Fatalf("not cached: %d calls", rn.calls.Load())
	}
	// The command line asks for the state now.
	do(t, h, "GET", "/v1/host/containers?fresh=1", nil, nil)
	if rn.calls.Load() != 2 {
		t.Fatalf("fresh=1 must read again: %d calls", rn.calls.Load())
	}
}

func TestOtherContainersNoneIsAnEmptyList(t *testing.T) {
	rn := &othersRunner{fakeRunner: newFakeRunner()}
	w := do(t, othersServer(t, rn), "GET", "/v1/host/containers", nil, nil)
	m := decodeObj(t, w.Body.String())
	if w.Code != 200 || m["running"] != float64(0) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if l, ok := m["containers"].([]any); !ok || len(l) != 0 {
		t.Fatalf("containers must be [] not null: %s", w.Body.String())
	}
}

func TestOtherContainersCannotBeRead(t *testing.T) {
	// Docker does not answer.
	rn := &othersRunner{fakeRunner: newFakeRunner(), err: errors.New("dial unix /var/run/docker.sock: connect: no such file")}
	w := do(t, othersServer(t, rn), "GET", "/v1/host/containers", nil, nil)
	body := w.Body.String()
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(body, `"MACHINE-04"`) || strings.Contains(body, "docker.sock") {
		t.Fatalf("a Docker that fails: %d %s", w.Code, body)
	}
	// An older runner without the route.
	rn = &othersRunner{fakeRunner: newFakeRunner(), err: errors.Join(runner.ErrUnsupported, errors.New("501"))}
	w = do(t, othersServer(t, rn), "GET", "/v1/host/containers", nil, nil)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "MACHINE-04") || !strings.Contains(w.Body.String(), "旧版本") {
		t.Fatalf("an older runner: %d %s", w.Code, w.Body.String())
	}
	// A runner that cannot list them at all (not even the type).
	w = do(t, othersServer(t, newFakeRunner()), "GET", "/v1/host/containers", nil, nil)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "MACHINE-04") {
		t.Fatalf("no capability: %d %s", w.Code, w.Body.String())
	}
}

// Read-only: there is no way to change one through the API.
func TestOtherContainersHasNoWriteRoutes(t *testing.T) {
	rn := &othersRunner{fakeRunner: newFakeRunner(), res: sampleOthers()}
	h := othersServer(t, rn)
	for _, c := range []struct{ method, path string }{
		{"POST", "/v1/host/containers"}, {"DELETE", "/v1/host/containers"}, {"PUT", "/v1/host/containers"}, {"PATCH", "/v1/host/containers"},
		{"POST", "/v1/host/containers/redis/stop"}, {"POST", "/v1/host/containers/redis/start"}, {"DELETE", "/v1/host/containers/redis"},
	} {
		if w := do(t, h, c.method, c.path, []byte(`{}`), nil); w.Code < 400 || w.Code == 503 {
			t.Errorf("%s %s answered %d", c.method, c.path, w.Code)
		}
	}
	if rn.calls.Load() != 0 {
		t.Fatalf("a write attempt reached Docker: %d", rn.calls.Load())
	}
}

func TestMachineViewListsOtherContainers(t *testing.T) {
	rn := &othersRunner{fakeRunner: newFakeRunner(), res: sampleOthers()}
	w := do(t, othersServer(t, rn), "GET", "/v1/machine", nil, nil)
	var v struct {
		Others []struct {
			Name    string
			Running bool
			Ports   []int
		} `json:"other_containers"`
		Warnings []struct{ Code, Part string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Others) != 3 || v.Others[0].Name != "grafana" || v.Others[2].Name != "redis" || len(v.Others[2].Ports) != 1 || v.Others[2].Ports[0] != 6379 || v.Others[1].Running || len(v.Warnings) != 0 {
		t.Fatalf("other_containers: %s", w.Body.String())
	}
	if rn.stats.Load() != 0 {
		t.Fatal("the machine view must not read usage")
	}
	// The view carries nothing beyond name, running and ports.
	if strings.Contains(w.Body.String(), "redis:7") || strings.Contains(w.Body.String(), "memory") {
		t.Fatalf("too much of an unmanaged container is shown: %s", w.Body.String())
	}

	// Docker not answering: named in the warnings, the rest still given.
	bad := &othersRunner{fakeRunner: newFakeRunner(), err: errors.New("boom")}
	w = do(t, othersServer(t, bad), "GET", "/v1/machine", nil, nil)
	m := decodeObj(t, w.Body.String())
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"part":"other_containers"`) {
		t.Fatalf("warning: %d %s", w.Code, w.Body.String())
	}
	if l, ok := m["other_containers"].([]any); !ok || len(l) != 0 {
		t.Fatalf("other_containers must be []: %s", w.Body.String())
	}
	// An older runner is not a warning.
	old := &othersRunner{fakeRunner: newFakeRunner(), err: runner.ErrUnsupported}
	w = do(t, othersServer(t, old), "GET", "/v1/machine", nil, nil)
	if strings.Contains(w.Body.String(), "other_containers\"") && strings.Contains(w.Body.String(), `"part":"other_containers"`) {
		t.Fatalf("an older runner is not a warning: %s", w.Body.String())
	}
}
