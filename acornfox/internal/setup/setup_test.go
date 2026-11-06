package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRuntime struct {
	mu        sync.Mutex
	reachable bool
	found     []Found
	canBI     bool
	builtin   func(opts StartOptions, r Reporter) (Detection, error)
	desktop   func() error
}

func (f *fakeRuntime) Detect(context.Context) Detection {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reachable {
		return Detection{Reachable: true, Host: "unix:///fake.sock", Kind: "docker", Name: "Docker", Version: "27.0", Found: f.found}
	}
	return Detection{Found: f.found}
}
func (f *fakeRuntime) CanBuiltin() bool { return f.canBI }
func (f *fakeRuntime) StartDockerDesktop(context.Context) error {
	if f.desktop != nil {
		return f.desktop()
	}
	f.mu.Lock()
	f.reachable = true
	f.mu.Unlock()
	return nil
}
func (f *fakeRuntime) Builtin(ctx context.Context, opts StartOptions, r Reporter) (Detection, error) {
	return f.builtin(opts, r)
}

func newTestManager(t *testing.T, rt RuntimeProvider, checks []Check) (*Manager, *[]Detection) {
	t.Helper()
	var got []Detection
	m := New(Config{
		DataDir: t.TempDir(),
		Version: "0.4.0-test",
		Runtime: rt,
		Edge: func(ctx context.Context, r Reporter) (string, error) {
			r.Item(Item{ID: "caddy", Title: "网关容器", State: StateDone})
			return "Caddy 2.8.4 · 运行中", nil
		},
		Checks:    func(context.Context) []Check { return checks },
		OnRuntime: func(d Detection) { got = append(got, d) },
	})
	return m, &got
}

// waitState waits until the setup leaves "running".
func waitState(t *testing.T, m *Manager) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st := m.Status()
		if st.State != StateRunning {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("setup still running")
	return Status{}
}

var okChecks = []Check{{ID: "docker", Title: "Docker", OK: true, Required: true}, {ID: "lan", Title: "局域网", OK: false}}

func TestExistingDockerGoesStraightThrough(t *testing.T) {
	m, got := newTestManager(t, &fakeRuntime{reachable: true}, okChecks)
	if err := m.Start(StartOptions{}); err != nil {
		t.Fatal(err)
	}
	st := waitState(t, m)
	if st.State != StateDone || st.Runtime != RuntimeExisting || !st.EverDone {
		t.Fatalf("state=%s runtime=%s ever=%v", st.State, st.Runtime, st.EverDone)
	}
	for _, s := range st.Steps {
		if s.State != StateDone {
			t.Fatalf("step %s = %s", s.ID, s.State)
		}
	}
	if len(*got) != 1 || (*got)[0].Host != "unix:///fake.sock" {
		t.Fatalf("OnRuntime = %+v", *got)
	}
	// The event stream ends with done and carries every check.
	evs, _ := m.Events(context.Background(), 0, 0)
	if evs[len(evs)-1].Event != "done" {
		t.Fatalf("last event %s", evs[len(evs)-1].Event)
	}
	n := 0
	for _, e := range evs {
		if e.Event == "check" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("check events = %d", n)
	}
}

func TestStoppedDockerDesktopAsksThenStarts(t *testing.T) {
	rt := &fakeRuntime{canBI: true, found: []Found{{Kind: RuntimeDockerDesktop, Name: "Docker Desktop", Version: "4.37"}}}
	m, _ := newTestManager(t, rt, okChecks)
	_ = m.Start(StartOptions{})
	st := waitState(t, m)
	if st.State != StateNeedsUser || st.Need == nil || st.Need.Kind != NeedChooseRuntime || len(st.Need.Options) != 2 || !st.Need.Options[0].Recommended {
		t.Fatalf("need = %+v state=%s", st.Need, st.State)
	}
	old := desktopWait
	desktopWait = 5 * time.Second
	defer func() { desktopWait = old }()
	_ = m.Start(StartOptions{Runtime: RuntimeDockerDesktop})
	st = waitState(t, m)
	if st.State != StateDone || st.Runtime != RuntimeDockerDesktop {
		t.Fatalf("state=%s runtime=%s err=%+v", st.State, st.Runtime, st.Error)
	}
}

func TestNothingInstalledUsesBuiltinAndRemembersIt(t *testing.T) {
	calls := 0
	rt := &fakeRuntime{canBI: true}
	rt.builtin = func(opts StartOptions, r Reporter) (Detection, error) {
		calls++
		r.Item(Item{ID: "vm-image", Title: "Linux 虚拟机镜像", State: StateDone, TotalBytes: 10})
		return Detection{Reachable: true, Host: "tcp://127.0.0.1:23750", Kind: RuntimeBuiltin, Name: "AcornFox 自带", Version: "27.3"}, nil
	}
	m, got := newTestManager(t, rt, okChecks)
	_ = m.Start(StartOptions{})
	st := waitState(t, m)
	if st.State != StateDone || st.Runtime != RuntimeBuiltin || st.Options.Runtime != RuntimeBuiltin {
		t.Fatalf("state=%s runtime=%s opts=%+v", st.State, st.Runtime, st.Options)
	}
	if len(*got) != 1 || (*got)[0].Host != "tcp://127.0.0.1:23750" {
		t.Fatalf("OnRuntime %+v", *got)
	}
	// The next boot starts the built-in runtime again.
	m2 := New(Config{DataDir: m.cfg.DataDir, Runtime: rt, Checks: func(context.Context) []Check { return okChecks }})
	m2.Boot()
	if st := waitState(t, m2); st.State != StateDone || calls != 2 {
		t.Fatalf("boot: state=%s calls=%d", st.State, calls)
	}
}

func TestNeedAdminThenRebootThenResume(t *testing.T) {
	stage := 0
	rt := &fakeRuntime{canBI: true}
	rt.builtin = func(opts StartOptions, r Reporter) (Detection, error) {
		switch {
		case !opts.ConfirmAdmin && stage == 0:
			return Detection{}, &NeedError{Need: Need{Kind: NeedAdmin, Message: "要管理员"}}
		case stage == 0:
			stage = 1
			return Detection{}, &NeedError{Need: Need{Kind: NeedReboot, Message: "要重启"}}
		}
		return Detection{Reachable: true, Host: "tcp://127.0.0.1:23750", Kind: RuntimeBuiltin, Name: "WSL", Version: "27"}, nil
	}
	m, _ := newTestManager(t, rt, okChecks)
	_ = m.Start(StartOptions{})
	if st := waitState(t, m); st.Need == nil || st.Need.Kind != NeedAdmin {
		t.Fatalf("want admin, got %+v", st.Need)
	}
	_ = m.Start(StartOptions{Runtime: RuntimeBuiltin, ConfirmAdmin: true})
	if st := waitState(t, m); st.Need == nil || st.Need.Kind != NeedReboot {
		t.Fatalf("want reboot, got %+v", st.Need)
	}
	// After the reboot the server starts again and resumes.
	m2 := New(Config{DataDir: m.cfg.DataDir, Runtime: rt, Checks: func(context.Context) []Check { return okChecks }})
	if m2.Status().State != StateNeedsUser {
		t.Fatalf("loaded state %s", m2.Status().State)
	}
	m2.Boot()
	if st := waitState(t, m2); st.State != StateDone {
		t.Fatalf("after reboot: %s %+v", st.State, st.Error)
	}
}

func TestFailureCarriesFaultAndResumeRetries(t *testing.T) {
	fail := true
	rt := &fakeRuntime{canBI: true}
	rt.builtin = func(opts StartOptions, r Reporter) (Detection, error) {
		if fail {
			return Detection{}, &Fault{Code: "SETUP-NET-02", Message: "镜像下载失败", KeptBytes: 18 << 20}
		}
		return Detection{Reachable: true, Host: "tcp://x", Kind: RuntimeBuiltin, Name: "b", Version: "1"}, nil
	}
	m, _ := newTestManager(t, rt, okChecks)
	_ = m.Start(StartOptions{})
	st := waitState(t, m)
	if st.State != StateFailed || st.Error == nil || st.Error.Code != "SETUP-NET-02" || st.Error.KeptBytes != 18<<20 || st.Error.Next == "" {
		t.Fatalf("error = %+v", st.Error)
	}
	if st.Steps[1].State != StateFailed || st.Steps[0].State != StateDone {
		t.Fatalf("steps %+v", st.Steps)
	}
	fail = false
	_ = m.Resume()
	if st := waitState(t, m); st.State != StateDone {
		t.Fatalf("resume: %s", st.State)
	}
}

func TestRequiredCheckFailing(t *testing.T) {
	m, _ := newTestManager(t, &fakeRuntime{reachable: true}, []Check{{ID: "edge_admin", Title: "管理口", OK: false, Required: true, Code: "SETUP-CHK-03", Detail: "返回 200"}})
	_ = m.Start(StartOptions{})
	st := waitState(t, m)
	if st.State != StateFailed || st.Error.Code != "SETUP-CHK-03" || st.Error.Step != StepChecks {
		t.Fatalf("%s %+v", st.State, st.Error)
	}
}

func TestPauseKeepsStateAndBusy(t *testing.T) {
	block := make(chan struct{})
	rt := &fakeRuntime{canBI: true}
	rt.builtin = func(opts StartOptions, r Reporter) (Detection, error) {
		<-block
		return Detection{}, context.Canceled
	}
	m, _ := newTestManager(t, rt, okChecks)
	_ = m.Start(StartOptions{})
	if err := m.Start(StartOptions{}); !errors.Is(err, ErrBusy) {
		t.Fatalf("second start: %v", err)
	}
	if !m.Pause() {
		t.Fatal("pause returned false")
	}
	close(block)
	if st := waitState(t, m); st.State != StatePaused {
		t.Fatalf("state %s", st.State)
	}
}

func TestEventsWaitAndResync(t *testing.T) {
	m, _ := newTestManager(t, &fakeRuntime{reachable: true}, okChecks)
	go func() { time.Sleep(50 * time.Millisecond); _ = m.Start(StartOptions{}) }()
	evs, _ := m.Events(context.Background(), 0, 2*time.Second)
	if len(evs) == 0 || evs[0].Event != "start" {
		t.Fatalf("events %+v", evs)
	}
	waitState(t, m)
	// Drop old events: asking from 0 must say resync.
	m.mu.Lock()
	for i := 0; i < maxEvents+10; i++ {
		m.emitLocked(Event{Event: "progress"})
	}
	m.mu.Unlock()
	if _, resync := m.Events(context.Background(), 0, 0); !resync {
		t.Fatal("want resync")
	}
}

func TestStatusSurvivesRestart(t *testing.T) {
	m, _ := newTestManager(t, &fakeRuntime{reachable: true}, okChecks)
	_ = m.Start(StartOptions{})
	waitState(t, m)
	if _, err := os.Stat(filepath.Join(m.cfg.DataDir, "setup.json")); err != nil {
		t.Fatal(err)
	}
	m2 := New(Config{DataDir: m.cfg.DataDir})
	if st := m2.Status(); st.State != StateDone || !st.EverDone {
		t.Fatalf("reloaded %s", st.State)
	}
}

func TestFaultCatalog(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range Faults {
		if seen[f.Code] || !strings.HasPrefix(f.Code, "SETUP-") || f.Title == "" || f.Next == "" || len(f.Causes) == 0 {
			t.Fatalf("bad entry %+v", f)
		}
		seen[f.Code] = true
	}
	// Every code the package returns is in the catalog.
	for _, c := range []string{"SETUP-NET-01", "SETUP-NET-02", "SETUP-SUM-01", "SETUP-RT-02", "SETUP-RT-03", "SETUP-RT-04", "SETUP-WIN-03", "SETUP-WIN-04", "SETUP-EDGE-01", "SETUP-CHK-01", "SETUP-GEN-01", "SETUP-SVC-01", "SETUP-DISK-01", "SETUP-PORT-01", "SETUP-PORT-02", "SETUP-PORT-03", "SETUP-PORT-04", "SETUP-PORT-05", "SETUP-MAC-01"} {
		if !seen[c] {
			t.Fatalf("missing %s", c)
		}
	}
}

// ---- downloader ----

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func TestDownloadResumeFallbackAndChecksum(t *testing.T) {
	payload := []byte(strings.Repeat("acornfox-runtime-", 4096))
	var hits sync.Map
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Store(r.Header.Get("Range"), true)
		http.ServeContent(w, r, "f", time.Time{}, strings.NewReader(string(payload)))
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("tampered")) }))
	defer bad.Close()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }))
	defer down.Close()

	dir := t.TempDir()
	// A partial file from an earlier run: the download continues it.
	if err := os.WriteFile(filepath.Join(dir, "rt.bin.part"), payload[:1000], 0o600); err != nil {
		t.Fatal(err)
	}
	a := Artifact{ID: "rt", Title: "测试文件", File: "rt.bin", SHA256: sum(payload), Size: int64(len(payload)),
		Mirrors: []string{down.URL + "/x"}, Upstream: []string{good.URL + "/x"}}
	d := &Downloader{Dir: dir, Sources: Sources{China: true}}
	var last Item
	p, err := d.Fetch(context.Background(), a, func(it Item) { last = it })
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); sum(b) != a.SHA256 {
		t.Fatal("content differs")
	}
	if _, ok := hits.Load("bytes=1000-"); !ok {
		t.Fatal("did not resume with Range")
	}
	if last.State != StateDone {
		t.Fatalf("last item %+v", last)
	}
	// Already there: no download.
	if _, err := d.Fetch(context.Background(), a, func(Item) {}); err != nil {
		t.Fatal(err)
	}

	// Only a tampered source: checksum fault, no file left behind.
	a2 := a
	a2.File, a2.Mirrors, a2.Upstream = "rt2.bin", []string{bad.URL + "/x"}, nil
	_, err = d.Fetch(context.Background(), a2, func(Item) {})
	var f *Fault
	if !errors.As(err, &f) || f.Code != "SETUP-SUM-01" {
		t.Fatalf("want SETUP-SUM-01, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "rt2.bin.part")); err == nil {
		t.Fatal("bad partial file kept")
	}
}

func TestDownloadStallKeepsPartial(t *testing.T) {
	payload := []byte(strings.Repeat("x", 300000))
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(200)
		w.Write(payload[:100000])
		w.(http.Flusher).Flush()
		<-r.Context().Done() // then nothing
	}))
	defer slow.Close()
	d := &Downloader{Dir: t.TempDir(), Stall: 300 * time.Millisecond}
	a := Artifact{ID: "s", Title: "慢", File: "s.bin", SHA256: sum(payload), Upstream: []string{slow.URL}}
	_, err := d.Fetch(context.Background(), a, func(Item) {})
	var f *Fault
	if !errors.As(err, &f) || f.Code != "SETUP-NET-02" || f.KeptBytes != 100000 {
		t.Fatalf("want SETUP-NET-02 with 100000 kept, got %v (%+v)", err, f)
	}
}

func TestSourcesOrder(t *testing.T) {
	a := Artifact{File: "f", Mirrors: []string{"https://m/f"}, Upstream: []string{"https://u/f"}}
	if got := (Sources{Base: "https://dl.example/", China: true}).URLs(a); strings.Join(got, " ") != "https://dl.example/runtime/f https://m/f https://u/f" {
		t.Fatal(got)
	}
	if got := (Sources{}).URLs(a); strings.Join(got, " ") != "https://u/f https://m/f" {
		t.Fatal(got)
	}
}

// A source that accepts the connection but never answers (the Windows test
// machine and an unreachable mirror) must not block: after the stall time
// the next source is used.
func TestDownloadNoResponseHeadersFallsBack(t *testing.T) {
	payload := []byte(strings.Repeat("y", 50000))
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer hang.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(payload) }))
	defer good.Close()
	d := &Downloader{Dir: t.TempDir(), Stall: 300 * time.Millisecond, Sources: Sources{China: true}}
	a := Artifact{ID: "h", Title: "挂住", File: "h.bin", SHA256: sum(payload), Mirrors: []string{hang.URL}, Upstream: []string{good.URL}}
	start := time.Now()
	if _, err := d.Fetch(context.Background(), a, func(Item) {}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %s", time.Since(start))
	}
}

// "Local only" sticks until the admin step is confirmed again; an admin
// confirmation is used for one run and never kept for the next boot.
func TestLocalOnlyAndConfirmAdminLifetime(t *testing.T) {
	var seen []StartOptions
	rt := &fakeRuntime{canBI: true}
	rt.builtin = func(opts StartOptions, r Reporter) (Detection, error) {
		seen = append(seen, opts)
		return Detection{Reachable: true, Host: "tcp://x", Kind: RuntimeBuiltin, Name: "b", Version: "1"}, nil
	}
	m, _ := newTestManager(t, rt, okChecks)
	_ = m.Start(StartOptions{Runtime: RuntimeBuiltin, LocalOnly: true})
	waitState(t, m)
	_ = m.Resume()
	waitState(t, m)
	if !seen[1].LocalOnly {
		t.Fatal("local-only not kept")
	}
	_ = m.Start(StartOptions{Runtime: RuntimeBuiltin, ConfirmAdmin: true})
	waitState(t, m)
	if seen[2].LocalOnly || !seen[2].ConfirmAdmin {
		t.Fatalf("confirm-admin run: %+v", seen[2])
	}
	if m.Status().Options.ConfirmAdmin {
		t.Fatal("confirm-admin kept after the run")
	}
	_ = m.Resume()
	waitState(t, m)
	if seen[3].ConfirmAdmin {
		t.Fatal("a later run elevated without asking")
	}
}
