package runner

// Running a command in an app's own container (exec).
//
// A catalog app's manifest may say, as steps, how it gives a consumer what it
// provides by running a command inside its own container (for example: make a
// database and an account for this consumer). The install engine renders the
// step and asks the runner, through this route, to run it. The runner does
// not know which app it is or what the command does; it only enforces the
// boundary below.
//
// Boundary (enforced here, on the runner side, whatever the caller sends):
//   - The target is one container of the app named in the request: managed
//     (LabelManaged=1, LabelApp=app, the name af-<app>-<deployment id>), of
//     role app (LabelRole=RoleApp, LabelDeployment equal to the name's
//     deployment id) and running (not paused, not restarting). The add-on
//     containers a deployed project gets, the never-started file helpers and
//     the edge container are out of scope by their role label or name.
//   - Command is an argument vector given to Docker's exec as is: no shell
//     reads it. The first element is the program.
//   - Never privileged, never a TTY; the user is a name or uid[:gid].
//   - Values meant to stay secret travel only in Env and Stdin: they are not
//     part of any process's argument list (on the host or in the container),
//     the runner never logs a request, and every error the runner returns has
//     the env values and the stdin text (and each stdin line) of at least
//     minSecretLen (4) bytes replaced by "***"; shorter values are left as
//     they are. The output is returned as the command wrote it; the caller
//     masks it. Docker's own event log does record the command vector (not
//     env): a manifest must never put a secret in Command.
//   - Stdout keeps its first MaxExecOutputBytes and stderr its last
//     MaxExecOutputBytes; more is dropped and marked truncated.
//   - TimeoutS (1..MaxExecTimeoutS, default DefaultExecTimeoutS) bounds the
//     run. Docker has no API to stop an exec'd process, so on time-out the
//     runner stops waiting, reports TimedOut, and the process may still be
//     running in the container (until it ends or the container stops). The
//     answer says so; it never claims the process was killed.
//
// What the runner cannot check, the caller must (the step executor in
// internal/appinstall):
//   - The runner only checks that the name, labels and deployment id agree;
//     it cannot tell the app's current deployment from an older one still
//     running, nor a catalog app from a user's deployed project (both are
//     role app). The caller computes the target only as
//     ContainerName(app, state.App.CurrentDeployment), and the app only from
//     the service registry or the app itself, never from request input.
//   - Truncated output is marked, not refused: a step that gets
//     Truncated == true must fail (the answer it parses may be cut).
//
// Like CatalogAPI, ExecAPI is optional: older runners and the API test fakes
// do not have it, and a Client talking to a runner without the route gets an
// error matching ErrUnsupported (the caller reports "runner too old").

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// PathContainerExec: POST ExecRequest -> ExecResult (long: up to
// MaxExecTimeoutS plus a little).
const PathContainerExec = "/v1/containers/exec"

// Bounds on ExecRequest and ExecResult. Together they keep the worst-case
// request under serverMaxBody (JSON escapes a control byte as six bytes;
// stdin travels as base64) and the worst-case answer under clientLargeBound.
const (
	MaxExecArgs         = 64
	MaxExecArgBytes     = 8 << 10   // one argument
	MaxExecCommandBytes = 32 << 10  // all arguments together
	MaxExecEnv          = 64        // variables
	MaxExecEnvBytes     = 32 << 10  // all names and values together
	MaxExecStdinBytes   = 128 << 10 // stdin
	MaxExecOutputBytes  = 64 << 10  // kept of stdout, and of stderr
	DefaultExecTimeoutS = 30
	MaxExecTimeoutS     = 300
)

// execClientSlack is how much longer than TimeoutS the client waits for the
// answer: creating the exec, the last inspect and the transport.
const execClientSlack = 30 * time.Second

// execPollInterval is how often the exit code is asked for after the
// output ended (a process may close its streams a moment before it exits).
// Tests shorten it.
var execPollInterval = 50 * time.Millisecond

// Codes an exec answer may carry besides the common ones.
const (
	// CodeExecFailed: Docker could not start the command (no such program,
	// no such user) and dropped the exec before reporting an exit code.
	CodeExecFailed = "exec_failed"
)

var (
	execUserPattern   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,31}(:[A-Za-z0-9_][A-Za-z0-9_.-]{0,31})?$`)
	execEnvKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

// ExecAPI is implemented by *Docker and *Client.
type ExecAPI interface {
	Exec(ctx context.Context, req ExecRequest) (ExecResult, error)
}

// ExecRequest runs Command in the running app container Name of App.
type ExecRequest struct {
	App  string `json:"app"`
	Name string `json:"name"` // ContainerName(App, <deployment id>): the app's current deployment
	// Command is the argument vector; Command[0] is the program. 1..MaxExecArgs
	// elements, no NUL bytes. Must not carry secrets (use Env or Stdin).
	Command []string `json:"command"`
	// User inside the container: a name or uid[:gid]; "" is the image's user.
	User string `json:"user,omitempty"`
	// Env is added to the container's environment for this command only.
	// Values may be secret.
	Env map[string]string `json:"env,omitempty"`
	// Stdin is written to the command's standard input, which is then
	// closed; empty means the command gets no input. May be secret. base64
	// on the wire.
	Stdin []byte `json:"stdin,omitempty"`
	// TimeoutS is 1..MaxExecTimeoutS seconds; 0 means DefaultExecTimeoutS.
	TimeoutS int `json:"timeout_s,omitempty"`
}

// String hides Env values and Stdin, so formatting a request (%v, %+v, %s)
// never prints a secret.
func (r ExecRequest) String() string {
	keys := make([]string, 0, len(r.Env))
	for k := range r.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return fmt.Sprintf("exec{app:%s name:%s command:%q user:%q env:%v stdin:%d bytes timeout_s:%d}",
		r.App, r.Name, r.Command, r.User, keys, len(r.Stdin), r.TimeoutS)
}

// GoString hides secrets for %#v too.
func (r ExecRequest) GoString() string { return r.String() }

// ExecResult is what the command did. A non-zero exit code is a result, not
// an error.
type ExecResult struct {
	// ExitCode of the command; -1 when TimedOut (not known).
	ExitCode int `json:"exit_code"`
	// Stdout is the first MaxExecOutputBytes the command wrote to stdout,
	// Stderr the last MaxExecOutputBytes it wrote to stderr (base64 on the
	// wire). Raw: the caller masks secrets before showing them.
	Stdout []byte `json:"stdout"`
	Stderr []byte `json:"stderr"`
	// StdoutTruncated / StderrTruncated: more was written and dropped.
	// Truncated is either of them.
	StdoutTruncated bool `json:"stdout_truncated,omitempty"`
	StderrTruncated bool `json:"stderr_truncated,omitempty"`
	Truncated       bool `json:"truncated"`
	// TimedOut: the command did not end within TimeoutS. The runner stopped
	// waiting; the process may still be running in the container (Docker
	// cannot stop an exec). Message says so in words.
	TimedOut bool   `json:"timed_out"`
	Message  string `json:"message,omitempty"`
}

// ExecTimeout is the run bound of a request's TimeoutS (0 = default).
func ExecTimeout(timeoutS int) time.Duration {
	if timeoutS == 0 {
		timeoutS = DefaultExecTimeoutS
	}
	return time.Duration(timeoutS) * time.Second
}

// ValidateExec checks a request the same way on both sides. Its messages
// name fields and variables but never contain a value.
func ValidateExec(req ExecRequest) error {
	if !ValidApp(req.App) {
		return errors.New("invalid app name")
	}
	prefix := VolumePrefix(req.App)
	if !strings.HasPrefix(req.Name, prefix) || !ValidDeploymentID(strings.TrimPrefix(req.Name, prefix)) {
		return errors.New("name must be " + prefix + "<12-hex deployment id>")
	}
	if len(req.Command) == 0 || len(req.Command) > MaxExecArgs {
		return fmt.Errorf("command must have 1..%d arguments", MaxExecArgs)
	}
	if strings.TrimSpace(req.Command[0]) == "" {
		return errors.New("command[0] (the program) is empty")
	}
	total := 0
	for i, a := range req.Command {
		if len(a) > MaxExecArgBytes {
			return fmt.Errorf("command[%d] is longer than %d bytes", i, MaxExecArgBytes)
		}
		if strings.ContainsRune(a, 0) || !utf8.ValidString(a) {
			return fmt.Errorf("command[%d] contains a NUL byte or invalid UTF-8", i)
		}
		total += len(a)
	}
	if total > MaxExecCommandBytes {
		return fmt.Errorf("command is longer than %d bytes", MaxExecCommandBytes)
	}
	if req.User != "" && !execUserPattern.MatchString(req.User) {
		return errors.New("user must be a name or uid[:gid]")
	}
	if len(req.Env) > MaxExecEnv {
		return fmt.Errorf("at most %d env variables", MaxExecEnv)
	}
	total = 0
	for k, v := range req.Env {
		if !execEnvKeyPattern.MatchString(k) {
			return errors.New("invalid env variable name")
		}
		if strings.ContainsRune(v, 0) || !utf8.ValidString(v) {
			return fmt.Errorf("env %s: value contains a NUL byte or invalid UTF-8", k)
		}
		total += len(k) + len(v)
	}
	if total > MaxExecEnvBytes {
		return fmt.Errorf("env is larger than %d bytes", MaxExecEnvBytes)
	}
	if len(req.Stdin) > MaxExecStdinBytes {
		return fmt.Errorf("stdin is larger than %d bytes", MaxExecStdinBytes)
	}
	if req.TimeoutS < 0 || req.TimeoutS > MaxExecTimeoutS {
		return fmt.Errorf("timeout_s must be 1..%d (0 = %d)", MaxExecTimeoutS, DefaultExecTimeoutS)
	}
	return nil
}

// execError builds the runner's answer for a failed exec. Like writeError,
// it takes the runner's generic codes positionally: they only reach acornfox
// server, which turns them into the user-facing fault codes.
func execError(status int, code, msg string) error {
	return &RemoteError{Status: status, ErrorResponse: ErrorResponse{Code: code, Message: msg}}
}

func invalidExec(err error) error {
	return execError(http.StatusBadRequest, "invalid_request", err.Error())
}

// execEnvList is Env as KEY=VALUE, sorted (stable for tests and Docker).
func execEnvList(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// minSecretLen: shorter values are not masked in error text (replacing "1"
// or "on" everywhere would garble the message and hides nothing real).
const minSecretLen = 4

// execSecrets lists the request's values that must not show in an error:
// every env value and the stdin text, longest first.
func execSecrets(req ExecRequest) []string {
	var out []string
	for _, v := range req.Env {
		if len(v) >= minSecretLen {
			out = append(out, v)
		}
	}
	if len(req.Stdin) >= minSecretLen {
		out = append(out, string(req.Stdin))
		for _, line := range strings.Split(string(req.Stdin), "\n") {
			if line = strings.TrimSpace(line); len(line) >= minSecretLen {
				out = append(out, line)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

func scrubText(s string, secrets []string) string {
	for _, v := range secrets {
		s = strings.ReplaceAll(s, v, "***")
	}
	return s
}

// scrubExecError returns err with the request's secrets replaced. An error
// whose text holds none is returned as it is (sentinels and wrapping kept).
// Otherwise, an error matching ErrStopped, ErrNotFound or ErrUnsupported
// becomes a scrubbedError that still matches it; any other becomes a
// RemoteError (the code and status of the RemoteError inside, or
// docker_error). Either way it carries only scrubbed text, so nothing below
// it can print the original.
func scrubExecError(err error, req ExecRequest) error {
	if err == nil {
		return nil
	}
	secrets := execSecrets(req)
	msg := err.Error()
	scrubbed := scrubText(msg, secrets)
	if scrubbed == msg {
		return err
	}
	for _, sentinel := range []error{ErrStopped, ErrNotFound, ErrUnsupported} {
		if errors.Is(err, sentinel) {
			return &scrubbedError{msg: scrubbed, sentinel: sentinel}
		}
	}
	var re *RemoteError
	if errors.As(err, &re) {
		return execError(re.Status, re.Code, scrubText(re.Message, secrets))
	}
	return execError(http.StatusInternalServerError, "docker_error", scrubbed)
}

// scrubbedError is a scrubbed message that still matches its sentinel.
type scrubbedError struct {
	msg      string
	sentinel error
}

func (e *scrubbedError) Error() string { return e.msg }
func (e *scrubbedError) Unwrap() error { return e.sentinel }

// checkExecTarget: on top of managedContainer's ownership check, the
// container must be an app-role container of the deployment its name says.
func checkExecTarget(c container.InspectResponse, app, name string) error {
	dep := strings.TrimPrefix(name, VolumePrefix(app))
	if c.Config == nil || c.Config.Labels[LabelRole] != RoleApp || c.Config.Labels[LabelDeployment] != dep || !ValidDeploymentID(dep) {
		return execError(http.StatusForbidden, "refused", fmt.Sprintf("container %q is not an app container of %q", name, app))
	}
	return nil
}

func execRunning(c container.InspectResponse) bool {
	return c.State != nil && c.State.Running && !c.State.Paused && !c.State.Restarting
}

// ---- Docker ----

// Exec implements ExecAPI.
func (d *Docker) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	if err := ValidateExec(req); err != nil {
		return ExecResult{}, invalidExec(err)
	}
	res, err := d.exec(ctx, req)
	return res, scrubExecError(err, req)
}

func (d *Docker) exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	c, err := d.managedContainer(ctx, req.App, req.Name)
	if err != nil {
		return ExecResult{}, err
	}
	if err := checkExecTarget(c, req.App, req.Name); err != nil {
		return ExecResult{}, err
	}
	if !execRunning(c) {
		return ExecResult{}, ErrStopped
	}
	created, err := d.c().ExecCreate(ctx, c.ID, client.ExecCreateOptions{
		User:         req.User,
		AttachStdin:  len(req.Stdin) > 0,
		AttachStdout: true,
		AttachStderr: true,
		Env:          execEnvList(req.Env),
		Cmd:          req.Command,
	})
	if err != nil {
		return ExecResult{}, execDockerError(err)
	}
	timeout := ExecTimeout(req.TimeoutS)
	deadline := time.Now().Add(timeout)
	hj, err := d.c().ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return ExecResult{}, execDockerError(err)
	}
	out, timedOut, err := collectExec(ctx, hijackedStream{hj.HijackedResponse}, req.Stdin, deadline)
	if err != nil {
		return ExecResult{}, err
	}
	if timedOut {
		return out.timedOut(timeout), nil
	}
	code, running, err := d.execExit(ctx, created.ID, deadline)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			// Docker drops an exec it could not start; what it said about
			// it is in the output.
			return ExecResult{}, execError(http.StatusBadGateway, CodeExecFailed, "command could not be started in the container: "+execExcerpt(out))
		}
		return ExecResult{}, err
	}
	if running {
		// The command closed its output but did not end in time.
		return out.timedOut(timeout), nil
	}
	return out.result(code), nil
}

// execDockerError maps Docker's answers about the target.
func execDockerError(err error) error {
	switch {
	case cerrdefs.IsNotFound(err):
		return ErrNotFound
	case cerrdefs.IsConflict(err):
		return ErrStopped // stopped or paused between inspect and exec
	}
	return err
}

// execExit waits until Docker reports the exec ended, at most until
// deadline; running is true when it was still running then.
func (d *Docker) execExit(ctx context.Context, id string, deadline time.Time) (code int, running bool, err error) {
	for {
		res, err := d.c().ExecInspect(ctx, id, client.ExecInspectOptions{})
		if err != nil {
			return 0, false, err
		}
		if !res.Running {
			return res.ExitCode, false, nil
		}
		if !time.Now().Before(deadline) {
			return 0, true, nil
		}
		select {
		case <-ctx.Done():
			return 0, false, ctx.Err()
		case <-time.After(execPollInterval):
		}
	}
}

// execStream is the attached exec connection: read the multiplexed output,
// write stdin, half-close to send EOF, close.
type execStream interface {
	io.Reader
	io.Writer
	CloseWrite() error
	Close() error
}

type hijackedStream struct{ h client.HijackedResponse }

func (s hijackedStream) Read(p []byte) (int, error)  { return s.h.Reader.Read(p) }
func (s hijackedStream) Write(p []byte) (int, error) { return s.h.Conn.Write(p) }
func (s hijackedStream) CloseWrite() error           { return s.h.CloseWrite() }
func (s hijackedStream) Close() error                { s.h.Close(); return nil }

// execOutput is what was collected from the streams.
type execOutput struct {
	stdout    []byte
	stdoutCut bool
	stderr    []byte
	stderrCut bool
}

func (o execOutput) result(code int) ExecResult {
	stdout, stderr := o.stdout, o.stderr
	if stdout == nil {
		stdout = []byte{}
	}
	if stderr == nil {
		stderr = []byte{}
	}
	return ExecResult{
		ExitCode:        code,
		Stdout:          stdout,
		Stderr:          stderr,
		StdoutTruncated: o.stdoutCut,
		StderrTruncated: o.stderrCut,
		Truncated:       o.stdoutCut || o.stderrCut,
	}
}

func (o execOutput) timedOut(timeout time.Duration) ExecResult {
	r := o.result(-1)
	r.TimedOut = true
	r.Message = fmt.Sprintf("超时：%d 秒内没有结束，已不再等待。Docker 没有停掉这个进程的接口，它可能还在容器里运行，直到自己结束或容器停止", int(timeout/time.Second))
	return r
}

// execExcerpt is the start of what the command (or Docker about it) wrote,
// for an error message; the caller scrubs it.
func execExcerpt(o execOutput) string {
	s := strings.TrimSpace(string(o.stdout) + " " + string(o.stderr))
	if len(s) > 512 {
		s = s[:512]
	}
	if s == "" {
		return "Docker gave no reason"
	}
	return strings.ToValidUTF8(s, "?")
}

// collectExec writes stdin (then half-closes), reads stdout and stderr until
// the stream ends, the deadline passes (timedOut) or ctx ends, and closes the
// stream. Output beyond the bounds is drained and dropped.
func collectExec(ctx context.Context, s execStream, stdin []byte, deadline time.Time) (execOutput, bool, error) {
	stdout := &headBuffer{max: MaxExecOutputBytes}
	stderr := newTailBuffer(MaxExecOutputBytes)
	var once sync.Once
	closeStream := func() { once.Do(func() { _ = s.Close() }) }
	defer closeStream()

	wrote := make(chan struct{})
	go func() {
		defer close(wrote)
		if len(stdin) == 0 {
			return
		}
		// A command that exits without reading stdin makes this fail; that
		// is its business, not an error of the run.
		if _, err := s.Write(stdin); err == nil {
			_ = s.CloseWrite()
		}
	}()
	copied := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(stdout, stderr, s)
		copied <- err
	}()

	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	var err error
	timedOut := false
	select {
	case err = <-copied:
	case <-timer.C:
		timedOut = true
		closeStream()
		<-copied
	case <-ctx.Done():
		closeStream()
		<-copied
		err = ctx.Err()
	}
	closeStream()
	<-wrote
	out := execOutput{stdout: stdout.buf, stdoutCut: stdout.cut, stderr: stderr.Bytes(), stderrCut: stderr.Dropped() > 0}
	if timedOut {
		return out, true, nil
	}
	return out, false, err
}

// headBuffer keeps the first max bytes written and notes whether more came.
// It never fails, so the stream is drained.
type headBuffer struct {
	buf []byte
	max int
	cut bool
}

func (b *headBuffer) Write(p []byte) (int, error) {
	room := b.max - len(b.buf)
	switch {
	case len(p) <= room:
		b.buf = append(b.buf, p...)
	default:
		if room > 0 {
			b.buf = append(b.buf, p[:room]...)
		}
		b.cut = true
	}
	return len(p), nil
}

// ---- Client ----

// Exec implements ExecAPI over the socket. It waits TimeoutS plus
// execClientSlack. A runner without the route (older version) or whose API
// lacks the capability yields an error matching ErrUnsupported; a container
// that is not running yields ErrStopped.
func (c *Client) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	if err := ValidateExec(req); err != nil {
		return ExecResult{}, invalidExec(err)
	}
	ctx, cancel := context.WithTimeout(ctx, ExecTimeout(req.TimeoutS)+execClientSlack)
	defer cancel()
	var out ExecResult
	err := c.call(ctx, c.long, PathContainerExec, req, clientLargeBound, &out)
	var re *RemoteError
	if errors.As(err, &re) && re.Code == "container_stopped" {
		return ExecResult{}, ErrStopped
	}
	if err != nil {
		return ExecResult{}, unsupportedRoute(scrubExecError(err, req))
	}
	return out, nil
}

// ---- Server ----

// handleContainerExec never logs the request: Env and Stdin may be secret.
func (s *Server) handleContainerExec(w http.ResponseWriter, r *http.Request) {
	var req ExecRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed request body")
		return
	}
	if !validContainerName(w, req.App, req.Name) {
		return
	}
	if err := ValidateExec(req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	e, ok := s.api.(ExecAPI)
	if !ok {
		writeError(w, http.StatusNotImplemented, "unsupported", "this runner cannot run commands in containers")
		return
	}
	res, err := e.Exec(r.Context(), req)
	if err != nil {
		// Scrubbed again: another ExecAPI implementation may not have.
		err = scrubExecError(err, req)
		if errors.Is(err, ErrNotFound) { // writeAPIError matches only the bare sentinel
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeAPIError(w, err)
		return
	}
	if res.Stdout == nil {
		res.Stdout = []byte{}
	}
	if res.Stderr == nil {
		res.Stderr = []byte{}
	}
	writeJSON(w, http.StatusOK, res)
}

var (
	_ ExecAPI = (*Client)(nil)
	_ ExecAPI = (*Docker)(nil)
)
