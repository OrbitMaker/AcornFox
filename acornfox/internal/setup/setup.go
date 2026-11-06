// Package setup prepares this computer's runtime after the acornfox program
// itself is installed: find or install Docker, prepare the gateway (Caddy)
// and run the self checks. The web console and `acornfox setup` read the same
// Manager: GET /v1/setup is the status, GET /v1/setup/events the event stream
// (one JSON object per event, the same objects `acornfox setup --json`
// prints), and POST /v1/setup/{start,resume,pause} change it.
//
// Four steps, always in this order:
//
//	binary   the acornfox program (already done when the server runs)
//	runtime  Docker: an existing one, Docker Desktop (only when the user picks
//	         it), or the built-in runtime (Colima + Lima on macOS, WSL 2 +
//	         Docker Engine on Windows; no Docker Desktop, no windows)
//	edge     the gateway component (Caddy image and container)
//	checks   self checks; every required one must pass
//
// The status is kept in the data directory (setup.json), so a restart (or a
// reboot Windows asked for) resumes where it stopped. Downloads keep their
// partial files; resuming continues them. Every failure carries a fault code
// from Faults (faults.go) with the next step to take.
package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Step IDs.
const (
	StepBinary  = "binary"
	StepRuntime = "runtime"
	StepEdge    = "edge"
	StepChecks  = "checks"
)

// States of the whole setup and of each step and item.
const (
	StateIdle      = "idle"       // never started
	StateWaiting   = "waiting"    // step not reached yet
	StateRunning   = "running"    // working on it
	StateNeedsUser = "needs_user" // waiting for the user (a choice, an admin confirmation, a reboot)
	StatePaused    = "paused"     // paused by the user; resume continues
	StateDone      = "done"
	StateFailed    = "failed"
)

// Runtime kinds (StartOptions.Runtime, Status.Runtime).
const (
	RuntimeExisting      = "existing"       // a Docker that already answers (any kind)
	RuntimeDockerDesktop = "docker-desktop" // start the installed Docker Desktop (opens its window)
	RuntimeBuiltin       = "builtin"        // acornfox's own runtime, no window
)

// Need kinds (Status.Need.Kind).
const (
	NeedChooseRuntime = "choose_runtime"
	NeedAdmin         = "admin"
	NeedReboot        = "reboot"
)

// Item is one piece of work inside a step (a download, starting the VM).
type Item struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	State      string `json:"state"`
	Detail     string `json:"detail,omitempty"`
	Source     string `json:"source,omitempty"`
	DoneBytes  int64  `json:"done_bytes,omitempty"`
	TotalBytes int64  `json:"total_bytes,omitempty"`
	SpeedBps   int64  `json:"speed_bps,omitempty"`
	ETASeconds int    `json:"eta_s,omitempty"`
}

// Step is one of the four steps.
type Step struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
	Items  []Item `json:"items,omitempty"`
}

// Option is one choice offered with a Need.
type Option struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Detail      string `json:"detail,omitempty"`
	Recommended bool   `json:"recommended,omitempty"`
}

// Need is what the setup waits for from the user.
type Need struct {
	Kind    string   `json:"kind"`
	Message string   `json:"message"`
	Steps   []string `json:"steps,omitempty"`   // what will happen after confirming
	Options []Option `json:"options,omitempty"` // for choose_runtime
}

// Check is one self check.
type Check struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	OK       bool   `json:"ok"`
	Required bool   `json:"required"`
	Detail   string `json:"detail,omitempty"`
	Code     string `json:"code,omitempty"` // fault code when not OK
}

// Found is a Docker runtime seen on this computer.
type Found struct {
	Kind    string `json:"kind"` // RuntimeDockerDesktop, "orbstack", "colima", "docker", RuntimeBuiltin
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Running bool   `json:"running"`
}

// Failure describes why the setup stopped.
type Failure struct {
	Step      string `json:"step"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Next      string `json:"next,omitempty"`
	Retry     string `json:"retry,omitempty"` // the command that resumes
	KeptBytes int64  `json:"kept_bytes,omitempty"`
	NeedsUser bool   `json:"needs_user"`
}

// Status is the whole setup state (GET /v1/setup).
type Status struct {
	State         string     `json:"state"`
	Version       string     `json:"version"`
	Network       string     `json:"network"` // "china" | "global"
	Runtime       string     `json:"runtime,omitempty"`
	RuntimeDetail string     `json:"runtime_detail,omitempty"`
	Steps         []Step     `json:"steps"`
	Found         []Found    `json:"found,omitempty"`
	Need          *Need      `json:"need,omitempty"`
	Checks        []Check    `json:"checks,omitempty"`
	Error         *Failure   `json:"error,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	Seq           int64      `json:"seq"`       // last event number; GET /v1/setup/events?since=Seq continues from here
	EverDone      bool       `json:"ever_done"` // finished successfully at least once (the console then only shows a banner while rechecking)

	// Options of the last start, kept so resume (and the next boot) repeat it.
	Options StartOptions `json:"options"`
}

// Event is one line of the event stream.
type Event struct {
	Seq    int64     `json:"seq"`
	Time   time.Time `json:"time"`
	Event  string    `json:"event"` // start, step, progress, needs_user, check, done, error, paused
	Step   string    `json:"step,omitempty"`
	State  string    `json:"state,omitempty"`
	Detail string    `json:"detail,omitempty"`
	Item   *Item     `json:"item,omitempty"`
	Check  *Check    `json:"check,omitempty"`
	Need   *Need     `json:"need,omitempty"`
	Error  *Failure  `json:"error,omitempty"`

	// start / done only
	Version string  `json:"version,omitempty"`
	Network string  `json:"network,omitempty"`
	Found   []Found `json:"found,omitempty"`
	OK      *bool   `json:"ok,omitempty"`
	TookS   int     `json:"took_s,omitempty"`
	Console string  `json:"console,omitempty"`
}

// StartOptions is the body of POST /v1/setup/start.
type StartOptions struct {
	Runtime      string `json:"runtime,omitempty"`       // "" = decide (existing, else ask or builtin)
	Mirror       string `json:"mirror,omitempty"`        // download base URL to try first (overrides the configured one)
	ConfirmAdmin bool   `json:"confirm_admin,omitempty"` // Windows: the user agreed to the admin prompt (used once, not kept)
	LocalOnly    bool   `json:"local_only,omitempty"`    // Windows: skip the LAN firewall rule; apps open on this computer only
}

// Detection is what RuntimeProvider.Detect saw.
type Detection struct {
	Reachable bool    // a Docker answers at Host
	Host      string  // DOCKER_HOST form
	Kind      string  // kind of the reachable runtime
	Name      string  // display name, e.g. "Docker Desktop"
	Version   string  // Docker server version
	Found     []Found // everything seen, running or not
}

// RuntimeProvider finds and installs Docker. One implementation per platform
// (runtime_darwin.go, runtime_windows.go, runtime_linux.go).
type RuntimeProvider interface {
	Detect(ctx context.Context) Detection
	// CanBuiltin reports whether this platform has a built-in runtime.
	CanBuiltin() bool
	// StartDockerDesktop starts the installed Docker Desktop.
	StartDockerDesktop(ctx context.Context) error
	// Builtin installs (if needed) and starts the built-in runtime, reporting
	// progress, and returns once its Docker answers. It may return a *NeedError
	// (admin confirmation, reboot) and is resumable after one.
	Builtin(ctx context.Context, opts StartOptions, r Reporter) (Detection, error)
}

// EdgePreparer prepares the gateway (pull the Caddy image with progress,
// make sure the container runs) and returns a short detail ("Caddy 2.8.4").
type EdgePreparer func(ctx context.Context, r Reporter) (string, error)

// Reporter receives progress from a step.
type Reporter interface {
	Item(it Item) // add or update an item of the current step
	Detail(s string)
	Log(format string, args ...any)
}

// NeedError makes the setup wait for the user.
type NeedError struct{ Need Need }

func (e *NeedError) Error() string { return e.Need.Message }

// Fault is an error with a fault code (faults.go).
type Fault struct {
	Code      string
	Message   string
	KeptBytes int64
	Err       error
}

func (e *Fault) Error() string {
	if e.Err != nil {
		return e.Message + "：" + e.Err.Error()
	}
	return e.Message
}
func (e *Fault) Unwrap() error { return e.Err }

// Faultf builds a *Fault.
func Faultf(code string, err error, format string, args ...any) *Fault {
	return &Fault{Code: code, Message: fmt.Sprintf(format, args...), Err: err}
}

// Config wires a Manager.
type Config struct {
	DataDir string
	Version string
	Network func() string // "china" | "global"
	Console string        // console URL shown when done
	Runtime RuntimeProvider
	Edge    EdgePreparer
	Checks  func(ctx context.Context) []Check
	// OnRuntime is called when a Docker answers (and again if the address
	// changes), so the server can point its Docker client there.
	OnRuntime func(d Detection)
	Logf      func(format string, args ...any)
	Now       func() time.Time
}

// ErrBusy is returned by Start while the setup is running.
var ErrBusy = errors.New("运行环境正在准备中")

const maxEvents = 500

// Manager runs the setup and keeps its status and event history.
type Manager struct {
	cfg Config

	mu       sync.Mutex
	st       Status
	events   []Event
	waiters  []chan struct{}
	cancel   context.CancelFunc
	running  bool
	lastHost string
	lastSave time.Time
}

// New loads the saved status (setup.json) or starts a fresh one.
func New(cfg Config) *Manager {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.Network == nil {
		cfg.Network = func() string { return "global" }
	}
	m := &Manager{cfg: cfg}
	if b, err := os.ReadFile(m.path()); err == nil {
		var st Status
		if json.Unmarshal(b, &st) == nil && len(st.Steps) == 4 {
			m.st = st
		}
	}
	if len(m.st.Steps) != 4 {
		m.st = Status{State: StateIdle, Steps: freshSteps()}
	}
	m.st.Version = cfg.Version
	m.st.Network = cfg.Network()
	// A run interrupted by a restart is not running any more.
	if m.st.State == StateRunning {
		m.st.State = StatePaused
	}
	return m
}

func freshSteps() []Step {
	return []Step{
		{ID: StepBinary, Title: "主程序", State: StateWaiting},
		{ID: StepRuntime, Title: "运行环境", State: StateWaiting},
		{ID: StepEdge, Title: "网关组件", State: StateWaiting},
		{ID: StepChecks, Title: "自检", State: StateWaiting},
	}
}

func (m *Manager) path() string { return filepath.Join(m.cfg.DataDir, "setup.json") }

// Status returns a copy of the current status.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.copyLocked()
}

func (m *Manager) copyLocked() Status {
	b, _ := json.Marshal(m.st)
	var out Status
	_ = json.Unmarshal(b, &out)
	return out
}

// Done reports whether the setup has finished successfully.
func (m *Manager) Done() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st.State == StateDone
}

// Events returns the events after since. When there are none it waits up to
// wait for new ones (or ctx). resync is true when events after since were
// already dropped from the history: the caller should re-read Status.
func (m *Manager) Events(ctx context.Context, since int64, wait time.Duration) (evs []Event, resync bool) {
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	for {
		m.mu.Lock()
		if len(m.events) > 0 && since < m.events[0].Seq-1 {
			resync = true
		}
		for _, e := range m.events {
			if e.Seq > since {
				evs = append(evs, e)
			}
		}
		if len(evs) > 0 || resync || wait <= 0 {
			m.mu.Unlock()
			return evs, resync
		}
		ch := make(chan struct{})
		m.waiters = append(m.waiters, ch)
		m.mu.Unlock()
		select {
		case <-ch:
		case <-deadline.C:
			return nil, false
		case <-ctx.Done():
			return nil, false
		}
	}
}

// emitLocked appends an event, wakes waiters and logs it.
func (m *Manager) emitLocked(e Event) {
	m.st.Seq++
	e.Seq = m.st.Seq
	e.Time = m.cfg.Now()
	m.events = append(m.events, e)
	if len(m.events) > maxEvents {
		m.events = append([]Event(nil), m.events[len(m.events)-maxEvents:]...)
	}
	for _, ch := range m.waiters {
		close(ch)
	}
	m.waiters = nil
	if e.Event != "progress" {
		b, _ := json.Marshal(e)
		m.cfg.Logf("setup %s", b)
	}
}

// saveLocked writes setup.json (progress events are throttled to every 2s).
func (m *Manager) saveLocked(force bool) {
	if !force && m.cfg.Now().Sub(m.lastSave) < 2*time.Second {
		return
	}
	m.lastSave = m.cfg.Now()
	b, err := json.MarshalIndent(m.st, "", "  ")
	if err != nil {
		return
	}
	tmp := m.path() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, m.path())
	}
}

func (m *Manager) step(id string) *Step {
	for i := range m.st.Steps {
		if m.st.Steps[i].ID == id {
			return &m.st.Steps[i]
		}
	}
	return nil
}

func (m *Manager) setStep(id, state, detail string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.step(id)
	s.State, s.Detail = state, detail
	if state == StateRunning {
		s.Items = nil
	}
	m.emitLocked(Event{Event: "step", Step: id, State: state, Detail: detail})
	m.saveLocked(true)
}

// Start begins (or repeats) the setup in the background. Steps already done
// are checked again quickly (a Docker that answers, an image that is there).
func (m *Manager) Start(opts StartOptions) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return ErrBusy
	}
	// Keep the runtime the user chose earlier unless this start names one.
	if opts.Runtime == "" {
		opts.Runtime = m.st.Options.Runtime
	}
	if opts.Mirror == "" {
		opts.Mirror = m.st.Options.Mirror
	}
	// "Local only" sticks until the user confirms the admin step again.
	if !opts.LocalOnly && !opts.ConfirmAdmin {
		opts.LocalOnly = m.st.Options.LocalOnly
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel, m.running = cancel, true
	now := m.cfg.Now()
	m.st.State, m.st.Error, m.st.Need, m.st.FinishedAt = StateRunning, nil, nil, nil
	m.st.StartedAt, m.st.Options = &now, opts
	m.st.Network = m.cfg.Network()
	// Every step runs again (done ones are quick); clear the old results so a
	// run that stops early does not show a previous run's checks as passed.
	for i := range m.st.Steps {
		if m.st.Steps[i].ID != StepBinary {
			m.st.Steps[i].State, m.st.Steps[i].Detail, m.st.Steps[i].Items = StateWaiting, "", nil
		}
	}
	m.st.Checks = nil
	m.emitLocked(Event{Event: "start", Version: m.cfg.Version, Network: m.st.Network, Detail: "steps=4"})
	m.saveLocked(true)
	m.mu.Unlock()
	go m.run(ctx, opts)
	return nil
}

// Resume repeats the last start (after a failure, a pause or a reboot).
func (m *Manager) Resume() error {
	m.mu.Lock()
	opts := m.st.Options
	m.mu.Unlock()
	return m.Start(opts)
}

// Pause stops the current run; partial downloads are kept.
func (m *Manager) Pause() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return false
	}
	m.cancel()
	return true
}

// Boot is called when the server starts: it repeats the setup when it was
// done before (so the built-in runtime is started at boot and the checks run
// again), and resumes it when a run was interrupted. A setup waiting for the
// user stays that way; one never started starts.
func (m *Manager) Boot() {
	m.mu.Lock()
	state, need := m.st.State, m.st.Need
	m.mu.Unlock()
	if state == StateNeedsUser && need != nil && need.Kind != NeedReboot {
		// A choice is still open; detect again in case Docker appeared.
		_ = m.Start(StartOptions{})
		return
	}
	_ = m.Resume()
}

func (m *Manager) finish(state string, f *Failure, need *Need) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
	// An admin confirmation covers one run: a later boot never elevates
	// without asking again.
	m.st.Options.ConfirmAdmin = false
	now := m.cfg.Now()
	m.st.State, m.st.Error, m.st.Need = state, f, need
	switch state {
	case StateDone:
		m.st.FinishedAt, m.st.EverDone = &now, true
		ok := true
		took := 0
		if m.st.StartedAt != nil {
			took = int(now.Sub(*m.st.StartedAt).Seconds())
		}
		m.emitLocked(Event{Event: "done", OK: &ok, TookS: took, Console: m.cfg.Console})
	case StateNeedsUser:
		m.emitLocked(Event{Event: "needs_user", Need: need})
	case StatePaused:
		m.emitLocked(Event{Event: "paused"})
	case StateFailed:
		m.emitLocked(Event{Event: "error", Error: f})
	}
	m.saveLocked(true)
}

// reporter implements Reporter for one step.
type reporter struct {
	m    *Manager
	step string
}

func (r reporter) Item(it Item) {
	m := r.m
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.step(r.step)
	found := false
	for i := range s.Items {
		if s.Items[i].ID == it.ID {
			s.Items[i], found = it, true
		}
	}
	if !found {
		s.Items = append(s.Items, it)
	}
	cp := it
	m.emitLocked(Event{Event: "progress", Step: r.step, Item: &cp})
	m.saveLocked(it.State != StateRunning)
}

func (r reporter) Detail(d string) {
	m := r.m
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.step(r.step)
	s.Detail = d
	m.emitLocked(Event{Event: "step", Step: r.step, State: s.State, Detail: d})
	m.saveLocked(false)
}

func (r reporter) Log(format string, args ...any) {
	r.m.cfg.Logf("setup %s: "+format, append([]any{r.step}, args...)...)
}

// run executes the four steps.
func (m *Manager) run(ctx context.Context, opts StartOptions) {
	fail := func(step string, err error) {
		if ctx.Err() != nil {
			m.setStep(step, StatePaused, "")
			m.finish(StatePaused, nil, nil)
			return
		}
		var ne *NeedError
		if errors.As(err, &ne) {
			m.setStep(step, StateNeedsUser, ne.Need.Message)
			m.finish(StateNeedsUser, nil, &ne.Need)
			return
		}
		f := &Failure{Step: step, Code: "SETUP-GEN-01", Message: err.Error(), Retry: "acornfox setup --resume"}
		var fe *Fault
		if errors.As(err, &fe) {
			f.Code, f.Message, f.KeptBytes = fe.Code, fe.Error(), fe.KeptBytes
		}
		if def, ok := FaultByCode(f.Code); ok {
			f.Next, f.NeedsUser = def.Next, def.NeedsUser
		}
		m.setStep(step, StateFailed, f.Message)
		m.finish(StateFailed, f, nil)
	}

	// 1. The program itself: running means installed.
	m.setStep(StepBinary, StateDone, m.cfg.Version)

	// 2. Runtime.
	m.setStep(StepRuntime, StateRunning, "检测这台电脑上的 Docker")
	d, err := m.runtimeStep(ctx, opts)
	if err != nil {
		fail(StepRuntime, err)
		return
	}
	m.mu.Lock()
	m.st.RuntimeDetail = d.Name + " " + d.Version
	switch {
	case d.Kind == RuntimeBuiltin:
		// Remember it: the next boot starts the built-in runtime again.
		m.st.Runtime, m.st.Options.Runtime = RuntimeBuiltin, RuntimeBuiltin
	case opts.Runtime == RuntimeDockerDesktop:
		m.st.Runtime = RuntimeDockerDesktop
	default:
		m.st.Runtime = RuntimeExisting
	}
	changed := d.Host != m.lastHost
	m.lastHost = d.Host
	m.mu.Unlock()
	if changed && m.cfg.OnRuntime != nil {
		m.cfg.OnRuntime(d)
	}
	m.setStep(StepRuntime, StateDone, d.Name+" "+d.Version+" · 运行中")

	// 3. Gateway.
	m.setStep(StepEdge, StateRunning, "准备 Caddy")
	if m.cfg.Edge != nil {
		detail, err := m.cfg.Edge(ctx, reporter{m, StepEdge})
		if err != nil {
			fail(StepEdge, err)
			return
		}
		m.setStep(StepEdge, StateDone, detail)
	} else {
		m.setStep(StepEdge, StateDone, "")
	}

	// 4. Checks.
	m.setStep(StepChecks, StateRunning, "逐项检查")
	var checks []Check
	if m.cfg.Checks != nil {
		checks = m.cfg.Checks(ctx)
	}
	passed, firstBad := 0, -1
	for i, c := range checks {
		cp := c
		m.mu.Lock()
		m.emitLocked(Event{Event: "check", Step: StepChecks, Check: &cp})
		m.mu.Unlock()
		if c.OK {
			passed++
		} else if c.Required && firstBad < 0 {
			firstBad = i
		}
	}
	m.mu.Lock()
	m.st.Checks = checks
	m.mu.Unlock()
	if ctx.Err() != nil {
		fail(StepChecks, ctx.Err())
		return
	}
	if firstBad >= 0 {
		c := checks[firstBad]
		code := c.Code
		if code == "" {
			code = "SETUP-CHK-01"
		}
		fail(StepChecks, &Fault{Code: code, Message: c.Title + "：" + c.Detail})
		return
	}
	m.setStep(StepChecks, StateDone, fmt.Sprintf("%d/%d 通过", passed, len(checks)))
	m.finish(StateDone, nil, nil)
}

// desktopWait bounds the wait for Docker Desktop to answer after starting it.
var desktopWait = 3 * time.Minute

func (m *Manager) runtimeStep(ctx context.Context, opts StartOptions) (Detection, error) {
	rt := m.cfg.Runtime
	if rt == nil {
		return Detection{}, Faultf("SETUP-RT-03", nil, "没有可用的运行环境检测")
	}
	d := rt.Detect(ctx)
	m.mu.Lock()
	m.st.Found = d.Found
	m.mu.Unlock()
	r := reporter{m, StepRuntime}

	switch opts.Runtime {
	case RuntimeBuiltin:
		if !rt.CanBuiltin() {
			return d, Faultf("SETUP-RT-03", nil, "这个系统没有自带运行环境，请先安装 Docker")
		}
		r.Detail("AcornFox 自带的运行环境（没有界面）")
		return rt.Builtin(ctx, opts, r)
	case RuntimeDockerDesktop:
		if d.Reachable && d.Kind == RuntimeDockerDesktop {
			return d, nil
		}
		r.Detail("正在打开 Docker Desktop，等它启动")
		if err := rt.StartDockerDesktop(ctx); err != nil {
			return d, Faultf("SETUP-RT-04", err, "打开 Docker Desktop 失败")
		}
		deadline := time.Now().Add(desktopWait)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return d, ctx.Err()
			case <-time.After(3 * time.Second):
			}
			if d = rt.Detect(ctx); d.Reachable {
				return d, nil
			}
		}
		return d, Faultf("SETUP-RT-04", nil, "Docker Desktop %d 分钟内没有启动好", int(desktopWait.Minutes()))
	}

	// No explicit choice: use a Docker that answers.
	if d.Reachable {
		return d, nil
	}
	if !rt.CanBuiltin() {
		return d, Faultf("SETUP-RT-03", nil, "连不上 Docker")
	}
	// Docker Desktop is installed but stopped: ask.
	for _, f := range d.Found {
		if f.Kind == RuntimeDockerDesktop && !f.Running {
			return d, &NeedError{Need: Need{
				Kind:    NeedChooseRuntime,
				Message: "这台电脑装了 Docker Desktop" + versionSuffix(f.Version) + "，但它现在没有运行。用哪个？",
				Options: []Option{
					{ID: RuntimeBuiltin, Title: "用 AcornFox 自带的运行环境", Recommended: true,
						Detail: "没有界面，在后台运行，不弹窗口；以后开机自动准备好。"},
					{ID: RuntimeDockerDesktop, Title: "打开 Docker Desktop",
						Detail: "用你已经装的 Docker Desktop。会打开它的窗口；以后要先开着它，应用才能用。"},
				},
			}}
		}
	}
	// Nothing usable: install the built-in runtime.
	r.Detail("这台电脑还没有 Docker，装 AcornFox 自带的运行环境（没有界面）")
	return rt.Builtin(ctx, opts, r)
}

func versionSuffix(v string) string {
	if v == "" {
		return ""
	}
	return " " + v
}
