package runner

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"

	"github.com/acornfox/acornfox/internal/edge"
)

func validSpec() EdgeSpec {
	return EdgeSpec{Mode: EdgeModeBridge, AdminPort: 18808, AdminToken: strings.Repeat("ab", 24), Ports: []EdgePort{
		{HostIP: "0.0.0.0", Port: 18810}, {HostIP: "127.0.0.1", Port: 18811}, {HostIP: "192.168.31.46", Port: 18811, ContainerPort: 28811}}}
}

func TestValidateEdgeSpec(t *testing.T) {
	if err := ValidateEdgeSpec(validSpec()); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	bad := map[string]func(*EdgeSpec){
		"mode":           func(s *EdgeSpec) { s.Mode = "privileged" },
		"host with port": func(s *EdgeSpec) { s.Mode = EdgeModeHost },
		"admin port":     func(s *EdgeSpec) { s.AdminPort = 80 },
		"short token":    func(s *EdgeSpec) { s.AdminToken = "abc" },
		"token chars":    func(s *EdgeSpec) { s.AdminToken = strings.Repeat("Z", 40) },
		"public address": func(s *EdgeSpec) { s.Ports[0].HostIP = "203.0.113.5" },
		"duplicate":      func(s *EdgeSpec) { s.Ports[1].HostIP = "0.0.0.0"; s.Ports[1].Port = 18810 },
		"same target":    func(s *EdgeSpec) { s.Ports[1].ContainerPort = 18810 },
		"admin clash":    func(s *EdgeSpec) { s.Ports[0].Port = 18808 },
		"internal port":  func(s *EdgeSpec) { s.Ports[0].Port = 2020 },
	}
	for name, mutate := range bad {
		s := validSpec()
		s.Ports = append([]EdgePort(nil), s.Ports...)
		mutate(&s)
		if err := ValidateEdgeSpec(s); err == nil {
			t.Errorf("%s: accepted %+v", name, s)
		}
	}
}

func TestEdgeCommand(t *testing.T) {
	s := validSpec()
	bridge := strings.Join(edgeCommand(s), " ")
	if bridge != edge.AdapterBinary+" run --admin-listen :2020" {
		t.Errorf("bridge command = %q", bridge)
	}
	s.Mode, s.Ports = EdgeModeHost, nil
	host := strings.Join(edgeCommand(s), " ")
	if host != edge.AdapterBinary+" run --admin-listen 127.0.0.1:18808" {
		t.Errorf("host command must listen on loopback only: %q", host)
	}
	if strings.Contains(bridge+host, s.AdminToken) {
		t.Error("the token must never be on the command line")
	}
}

func TestEdgeHashFollowsSpec(t *testing.T) {
	a := validSpec()
	b := validSpec()
	b.Ports = []EdgePort{b.Ports[2], b.Ports[1], b.Ports[0]}
	if edgeHash(a, edge.Image) != edgeHash(b, edge.Image) {
		t.Error("port order must not matter")
	}
	b.Ports[0].HostIP = "0.0.0.0"
	if edgeHash(a, edge.Image) == edgeHash(b, edge.Image) {
		t.Error("a changed bind address must change the hash")
	}
	if edgeHash(a, edge.Image) == edgeHash(a, "acornfox/edge:0.0.0-0") {
		t.Error("a changed image must change the hash")
	}
}

// edgeDocker is a fake Docker for EnsureEdge: it holds the entry
// containers (by ID) and records what EnsureEdge creates, copies, starts,
// stops, renames and removes.
type edgeDocker struct {
	t        *testing.T
	image    bool // the entry image is present
	ctrs     map[string]*fakeEdge
	order    []string
	foreign  bool // a container named EdgeContainerName without our label exists
	created  container.CreateRequest
	copied   map[string]string // path in the container -> content (last copy)
	copyMode map[string]int64
	pulled   bool
	calls    []string
	nextID   int
}

type fakeEdge struct {
	id, name string
	labels   map[string]string
	running  bool
	mounts   []container.MountPoint
}

func (f *edgeDocker) add(c *fakeEdge) {
	f.ctrs[c.id] = c
	f.order = append(f.order, c.id)
}

func (f *edgeDocker) byRef(ref string) *fakeEdge {
	if c, ok := f.ctrs[ref]; ok {
		return c
	}
	for _, id := range f.order {
		if c := f.ctrs[id]; c != nil && c.name == ref {
			return c
		}
	}
	return nil
}

func (f *edgeDocker) handler(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	notFound := func() { w.WriteHeader(404); _, _ = w.Write([]byte(`{"message":"No such container"}`)) }
	ref := func(suffix string) string {
		i := strings.Index(p, "/containers/")
		return strings.TrimSuffix(p[i+len("/containers/"):], suffix)
	}
	switch {
	case strings.Contains(p, "/images/create"):
		f.pulled = true
		w.WriteHeader(500)
	case strings.Contains(p, "/images/") && strings.HasSuffix(p, "/json"):
		if !f.image {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"No such image"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(image.InspectResponse{ID: "sha256:edge"})
	case strings.HasSuffix(p, "/containers/json"):
		var out []container.Summary
		for _, id := range f.order {
			c := f.ctrs[id]
			if c == nil {
				continue
			}
			st := container.StateExited
			if c.running {
				st = container.StateRunning
			}
			out = append(out, container.Summary{ID: c.id, Names: []string{"/" + c.name}, State: st, Labels: c.labels, Mounts: c.mounts})
		}
		_ = json.NewEncoder(w).Encode(out)
	case strings.HasSuffix(p, "/containers/create"):
		name := r.URL.Query().Get("name")
		if f.byRef(name) != nil {
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"message":"Conflict. The container name is already in use"}`))
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&f.created); err != nil {
			f.t.Fatal(err)
		}
		f.nextID++
		id := "new-id"
		if f.nextID > 1 {
			id = "new-id-" + strconv.Itoa(f.nextID)
		}
		var mounts []container.MountPoint
		for _, m := range f.created.HostConfig.Mounts {
			mounts = append(mounts, container.MountPoint{Type: m.Type, Name: m.Source, Destination: m.Target})
		}
		f.add(&fakeEdge{id: id, name: name, labels: f.created.Labels, mounts: mounts})
		f.calls = append(f.calls, "create "+name)
		_ = json.NewEncoder(w).Encode(container.CreateResponse{ID: id})
	case strings.HasSuffix(p, "/archive") && r.Method == http.MethodPut:
		if got := r.URL.Query().Get("path"); got != "/" {
			f.t.Errorf("copy destination = %q", got)
		}
		tr := tar.NewReader(r.Body)
		for {
			h, err := tr.Next()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(tr)
			f.copied[h.Name] = string(b)
			f.copyMode[h.Name] = h.Mode
		}
		w.WriteHeader(200)
	case strings.HasSuffix(p, "/start"):
		c := f.byRef(ref("/start"))
		if c == nil {
			notFound()
			return
		}
		c.running = true
		f.calls = append(f.calls, "start "+c.id)
		w.WriteHeader(204)
	case strings.HasSuffix(p, "/stop"):
		c := f.byRef(ref("/stop"))
		if c == nil {
			notFound()
			return
		}
		if r.URL.Query().Get("t") != "10" {
			f.t.Errorf("stop timeout = %q, want 10", r.URL.Query().Get("t"))
		}
		c.running = false
		f.calls = append(f.calls, "stop "+c.id)
		w.WriteHeader(204)
	case strings.HasSuffix(p, "/rename"):
		c := f.byRef(ref("/rename"))
		if c == nil {
			notFound()
			return
		}
		c.name = r.URL.Query().Get("name")
		f.calls = append(f.calls, "rename "+c.id+" "+c.name)
		w.WriteHeader(204)
	case r.Method == http.MethodDelete && strings.Contains(p, "/containers/"):
		c := f.byRef(ref(""))
		if c == nil {
			notFound()
			return
		}
		delete(f.ctrs, c.id)
		f.calls = append(f.calls, "remove "+c.id)
		w.WriteHeader(204)
	case strings.HasSuffix(p, "/json") && strings.Contains(p, "/containers/"):
		name := ref("/json")
		if name == EdgeContainerName && f.foreign {
			_ = json.NewEncoder(w).Encode(container.InspectResponse{ID: "foreign", Config: &container.Config{Labels: map[string]string{}}})
			return
		}
		c := f.byRef(name)
		if c == nil {
			notFound()
			return
		}
		_ = json.NewEncoder(w).Encode(container.InspectResponse{ID: c.id, Config: &container.Config{Labels: c.labels}, State: &container.State{Running: c.running}})
	default:
		f.t.Errorf("unexpected Docker call %s %s", r.Method, p)
		w.WriteHeader(500)
	}
}

func newEdgeDocker(t *testing.T) (*edgeDocker, *Docker) {
	f := &edgeDocker{t: t, image: true, ctrs: map[string]*fakeEdge{}, copied: map[string]string{}, copyMode: map[string]int64{}}
	d := observationDocker(t, f.handler)
	// The health check runs in the container (afx-edge health); here every
	// container is healthy unless a test says otherwise.
	d.edgeProbeHook = func(context.Context, string, string) (bool, string) { return true, "" }
	return f, d
}

func hostSpec() EdgeSpec {
	cfg := edge.Config{Routes: []edge.Route{{App: "web", PublicPort: 18810, Upstream: "127.0.0.1:32768"}}}
	return EdgeSpec{Mode: EdgeModeHost, AdminPort: 18807, AdminToken: strings.Repeat("ab", 24), Config: &cfg}
}

// A new entry container runs the adapter from the local entry image, gets the
// token as a file (never on the command line or in the environment) and the
// whole configuration in its settings volume before it starts, keeps its
// data and settings on their own volumes and carries the health check.
func TestEnsureEdgeCreatesAdapterContainer(t *testing.T) {
	f, d := newEdgeDocker(t)
	spec := hostSpec()
	info, err := d.EnsureEdge(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Recreated || !f.ctrs["new-id"].running || info.Image != edge.Image || f.pulled || info.AdminPort != 18807 || info.Replaced != "" {
		t.Fatalf("info %+v pulled %v", info, f.pulled)
	}
	c := f.created
	if f.ctrs["new-id"].name != EdgeContainerName || c.Image != edge.Image {
		t.Fatalf("created %q from %q", f.ctrs["new-id"].name, c.Image)
	}
	if got := strings.Join(c.Cmd, " "); got != edge.AdapterBinary+" run --admin-listen 127.0.0.1:18807" {
		t.Fatalf("cmd = %q", got)
	}
	all := strings.Join(c.Cmd, " ") + strings.Join(c.Env, " ")
	for k, v := range c.Labels {
		all += k + v
	}
	if strings.Contains(all, spec.AdminToken) {
		t.Fatal("the token leaked into the command, environment or labels")
	}
	if c.Labels[LabelEdge] != "1" || c.Labels[LabelEdgeImage] != edge.ImageVersion || c.Labels[LabelEdgeHash] == "" || c.Labels[LabelEdgeAdmin] != "18807" {
		t.Fatalf("labels = %v", c.Labels)
	}
	if c.Healthcheck == nil || strings.Join(c.Healthcheck.Test, " ") != "CMD "+edge.AdapterBinary+" health" {
		t.Fatalf("healthcheck = %+v", c.Healthcheck)
	}
	mounts := map[string]string{}
	for _, m := range c.HostConfig.Mounts {
		mounts[m.Target] = m.Source
	}
	if mounts[edge.DataMount] != EdgeDataVolume || mounts[edge.SettingsMount] != EdgeSettingsVolume || len(mounts) != 2 {
		t.Fatalf("mounts = %v", mounts)
	}
	if c.HostConfig.NetworkMode != "host" {
		t.Fatalf("network = %q", c.HostConfig.NetworkMode)
	}
	tokenPath := strings.TrimPrefix(edge.TokenFile, "/")
	if strings.TrimSpace(f.copied[tokenPath]) != spec.AdminToken || f.copyMode[tokenPath] != 0o600 {
		t.Fatalf("token file %q mode %o (copied %v)", f.copied[tokenPath], f.copyMode[tokenPath], f.copied)
	}
	var st edge.Stored
	storePath := strings.TrimPrefix(edge.StoreFile, "/")
	if err := json.Unmarshal([]byte(f.copied[storePath]), &st); err != nil || f.copyMode[storePath] != 0o600 {
		t.Fatalf("settings file %q mode %o: %v", f.copied[storePath], f.copyMode[storePath], err)
	}
	if st.Config.Version() != spec.Config.Version() || st.Version != spec.Config.Version() || len(st.Config.Routes) != 1 {
		t.Fatalf("settings file holds %+v", st)
	}
}

// Without the entry image the runner says so (EDGE-02) and never pulls one.
func TestEnsureEdgeImageMissing(t *testing.T) {
	f, d := newEdgeDocker(t)
	f.image = false
	_, err := d.EnsureEdge(context.Background(), hostSpec())
	var re *RemoteError
	if !errors.As(err, &re) || re.Code != edge.FaultImageMissing || !strings.Contains(re.Message, edge.Image) {
		t.Fatalf("err = %v", err)
	}
	if f.pulled || len(f.calls) != 0 {
		t.Fatalf("nothing may be pulled or created without the image: %v", f.calls)
	}
}

// The same spec reuses the running container.
func TestEnsureEdgeReusesSameSpec(t *testing.T) {
	f, d := newEdgeDocker(t)
	spec := hostSpec()
	f.add(&fakeEdge{id: "cur", name: EdgeContainerName, running: true, labels: map[string]string{LabelEdge: "1", LabelEdgeHash: edgeHash(spec, edge.Image), LabelEdgeImage: edge.ImageVersion, LabelEdgeAdmin: "18807"}})
	info, err := d.EnsureEdge(context.Background(), spec)
	if err != nil || info.Recreated || info.ID != "cur" || len(f.calls) != 0 || info.AdminPort != 18807 {
		t.Fatalf("info %+v err %v calls %v", info, err, f.calls)
	}
	// Stopped: started again, not recreated.
	f.ctrs["cur"].running = false
	if info, err := d.EnsureEdge(context.Background(), spec); err != nil || info.Recreated || !f.ctrs["cur"].running {
		t.Fatalf("info %+v err %v calls %v", info, err, f.calls)
	}
}

// A container named like ours without our label is never touched.
func TestEnsureEdgeRefusesForeignName(t *testing.T) {
	f, d := newEdgeDocker(t)
	f.foreign = true
	_, err := d.EnsureEdge(context.Background(), hostSpec())
	var re *RemoteError
	if !errors.As(err, &re) || re.Code != "refused" || len(f.calls) != 0 {
		t.Fatalf("err = %v calls %v", err, f.calls)
	}
}

// legacyEdge is the entry container an older version created under another
// name, with its own data and settings volumes.
func legacyEdge() *fakeEdge {
	return &fakeEdge{id: "old-id-000000000000", name: "old-entry", running: true, labels: map[string]string{LabelEdge: "1", LabelEdgeHash: "x"},
		mounts: []container.MountPoint{
			{Type: mount.TypeVolume, Name: "old-entry-data", Destination: edge.DataMount},
			{Type: mount.TypeVolume, Name: "old-entry-config", Destination: edge.SettingsMount},
		}}
}

// Switching from the entry container an older version created: the new one
// mounts the old one's data volume (certificates carry over), gets its own
// settings volume with the whole configuration, starts next to the old one,
// is asked for its health with the version written, and only then the old
// one is stopped (kept, not removed). The new one records the old one's ID.
func TestEnsureEdgeSwitchesFromOlderEdge(t *testing.T) {
	f, d := newEdgeDocker(t)
	f.add(legacyEdge())
	spec := hostSpec()
	var probedWant string
	d.edgeProbeHook = func(_ context.Context, id, want string) (bool, string) {
		if !f.ctrs["old-id-000000000000"].running {
			t.Error("the old entry container was stopped before the new one was healthy")
		}
		probedWant = want
		return id == "new-id", ""
	}
	info, err := d.EnsureEdge(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "new-id" || info.Replaced != "old-id-000000000000" || info.ReplacedName != "old-entry" || !info.Recreated || info.AdminPort != 18807 {
		t.Fatalf("info = %+v", info)
	}
	if probedWant != spec.Config.Version() {
		t.Fatalf("health asked for %q, want %q", probedWant, spec.Config.Version())
	}
	old := f.ctrs["old-id-000000000000"]
	if old == nil || old.running {
		t.Fatalf("the old entry container must be stopped and kept: %+v", old)
	}
	want := []string{"create " + EdgeContainerName, "start new-id", "stop old-id-000000000000"}
	if strings.Join(f.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
	mounts := map[string]string{}
	for _, m := range f.created.HostConfig.Mounts {
		mounts[m.Target] = m.Source
	}
	if mounts[edge.DataMount] != "old-entry-data" || mounts[edge.SettingsMount] != EdgeSettingsVolume {
		t.Fatalf("mounts = %v", mounts)
	}
	if f.created.Labels[LabelEdgeReplaced] != "old-id-000000000000" {
		t.Fatalf("labels = %v", f.created.Labels)
	}
	if _, ok := f.copied[strings.TrimPrefix(edge.StoreFile, "/")]; !ok {
		t.Fatal("the new container must get the configuration before it starts")
	}
	// The next call reuses the new container and leaves the stopped old one alone.
	f.calls = nil
	if info, err := d.EnsureEdge(context.Background(), spec); err != nil || info.Recreated || info.ID != "new-id" || len(f.calls) != 0 {
		t.Fatalf("info %+v err %v calls %v", info, err, f.calls)
	}
}

// A new entry container that does not become healthy is removed; the old
// one never stopped. The error carries EDGE-05.
func TestEnsureEdgeSwitchFailureKeepsOldEdge(t *testing.T) {
	f, d := newEdgeDocker(t)
	f.add(legacyEdge())
	saved := edgeSwitchTimeout
	edgeSwitchTimeout, edgeSwitchPoll = 20*time.Millisecond, time.Millisecond
	t.Cleanup(func() { edgeSwitchTimeout, edgeSwitchPoll = saved, 300*time.Millisecond })
	d.edgeProbeHook = func(context.Context, string, string) (bool, string) { return false, "这些端口没有在听：18810" }
	_, err := d.EnsureEdge(context.Background(), hostSpec())
	var re *RemoteError
	if !errors.As(err, &re) || re.Code != edge.FaultSwitchFailed || !strings.Contains(re.Message, "18810") || !strings.Contains(re.Message, "old-entry") {
		t.Fatalf("err = %v", err)
	}
	if old := f.ctrs["old-id-000000000000"]; old == nil || !old.running {
		t.Fatal("the old entry container must keep running")
	}
	if _, ok := f.ctrs["new-id"]; ok {
		t.Fatal("the unhealthy new container must be removed")
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "stop ") {
			t.Fatalf("nothing may be stopped: %v", f.calls)
		}
	}
}

// Switching needs the whole configuration: a new container without it would
// serve nothing once the old one stops.
func TestEnsureEdgeSwitchNeedsConfig(t *testing.T) {
	f, d := newEdgeDocker(t)
	f.add(legacyEdge())
	spec := hostSpec()
	spec.Config = nil
	_, err := d.EnsureEdge(context.Background(), spec)
	var re *RemoteError
	if !errors.As(err, &re) || re.Code != "invalid_request" || len(f.calls) != 0 {
		t.Fatalf("err = %v calls %v", err, f.calls)
	}
}

// From an entry container of an older entry image under our own name: the
// old one is renamed first, the new one gets the other management port and
// the other settings volume (the old adapter still holds both).
func TestEnsureEdgeSwitchBetweenEntryVersions(t *testing.T) {
	f, d := newEdgeDocker(t)
	f.add(&fakeEdge{id: "prev-id-0000000000", name: EdgeContainerName, running: true,
		labels: map[string]string{LabelEdge: "1", LabelEdgeHash: "older", LabelEdgeImage: "0.0.0-0", LabelEdgeAdmin: "18807"},
		mounts: []container.MountPoint{
			{Type: mount.TypeVolume, Name: EdgeDataVolume, Destination: edge.DataMount},
			{Type: mount.TypeVolume, Name: EdgeSettingsVolume, Destination: edge.SettingsMount},
		}})
	info, err := d.EnsureEdge(context.Background(), hostSpec())
	if err != nil {
		t.Fatal(err)
	}
	if info.AdminPort != 18806 || info.Replaced != "prev-id-0000000000" {
		t.Fatalf("info = %+v", info)
	}
	if got := strings.Join(f.created.Cmd, " "); got != edge.AdapterBinary+" run --admin-listen 127.0.0.1:18806" {
		t.Fatalf("cmd = %q", got)
	}
	mounts := map[string]string{}
	for _, m := range f.created.HostConfig.Mounts {
		mounts[m.Target] = m.Source
	}
	if mounts[edge.DataMount] != EdgeDataVolume || mounts[edge.SettingsMount] != EdgeSettingsVolume+"-b" {
		t.Fatalf("mounts = %v", mounts)
	}
	if f.calls[0] != "rename prev-id-0000000000 "+EdgeOldPrefix+"prev-id-0000" {
		t.Fatalf("calls = %v", f.calls)
	}
	if f.ctrs["new-id"].name != EdgeContainerName || f.ctrs["prev-id-0000000000"].running {
		t.Fatalf("containers: %+v %+v", f.ctrs["new-id"], f.ctrs["prev-id-0000000000"])
	}
	// Next call: the new one (on 18806) matches the spec (base port 18807).
	f.calls = nil
	if info, err := d.EnsureEdge(context.Background(), hostSpec()); err != nil || info.Recreated || info.AdminPort != 18806 || len(f.calls) != 0 {
		t.Fatalf("info %+v err %v calls %v", info, err, f.calls)
	}
}

// The same entry image with another spec (a new token) is recreated in
// place on the same volumes, without a switch.
func TestEnsureEdgeRecreatesOnSpecChange(t *testing.T) {
	f, d := newEdgeDocker(t)
	spec := hostSpec()
	f.add(&fakeEdge{id: "cur", name: EdgeContainerName, running: true,
		labels: map[string]string{LabelEdge: "1", LabelEdgeHash: "other", LabelEdgeImage: edge.ImageVersion, LabelEdgeAdmin: "18807"},
		mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "kept-data", Destination: edge.DataMount}}})
	info, err := d.EnsureEdge(context.Background(), spec)
	if err != nil || info.Replaced != "" || !info.Recreated {
		t.Fatalf("info %+v err %v", info, err)
	}
	if f.calls[0] != "remove cur" {
		t.Fatalf("calls = %v", f.calls)
	}
	mounts := map[string]string{}
	for _, m := range f.created.HostConfig.Mounts {
		mounts[m.Target] = m.Source
	}
	if mounts[edge.DataMount] != "kept-data" {
		t.Fatalf("mounts = %v", mounts)
	}
}

// Recreate rebuilds a container made from this very spec in place (the
// reconciler asks for it when the container refuses the token in the data
// directory): same data and settings volumes, the token copied in again as a
// 0600 file, the whole configuration in the settings volume, the same hash
// label (Recreate is not part of it) and the token nowhere else.
func TestEnsureEdgeRecreateRebuildsSameSpec(t *testing.T) {
	f, d := newEdgeDocker(t)
	spec := hostSpec()
	hash := edgeHash(spec, edge.Image)
	f.add(&fakeEdge{id: "cur", name: EdgeContainerName, running: true,
		labels: map[string]string{LabelEdge: "1", LabelEdgeHash: hash, LabelEdgeImage: edge.ImageVersion, LabelEdgeAdmin: "18807"},
		mounts: []container.MountPoint{
			{Type: mount.TypeVolume, Name: "kept-data", Destination: edge.DataMount},
			{Type: mount.TypeVolume, Name: "kept-settings", Destination: edge.SettingsMount},
		}})
	spec.Recreate = true
	info, err := d.EnsureEdge(context.Background(), spec)
	if err != nil || !info.Recreated || info.Replaced != "" || info.ID == "cur" || info.AdminPort != 18807 {
		t.Fatalf("info %+v err %v", info, err)
	}
	if len(f.calls) < 3 || f.calls[0] != "remove cur" || f.calls[1] != "create "+EdgeContainerName || f.calls[2] != "start "+info.ID {
		t.Fatalf("calls = %v, want remove, create, start", f.calls)
	}
	mounts := map[string]string{}
	for _, m := range f.created.HostConfig.Mounts {
		mounts[m.Target] = m.Source
	}
	if mounts[edge.DataMount] != "kept-data" || mounts[edge.SettingsMount] != "kept-settings" {
		t.Fatalf("mounts = %v, want the old container's volumes", mounts)
	}
	if f.created.Labels[LabelEdgeHash] != hash {
		t.Fatalf("hash label %q, want %q (Recreate is not part of the spec hash)", f.created.Labels[LabelEdgeHash], hash)
	}
	all := strings.Join(f.created.Cmd, " ") + strings.Join(f.created.Env, " ")
	for k, v := range f.created.Labels {
		all += k + v
	}
	if strings.Contains(all, spec.AdminToken) {
		t.Fatal("the token leaked into the command, environment or labels")
	}
	tokenPath := strings.TrimPrefix(edge.TokenFile, "/")
	if strings.TrimSpace(f.copied[tokenPath]) != spec.AdminToken || f.copyMode[tokenPath] != 0o600 {
		t.Fatalf("token file %q mode %o", f.copied[tokenPath], f.copyMode[tokenPath])
	}
	var st edge.Stored
	if err := json.Unmarshal([]byte(f.copied[strings.TrimPrefix(edge.StoreFile, "/")]), &st); err != nil || st.Config.Version() != spec.Config.Version() {
		t.Fatalf("settings file: %+v %v", st, err)
	}

	// Without Recreate the rebuilt container is kept as it is.
	spec.Recreate = false
	f.calls = nil
	if again, err := d.EnsureEdge(context.Background(), spec); err != nil || again.Recreated || again.ID != info.ID || len(f.calls) != 0 {
		t.Fatalf("info %+v err %v calls %v", again, err, f.calls)
	}
}

func TestEdgeHashIgnoresRecreate(t *testing.T) {
	a := hostSpec()
	b := hostSpec()
	b.Recreate = true
	if edgeHash(a, edge.Image) != edgeHash(b, edge.Image) {
		t.Error("Recreate must not change the hash")
	}
}

func TestOnlyEdgeAttached(t *testing.T) {
	n := network.Inspect{}
	n.Containers = map[string]network.EndpointResource{"x": {Name: EdgeContainerName}}
	if !onlyEdgeAttached(n) {
		t.Error("edge only")
	}
	n.Containers["y"] = network.EndpointResource{Name: "af-web-aaaaaaaaaaaa"}
	if onlyEdgeAttached(n) {
		t.Error("an app container is still attached")
	}
}
