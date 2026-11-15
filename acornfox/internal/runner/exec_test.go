package runner

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

const (
	execApp  = "demo-sql"
	execDep  = "0123456789ab"
	execName = "af-demo-sql-0123456789ab"
	// execSecret stands for a password a step passes in env or stdin.
	execSecret = "S3cret-Pa55word-xyz"
)

func goodExec() ExecRequest {
	return ExecRequest{
		App:     execApp,
		Name:    execName,
		Command: []string{"demo-cli", "-X", "--file=-"},
		User:    "nobody",
		Env:     map[string]string{"AF_TOKEN": execSecret, "AF_MODE": "on"},
		Stdin:   []byte("CREATE THING x WITH SECRET '" + execSecret + "';\n"),
	}
}

func TestValidateExec(t *testing.T) {
	if err := ValidateExec(goodExec()); err != nil {
		t.Fatalf("good request: %v", err)
	}
	ok := []func(*ExecRequest){
		func(r *ExecRequest) { r.User = "" },
		func(r *ExecRequest) { r.User = "999:999" },
		func(r *ExecRequest) { r.User = "app_user.1" },
		func(r *ExecRequest) { r.Env, r.Stdin = nil, nil },
		func(r *ExecRequest) { r.TimeoutS = MaxExecTimeoutS },
		func(r *ExecRequest) { r.TimeoutS = 1 },
		func(r *ExecRequest) { r.Command = append(r.Command, "") }, // an empty argument is an argument
		func(r *ExecRequest) { r.Command = []string{"x", "a b; c && d $(e) `f` | g > h"} },
	}
	for i, mut := range ok {
		r := goodExec()
		mut(&r)
		if err := ValidateExec(r); err != nil {
			t.Fatalf("ok case %d: %v", i, err)
		}
	}
	bad := map[string]func(*ExecRequest){
		"bad app":          func(r *ExecRequest) { r.App = "Demo" },
		"other app":        func(r *ExecRequest) { r.Name = "af-other-0123456789ab" },
		"not a deployment": func(r *ExecRequest) { r.Name = "af-demo-sql-addon-cache" },
		"no command":       func(r *ExecRequest) { r.Command = nil },
		"empty program":    func(r *ExecRequest) { r.Command = []string{" ", "x"} },
		"too many args":    func(r *ExecRequest) { r.Command = make([]string, MaxExecArgs+1); r.Command[0] = "x" },
		"long arg":         func(r *ExecRequest) { r.Command = []string{"x", strings.Repeat("a", MaxExecArgBytes+1)} },
		"long command": func(r *ExecRequest) {
			r.Command = []string{"x", strings.Repeat("a", 8000), strings.Repeat("a", 8000), strings.Repeat("a", 8000), strings.Repeat("a", 8000), strings.Repeat("a", 8000)}
		},
		"NUL arg":              func(r *ExecRequest) { r.Command = []string{"x", "a\x00b"} },
		"bad UTF-8 arg":        func(r *ExecRequest) { r.Command = []string{"x", "\xff"} },
		"user with space":      func(r *ExecRequest) { r.User = "a b" },
		"user with semicolon":  func(r *ExecRequest) { r.User = "root;id" },
		"bad env key":          func(r *ExecRequest) { r.Env = map[string]string{"1X": "v"} },
		"env key with =":       func(r *ExecRequest) { r.Env = map[string]string{"A=B": "v"} },
		"NUL env value":        func(r *ExecRequest) { r.Env = map[string]string{"AF_TOKEN": execSecret + "\x00"} },
		"env too big":          func(r *ExecRequest) { r.Env = map[string]string{"A": strings.Repeat("v", MaxExecEnvBytes)} },
		"too many env":         func(r *ExecRequest) { r.Env = manyEnv(MaxExecEnv + 1) },
		"stdin too big":        func(r *ExecRequest) { r.Stdin = make([]byte, MaxExecStdinBytes+1) },
		"negative timeout":     func(r *ExecRequest) { r.TimeoutS = -1 },
		"timeout over the cap": func(r *ExecRequest) { r.TimeoutS = MaxExecTimeoutS + 1 },
	}
	for name, mut := range bad {
		r := goodExec()
		mut(&r)
		err := ValidateExec(r)
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
		if strings.Contains(err.Error(), execSecret) {
			t.Fatalf("%s: message carries the secret: %v", name, err)
		}
	}
}

func manyEnv(n int) map[string]string {
	m := map[string]string{}
	for i := range n {
		m[fmt.Sprintf("K%d", i)] = "v"
	}
	return m
}

func TestExecTimeoutDefault(t *testing.T) {
	if ExecTimeout(0) != DefaultExecTimeoutS*time.Second || ExecTimeout(7) != 7*time.Second {
		t.Fatalf("timeouts: %v %v", ExecTimeout(0), ExecTimeout(7))
	}
}

func TestExecRequestFormattingHidesSecrets(t *testing.T) {
	r := goodExec()
	for _, s := range []string{fmt.Sprint(r), fmt.Sprintf("%v %+v %#v %s", r, r, r, r), fmt.Sprintf("%v", &r)} {
		if strings.Contains(s, execSecret) || strings.Contains(s, "CREATE") {
			t.Fatalf("formatted request shows a secret: %s", s)
		}
		if !strings.Contains(s, "AF_TOKEN") || !strings.Contains(s, "demo-cli") {
			t.Fatalf("formatted request lost the non-secret parts: %s", s)
		}
	}
}

func TestScrubExecError(t *testing.T) {
	req := goodExec()
	if scrubExecError(nil, req) != nil {
		t.Fatal("nil error")
	}
	for _, s := range []error{ErrNotFound, ErrStopped, ErrUnavailable, context.DeadlineExceeded} {
		if scrubExecError(s, req) != s {
			t.Fatalf("sentinel %v changed", s)
		}
	}
	joined := errors.Join(ErrUnsupported, &RemoteError{Status: 501, ErrorResponse: ErrorResponse{Code: "unsupported"}})
	if !errors.Is(scrubExecError(joined, req), ErrUnsupported) {
		t.Fatal("a clean wrapped error lost what it wraps")
	}
	plain := errors.New("invalid environment variable: AF_TOKEN=" + execSecret)
	err := scrubExecError(plain, req)
	var re *RemoteError
	if !errors.As(err, &re) || strings.Contains(err.Error(), execSecret) || !strings.Contains(err.Error(), "AF_TOKEN=***") {
		t.Fatalf("plain error not scrubbed: %v", err)
	}
	if errors.Is(err, plain) {
		t.Fatal("the original error is still reachable")
	}
	remote := &RemoteError{Status: 500, ErrorResponse: ErrorResponse{Code: "docker_error", Message: "stdin was: CREATE THING x WITH SECRET '" + execSecret + "';"}}
	err = scrubExecError(fmt.Errorf("wrapped: %w", remote), req)
	if strings.Contains(err.Error(), execSecret) || !errors.As(err, &re) || re.Code != "docker_error" || re.Status != 500 {
		t.Fatalf("remote error not scrubbed: %v", err)
	}
	// A sentinel whose wrapping text holds a secret: scrubbed, still matches.
	for _, sentinel := range []error{ErrStopped, ErrNotFound, ErrUnsupported} {
		err = scrubExecError(fmt.Errorf("%w: AF_TOKEN=%s", sentinel, execSecret), req)
		if !errors.Is(err, sentinel) || strings.Contains(err.Error(), execSecret) || !strings.Contains(err.Error(), "AF_TOKEN=***") {
			t.Fatalf("sentinel %v: %v", sentinel, err)
		}
	}
	clean := errors.New("no such program")
	if scrubExecError(clean, req) != clean {
		t.Fatal("a clean error must stay as it is")
	}
	// Values shorter than minSecretLen ("on") are not masked.
	if got := scrubExecError(errors.New("mode on"), req).Error(); got != "mode on" {
		t.Fatalf("short value masked: %q", got)
	}
}

func TestHeadBuffer(t *testing.T) {
	b := &headBuffer{max: 4}
	for _, p := range []string{"ab", "c", "de", "f"} {
		if n, err := b.Write([]byte(p)); n != len(p) || err != nil {
			t.Fatal("write must report full length")
		}
	}
	if string(b.buf) != "abcd" || !b.cut {
		t.Fatalf("head=%q cut=%v", b.buf, b.cut)
	}
	b = &headBuffer{max: 4}
	_, _ = b.Write([]byte("abcd"))
	if b.cut {
		t.Fatal("exactly max is not cut")
	}
}

// ---- Docker side, against a fake Docker daemon ----

type execFake struct {
	labels   map[string]string
	state    container.State
	creates  atomic.Int32
	starts   atomic.Int32
	mu       sync.Mutex
	create   container.ExecCreateRequest
	stdin    []byte
	createFn func(w http.ResponseWriter) bool // true: answered (an error)
	// start writes the exec's output frames after the stdin was read.
	start      func(w io.Writer)
	hold       chan struct{} // non-nil: keep the stream open without output until closed (time-out)
	inspectFn  func(w http.ResponseWriter, n int)
	inspectsMu sync.Mutex
	inspects   int
}

func appLabels() map[string]string {
	return map[string]string{LabelManaged: "1", LabelApp: execApp, LabelDeployment: execDep, LabelRole: RoleApp}
}

func writeExecFrame(w io.Writer, stream byte, data []byte) {
	for len(data) > 0 {
		chunk := data
		if len(chunk) > 32<<10 {
			chunk = chunk[:32<<10]
		}
		var h [8]byte
		h[0] = stream
		binary.BigEndian.PutUint32(h[4:], uint32(len(chunk)))
		_, _ = w.Write(h[:])
		_, _ = w.Write(chunk)
		data = data[len(chunk):]
	}
}

// docker serves the fake over TCP (an exec attach hijacks the connection,
// which needs a tcp:// host, not http://).
func (f *execFake) docker(t *testing.T) *Docker {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.serve(t)))
	t.Cleanup(srv.Close)
	cli, err := client.New(client.WithHost("tcp://"+srv.Listener.Addr().String()), client.WithVersion("1.56"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return NewDocker(cli)
}

func (f *execFake) serve(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(p, "/containers/"+execName+"/json"):
			st := f.state
			_ = json.NewEncoder(w).Encode(container.InspectResponse{ID: "cid", Name: "/" + execName, Config: &container.Config{Labels: f.labels}, State: &st})
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/containers/cid/exec"):
			f.creates.Add(1)
			var req container.ExecCreateRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			f.mu.Lock()
			f.create = req
			f.mu.Unlock()
			if f.createFn != nil && f.createFn(w) {
				return
			}
			_ = json.NewEncoder(w).Encode(container.ExecCreateResponse{ID: "eid"})
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/exec/eid/start"):
			f.starts.Add(1)
			_, _ = io.Copy(io.Discard, r.Body)
			conn, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			defer conn.Close()
			_, _ = rw.WriteString("HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.multiplexed-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
			_ = rw.Flush()
			f.mu.Lock()
			attach := f.create.AttachStdin
			f.mu.Unlock()
			if attach {
				in, _ := io.ReadAll(rw.Reader) // until the client half-closes
				f.mu.Lock()
				f.stdin = in
				f.mu.Unlock()
			}
			if f.hold != nil {
				<-f.hold
				return
			}
			if f.start != nil {
				f.start(rw)
			}
			_ = rw.Flush()
		case r.Method == http.MethodGet && strings.HasSuffix(p, "/exec/eid/json"):
			f.inspectsMu.Lock()
			f.inspects++
			n := f.inspects
			f.inspectsMu.Unlock()
			if f.inspectFn != nil {
				f.inspectFn(w, n)
				return
			}
			code := 3
			_ = json.NewEncoder(w).Encode(container.ExecInspectResponse{ID: "eid", Running: false, ExitCode: &code})
		default:
			t.Errorf("unexpected Docker call %s %s", r.Method, p)
			w.WriteHeader(500)
		}
	}
}

func newExecFake() *execFake {
	return &execFake{labels: appLabels(), state: container.State{Running: true}}
}

func TestDockerExecRunsTheVectorWithStdinAndEnv(t *testing.T) {
	f := newExecFake()
	f.start = func(w io.Writer) {
		f.mu.Lock()
		in := f.stdin
		f.mu.Unlock()
		writeExecFrame(w, 1, append([]byte("got:"), in...))
		writeExecFrame(w, 2, []byte("warn\n"))
	}
	d := f.docker(t)
	req := goodExec()
	res, err := d.Exec(context.Background(), req)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 3 || res.TimedOut || res.Truncated || string(res.Stderr) != "warn\n" || string(res.Stdout) != "got:"+string(req.Stdin) {
		t.Fatalf("result: %+v stdout=%q", res, res.Stdout)
	}
	f.mu.Lock()
	cr := f.create
	f.mu.Unlock()
	if strings.Join(cr.Cmd, "\x00") != strings.Join(req.Command, "\x00") {
		t.Fatalf("command not passed as is: %q", cr.Cmd)
	}
	if cr.User != "nobody" || cr.Privileged || cr.Tty || !cr.AttachStdin || !cr.AttachStdout || !cr.AttachStderr {
		t.Fatalf("exec config: %+v", cr)
	}
	if strings.Join(cr.Env, ",") != "AF_MODE=on,AF_TOKEN="+execSecret {
		t.Fatalf("env: %q", cr.Env)
	}
}

func TestDockerExecWithoutStdinDoesNotAttachIt(t *testing.T) {
	f := newExecFake()
	f.start = func(w io.Writer) { writeExecFrame(w, 1, []byte("ok")) }
	f.inspectFn = func(w http.ResponseWriter, n int) {
		code := 0
		running := n < 3 // the stream ended a moment before the process
		_ = json.NewEncoder(w).Encode(container.ExecInspectResponse{ID: "eid", Running: running, ExitCode: &code})
	}
	old := execPollInterval
	execPollInterval = time.Millisecond
	t.Cleanup(func() { execPollInterval = old })
	req := goodExec()
	req.Stdin, req.Env = nil, nil
	res, err := f.docker(t).Exec(context.Background(), req)
	if err != nil || res.ExitCode != 0 || string(res.Stdout) != "ok" || res.Stderr == nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.create.AttachStdin {
		t.Fatal("stdin attached without stdin")
	}
}

func TestDockerExecTruncatesOutput(t *testing.T) {
	f := newExecFake()
	out := bytes.Repeat([]byte("o"), MaxExecOutputBytes+1000)
	out[0] = 'A'
	errb := bytes.Repeat([]byte("e"), MaxExecOutputBytes+1000)
	errb[len(errb)-1] = 'Z'
	f.start = func(w io.Writer) {
		writeExecFrame(w, 1, out)
		writeExecFrame(w, 2, errb)
	}
	res, err := f.docker(t).Exec(context.Background(), goodExec())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Stdout) != MaxExecOutputBytes || res.Stdout[0] != 'A' || !res.StdoutTruncated {
		t.Fatalf("stdout: %d bytes, cut %v", len(res.Stdout), res.StdoutTruncated)
	}
	if len(res.Stderr) != MaxExecOutputBytes || res.Stderr[len(res.Stderr)-1] != 'Z' || !res.StderrTruncated {
		t.Fatalf("stderr: %d bytes, cut %v", len(res.Stderr), res.StderrTruncated)
	}
	if !res.Truncated || res.ExitCode != 3 {
		t.Fatalf("result: %+v", res.ExitCode)
	}
}

func TestDockerExecTimesOutHonestly(t *testing.T) {
	f := newExecFake()
	f.hold = make(chan struct{})
	d := f.docker(t)
	defer close(f.hold)
	req := goodExec()
	req.TimeoutS = 1
	start := time.Now()
	res, err := d.Exec(context.Background(), req)
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut || res.ExitCode != -1 || !strings.Contains(res.Message, "可能还在容器里运行") {
		t.Fatalf("result: %+v", res)
	}
	if took < time.Second || took > 5*time.Second {
		t.Fatalf("took %v", took)
	}
	f.inspectsMu.Lock()
	defer f.inspectsMu.Unlock()
	if f.inspects != 0 {
		t.Fatal("asked for an exit code after the time-out")
	}
}

func TestDockerExecStillRunningAfterOutputIsATimeOut(t *testing.T) {
	f := newExecFake()
	f.start = func(w io.Writer) { writeExecFrame(w, 1, []byte("bye")) }
	f.inspectFn = func(w http.ResponseWriter, _ int) {
		_ = json.NewEncoder(w).Encode(container.ExecInspectResponse{ID: "eid", Running: true})
	}
	old := execPollInterval
	execPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { execPollInterval = old })
	req := goodExec()
	req.TimeoutS = 1
	res, err := f.docker(t).Exec(context.Background(), req)
	if err != nil || !res.TimedOut || res.ExitCode != -1 || string(res.Stdout) != "bye" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestDockerExecRefusesBeforeExec(t *testing.T) {
	mutations := map[string]func(*execFake){
		"unmanaged":        func(f *execFake) { delete(f.labels, LabelManaged) },
		"other app":        func(f *execFake) { f.labels[LabelApp] = "other" },
		"add-on role":      func(f *execFake) { f.labels[LabelRole] = RoleAddon },
		"file helper role": func(f *execFake) { f.labels[LabelRole] = RoleFiles },
		"no role":          func(f *execFake) { delete(f.labels, LabelRole) },
		"other deployment": func(f *execFake) { f.labels[LabelDeployment] = "ffffffffffff" },
	}
	for name, mut := range mutations {
		f := newExecFake()
		mut(f)
		_, err := f.docker(t).Exec(context.Background(), goodExec())
		var re *RemoteError
		if !errors.As(err, &re) || re.Code != "refused" || re.Status != http.StatusForbidden {
			t.Fatalf("%s: want refused, got %v", name, err)
		}
		if f.creates.Load() != 0 {
			t.Fatalf("%s: exec created", name)
		}
	}
	states := map[string]container.State{
		"stopped":    {Running: false},
		"paused":     {Running: true, Paused: true},
		"restarting": {Running: true, Restarting: true},
	}
	for name, st := range states {
		f := newExecFake()
		f.state = st
		if _, err := f.docker(t).Exec(context.Background(), goodExec()); !errors.Is(err, ErrStopped) {
			t.Fatalf("%s: want ErrStopped, got %v", name, err)
		}
		if f.creates.Load() != 0 {
			t.Fatalf("%s: exec created", name)
		}
	}
	// A malformed request never reaches Docker.
	f := newExecFake()
	req := goodExec()
	req.Name = "af-demo-sql-addon-cache"
	_, err := f.docker(t).Exec(context.Background(), req)
	assertInvalidRequest(t, err)
}

func TestDockerExecCreateErrors(t *testing.T) {
	f := newExecFake()
	f.createFn = func(w http.ResponseWriter) bool {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"container cid is not running"}`))
		return true
	}
	if _, err := f.docker(t).Exec(context.Background(), goodExec()); !errors.Is(err, ErrStopped) {
		t.Fatalf("conflict: %v", err)
	}
	// Docker echoing an env entry in its error must not leak the value.
	f = newExecFake()
	f.createFn = func(w http.ResponseWriter) bool {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"invalid environment variable: AF_TOKEN=` + execSecret + `"}`))
		return true
	}
	_, err := f.docker(t).Exec(context.Background(), goodExec())
	if err == nil || strings.Contains(err.Error(), execSecret) {
		t.Fatalf("leaked or nil: %v", err)
	}
	if f.starts.Load() != 0 {
		t.Fatal("started after a failed create")
	}
}

func TestDockerExecDroppedByDockerIsExecFailed(t *testing.T) {
	f := newExecFake()
	f.start = func(w io.Writer) {
		writeExecFrame(w, 1, []byte("OCI runtime exec failed: exec: \"demo-cli\": executable file not found; token "+execSecret+"\r\n"))
	}
	f.inspectFn = func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"No such exec instance: eid"}`))
	}
	_, err := f.docker(t).Exec(context.Background(), goodExec())
	var re *RemoteError
	if !errors.As(err, &re) || re.Code != CodeExecFailed || !strings.Contains(re.Message, "executable file not found") || strings.Contains(re.Message, execSecret) {
		t.Fatalf("err=%v", err)
	}
}

// ---- Client and Server over the peer socket ----

type execAPIFake struct {
	*fakeAPI
	mu    sync.Mutex
	calls int
	got   ExecRequest
	res   ExecResult
	err   error
}

func (f *execAPIFake) Exec(_ context.Context, req ExecRequest) (ExecResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.got = req
	return f.res, f.err
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

func TestClientExecRoundTrip(t *testing.T) {
	logs := captureLog(t)
	bin := []byte{0, 1, 2, 0xff, '<', '&', '\n'}
	f := &execAPIFake{fakeAPI: &fakeAPI{}, res: ExecResult{ExitCode: 7, Stdout: bin, Stderr: []byte("e"), StderrTruncated: true, Truncated: true}}
	_, cli := newTestServer(t, f)
	req := goodExec()
	res, err := cli.Exec(context.Background(), req)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 7 || !bytes.Equal(res.Stdout, bin) || string(res.Stderr) != "e" || !res.Truncated || !res.StderrTruncated || res.StdoutTruncated {
		t.Fatalf("result: %+v", res)
	}
	f.mu.Lock()
	got := f.got
	f.mu.Unlock()
	if got.String() != req.String() || !bytes.Equal(got.Stdin, req.Stdin) || got.Env["AF_TOKEN"] != execSecret {
		t.Fatalf("server got %v", got)
	}
	// Empty output comes back as empty, not missing.
	f.res = ExecResult{}
	res, err = cli.Exec(context.Background(), req)
	if err != nil || res.Stdout == nil || res.Stderr == nil {
		t.Fatalf("empty: %+v %v", res, err)
	}
	if strings.Contains(logs.String(), execSecret) {
		t.Fatal("a secret reached the log")
	}
}

func TestClientExecErrors(t *testing.T) {
	logs := captureLog(t)
	f := &execAPIFake{fakeAPI: &fakeAPI{}, err: ErrStopped}
	_, cli := newTestServer(t, f)
	if _, err := cli.Exec(context.Background(), goodExec()); !errors.Is(err, ErrStopped) {
		t.Fatalf("stopped: %v", err)
	}
	f.err = ErrNotFound
	if _, err := cli.Exec(context.Background(), goodExec()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	// Wrapped sentinels whose text holds a secret: still recognised, scrubbed.
	for _, sentinel := range []error{ErrStopped, ErrNotFound} {
		f.err = fmt.Errorf("%w: AF_TOKEN=%s", sentinel, execSecret)
		if _, err := cli.Exec(context.Background(), goodExec()); !errors.Is(err, sentinel) || strings.Contains(err.Error(), execSecret) {
			t.Fatalf("wrapped %v: %v", sentinel, err)
		}
	}
	f.err = refusedContainer(execName, execApp)
	_, err := cli.Exec(context.Background(), goodExec())
	var re *RemoteError
	if !errors.As(err, &re) || re.Code != "refused" || errors.Is(err, ErrUnsupported) {
		t.Fatalf("refused: %v", err)
	}
	// An implementation that does not scrub its errors is scrubbed by the server.
	f.err = errors.New("docker said: AF_TOKEN=" + execSecret)
	_, err = cli.Exec(context.Background(), goodExec())
	if err == nil || strings.Contains(err.Error(), execSecret) {
		t.Fatalf("leaked: %v", err)
	}
	f.err = &RemoteError{Status: 500, ErrorResponse: ErrorResponse{Code: "docker_error", Message: "x " + execSecret}}
	_, err = cli.Exec(context.Background(), goodExec())
	if err == nil || strings.Contains(err.Error(), execSecret) {
		t.Fatalf("leaked: %v", err)
	}
	if strings.Contains(logs.String(), execSecret) {
		t.Fatal("a secret reached the log")
	}
}

func TestClientExecValidatesOnBothSides(t *testing.T) {
	f := &execAPIFake{fakeAPI: &fakeAPI{}}
	_, cli := newTestServer(t, f)
	req := goodExec()
	req.TimeoutS = MaxExecTimeoutS + 1
	_, err := cli.Exec(context.Background(), req)
	assertInvalidRequest(t, err)
	// Past the client's own check, the server refuses too.
	for _, bad := range []ExecRequest{req, {App: execApp, Name: "af-demo-sql-addon-cache", Command: []string{"x"}}, {App: execApp, Name: execName}} {
		err = cli.call(context.Background(), cli.standard, PathContainerExec, bad, clientDefaultBound, nil)
		assertInvalidRequest(t, err)
	}
	if f.calls != 0 {
		t.Fatal("an invalid request reached the API")
	}
}

func TestServerExecUnsupported(t *testing.T) {
	_, cli := newTestServer(t, apiOnly{&fakeAPI{}})
	_, err := cli.Exec(context.Background(), goodExec())
	var re *RemoteError
	if !errors.Is(err, ErrUnsupported) || !errors.As(err, &re) || re.Status != http.StatusNotImplemented {
		t.Fatalf("want 501 ErrUnsupported, got %v", err)
	}
}

func TestClientExecOldRunner(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+PathPing, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, PingResponse{}) })
	cli := startTestServer(t, &Server{api: &fakeAPI{}, mux: mux})
	_, err := cli.Exec(context.Background(), goodExec())
	if !errors.Is(err, ErrUnsupported) || errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

// The largest request the bounds allow fits the runner's body limit, and the
// largest answer fits the client's bound; the client waits long enough.
func TestExecBoundsFitThePeerLimits(t *testing.T) {
	ctrl := strings.Repeat("\x01", MaxExecArgBytes)
	req := ExecRequest{App: execApp, Name: execName, Stdin: bytes.Repeat([]byte{0xff}, MaxExecStdinBytes), TimeoutS: MaxExecTimeoutS, User: strings.Repeat("u", 32)}
	for i := 0; i < MaxExecCommandBytes/MaxExecArgBytes; i++ {
		req.Command = append(req.Command, ctrl)
	}
	req.Env = map[string]string{"A": strings.Repeat("\x01", MaxExecEnvBytes-1)}
	if err := ValidateExec(req); err != nil {
		t.Fatalf("worst-case request invalid: %v", err)
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) >= serverMaxBody {
		t.Fatalf("worst-case request is %d bytes, runner reads %d", len(body), serverMaxBody)
	}
	res := ExecResult{ExitCode: 1, Stdout: bytes.Repeat([]byte{0xff}, MaxExecOutputBytes), Stderr: bytes.Repeat([]byte{0xff}, MaxExecOutputBytes), Truncated: true, TimedOut: true, Message: strings.Repeat("字", 100)}
	out, err := marshalJSON(res)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) >= clientLargeBound {
		t.Fatalf("worst-case answer %d bytes, client reads %d", len(out), clientLargeBound)
	}
	if ExecTimeout(MaxExecTimeoutS)+execClientSlack >= buildTimeout {
		t.Fatal("the long peer client would cut an exec short")
	}
	// And it really goes through the socket.
	f := &execAPIFake{fakeAPI: &fakeAPI{}, res: res}
	_, cli := newTestServer(t, f)
	got, err := cli.Exec(context.Background(), req)
	if err != nil || len(got.Stdout) != MaxExecOutputBytes || len(got.Stderr) != MaxExecOutputBytes {
		t.Fatalf("worst case over the socket: %v", err)
	}
	if f.got.Env["A"] != req.Env["A"] || !bytes.Equal(f.got.Stdin, req.Stdin) {
		t.Fatal("worst-case request altered")
	}
}

// collectExec with a stream that ends on ctx: the caller's cancellation is
// an error, not a time-out.
func TestCollectExecContextCanceled(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, timedOut, err := collectExec(ctx, pipeStream{a}, nil, time.Now().Add(time.Minute))
	if timedOut || !errors.Is(err, context.Canceled) {
		t.Fatalf("timedOut=%v err=%v", timedOut, err)
	}
}

type pipeStream struct{ net.Conn }

func (p pipeStream) CloseWrite() error { return nil }
