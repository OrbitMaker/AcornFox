package appinstall

// Command steps (catalog.StepExec): a command run in the container of the
// app the group of steps belongs to, through the runner (runner.ExecAPI).
//
// Where it runs. Only in ActionEnv.Exec: the container of the app's current
// deployment, which the caller works out from the store (the provider of a
// registered service for issue, revoke and check; the app being installed
// for init). Nothing a manifest writes can name another container, and the
// runner checks again that it is a running app container of that app.
//
// How it is rendered. Each item of command becomes exactly one argument (no
// shell reads it): an item that holds a line break once its values are in
// is refused, and so is a command or a user that holds a secret value (the
// arguments of a process are visible to every user of the host; Docker's
// event log records them too). Lint refuses a secret written there; this is
// the check at run time, for values only known then. A secret goes in env or
// stdin, which may hold one.
//
// How it is judged. In this order: a command that did not end within its
// timeout fails (it is never run again; it may still be running in the
// container, which the platform cannot stop, and the error says so); output
// the runner had to cut (more than catalog.MaxExecOutput) fails, whatever
// the exit code; in revoke steps an exit code finish_if lists ends the group
// as a success; an exit code expect_exit does not list ([0] when not
// written) fails; then the values to save are taken from the standard output
// ({ stdout: true }: all of it, trimmed; the other forms read it as JSON).
//
// Sending again follows the rule of HTTP steps: only what never ran is sent
// again (the runner could not be reached, or the container was not running
// yet: restarting), attempts in all, retryWait apart. Once the runner has
// taken the command it is not sent again.
//
// What an error says: the step, the exit code and the end of what the
// command wrote to standard error, secrets masked. Never the stdin or the
// env, nor the standard output.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/runner"
)

// checkExec checks a command step before anything of the group is done:
// its bounds, its finish_if (revoke steps only), its saves, and that the
// runner can run it and there is a container to run it in.
func (s *actionSession) checkExec(n int, st catalog.Step) error {
	e := st.Exec
	bad := func(format string, a ...any) error {
		return s.failExec(n, st, ActionBadStep, fmt.Sprintf(format, a...))
	}
	switch {
	case len(e.Command) == 0:
		return bad("command 至少要写一项（要执行的程序）")
	case len(e.Command) > runner.MaxExecArgs:
		return bad("command 最多 %d 项，写了 %d 项", runner.MaxExecArgs, len(e.Command))
	case e.TimeoutS < 0 || e.TimeoutS > catalog.MaxExecTimeoutS:
		return bad("timeout_s 要在 1 到 %d 之间，写的是 %d", catalog.MaxExecTimeoutS, e.TimeoutS)
	}
	for _, code := range e.ExpectExit {
		if code < 0 || code > 255 {
			return bad("expect_exit 里的 %d 不是退出码（0～255）", code)
		}
	}
	if f := e.FinishIf; f != nil {
		if s.env.Group != GroupRevoke && s.env.Group != GroupPurge {
			return bad("finish_if 只能写在 revoke、purge 的步骤里（要收回、要删的东西已经没了算成功），这里用它会让一组步骤没做完就算成功")
		}
		if len(f.Exit) == 0 {
			return bad("finish_if 里要写 exit")
		}
		for _, code := range f.Exit {
			if code < 0 || code > 255 {
				return bad("finish_if.exit 里的 %d 不是退出码（0～255）", code)
			}
		}
	}
	for _, k := range sortedKeys(e.Save) {
		if msg := checkSave(e.Save[k]); msg != "" {
			return bad("save.%s：%s", k, msg)
		}
	}
	if s.r.exec == nil {
		return s.failExec(n, st, ActionRunnerOld, "")
	}
	if s.env.Exec.App == "" || s.env.Exec.Container == "" {
		return s.failExec(n, st, ActionNotRunning, "这个应用现在没有部署好的容器")
	}
	return nil
}

// failExec is fail for a command step.
func (s *actionSession) failExec(n int, st catalog.Step, kind, detail string) *ActionError {
	f := s.fail(n, st, kind, detail)
	f.Exec = true
	return f
}

// execStep runs one command step and judges what it did.
func (s *actionSession) execStep(ctx context.Context, n int, st catalog.Step) error {
	req, where, err := s.prepareExec(st.Exec)
	if err != nil {
		f := s.failExec(n, st, ActionTemplate, err.Error())
		f.err = err
		return f
	}
	res, ferr := s.sendExec(ctx, n, st, req)
	if ferr != nil {
		return ferr
	}
	return s.judgeExec(n, st, where, res)
}

// holdsSecret reports whether text holds a secret value of this group. A
// value shorter than 4 bytes counts only when it is the whole text.
func (s *actionSession) holdsSecret(text string) bool {
	for v := range s.raw {
		if text == v || (len(v) >= 4 && strings.Contains(text, v)) {
			return true
		}
	}
	return false
}

// prepareExec renders a command step into the runner's request, and the
// where of its list saves. Its errors name the place, never a value.
func (s *actionSession) prepareExec(e *catalog.StepExec) (runner.ExecRequest, map[string]map[string]any, error) {
	req := runner.ExecRequest{App: s.env.Exec.App, Name: s.env.Exec.Container, TimeoutS: e.Timeout()}
	for i, item := range e.Command {
		v, err := s.render(item, nil)
		if err != nil {
			return req, nil, err
		}
		if strings.ContainsAny(v, "\r\n\x00") {
			return req, nil, fmt.Errorf("command 的第 %d 项填进值以后有换行（或空字符），没有执行：一项只能是一个参数", i+1)
		}
		if s.holdsSecret(v) {
			return req, nil, fmt.Errorf("command 的第 %d 项填进值以后有保密值，没有执行：命令的参数在进程列表里谁都看得到，保密值只能经 env 或 stdin 传给命令", i+1)
		}
		req.Command = append(req.Command, v)
	}
	if strings.TrimSpace(req.Command[0]) == "" {
		return req, nil, errors.New("command 的第 1 项（要执行的程序）填进值以后是空的，没有执行")
	}
	if e.User != "" {
		v, err := s.render(e.User, nil)
		if err != nil {
			return req, nil, err
		}
		switch {
		case v == "" || !singleLine(v):
			return req, nil, errors.New("user 填进值以后是空的或者有换行，没有执行")
		case s.holdsSecret(v):
			return req, nil, errors.New("user 填进值以后有保密值，没有执行：保密值只能经 env 或 stdin 传给命令")
		}
		req.User = v
	}
	if len(e.Env) > 0 {
		req.Env = make(map[string]string, len(e.Env))
		for _, k := range e.EnvKeys() {
			v, err := s.render(e.Env[k], nil)
			if err != nil {
				return req, nil, err
			}
			req.Env[k] = v
		}
	}
	if e.Stdin != "" {
		v, err := s.render(e.Stdin, nil)
		if err != nil {
			return req, nil, err
		}
		req.Stdin = []byte(v)
	}
	where := map[string]map[string]any{}
	for _, k := range sortedKeys(e.Save) {
		w, err := s.renderMap(e.Save[k].Where)
		if err != nil {
			return req, nil, err
		}
		where[k] = w
	}
	return req, where, nil
}

// sendExec hands the command to the runner, again while it never ran (the
// runner could not be reached, the container was not running), attempts in
// all. Any other failure is final: the command may have run.
func (s *actionSession) sendExec(ctx context.Context, n int, st catalog.Step, req runner.ExecRequest) (runner.ExecResult, error) {
	var last error
	for attempt := 0; attempt < s.r.attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				f := s.failExec(n, st, ActionCanceled, "")
				f.err = ctx.Err()
				return runner.ExecResult{}, f
			case <-time.After(s.r.retryWait):
			}
		}
		res, err := s.r.exec.Exec(ctx, req)
		if err == nil {
			return res, nil
		}
		var re *runner.RemoteError
		switch {
		case errors.Is(err, runner.ErrUnsupported):
			return res, s.failExec(n, st, ActionRunnerOld, "")
		case errors.Is(err, runner.ErrUnavailable), errors.Is(err, runner.ErrStopped):
			last = err
			continue
		case errors.Is(err, runner.ErrNotFound):
			return res, s.failExec(n, st, ActionNotRunning, "这个应用当前部署的容器不在了")
		case ctx.Err() != nil:
			f := s.failExec(n, st, ActionCanceled, "")
			f.err = ctx.Err()
			return res, f
		case errors.As(err, &re) && re.Code == runner.CodeExecFailed:
			return res, s.failExec(n, st, ActionExecFailed, re.Message)
		case errors.As(err, &re) && re.Code == "invalid_request":
			return res, s.failExec(n, st, ActionBadStep, "运行器不接受这条命令："+re.Message)
		}
		return res, s.failExec(n, st, ActionNoAnswer, err.Error())
	}
	if errors.Is(last, runner.ErrStopped) {
		return runner.ExecResult{}, s.failExec(n, st, ActionNotRunning, "等了一会儿还是没在运行")
	}
	return runner.ExecResult{}, s.failExec(n, st, ActionNotSent, fmt.Sprintf("试了 %d 次都没连上", s.r.attempts))
}

// judgeExec judges what a command did (see the top of this file) and saves
// from its standard output.
func (s *actionSession) judgeExec(n int, st catalog.Step, where map[string]map[string]any, res runner.ExecResult) error {
	e := st.Exec
	var body, exact any
	isJSON := json.Unmarshal(res.Stdout, &body) == nil
	if isJSON {
		dec := json.NewDecoder(bytes.NewReader(res.Stdout))
		dec.UseNumber()
		if dec.Decode(&exact) != nil {
			exact = nil
		}
	} else {
		body = nil
	}
	// Whatever the output holds at a secret save's place is hidden from here
	// on, also when the step fails before it is saved.
	for _, sv := range e.Save {
		switch {
		case !sv.Secret:
		case sv.FromStdout():
			s.hide(strings.TrimSpace(string(res.Stdout)))
		case isJSON:
			s.hideCandidates(sv, exact)
		}
	}
	output := ""
	if len(res.Stderr) > 0 {
		output = quoteTail(s.mask(string(res.Stderr)))
	}
	failed := func(kind, detail string) *ActionError {
		f := s.failExec(n, st, kind, detail)
		f.ExitCode, f.Output = res.ExitCode, output
		return f
	}
	switch {
	case res.TimedOut:
		return failed(ActionTimeout, strconv.Itoa(e.Timeout())+" 秒")
	case res.Truncated || len(res.Stdout)+len(res.Stderr) > catalog.MaxExecOutput:
		return failed(ActionTooLarge, fmt.Sprintf("标准输出和标准错误合起来超过了 %d KiB，多出来的被截掉了", catalog.MaxExecOutput>>10))
	}
	if f := e.FinishIf; f != nil && (s.env.Group == GroupRevoke || s.env.Group == GroupPurge) {
		for _, c := range f.Exit {
			if c == res.ExitCode {
				// What the group is there for is done already: over, as a
				// success; nothing is saved from it.
				s.finished = true
				return nil
			}
		}
	}
	if !e.ExitOK(res.ExitCode) {
		return failed(ActionExit, "算通过的是 "+exitList(e.ExpectExit))
	}
	if len(e.Save) == 0 {
		return nil
	}
	// All of the step's values or none.
	got := map[string]SavedValue{}
	for _, k := range sortedKeys(e.Save) {
		sv := e.Save[k]
		var v string
		switch {
		case sv.FromStdout():
			v = strings.TrimSpace(string(res.Stdout))
			if v == "" {
				return failed(ActionSaveFailed, fmt.Sprintf("存 %s 时，命令的标准输出是空的", k))
			}
		case !isJSON:
			return failed(ActionSaveFailed, fmt.Sprintf("存 %s 时，命令的标准输出不是 JSON，取不到要存的值", k))
		default:
			var err error
			v, err = pickSaved(sv, where[k], body, exact)
			if err != nil {
				return failed(ActionSaveFailed, fmt.Sprintf("存 %s 时%s", k, strings.ReplaceAll(err.Error(), "应答", "标准输出")))
			}
		}
		if sv.Secret {
			s.hide(v)
		}
		if sv.Prefix != "" && !strings.HasPrefix(v, sv.Prefix) {
			v = sv.Prefix + v
		}
		if sv.Secret {
			s.hide(v)
		}
		got[k] = SavedValue{Value: v, Secret: sv.Secret}
	}
	for k, v := range got {
		s.saved[k] = v
	}
	return nil
}

// exitList is the exit codes that pass, as said to people.
func exitList(codes []int) string {
	if len(codes) == 0 {
		return "0"
	}
	parts := make([]string, len(codes))
	for i, c := range codes {
		parts[i] = strconv.Itoa(c)
	}
	return strings.Join(parts, "、")
}

// stepsRunCommands reports whether any of the groups has a command step.
func stepsRunCommands(groups ...[]catalog.Step) bool {
	for _, steps := range groups {
		for _, st := range steps {
			if st.IsExec() {
				return true
			}
		}
	}
	return false
}

// stepsSendRequests reports whether any of the groups has an HTTP step.
func stepsSendRequests(groups ...[]catalog.Step) bool {
	for _, steps := range groups {
		for _, st := range steps {
			if !st.IsExec() {
				return true
			}
		}
	}
	return false
}

// manifestRunsCommands reports whether an app's manifest has command steps
// the platform runs: in its init, or in what it provides.
func manifestRunsCommands(m *catalog.Manifest) bool {
	if stepsRunCommands(initActionSteps(m)) {
		return true
	}
	for _, p := range m.Provides {
		if stepsRunCommands(p.Issue, p.Revoke, p.Purge, p.Check, p.ReadySteps()) {
			return true
		}
	}
	return false
}
