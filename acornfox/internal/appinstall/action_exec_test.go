package appinstall

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/runner"
)

// Command steps (action_exec.go), against a fake runner. The whole flow
// with the database-shaped test material is in exec_flow_test.go.

// execRunner is a runner's ExecAPI: it records every request, checks it as
// the real runner does (runner.ValidateExec) and answers with do (exit 0, no
// output, when nil).
type execRunner struct {
	mu   sync.Mutex
	reqs []runner.ExecRequest
	do   func(req runner.ExecRequest) (runner.ExecResult, error)
}

func (f *execRunner) Exec(ctx context.Context, req runner.ExecRequest) (runner.ExecResult, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	do := f.do
	f.mu.Unlock()
	if err := runner.ValidateExec(req); err != nil {
		return runner.ExecResult{}, &runner.RemoteError{Status: 400, ErrorResponse: runner.ErrorResponse{Code: "invalid_request", Message: err.Error()}}
	}
	if do == nil {
		return runner.ExecResult{Stdout: []byte{}, Stderr: []byte{}}, nil
	}
	return do(req)
}

func (f *execRunner) requests() []runner.ExecRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]runner.ExecRequest(nil), f.reqs...)
}

// execWorld is the fake world whose runner also runs commands.
type execWorld struct {
	*fakeWorld
	*execRunner
}

var _ runner.ExecAPI = (*execWorld)(nil)

const execTestDeployment = "0123456789ab"

// execActions is testActions with a runner that runs commands.
func execActions(f *execRunner) actionRunner {
	r := testActions(http.DefaultClient)
	r.exec = f
	return r
}

// execEnv runs a group's commands in the demo-sql app's container, with an
// admin password as a secret setting.
func execEnv(group, adminPassword string) ActionEnv {
	env := adminEnv(adminPassword)
	env.Group = group
	env.Exec = ExecTarget{App: "demo-sql", Container: runner.ContainerName("demo-sql", execTestDeployment)}
	return env
}

func ok(stdout string) (runner.ExecResult, error) {
	return runner.ExecResult{ExitCode: 0, Stdout: []byte(stdout), Stderr: []byte{}}, nil
}

// A command step is rendered item by item (one argument each, spaces and
// all), its env and stdin may hold secrets, it runs in the target container
// only, and what it prints is saved: all of it, or read as JSON.
func TestExecStepRuns(t *testing.T) {
	const admin = "admin-pass-0001"
	f := &execRunner{do: func(req runner.ExecRequest) (runner.ExecResult, error) {
		if req.Command[1] == "--describe" {
			return ok(`{"data": {"oid": 16384, "items": [{"name": "a", "id": 1}, {"name": "b", "id": 2}]}, "secret": "s3cr3t-value"}` + "\n")
		}
		return ok("  line one\n")
	}}
	steps := stepsYAML(t, `
- name: create
  exec:
    user: "{{params.admin_username}}"
    command: [demo-sql, "--comment", "made for {{consumer.name}}", "{{issue.id}}"]
    env: { DEMO_SQL_PASSWORD: "{{params.admin_password}}", DB: "{{issue.database}}" }
    stdin: |
      CREATE ROLE "{{issue.username}}" PASSWORD '{{issue.password}}';
    timeout_s: 60
    save:
      line: { stdout: true, prefix: "L:" }
- name: describe
  exec:
    command: [demo-sql, --describe]
    save:
      oid: data.oid
      b_id: { list: data.items, where: { name: b }, field: id }
      token: { path: secret, secret: true }
`)
	env := execEnv(GroupIssue, admin)
	env.Consumer = &ActionConsumer{Name: "web app", ID: "web app"}
	env.IssueID = "1a2b3c4d"
	env.IssueVars = map[string]SavedValue{"database": {Value: "af_1a2b3c4d"}, "username": {Value: "af_1a2b3c4d"}, "password": {Value: "generated-pass-0002", Secret: true}}
	saved, err := execActions(f).run(context.Background(), "", steps, env)
	if err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) != 2 {
		t.Fatalf("%d requests", len(reqs))
	}
	r := reqs[0]
	if r.App != "demo-sql" || r.Name != "af-demo-sql-"+execTestDeployment || r.User != "admin" || r.TimeoutS != 60 {
		t.Fatalf("request %v", r)
	}
	if strings.Join(r.Command, "|") != "demo-sql|--comment|made for web app|1a2b3c4d" {
		t.Fatalf("command %q", r.Command)
	}
	if r.Env["DEMO_SQL_PASSWORD"] != admin || r.Env["DB"] != "af_1a2b3c4d" || string(r.Stdin) != "CREATE ROLE \"af_1a2b3c4d\" PASSWORD 'generated-pass-0002';\n" {
		t.Fatalf("env %v stdin %q", r.Env, r.Stdin)
	}
	if reqs[1].TimeoutS != catalog.DefaultExecTimeoutS || reqs[1].Env != nil || reqs[1].Stdin != nil {
		t.Fatalf("defaults %v", reqs[1])
	}
	want := map[string]SavedValue{"line": {Value: "L:line one"}, "oid": {Value: "16384"}, "b_id": {Value: "2"}, "token": {Value: "s3cr3t-value", Secret: true}}
	if len(saved) != len(want) {
		t.Fatalf("saved %v", saved)
	}
	for k, v := range want {
		if saved[k] != v {
			t.Errorf("saved %s = %+v, want %+v", k, saved[k], v)
		}
	}
}

// What only steps have is checked for command steps too before anything
// runs: an issue variable the entry does not have, issue.* outside issue, a
// save that cannot be read, finish_if outside revoke, bounds.
func TestExecPrecheck(t *testing.T) {
	f := &execRunner{}
	issueEnv := func() ActionEnv {
		e := execEnv(GroupIssue, "admin-pass-0001")
		e.Consumer, e.IssueID = &ActionConsumer{Name: "web", ID: "web"}, "1a2b3c4d"
		e.IssueVars = map[string]SavedValue{"username": {Value: "af_1"}}
		return e
	}
	first := "- name: first\n  exec: { command: [\"true\"] }\n"
	for name, tc := range map[string]struct {
		steps  string
		env    ActionEnv
		detail string
	}{
		"unknown issue var": {first + "- exec: { command: [x, \"{{issue.password}}\"] }\n", issueEnv(), "issue 只有 id 和 issue_vars 里的键（username），没有 issue.password"},
		"issue var in revoke": {"- exec: { command: [x], stdin: \"{{issue.username}}\" }\n", execEnv(GroupRevoke, "p-0001"),
			"引用了 issue.username，但这里不是发凭据的步骤（只有 issue 的步骤里有；revoke、check 里用 issued.<键>）"},
		"saved later":       {"- exec: { command: [x], env: { A: \"{{saved.k}}\" } }\n- exec: { command: [y], save: { k: { stdout: true } } }\n", execEnv(GroupInit, "p-0001"), "引用了 saved.k，但前面的步骤没有存过它"},
		"stdout and path":   {"- exec: { command: [x], save: { k: { stdout: true, path: a } } }\n", execEnv(GroupInit, "p-0001"), "save.k：stdout 取的是整段标准输出，不能再写 path、list、where、sort_by、pick、field"},
		"finish_if in init": {"- exec: { command: [x], finish_if: { exit: [3] } }\n", execEnv(GroupInit, "p-0001"), "finish_if 只能写在 revoke、purge 的步骤里（要收回、要删的东西已经没了算成功），这里用它会让一组步骤没做完就算成功"},
		"finish_if in check": {"- exec: { command: [x], finish_if: { exit: [3] } }\n", execEnv(GroupCheck, "p-0001"),
			"finish_if 只能写在 revoke、purge 的步骤里（要收回、要删的东西已经没了算成功），这里用它会让一组步骤没做完就算成功"},
		"timeout":    {"- exec: { command: [x], timeout_s: 301 }\n", execEnv(GroupInit, "p-0001"), "timeout_s 要在 1 到 300 之间，写的是 301"},
		"exit code":  {"- exec: { command: [x], expect_exit: [0, 256] }\n", execEnv(GroupInit, "p-0001"), "expect_exit 里的 256 不是退出码（0～255）"},
		"no command": {"- name: empty\n  exec: { user: demo }\n", execEnv(GroupInit, "p-0001"), "command 至少要写一项（要执行的程序）"},
		"stdout save in http": {"- http: { method: GET, path: /, save: { k: { stdout: true } } }\n", execEnv(GroupInit, "p-0001"),
			"save.k：stdout 只能用在 exec 步骤里（取命令的整段标准输出），http 步骤从应答里用 path 或 list 取"},
	} {
		_, err := execActions(f).run(context.Background(), "http://127.0.0.1:1", stepsYAML(t, tc.steps), tc.env)
		ae := asActionError(t, err)
		if ae.Kind != ActionBadStep || ae.Detail != tc.detail {
			t.Errorf("%s: %+v", name, ae)
		}
	}
	if n := len(f.requests()); n != 0 {
		t.Fatalf("%d commands ran before a refusal", n)
	}
	// No container to run in: nothing runs.
	env := execEnv(GroupInit, "p-0001")
	env.Exec = ExecTarget{}
	_, err := execActions(f).run(context.Background(), "", stepsYAML(t, first), env)
	if ae := asActionError(t, err); ae.Kind != ActionNotRunning || !ae.Exec || len(f.requests()) != 0 {
		t.Fatalf("%+v", ae)
	}
}

// Each item of command is one argument: an item that holds a line break once
// its values are in is refused, and so is a command or a user that holds a
// secret value (a setting, an issued value, an issue variable, a saved one).
// env and stdin may hold one. Nothing of a refused step runs, and the
// refusal holds no secret.
func TestExecRenderRefusals(t *testing.T) {
	const admin = "admin-pass-0001"
	f := &execRunner{do: func(req runner.ExecRequest) (runner.ExecResult, error) { return ok("saved-secret-0003\n") }}
	base := func(group string) ActionEnv {
		e := execEnv(group, admin)
		e.Issued = map[string]SavedValue{"user": {Value: "line1\nline2"}, "password": {Value: "issued-pass-0004", Secret: true}}
		return e
	}
	for name, tc := range map[string]struct {
		steps  string
		detail string
		ran    int
	}{
		"line break":     {"- exec: { command: [x, \"{{issued.user}}\"] }\n", "command 的第 2 项填进值以后有换行（或空字符），没有执行：一项只能是一个参数", 0},
		"secret setting": {"- exec: { command: [x, \"--password={{params.admin_password}}\"] }\n", "command 的第 2 项填进值以后有保密值，没有执行：命令的参数在进程列表里谁都看得到，保密值只能经 env 或 stdin 传给命令", 0},
		"secret issued":  {"- exec: { command: [x, \"{{issued.password}}\"] }\n", "command 的第 2 项填进值以后有保密值，没有执行：命令的参数在进程列表里谁都看得到，保密值只能经 env 或 stdin 传给命令", 0},
		"secret in user": {"- exec: { command: [x], user: \"{{params.admin_password}}\" }\n", "user 填进值以后有保密值，没有执行：保密值只能经 env 或 stdin 传给命令", 0},
		"line in user":   {"- exec: { command: [x], user: \"{{issued.user}}\" }\n", "user 填进值以后是空的或者有换行，没有执行", 0},
		"secret saved": {"- exec: { command: [x], save: { s: { stdout: true, secret: true } } }\n- exec: { command: [y, \"{{saved.s}}\"] }\n",
			"command 的第 2 项填进值以后有保密值，没有执行：命令的参数在进程列表里谁都看得到，保密值只能经 env 或 stdin 传给命令", 1},
	} {
		f.mu.Lock()
		f.reqs = nil
		f.mu.Unlock()
		_, err := execActions(f).run(context.Background(), "", stepsYAML(t, tc.steps), base(GroupRevoke))
		ae := asActionError(t, err)
		if ae.Kind != ActionTemplate || ae.Detail != tc.detail || len(f.requests()) != tc.ran {
			t.Errorf("%s: %+v after %d commands", name, ae, len(f.requests()))
		}
		for _, secret := range []string{admin, "issued-pass-0004", "saved-secret-0003"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s: the refusal holds a secret: %v", name, err)
			}
		}
	}
	// env and stdin carry them.
	f.mu.Lock()
	f.reqs = nil
	f.mu.Unlock()
	steps := stepsYAML(t, "- exec: { command: [x], env: { P: \"{{params.admin_password}}\", Q: \"{{issued.password}}\" }, stdin: \"{{issued.password}}\" }\n")
	if _, err := execActions(f).run(context.Background(), "", steps, base(GroupRevoke)); err != nil {
		t.Fatal(err)
	}
	if r := f.requests()[0]; r.Env["P"] != admin || r.Env["Q"] != "issued-pass-0004" || string(r.Stdin) != "issued-pass-0004" {
		t.Fatalf("%v", r)
	}
}

// How a command is judged: an exit code expect_exit does not list fails
// (with the code and the end of stderr, masked; never stdin or env); one it
// lists passes; cut output fails whatever the code; a time-out fails and
// says the command may still run, and it is not run again.
func TestExecJudging(t *testing.T) {
	const admin = "admin-pass-0001"
	var res runner.ExecResult
	f := &execRunner{do: func(req runner.ExecRequest) (runner.ExecResult, error) { return res, nil }}
	steps := func(extra string) []catalog.Step {
		return stepsYAML(t, "- name: grant\n  exec: { command: [demo-sql], env: { P: \"{{params.admin_password}}\" }, stdin: \"GRANT stdin-text-0005\""+extra+" }\n")
	}
	run := func(extra string) (*ActionError, int) {
		f.mu.Lock()
		f.reqs = nil
		f.mu.Unlock()
		_, err := execActions(f).run(context.Background(), "", steps(extra), execEnv(GroupInit, admin))
		if err == nil {
			return nil, len(f.requests())
		}
		return asActionError(t, err), len(f.requests())
	}

	long := strings.Repeat("x", 400)
	res = runner.ExecResult{ExitCode: 1, Stdout: []byte("out-0006"), Stderr: []byte(long + "\nERROR: password " + admin + " is wrong\n")}
	ae, n := run("")
	if ae == nil || ae.Kind != ActionExit || ae.ExitCode != 1 || n != 1 || !ae.Answered() || !ae.Exec {
		t.Fatalf("%+v after %d", ae, n)
	}
	msg := ae.Error()
	if !strings.HasPrefix(msg, "第 1 步（grant）：命令的退出码是 1（算通过的是 0），标准错误的最后：…") || !strings.HasSuffix(msg, "ERROR: password *** is wrong") {
		t.Fatalf("message %q", msg)
	}
	for _, leak := range []string{admin, "stdin-text-0005", "out-0006"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("the error holds %q: %s", leak, msg)
		}
	}
	if len([]rune(ae.Output)) > maxQuoted+1 {
		t.Fatalf("output not cut: %d", len([]rune(ae.Output)))
	}
	if ae, _ := run(", expect_exit: [0, 1]"); ae != nil {
		t.Fatalf("exit 1 listed: %+v", ae)
	}

	res = runner.ExecResult{ExitCode: 0, Stdout: []byte("partial"), Truncated: true, StdoutTruncated: true}
	if ae, _ := run(""); ae == nil || ae.Kind != ActionTooLarge || !strings.Contains(ae.Error(), "64 KiB") {
		t.Fatalf("truncated: %+v", ae)
	}
	res = runner.ExecResult{ExitCode: 0, Stdout: make([]byte, 40<<10), Stderr: make([]byte, 30<<10)}
	if ae, _ := run(""); ae == nil || ae.Kind != ActionTooLarge {
		t.Fatalf("over the bound together: %+v", ae)
	}

	res = runner.ExecResult{ExitCode: -1, TimedOut: true, Stderr: []byte("waiting for lock\n"), Message: "the command did not end"}
	ae, n = run(", timeout_s: 5")
	if ae == nil || ae.Kind != ActionTimeout || n != 1 || ae.Answered() {
		t.Fatalf("%+v after %d", ae, n)
	}
	if msg := ae.Error(); !strings.Contains(msg, "命令在 5 秒内没有结束") || !strings.Contains(msg, "可能还在应用的容器里运行") || !strings.Contains(msg, "没有重新执行") || !strings.Contains(msg, "waiting for lock") {
		t.Fatalf("message %q", msg)
	}
	if f := initFault(ae); f.Code != CodeInit || !strings.Contains(f.Message, "可能还在应用的容器里运行") {
		t.Fatalf("%+v", f)
	}
}

// Saving from the output: an empty output, an output that is no JSON, a
// path that is not in it.
func TestExecSaveFailures(t *testing.T) {
	var out string
	f := &execRunner{do: func(req runner.ExecRequest) (runner.ExecResult, error) { return ok(out) }}
	for _, tc := range []struct{ out, save, detail string }{
		{"  \n", "{ stdout: true }", "存 k 时，命令的标准输出是空的"},
		{"not json", "data.id", "存 k 时，命令的标准输出不是 JSON，取不到要存的值"},
		{`{"data": {}}`, "data.id", "存 k 时，标准输出里没有 data.id"},
	} {
		out = tc.out
		_, err := execActions(f).run(context.Background(), "", stepsYAML(t, "- exec: { command: [x], save: { k: "+tc.save+" } }\n"), execEnv(GroupInit, "p-0001"))
		ae := asActionError(t, err)
		if ae.Kind != ActionSaveFailed || ae.Detail != tc.detail || !strings.Contains(ae.Error(), "（命令的退出码 0）") {
			t.Errorf("%q: %+v / %s", tc.out, ae, ae.Error())
		}
	}
}

// finish_if counts in revoke steps only: an exit code it lists ends the
// group as a success, the steps after it are not run.
func TestExecFinishIfInRevoke(t *testing.T) {
	f := &execRunner{do: func(req runner.ExecRequest) (runner.ExecResult, error) {
		return runner.ExecResult{ExitCode: 3, Stderr: []byte("no such role")}, nil
	}}
	steps := stepsYAML(t, "- name: lock\n  exec: { command: [lock], finish_if: { exit: [3] } }\n- name: drop\n  exec: { command: [drop] }\n")
	if _, err := execActions(f).run(context.Background(), "", steps, execEnv(GroupRevoke, "p-0001")); err != nil {
		t.Fatal(err)
	}
	if reqs := f.requests(); len(reqs) != 1 || reqs[0].Command[0] != "lock" {
		t.Fatalf("ran %v", reqs)
	}
	// Exit 3 without finish_if fails.
	steps = stepsYAML(t, "- name: lock\n  exec: { command: [lock] }\n")
	_, err := execActions(f).run(context.Background(), "", steps, execEnv(GroupRevoke, "p-0001"))
	if ae := asActionError(t, err); ae.Kind != ActionExit || ae.ExitCode != 3 {
		t.Fatalf("%+v", ae)
	}
}

// What the runner says. Only what never ran is sent again (the runner could
// not be reached, the container was not running yet): attempts in all.
// Anything else is final, once.
func TestExecRunnerErrors(t *testing.T) {
	steps := stepsYAML(t, "- name: grant\n  exec: { command: [x], env: { P: \"{{params.admin_password}}\" } }\n")
	for name, tc := range map[string]struct {
		errs  []error
		kind  string
		calls int
	}{
		"too old":           {[]error{errors.Join(runner.ErrUnsupported, errors.New("runner 501 unsupported: x"))}, ActionRunnerOld, 1},
		"not found":         {[]error{runner.ErrNotFound}, ActionNotRunning, 1},
		"stopped":           {[]error{runner.ErrStopped, runner.ErrStopped, runner.ErrStopped, runner.ErrStopped, runner.ErrStopped}, ActionNotRunning, 5},
		"stopped then runs": {[]error{runner.ErrStopped, nil}, "", 2},
		"unavailable":       {[]error{runner.ErrUnavailable, runner.ErrUnavailable, runner.ErrUnavailable, runner.ErrUnavailable, runner.ErrUnavailable}, ActionNotSent, 5},
		"could not start": {[]error{&runner.RemoteError{Status: 502, ErrorResponse: runner.ErrorResponse{Code: runner.CodeExecFailed, Message: "command could not be started in the container: no such file"}}},
			ActionExecFailed, 1},
		"docker error": {[]error{&runner.RemoteError{Status: 500, ErrorResponse: runner.ErrorResponse{Code: "docker_error", Message: "boom"}}}, ActionNoAnswer, 1},
	} {
		var calls atomic.Int32
		f := &execRunner{do: func(req runner.ExecRequest) (runner.ExecResult, error) {
			i := int(calls.Add(1)) - 1
			if i < len(tc.errs) && tc.errs[i] != nil {
				return runner.ExecResult{}, tc.errs[i]
			}
			return ok("")
		}}
		_, err := execActions(f).run(context.Background(), "", steps, execEnv(GroupInit, "admin-pass-0001"))
		if int(calls.Load()) != tc.calls {
			t.Errorf("%s: %d calls", name, calls.Load())
		}
		if tc.kind == "" {
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		if ae := asActionError(t, err); ae.Kind != tc.kind || !ae.Exec {
			t.Errorf("%s: %+v", name, ae)
		}
	}
	// The runner refusing the request (a bad env name) is the step's fault.
	f := &execRunner{}
	_, err := execActions(f).run(context.Background(), "", stepsYAML(t, "- exec: { command: [x], env: { \"1BAD\": v } }\n"), execEnv(GroupInit, "p-0001"))
	if ae := asActionError(t, err); ae.Kind != ActionBadStep || !strings.HasPrefix(ae.Detail, "运行器不接受这条命令") {
		t.Fatalf("%+v", ae)
	}
	if f := initFault(err); f.Code != CodeInit {
		t.Fatalf("%+v", f)
	}
}

// A command step's "could not reach" and "no result" speak of the runner
// and the command, not of the app and a request; NothingDone is set only
// when no step reached the container.
func TestExecRunnerWordsAndNothingDone(t *testing.T) {
	steps := stepsYAML(t, "- name: grant\n  exec: { command: [x] }\n- name: after\n  exec: { command: [y] }\n")
	run := func(do func(runner.ExecRequest) (runner.ExecResult, error)) *ActionError {
		_, err := execActions(&execRunner{do: do}).run(context.Background(), "", steps, execEnv(GroupIssue, "p-0001"))
		return asActionError(t, err)
	}
	unavailable := run(func(runner.ExecRequest) (runner.ExecResult, error) { return runner.ExecResult{}, runner.ErrUnavailable })
	if unavailable.Kind != ActionNotSent || !unavailable.NothingDone() {
		t.Fatalf("%+v", unavailable)
	}
	for _, text := range []string{unavailable.Error(), initFault(unavailable).Message} {
		if !strings.Contains(text, "连不上运行器（runner），命令没有执行") || strings.Contains(text, "连不上应用") {
			t.Fatalf("%q", text)
		}
	}
	noResult := run(func(runner.ExecRequest) (runner.ExecResult, error) {
		return runner.ExecResult{}, &runner.RemoteError{Status: 500, ErrorResponse: runner.ErrorResponse{Code: "docker_error", Message: "boom"}}
	})
	if noResult.Kind != ActionNoAnswer || noResult.NothingDone() {
		t.Fatalf("%+v", noResult)
	}
	for _, text := range []string{noResult.Error(), initFault(noResult).Message} {
		if !strings.Contains(text, "命令交给运行器（runner）以后没有收到结果") || !strings.Contains(text, "它可能已经执行了") || strings.Contains(text, "请求") {
			t.Fatalf("%q", text)
		}
	}
	// The second step not running: the first one ran, so something may be
	// there to undo.
	second := run(func(req runner.ExecRequest) (runner.ExecResult, error) {
		if req.Command[0] == "y" {
			return runner.ExecResult{}, runner.ErrNotFound
		}
		return ok("")
	})
	if second.Kind != ActionNotRunning || second.Step != 2 || second.NothingDone() {
		t.Fatalf("%+v", second)
	}
	// Refused before sending (no container to run in).
	env := execEnv(GroupIssue, "p-0001")
	env.Exec = ExecTarget{}
	_, err := execActions(&execRunner{}).run(context.Background(), "", steps, env)
	if ae := asActionError(t, err); ae.Kind != ActionNotRunning || !ae.NothingDone() {
		t.Fatalf("%+v", ae)
	}
}

// HTTP and command steps in one group share what they save; a group of
// command steps only needs no address.
func TestExecAndHTTPInOneGroup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"id": r.URL.Query().Get("v")})
	}))
	defer srv.Close()
	f := &execRunner{do: func(req runner.ExecRequest) (runner.ExecResult, error) { return ok(`{"v": "from-exec"}`) }}
	steps := stepsYAML(t, "- exec: { command: [x], save: { v: v } }\n- http: { method: GET, path: \"/?v={{saved.v}}\", save: { id: id } }\n")
	r := execActions(f)
	r.http = srv.Client()
	saved, err := r.run(context.Background(), srv.URL, steps, execEnv(GroupInit, "p-0001"))
	if err != nil || saved["id"].Value != "from-exec" {
		t.Fatalf("%v %v", saved, err)
	}
	// Commands only: no address needed; an HTTP step needs one.
	if _, err := r.run(context.Background(), "", steps[:1], execEnv(GroupInit, "p-0001")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.run(context.Background(), "", steps, execEnv(GroupInit, "p-0001")); err == nil || asActionError(t, err).Detail != "应用入口的地址不对" {
		t.Fatalf("%v", err)
	}
}

// The keys a credential keeps (credentialKeys) and which are secret
// (issuedSecretKeys) count command steps, issue_vars and value templates.
func TestCredentialKeysWithCommands(t *testing.T) {
	m, err := catalog.ParseManifest([]byte(sqlManifest(t, "demo-sql")))
	if err != nil {
		t.Fatal(err)
	}
	pv := m.Provides[0]
	keep := credentialKeys(pv)
	for _, k := range []string{"database", "username", "password", "database_oid", catalog.IssueIDKey} {
		if !keep[k] {
			t.Errorf("%s not kept", k)
		}
	}
	if keep["server_line"] {
		t.Error("server_line kept: nothing reads it after the issue")
	}
	secret := issuedSecretKeys(pv, m, catalog.ServiceType{})
	if !secret["password"] || secret["username"] || secret["database"] {
		t.Fatalf("secret %v", secret)
	}
	if !hasIssuedKeys(pv, map[string]string{"database": "d", "username": "u", "password": "p"}) || hasIssuedKeys(pv, map[string]string{"database": "d", "password": "p"}) {
		t.Fatal("hasIssuedKeys")
	}
}
