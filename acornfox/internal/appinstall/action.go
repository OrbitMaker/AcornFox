package appinstall

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	neturl "net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/runner"
)

// Actions are the steps a manifest asks the platform to do on an app that is
// up: its init steps, and the steps that issue, revoke and check what it
// provides to other apps. The steps are the manifest's own (catalog.Step):
// one HTTP request to the app's own endpoint, or a command run in the app's
// own container (action_exec.go). The platform knows nothing of the app: it
// does what the steps say.
//
// The steps of one group share a session: one cookie jar, and the values
// earlier steps saved from the app's answers (or from a command's standard
// output). Templates in a step's path, headers, json, expect_json and
// save.where, and in a command step's command, user, env values and stdin,
// may reference
//
//	params.<key>, platform.<name>   the app's settings, as everywhere else
//	uses.<need>.<item>              in init: what a need is plugged into
//	saved.<key>                     saved by an earlier step of this group
//	issued.<key>                    in revoke, purge and check: the credential
//	self.host, self.port, ...       in a provided service's steps (not issue):
//	                                the provider on the app network
//	consumer.name, consumer.id      the app the service is being connected to
//	issue.id                        in issue: this issue's number (below)
//	issue.<key>                     in issue: a value of the entry's issue_vars
//
// Every value is text: a number or a boolean taken from an answer is kept
// as its text, and a template always renders to a string. Where such text is
// compared with a number of an answer (expect_json, save.where) it is equal
// when it is that number, whatever its size.
//
// Sending again. A request that could not connect (a command the runner
// could not be reached for, or whose container was not running yet) is sent
// again (attempts in all, retryWait apart); one that was sent is not, it may
// have taken effect. Besides, a GET step that writes retry is sent again
// when its answer does not pass (status, expect_json) or a value to save is
// not in it yet: at most retry.times more requests, retry.delay_s seconds
// apart, each of them with the connection rule above. A command step has no
// retry: once started it is never run again.
//
// How a caller runs a provided service's steps (catalog.Provide):
//
//   - issue: draw a number with newIssueID, work out the entry's issue_vars
//     (providerConn.issueVars), run the Issue steps with ActionEnv{IssueID:
//     id, IssueVars: …, Consumer: …}, and keep credentialOf(saved, vars, id):
//     the issue_vars, what the steps saved, and the number under
//     catalog.IssueIDKey. That map, cut down to the keys something reads
//     later (credentialKeys), is the credential handed to the consumer,
//     stored encrypted.
//   - revoke and check: pass the stored credential as ActionEnv.Issued (and
//     the same Consumer). {{issued.issue_id}} then reads the number, like
//     any key the issue steps saved. IssueID stays empty there, so a stray
//     {{issue.id}} is refused before anything is sent.
//   - init: neither Issued, nor Consumer, nor IssueID.
//
// A command step runs in the container of the app the group belongs to and
// nowhere else (ActionEnv.Exec): the caller works it out from the store, as
// runner.ContainerName(app, its current deployment), the app being the
// provider of a registered service or the app being installed itself.

// newIssueID draws the number of one issue: 8 random hex digits. It names
// what the issue steps make in the app ({{issue.id}}), so two issues never
// make things of the same name.
func newIssueID() string {
	b := make([]byte, 4)
	// crypto/rand.Read does not fail (it crashes the program if the system's
	// randomness is not available).
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// credential is the credential of one issue: what the issue steps saved and
// the issue number under catalog.IssueIDKey (the platform's, whatever a step
// saved under that key; lint refuses such a step).
func credential(saved map[string]SavedValue, issueID string) map[string]SavedValue {
	return credentialOf(saved, nil, issueID)
}

// credentialOf is credential with the entry's issue_vars too: they go in
// first, so a key a step saved wins (lint refuses a step that saves under
// the key of an issue variable).
func credentialOf(saved, vars map[string]SavedValue, issueID string) map[string]SavedValue {
	out := make(map[string]SavedValue, len(saved)+len(vars)+1)
	for k, v := range vars {
		out[k] = v
	}
	for k, v := range saved {
		out[k] = v
	}
	out[catalog.IssueIDKey] = SavedValue{Value: issueID}
	return out
}

// SavedValue is a value a step saved.
type SavedValue struct {
	Value  string `json:"value"`
	Secret bool   `json:"secret,omitempty"`
}

// ActionConsumer is the app a service is being connected to.
type ActionConsumer struct {
	Name string
	ID   string
}

// ExecTarget is the container a group's command steps run in: the
// container of App's current deployment, runner.ContainerName(App, its
// current deployment). The caller works it out from the store, never from
// what a request or a manifest says.
type ExecTarget struct {
	App       string
	Container string
}

// Groups of steps (ActionEnv.Group).
const (
	GroupInit   = "init"
	GroupIssue  = "issue"
	GroupRevoke = "revoke"
	GroupCheck  = "check"
	// GroupPurge deletes what was made for one consumer (its data), after
	// GroupRevoke, when the consumer is deleted with its data.
	GroupPurge = "purge"
	// GroupReady is a provided service's readiness check: read-only, no
	// consumer, no credential.
	GroupReady = "ready"
)

// ActionEnv is what a group of steps runs with: what its templates can read,
// which group it is, and where its command steps run.
type ActionEnv struct {
	// Lookup resolves params.* and platform.* references (Values.lookup).
	Lookup func(ref string) (string, error)
	// SecretRef reports whether a reference Lookup resolves is a secret
	// setting; its value is then hidden in every error. May be nil.
	SecretRef func(ref string) bool
	// Issued is what the issue steps saved (revoke and check).
	Issued map[string]SavedValue
	// Consumer is nil in init.
	Consumer *ActionConsumer
	// IssueID is the number of this issue (newIssueID), for {{issue.id}}.
	// Set it when running issue steps only.
	IssueID string
	// IssueVars are the entry's issue_vars worked out for this issue, for
	// {{issue.<key>}} (issue steps only). Those marked secret are hidden in
	// every error, like a secret setting.
	IssueVars map[string]SavedValue
	// Group is GroupInit, GroupIssue, GroupRevoke, GroupCheck, GroupPurge
	// or GroupReady. A command step's finish_if counts in revoke and purge
	// steps only (and is refused elsewhere).
	Group string
	// Exec is where the group's command steps run; empty when the group
	// has none (or the app has no container now: such a step is refused).
	Exec ExecTarget
}

// Kinds of ActionError.
const (
	ActionBadStep     = "bad_step"    // the steps are not well formed; nothing was sent
	ActionUnsupported = "unsupported" // a step this version does not run; nothing was sent (no step is such now)
	ActionTemplate    = "template"    // a params or platform reference could not be resolved; the step was not sent
	ActionCanceled    = "canceled"    // canceled while waiting to connect again
	ActionNotSent     = "not_sent"    // the app (a command step: the runner) could not be connected to, after every attempt
	ActionNoAnswer    = "no_answer"   // sent, no answer (a command step: no result from the runner): not sent again, it may have taken effect
	ActionStatus      = "status"      // answered with a status that does not pass
	ActionExpect      = "expect"      // answered, expect_json does not match
	ActionSaveFailed  = "save"        // answered and passed, but a value to save is not in the answer
	ActionLimited     = "limited"     // the app answered 429 (too many requests): not sent again, nor retried

	// Command steps (action_exec.go).
	ActionExit       = "exit"        // the command ran and ended with an exit code not in expect_exit
	ActionTimeout    = "timeout"     // the command did not end in time: not run again, it may still be running in the container
	ActionNotRunning = "not_running" // the app's container is not running (or there is none): nothing was run
	ActionTooLarge   = "too_large"   // the command wrote more than the platform keeps: its output was cut, the step fails
	ActionRunnerOld  = "runner_old"  // the runner cannot run commands in containers (an older version): nothing was run
	ActionExecFailed = "exec_failed" // the command could not be started in the container (no such program or user)
)

// ActionError is the failure of one step. Nothing in it holds a secret:
// saved values marked secret and secret settings are replaced by "***",
// also where the app's answer repeats them.
type ActionError struct {
	Step int    // 1-based; 0 when it is not about one step
	Name string // the step's name, may be empty
	Kind string
	// Status is the app's HTTP status (status, expect, save).
	Status int
	// AppMessage is the "message" field of the app's JSON answer, if any.
	AppMessage string
	// Detail is the executor's own account. With Kind status it is set only
	// when the answer was a redirect that was not followed.
	Detail string
	// Command steps: Exec is set; ExitCode is the command's (Kinds exit
	// and save); Output is the end of what it wrote to standard error, its
	// secrets masked (never its stdin or env).
	Exec     bool
	ExitCode int
	Output   string
	err      error
	// untouched: the group failed before any of its steps reached the app
	// or ran (refused by precheck, or its first step failed in a way that
	// sends or runs nothing). Set by run only.
	untouched bool
}

// NothingDone reports whether the group failed before any of its steps
// reached the app or ran a command: there is nothing in the app to undo.
func (e *ActionError) NothingDone() bool { return e.untouched }

// sendsNothing are the kinds of a step failure in which the step reached
// neither the app nor its container. ActionCanceled is not one: a step can
// be canceled after it was sent.
var sendsNothing = map[string]bool{
	ActionBadStep: true, ActionUnsupported: true, ActionTemplate: true, ActionNotSent: true,
	ActionNotRunning: true, ActionRunnerOld: true, ActionExecFailed: true,
}

// maxQuoted bounds how much of an app's message an error text repeats.
const maxQuoted = 300

func (e *ActionError) Error() string {
	var b strings.Builder
	if e.Step > 0 {
		fmt.Fprintf(&b, "第 %d 步", e.Step)
		if e.Name != "" {
			fmt.Fprintf(&b, "（%s）", e.Name)
		}
		b.WriteString("：")
	}
	b.WriteString(e.account())
	return b.String()
}

// account is what went wrong, without the step it was.
func (e *ActionError) account() string {
	var b strings.Builder
	said := ""
	if e.AppMessage != "" {
		said = "，应用说：" + quoteHead(e.AppMessage)
	}
	if e.Output != "" {
		said = "，标准错误的最后：" + e.Output
	}
	switch e.Kind {
	case ActionLimited:
		fmt.Fprintf(&b, "这个应用限制了请求次数（HTTP %d），过一会儿再试", e.Status)
		b.WriteString(said)
	case ActionStatus:
		fmt.Fprintf(&b, "应用返回 HTTP %d", e.Status)
		if e.Detail != "" {
			fmt.Fprintf(&b, "（%s）", e.Detail)
		}
		b.WriteString(said)
	case ActionExpect:
		fmt.Fprintf(&b, "应用的应答和预期不符（HTTP %d）%s", e.Status, said)
	case ActionSaveFailed:
		if e.Exec {
			fmt.Fprintf(&b, "%s（命令的退出码 %d）%s", e.Detail, e.ExitCode, said)
		} else {
			fmt.Fprintf(&b, "%s（HTTP %d）%s", e.Detail, e.Status, said)
		}
	case ActionNotSent:
		if e.Exec {
			// What could not be reached is the runner, not the app.
			fmt.Fprintf(&b, "连不上运行器（runner），命令没有执行：%s", e.Detail)
		} else {
			fmt.Fprintf(&b, "连不上应用：%s", e.Detail)
		}
	case ActionNoAnswer:
		if e.Exec {
			fmt.Fprintf(&b, "命令交给运行器（runner）以后没有收到结果（%s），不再重新执行：它可能已经执行了", e.Detail)
		} else {
			fmt.Fprintf(&b, "请求发出后没有收到应答（%s），不再重发：它可能已经生效", e.Detail)
		}
	case ActionCanceled:
		b.WriteString("被取消")
	case ActionTemplate:
		fmt.Fprintf(&b, "模板：%s", e.Detail)
	case ActionExit:
		fmt.Fprintf(&b, "命令的退出码是 %d（%s）%s", e.ExitCode, e.Detail, said)
	case ActionTimeout:
		fmt.Fprintf(&b, "命令在 %s内没有结束，平台不再等它，也没有重新执行：它可能还在应用的容器里运行（平台停不掉在容器里执行的命令），可能已经部分生效", e.Detail)
		b.WriteString(said)
	case ActionTooLarge:
		fmt.Fprintf(&b, "命令的输出太多（%s），按失败处理", e.Detail)
	case ActionNotRunning:
		fmt.Fprintf(&b, "应用的容器没在运行，命令没有执行（%s）", e.Detail)
	case ActionRunnerOld:
		b.WriteString("运行器（runner）太旧，不能在容器里执行命令，命令没有执行：升级 AcornFox 后重试")
	case ActionExecFailed:
		fmt.Fprintf(&b, "命令在应用的容器里启动不了（%s）", e.Detail)
	default:
		b.WriteString(e.Detail)
	}
	return b.String()
}

// quoteHead is the start of an app's message, at most maxQuoted characters.
func quoteHead(text string) string {
	msg := []rune(text)
	if len(msg) > maxQuoted {
		msg = append(msg[:maxQuoted], '…')
	}
	return string(msg)
}

// quoteTail is the end of a command's output, at most maxQuoted characters.
func quoteTail(text string) string {
	msg := []rune(strings.TrimSpace(text))
	if len(msg) > maxQuoted {
		msg = append([]rune{'…'}, msg[len(msg)-maxQuoted:]...)
	}
	return string(msg)
}

func (e *ActionError) Unwrap() error { return e.err }

// Answered reports whether the app answered and said no (a status that does
// not pass, an answer other than expected, or a command that ran and ended
// with an exit code that does not pass), as opposed to a request that never
// got through or was never sent.
func (e *ActionError) Answered() bool {
	return e.Kind == ActionStatus || e.Kind == ActionExpect || e.Kind == ActionExit
}

// actionRunner runs groups of steps.
type actionRunner struct {
	http *http.Client
	// A request that could not connect is sent again, attempts times in all,
	// retryWait apart. Any other failure is final: the request may have
	// reached the app.
	attempts  int
	retryWait time.Duration
	timeout   time.Duration // of one request
	// second is how long a step's retry.delay_s counts per unit: a second,
	// shorter in tests.
	second time.Duration
	// exec runs command steps; nil when the runner cannot (a command step
	// is then refused before anything is done: ActionRunnerOld).
	exec runner.ExecAPI
}

// maxActionAnswer bounds how much of an answer is read.
const maxActionAnswer = 4 << 20

func (e *Engine) actions() actionRunner {
	ex, _ := e.cfg.Runner.(runner.ExecAPI)
	return actionRunner{http: e.cfg.HTTP, attempts: 5, retryWait: 2 * time.Second, timeout: 15 * time.Second, second: time.Second, exec: ex}
}

var actionMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true, http.MethodDelete: true,
}

// run does steps in order: HTTP steps against the app's endpoint at base
// (scheme, host and port; may be "" when no step is an HTTP one), command
// steps in env.Exec's container. It returns what they saved. It stops at the
// first step that does not pass; the values saved until then are returned
// with the error (the caller may need them to undo what earlier steps did).
// A step whose answer (or exit code) matches its finish_if ends the group
// there, as a success.
func (r actionRunner) run(ctx context.Context, base string, steps []catalog.Step, env ActionEnv) (map[string]SavedValue, error) {
	s := &actionSession{r: r, env: env, saved: map[string]SavedValue{}, secrets: map[string]struct{}{}, raw: map[string]struct{}{}}
	for _, v := range env.Issued {
		if v.Secret {
			s.hide(v.Value)
		}
	}
	for _, v := range env.IssueVars {
		if v.Secret {
			s.hide(v.Value)
		}
	}
	anyHTTP := false
	for _, st := range steps {
		anyHTTP = anyHTTP || !st.IsExec()
	}
	var bu *neturl.URL
	if anyHTTP || base != "" {
		u, err := neturl.Parse(strings.TrimRight(base, "/"))
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return s.saved, &ActionError{Kind: ActionBadStep, Detail: "应用入口的地址不对", untouched: true}
		}
		bu = u
	}
	s.base = bu
	if err := s.precheck(steps); err != nil {
		var ae *ActionError
		if errors.As(err, &ae) {
			ae.untouched = true
		}
		return s.saved, err
	}
	if anyHTTP {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return s.saved, &ActionError{Kind: ActionBadStep, Detail: "建不了会话", err: err}
		}
		client := http.Client{}
		if r.http != nil {
			client = *r.http
		}
		client.Jar = jar
		// A redirect is followed within the app's endpoint only. One that is
		// not followed is the app's answer: the step is judged by its status.
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			switch {
			case req.URL.Host != bu.Host || req.URL.Scheme != bu.Scheme:
				s.notFollowed = "应用把请求转到了别的地址，没有跟过去"
			case len(via) >= 10:
				s.notFollowed = "应用把请求转了 10 次还没到，没有再跟"
			default:
				return nil
			}
			return http.ErrUseLastResponse
		}
		s.client = &client
	}
	for i, st := range steps {
		var err error
		if st.IsExec() {
			err = s.execStep(ctx, i+1, st)
		} else {
			err = s.step(ctx, i+1, st)
		}
		if err != nil {
			var ae *ActionError
			if i == 0 && errors.As(err, &ae) && sendsNothing[ae.Kind] {
				ae.untouched = true
			}
			return s.saved, err
		}
		if s.finished {
			break
		}
	}
	return s.saved, nil
}

type actionSession struct {
	r       actionRunner
	client  *http.Client
	base    *neturl.URL
	env     ActionEnv
	saved   map[string]SavedValue
	secrets map[string]struct{}
	// raw holds the secret values themselves (secrets holds the forms they
	// take in a URL or JSON too): a command's arguments must hold none.
	raw map[string]struct{}
	// notFollowed is set when the answer to the request just sent is a
	// redirect that was not followed, and says why.
	notFollowed string
	// finished: a step's answer matched its finish_if; the group is over.
	finished bool
}

// hide registers a secret value: mask replaces it, and the forms it takes
// in a URL or inside JSON text.
func (s *actionSession) hide(v string) {
	if v == "" {
		return
	}
	s.raw[v] = struct{}{}
	forms := []string{v, neturl.PathEscape(v), neturl.QueryEscape(v)}
	if b, err := json.Marshal(v); err == nil && len(b) >= 2 {
		forms = append(forms, string(b[1:len(b)-1]))
	}
	for _, f := range forms {
		s.secrets[f] = struct{}{}
	}
}

func (s *actionSession) mask(text string) string {
	if len(s.secrets) == 0 || text == "" {
		return text
	}
	all := make([]string, 0, len(s.secrets))
	for v := range s.secrets {
		all = append(all, v)
	}
	// Longest first, so a secret that contains another is hidden whole.
	sort.Slice(all, func(i, j int) bool {
		if len(all[i]) != len(all[j]) {
			return len(all[i]) > len(all[j])
		}
		return all[i] < all[j]
	})
	for _, v := range all {
		text = strings.ReplaceAll(text, v, "***")
	}
	return text
}

func (s *actionSession) fail(n int, st catalog.Step, kind, detail string) *ActionError {
	return &ActionError{Step: n, Name: st.Name, Kind: kind, Detail: s.mask(detail)}
}

// precheck refuses a group that is not well formed before any of it is
// sent: a method or path that is not allowed, a save that cannot be read, a
// reference to a value no earlier step saves, or a command step that cannot
// run here (checkExec).
func (s *actionSession) precheck(steps []catalog.Step) error {
	willSave := map[string]bool{}
	for i, st := range steps {
		n := i + 1
		switch {
		case st.IsExec() && st.HTTP.Written():
			return s.fail(n, st, ActionBadStep, "一个步骤只能写 http 或 exec 中的一种")
		case st.IsExec():
			if err := s.checkExec(n, st); err != nil {
				return err
			}
		case !st.HTTP.Written():
			return s.fail(n, st, ActionBadStep, "步骤里没有写 http 或 exec")
		default:
			if err := s.checkHTTP(n, st); err != nil {
				return err
			}
		}
		for _, ref := range actionRefs(stepTexts(st)) {
			if msg := s.checkRef(ref, willSave); msg != "" {
				return s.fail(n, st, ActionBadStep, msg)
			}
		}
		for _, k := range st.SaveKeys() {
			willSave[k] = true
		}
	}
	// The secret settings any step of the group reads are hidden from the
	// first request on: the app may repeat one in an answer to an earlier
	// step than the one that sends it. One that cannot be resolved is
	// reported when its step is rendered.
	if s.env.Lookup != nil && s.env.SecretRef != nil {
		for _, st := range steps {
			for _, ref := range actionRefs(stepTexts(st)) {
				if stepOnlyRef(ref) || !s.env.SecretRef(ref) {
					continue
				}
				if v, err := s.env.Lookup(ref); err == nil {
					s.hide(v)
				}
			}
		}
	}
	return nil
}

// checkHTTP checks an HTTP step: method, path, statuses, header names,
// retry, finish_if and saves.
func (s *actionSession) checkHTTP(n int, st catalog.Step) error {
	h := &st.HTTP
	if !actionMethods[h.Method] {
		return s.fail(n, st, ActionBadStep, fmt.Sprintf("method 只能是 GET、POST、PUT、PATCH、DELETE，写的是 %q", h.Method))
	}
	if !strings.HasPrefix(h.Path, "/") {
		return s.fail(n, st, ActionBadStep, "path 要以 / 开头，只写路径")
	}
	for _, code := range h.ExpectStatus {
		if code < 100 || code > 599 {
			return s.fail(n, st, ActionBadStep, fmt.Sprintf("expect_status 里的 %d 不是 HTTP 状态码", code))
		}
	}
	for _, k := range sortedKeys(h.Headers) {
		if !headerName(k) {
			return s.fail(n, st, ActionBadStep, fmt.Sprintf("headers 里的 %q 不能当请求头的名字", k))
		}
	}
	if msg := checkRetry(h); msg != "" {
		return s.fail(n, st, ActionBadStep, msg)
	}
	if f := h.FinishIf; f != nil {
		if !f.Written() {
			return s.fail(n, st, ActionBadStep, "finish_if 里要写 status 或 json")
		}
		for _, code := range f.Status {
			if code < 100 || code > 599 {
				return s.fail(n, st, ActionBadStep, fmt.Sprintf("finish_if.status 里的 %d 不是 HTTP 状态码", code))
			}
		}
	}
	for _, k := range sortedKeys(h.Save) {
		sv := h.Save[k]
		if sv.FromStdout() {
			return s.fail(n, st, ActionBadStep, fmt.Sprintf("save.%s：stdout 只能用在 exec 步骤里（取命令的整段标准输出），http 步骤从应答里用 path 或 list 取", k))
		}
		if msg := checkSave(sv); msg != "" {
			return s.fail(n, st, ActionBadStep, fmt.Sprintf("save.%s：%s", k, msg))
		}
	}
	return nil
}

// stepTexts are the parts of a step that hold templates: of an HTTP step its
// path, body, expected answer, header values, the where of its list saves
// and its finish_if.json; of a command step its command, user, env values,
// stdin and the where of its list saves.
func stepTexts(st catalog.Step) []any {
	if e := st.Exec; e != nil {
		var texts []any
		for _, a := range e.Command {
			texts = append(texts, a)
		}
		texts = append(texts, e.User)
		for _, k := range e.EnvKeys() {
			texts = append(texts, e.Env[k])
		}
		texts = append(texts, e.Stdin)
		for _, k := range sortedKeys(e.Save) {
			texts = append(texts, map[string]any(e.Save[k].Where))
		}
		return texts
	}
	h := &st.HTTP
	texts := []any{h.Path, map[string]any(h.JSON), map[string]any(h.ExpectJSON)}
	for _, k := range sortedKeys(h.Headers) {
		texts = append(texts, h.Headers[k])
	}
	for _, k := range sortedKeys(h.Save) {
		texts = append(texts, map[string]any(h.Save[k].Where))
	}
	if h.FinishIf != nil {
		texts = append(texts, map[string]any(h.FinishIf.JSON))
	}
	return texts
}

// stepOnlyRef reports whether a reference is to one of the values only steps
// have, as opposed to a setting.
func stepOnlyRef(ref string) bool {
	return strings.HasPrefix(ref, "saved.") || strings.HasPrefix(ref, "issued.") || strings.HasPrefix(ref, "consumer.") ||
		strings.HasPrefix(ref, "issue.")
}

// headerName reports whether name can be the name of a request header.
func headerName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// checkRetry checks a step's retry: GET only (sending again changes
// nothing), within the bounds of the format.
func checkRetry(h *catalog.StepHTTP) string {
	r := h.Retry
	switch {
	case r == nil:
		return ""
	case h.Method != http.MethodGet:
		return fmt.Sprintf("retry 只能写在 GET 步骤上，这一步是 %s：再发一遍可能把东西再建一次或再删一次", h.Method)
	case r.Times < 1 || r.Times > catalog.MaxStepRetryTimes:
		return fmt.Sprintf("retry.times 要在 1 到 %d 之间，写的是 %d", catalog.MaxStepRetryTimes, r.Times)
	case r.DelayS < 0 || r.DelayS > catalog.MaxStepRetryDelayS:
		return fmt.Sprintf("retry.delay_s 要在 0 到 %d 之间，写的是 %d", catalog.MaxStepRetryDelayS, r.DelayS)
	}
	return ""
}

func checkSave(sv catalog.SaveValue) string {
	switch {
	case sv.FromStdout() && (sv.Path != "" || sv.List != "" || len(sv.Where) > 0 || sv.SortBy != "" || sv.Pick != "" || sv.Field != ""):
		return "stdout 取的是整段标准输出，不能再写 path、list、where、sort_by、pick、field"
	case sv.FromStdout():
		return ""
	case sv.Path != "" && sv.List != "":
		return "path 和 list 只能写一种"
	case sv.Path == "" && sv.List == "":
		return "要写 path，或者 list 加 field"
	case sv.Path != "" && (len(sv.Where) > 0 || sv.SortBy != "" || sv.Pick != "" || sv.Field != ""):
		return "where、sort_by、pick、field 只和 list 一起用"
	case sv.List != "" && sv.Field == "":
		return "从列表里挑一项时要写 field"
	case sv.Pick != "" && sv.Pick != "first" && sv.Pick != "last":
		return "pick 只能是 first 或 last"
	}
	return ""
}

// checkRef checks a reference to one of the values only steps have. Other
// references are for Lookup, when the step is rendered.
func (s *actionSession) checkRef(ref string, willSave map[string]bool) string {
	switch {
	case strings.HasPrefix(ref, "saved."):
		if k := strings.TrimPrefix(ref, "saved."); !willSave[k] {
			return fmt.Sprintf("引用了 saved.%s，但前面的步骤没有存过它", k)
		}
	case strings.HasPrefix(ref, "issued."):
		if _, ok := s.env.Issued[strings.TrimPrefix(ref, "issued.")]; !ok {
			return fmt.Sprintf("引用了 %s，但发凭据时没有存过它", ref)
		}
	case strings.HasPrefix(ref, "consumer."):
		if s.env.Consumer == nil {
			return fmt.Sprintf("引用了 %s，但这里没有正在接入的应用（初始化步骤里不能用 consumer）", ref)
		}
		if ref != "consumer.name" && ref != "consumer.id" {
			return fmt.Sprintf("consumer 只有 name 和 id，没有 %s", ref)
		}
	case strings.HasPrefix(ref, "issue."):
		if s.env.IssueID == "" {
			back := "issued.<键>"
			if ref == catalog.IssueIDRef {
				back = "issued." + catalog.IssueIDKey
			}
			return fmt.Sprintf("引用了 %s，但这里不是发凭据的步骤（只有 issue 的步骤里有；revoke、check 里用 %s）", ref, back)
		}
		if ref == catalog.IssueIDRef {
			return ""
		}
		if _, ok := s.env.IssueVars[strings.TrimPrefix(ref, "issue.")]; !ok {
			if len(s.env.IssueVars) == 0 {
				return fmt.Sprintf("issue 只有 id，没有 %s", ref)
			}
			return fmt.Sprintf("issue 只有 id 和 issue_vars 里的键（%s），没有 %s", strings.Join(sortedKeys(s.env.IssueVars), "、"), ref)
		}
	}
	return ""
}

// actionRefs lists the references in the strings of JSON-like values.
func actionRefs(values []any) []string {
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			out = append(out, catalog.TemplateRefs(x)...)
		case map[string]any:
			for _, k := range sortedKeys(x) {
				walk(x[k])
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	for _, v := range values {
		walk(v)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// resolve is the value of one reference.
func (s *actionSession) resolve(ref string) (string, error) {
	switch {
	case strings.HasPrefix(ref, "saved."):
		v, ok := s.saved[strings.TrimPrefix(ref, "saved.")]
		if !ok {
			return "", fmt.Errorf("前面的步骤没有存过 %s", ref)
		}
		return v.Value, nil
	case strings.HasPrefix(ref, "issued."):
		v, ok := s.env.Issued[strings.TrimPrefix(ref, "issued.")]
		if !ok {
			return "", fmt.Errorf("发凭据时没有存过 %s", ref)
		}
		return v.Value, nil
	case strings.HasPrefix(ref, "consumer."):
		switch {
		case s.env.Consumer == nil:
			return "", fmt.Errorf("这里没有正在接入的应用，不能用 %s", ref)
		case ref == "consumer.name":
			return s.env.Consumer.Name, nil
		case ref == "consumer.id":
			return s.env.Consumer.ID, nil
		}
		return "", fmt.Errorf("consumer 只有 name 和 id，没有 %s", ref)
	case strings.HasPrefix(ref, "issue."):
		if s.env.IssueID == "" {
			return "", fmt.Errorf("这里没有 %s", ref)
		}
		if ref == catalog.IssueIDRef {
			return s.env.IssueID, nil
		}
		v, ok := s.env.IssueVars[strings.TrimPrefix(ref, "issue.")]
		if !ok {
			return "", fmt.Errorf("这里没有 %s", ref)
		}
		return v.Value, nil
	}
	if s.env.Lookup == nil {
		return "", fmt.Errorf("不支持的引用 %s", ref)
	}
	v, err := s.env.Lookup(ref)
	if err != nil {
		return "", err
	}
	if s.env.SecretRef != nil && s.env.SecretRef(ref) {
		s.hide(v)
	}
	return v, nil
}

// render fills in every reference of tmpl (catalog.ParseTemplate); escape
// (may be nil) is applied to each inserted value after its transforms.
func (s *actionSession) render(tmpl string, escape func(string) string) (string, error) {
	t, err := catalog.ParseTemplate(tmpl)
	if err != nil {
		return tmpl, err
	}
	// What a transform makes of a secret (its b64, its urlencode …) is as
	// secret as the secret, and hide only knows the secret's own forms. A
	// map's result is a word the manifest wrote, no secret.
	for _, x := range t.Exprs() {
		if len(x.Transforms) == 0 || x.HasTransform("map") {
			continue
		}
		raw, err := s.resolve(x.Ref)
		if err != nil {
			continue
		}
		if _, secret := s.raw[raw]; !secret {
			continue
		}
		if v, err := catalog.RenderTemplate(x.Text, s.resolve, nil); err == nil {
			s.hide(v)
		}
	}
	return t.Render(s.resolve, escape)
}

// renderJSON renders every string in a JSON-like value; other types are
// kept.
func (s *actionSession) renderJSON(in any) (any, error) {
	switch x := in.(type) {
	case string:
		return s.render(x, nil)
	case map[string]any:
		out := make(map[string]any, len(x))
		for _, k := range sortedKeys(x) {
			r, err := s.renderJSON(x[k])
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			r, err := s.renderJSON(e)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	}
	return in, nil
}

func (s *actionSession) renderMap(in map[string]any) (map[string]any, error) {
	if in == nil {
		return nil, nil
	}
	out, err := s.renderJSON(in)
	if err != nil {
		return nil, err
	}
	return out.(map[string]any), nil
}

// renderPath renders a path: an inserted value is escaped as one path
// segment before the "?", as a query value after it, so it cannot add
// segments or parameters of its own. Nor can it remove one: a segment that
// values leave empty, "." or ".." would send the request to another path
// than the one written (DELETE /items/{{id}} to /items/), and is refused.
func (s *actionSession) renderPath(tmpl string) (string, error) {
	written, q, hasQuery := strings.Cut(tmpl, "?")
	p, err := s.render(written, neturl.PathEscape)
	if err != nil {
		return "", err
	}
	// An escaped value holds no "/": the segments are those written.
	was, is := strings.Split(written, "/"), strings.Split(p, "/")
	if len(was) != len(is) {
		return "", errors.New("path 填进值以后段数变了，没有发")
	}
	for i, seg := range is {
		if (seg == "" || seg == "." || seg == "..") && len(catalog.TemplateRefs(was[i])) > 0 {
			return "", fmt.Errorf("path 里 %s 这一段填进值以后是空的（或者是 . 或 ..），请求会落到别的路径上，没有发", was[i])
		}
	}
	if !hasQuery {
		return p, nil
	}
	q, err = s.render(q, neturl.QueryEscape)
	if err != nil {
		return "", err
	}
	return p + "?" + q, nil
}

// actionRequest is a step with its templates rendered.
type actionRequest struct {
	method  string
	url     string
	headers [][2]string
	body    []byte // nil: no body
	expect  map[string]any
	where   map[string]map[string]any // save key → rendered where
	finish  map[string]any            // finish_if.json rendered
}

func (s *actionSession) prepare(h *catalog.StepHTTP) (*actionRequest, error) {
	req := &actionRequest{method: h.Method, where: map[string]map[string]any{}}
	p, err := s.renderPath(h.Path)
	if err != nil {
		return nil, err
	}
	u, err := neturl.Parse(s.base.String() + p)
	if err != nil || u.Host != s.base.Host || u.Scheme != s.base.Scheme {
		return nil, errors.New("path 渲染后不是这个应用入口下的地址")
	}
	req.url = u.String()
	for _, k := range sortedKeys(h.Headers) {
		v, err := s.render(h.Headers[k], nil)
		if err != nil {
			return nil, err
		}
		if !singleLine(v) {
			return nil, fmt.Errorf("请求头 %s 的值里不能有换行或控制字符", k)
		}
		req.headers = append(req.headers, [2]string{k, v})
	}
	if h.JSON != nil {
		body, err := s.renderMap(h.JSON)
		if err != nil {
			return nil, err
		}
		if req.body, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	if req.expect, err = s.renderMap(h.ExpectJSON); err != nil {
		return nil, err
	}
	if h.FinishIf != nil {
		if req.finish, err = s.renderMap(h.FinishIf.JSON); err != nil {
			return nil, err
		}
	}
	for _, k := range sortedKeys(h.Save) {
		w, err := s.renderMap(h.Save[k].Where)
		if err != nil {
			return nil, err
		}
		req.where[k] = w
	}
	return req, nil
}

// step sends one step and judges the answer. With a retry (GET only, see
// precheck) an answer that does not pass, or that lacks a value to save, is
// asked for again, at most retry.times more times, retry.delay_s apart; the
// last answer's failure is the step's.
func (s *actionSession) step(ctx context.Context, n int, st catalog.Step) error {
	req, err := s.prepare(&st.HTTP)
	if err != nil {
		f := s.fail(n, st, ActionTemplate, err.Error())
		f.err = err
		return f
	}
	more, delay := 0, time.Duration(0)
	if r := st.HTTP.Retry; r != nil && st.HTTP.Method == http.MethodGet {
		more, delay = r.Times, time.Duration(r.DelayS)*s.r.second
	}
	for {
		err := s.sendOnce(ctx, n, st, req)
		var ae *ActionError
		if more == 0 || !errors.As(err, &ae) || !answerMayChange(ae.Kind) {
			return err
		}
		more--
		select {
		case <-ctx.Done():
			f := s.fail(n, st, ActionCanceled, "")
			f.err = ctx.Err()
			return f
		case <-time.After(delay):
		}
	}
}

// answerMayChange reports whether a failure is about the app's answer, which
// a moment later may be another: the kinds a retry asks again for.
func answerMayChange(kind string) bool {
	return kind == ActionStatus || kind == ActionExpect || kind == ActionSaveFailed
}

// sendOnce sends a step's request, again while it cannot connect (attempts
// in all), and judges the answer.
func (s *actionSession) sendOnce(ctx context.Context, n int, st catalog.Step, req *actionRequest) error {
	lastErr := ""
	for attempt := 0; attempt < s.r.attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				f := s.fail(n, st, ActionCanceled, "")
				f.err = ctx.Err()
				return f
			case <-time.After(s.r.retryWait):
			}
		}
		code, raw, err := s.send(ctx, req)
		if err != nil {
			if errors.Is(err, errNotBuilt) {
				return s.fail(n, st, ActionBadStep, err.Error())
			}
			if notSent(err) {
				lastErr = scrubURL(err).Error()
				continue
			}
			return s.fail(n, st, ActionNoAnswer, scrubURL(err).Error())
		}
		return s.answer(n, st, req, code, raw)
	}
	return s.fail(n, st, ActionNotSent, lastErr)
}

// errNotBuilt: the request could not be put together; nothing was sent.
var errNotBuilt = errors.New("请求拼不出来，没有发")

func (s *actionSession) send(ctx context.Context, req *actionRequest) (int, []byte, error) {
	s.notFollowed = ""
	var rdr io.Reader
	if req.body != nil {
		rdr = bytes.NewReader(req.body)
	}
	rctx, cancel := context.WithTimeout(ctx, s.r.timeout)
	defer cancel()
	hr, err := http.NewRequestWithContext(rctx, req.method, req.url, rdr)
	if err != nil {
		return 0, nil, errNotBuilt
	}
	if req.body != nil {
		hr.Header.Set("Content-Type", "application/json")
	}
	for _, kv := range req.headers {
		hr.Header.Set(kv[0], kv[1])
	}
	resp, err := s.client.Do(hr)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxActionAnswer))
	return resp.StatusCode, raw, nil
}

// answer judges the app's answer and saves from it.
func (s *actionSession) answer(n int, st catalog.Step, req *actionRequest, code int, raw []byte) error {
	h := st.HTTP
	// The answer twice: numbers as float64, and as the app wrote them (an id
	// past 2^53 keeps its digits, for saving and for comparing).
	var body, exact any
	isJSON := json.Unmarshal(raw, &body) == nil
	if isJSON {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if dec.Decode(&exact) != nil {
			exact = nil
		}
	} else {
		body = nil
	}
	appMsg := ""
	if m, ok := body.(map[string]any); ok {
		appMsg, _ = m["message"].(string)
	}
	// Whatever this answer holds at a secret save's place is hidden from
	// here on, also when the step fails before it is saved.
	for _, sv := range h.Save {
		if sv.Secret {
			s.hideCandidates(sv, exact)
		}
	}
	failed := func(kind, detail string) *ActionError {
		f := s.fail(n, st, kind, detail)
		f.Status, f.AppMessage = code, s.mask(appMsg)
		return f
	}
	if fi := h.FinishIf; fi != nil && finishMatches(fi, req.finish, code, body, exact) {
		// What the group is there for is done already (taking back a thing
		// that is gone): over, as a success; nothing is saved from it.
		s.finished = true
		return nil
	}
	if code == http.StatusTooManyRequests && !h.StatusOK(code) {
		// The app limits how often it may be asked: sending again now (or
		// on a retry) only makes it worse.
		return failed(ActionLimited, "")
	}
	pass := code >= 200 && code < 300
	if len(h.ExpectStatus) > 0 {
		pass = false
		for _, c := range h.ExpectStatus {
			pass = pass || c == code
		}
	}
	if !pass {
		return failed(ActionStatus, s.notFollowed)
	}
	if len(req.expect) > 0 && !matchAnswer(req.expect, body, exact) {
		return failed(ActionExpect, "")
	}
	if len(h.Save) == 0 {
		return nil
	}
	if !isJSON {
		return failed(ActionSaveFailed, "应答不是 JSON，取不到要存的值")
	}
	// All of the step's values or none.
	got := map[string]SavedValue{}
	for _, k := range sortedKeys(h.Save) {
		sv := h.Save[k]
		v, err := pickSaved(sv, req.where[k], body, exact)
		if err != nil {
			return failed(ActionSaveFailed, fmt.Sprintf("存 %s 时%s", k, err.Error()))
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

// finishMatches reports whether an answer matches a step's finish_if: its
// status is one of the listed ones (when listed) and its JSON holds want
// (finish_if.json rendered, when written).
func finishMatches(fi *catalog.StepFinishIf, want map[string]any, code int, body, exact any) bool {
	if !fi.Written() {
		return false
	}
	if len(fi.Status) > 0 {
		hit := false
		for _, c := range fi.Status {
			hit = hit || c == code
		}
		if !hit {
			return false
		}
	}
	return len(want) == 0 || matchAnswer(want, body, exact)
}

// hideCandidates hides every value a secret save could take from the answer.
func (s *actionSession) hideCandidates(sv catalog.SaveValue, exact any) {
	if sv.Path != "" {
		if v, ok := digPath(exact, sv.Path); ok {
			s.hideAll(v)
		}
		return
	}
	list, _ := digPath(exact, sv.List)
	items, _ := list.([]any)
	for _, it := range items {
		if v, ok := digPath(it, sv.Field); ok {
			s.hideAll(v)
		}
	}
}

// hideAll hides a value of an answer; of an object or a list (a secret save
// that points one level too high cannot be saved, and still must not leak)
// every text and number in it.
func (s *actionSession) hideAll(v any) {
	switch x := v.(type) {
	case map[string]any:
		for _, e := range x {
			s.hideAll(e)
		}
	case []any:
		for _, e := range x {
			s.hideAll(e)
		}
	case bool:
		// "true" is no secret, and hiding it would garble every message.
	default:
		if text, ok := scalarOf(v); ok {
			s.hide(text)
		}
	}
}

// pickSaved reads one saved value from an answer. body and exact are the
// same answer (see answer); where is sv.Where rendered. The errors name
// places in the answer, never values.
func pickSaved(sv catalog.SaveValue, where map[string]any, body, exact any) (string, error) {
	if sv.Path != "" {
		v, _ := digPath(exact, sv.Path)
		return savedText(v, sv.Path)
	}
	lb, _ := digPath(body, sv.List)
	le, _ := digPath(exact, sv.List)
	plain, ok1 := lb.([]any)
	items, ok2 := le.([]any)
	if !ok1 || !ok2 || len(plain) != len(items) {
		return "", fmt.Errorf("，应答里的 %s 不是列表", sv.List)
	}
	var idx []int
	for i := range plain {
		if len(where) == 0 || matchAnswer(where, plain[i], items[i]) {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return "", fmt.Errorf("，列表 %s 里没有符合条件的项", sv.List)
	}
	if sv.SortBy != "" {
		keys := make(map[int]sortKey, len(idx))
		ints, nums := true, true
		for _, i := range idx {
			v, _ := digPath(items[i], sv.SortBy)
			text, ok := scalarOf(v)
			if !ok {
				return "", fmt.Errorf("，列表 %s 里有的项没有 %s，排不了序", sv.List, sv.SortBy)
			}
			k := sortKey{text: text}
			// A number, or text that reads as one: many apps write an id as
			// text to keep its digits.
			num := ""
			switch x := v.(type) {
			case json.Number:
				num = x.String()
			case string:
				if jsonNumberRe.MatchString(x) {
					num = x
				}
			}
			var err error
			if k.i, err = strconv.ParseInt(num, 10, 64); err != nil {
				ints = false
			}
			if k.f, err = strconv.ParseFloat(num, 64); err != nil {
				nums = false
			}
			keys[i] = k
		}
		// By value when every item's is a number (whole numbers exactly),
		// as text otherwise.
		sort.SliceStable(idx, func(a, b int) bool {
			ka, kb := keys[idx[a]], keys[idx[b]]
			switch {
			case ints:
				return ka.i < kb.i
			case nums:
				return ka.f < kb.f
			}
			return ka.text < kb.text
		})
	}
	chosen := idx[len(idx)-1]
	if sv.Pick == "first" {
		chosen = idx[0]
	}
	v, _ := digPath(items[chosen], sv.Field)
	return savedText(v, sv.List+" 里挑出那一项的 "+sv.Field)
}

type sortKey struct {
	text string
	i    int64
	f    float64
}

// matchAnswer reports whether every key of want has the same value in an
// answer (nested objects compared the same way), as matchJSON does, and
// compares numbers as the app wrote them. body and exact are the same answer,
// numbers decoded as float64 and as json.Number.
//
// A rendered template is text, an id in an answer is a number: text equals a
// number when it reads as that number, digit for digit, however large. (As
// float64 an id from a million up prints as 1e+06 and two ids past 2^53 can
// be the same float.)
func matchAnswer(want map[string]any, body, exact any) bool {
	g, ok := body.(map[string]any)
	if !ok {
		return false
	}
	x, _ := exact.(map[string]any)
	for k, w := range want {
		gv, ok := g[k]
		if !ok {
			return false
		}
		switch wv := w.(type) {
		case map[string]any:
			if !matchAnswer(wv, gv, x[k]) {
				return false
			}
		default:
			if !answerEqual(wv, gv, x[k]) {
				return false
			}
		}
	}
	return true
}

// answerEqual compares one wanted value with the answer's, given both ways
// (see matchAnswer).
func answerEqual(want, got, exact any) bool {
	num, ok := exact.(json.Number)
	if !ok {
		return scalarEqual(want, got)
	}
	wholeWant := false
	text := ""
	switch w := want.(type) {
	case string:
		text = w
	case int, int64, uint64:
		text, wholeWant = fmt.Sprint(w), true
	case float64:
		text = strconv.FormatFloat(w, 'g', -1, 64)
	default:
		return scalarEqual(want, got)
	}
	a, aok := decimalOf(text)
	b, bok := decimalOf(num.String())
	if aok && bok && a == b {
		return true
	}
	if wholeWant && aok && bok && !strings.ContainsAny(num.String(), ".eE") {
		// Two whole numbers written out in full: float64 could add nothing
		// but a false match between neighbours past 2^53.
		return false
	}
	return scalarEqual(want, got)
}

var jsonNumberRe = regexp.MustCompile(`^(-?)(0|[1-9][0-9]*)(?:\.([0-9]+))?(?:[eE]([+-]?[0-9]+))?$`)

// decimal is a number as digits times a power of ten, in the one form a
// value has: no leading or trailing zeros in the digits.
type decimal struct {
	neg    bool
	digits string
	exp    int
}

// decimalOf reads text written the way JSON writes a number. It does no
// arithmetic on the value, so its size costs nothing.
func decimalOf(text string) (decimal, bool) {
	m := jsonNumberRe.FindStringSubmatch(text)
	if m == nil || len(m[4]) > 9 {
		return decimal{}, false
	}
	exp := 0
	if m[4] != "" {
		exp, _ = strconv.Atoi(m[4])
	}
	digits := m[2] + m[3]
	exp -= len(m[3])
	trimmed := strings.TrimRight(digits, "0")
	exp += len(digits) - len(trimmed)
	trimmed = strings.TrimLeft(trimmed, "0")
	if trimmed == "" {
		return decimal{digits: "0"}, true
	}
	return decimal{neg: m[1] == "-", digits: trimmed, exp: exp}, true
}

func savedText(v any, place string) (string, error) {
	if v == nil {
		return "", fmt.Errorf("，应答里没有 %s", place)
	}
	text, ok := scalarOf(v)
	if !ok {
		return "", fmt.Errorf("，应答里的 %s 不是一个值（是对象或列表）", place)
	}
	if text == "" {
		return "", fmt.Errorf("，应答里的 %s 是空的", place)
	}
	return text, nil
}

// scalarOf is the text of a JSON string, number or boolean.
func scalarOf(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case json.Number:
		return x.String(), true
	case bool:
		return strconv.FormatBool(x), true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	}
	return "", false
}

// digPath follows a dot path ("data.user.id") into a decoded JSON value; a
// number steps into an array ("data.items.0.id").
func digPath(v any, path string) (any, bool) {
	for _, seg := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			next, ok := x[seg]
			if !ok {
				return nil, false
			}
			v = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}

// ---- init steps ----

// initActionStep is an init step as the executor runs it: the manifest's
// step itself, except that a manifest written before methods were required
// may leave the method out (POST). A command step stays a command step; it
// is never taken for an empty request.
func initActionStep(st catalog.Step) catalog.Step {
	if !st.IsExec() && st.HTTP.Method == "" {
		st.HTTP.Method = http.MethodPost
	}
	return st
}

// initActionSteps are the manifest's init steps that run once the app is
// healthy.
func initActionSteps(m *catalog.Manifest) []catalog.Step {
	var out []catalog.Step
	for _, st := range m.Init {
		if st.When != "" && st.When != "after_healthy" {
			continue
		}
		out = append(out, initActionStep(st))
	}
	return out
}

// secretSettingRefs reports which params.* and platform.* references of an
// app are secret settings: parameters of type secret, those an env entry or
// a first-login field marks secret, and secret platform variables.
func secretSettingRefs(m *catalog.Manifest) func(ref string) bool {
	secret := secretParams(m)
	for _, p := range m.Params {
		if p.Type == "secret" {
			secret[p.Key] = true
		}
	}
	return func(ref string) bool {
		if name, ok := strings.CutPrefix(ref, "platform."); ok {
			pv, ok := catalog.LookupPlatformVar(name)
			return ok && pv.Secret
		}
		key, ok := strings.CutPrefix(ref, "params.")
		return ok && secret[key]
	}
}

// initFault is the fault of an init step that did not pass. A fault that
// carries answered means the app itself said no.
func initFault(err error) *Fault {
	var ae *ActionError
	if !errors.As(err, &ae) {
		return fault(CodeInit, err, "初始化失败：%v", err)
	}
	switch ae.Kind {
	case ActionTemplate:
		code := CodeInit
		if catalog.IsRenderError(ae.err) {
			code = CodeRender // a transform failed, or the template cannot be read
		}
		return fault(code, ae.err, "初始化 %s 的模板：%s", ae.Name, ae.Detail)
	case ActionCanceled:
		return fault(CodeInit, ae.err, "初始化被取消")
	case ActionNoAnswer:
		if ae.Exec {
			return fault(CodeInit, nil, "初始化 %s：%s", ae.Name, ae.account())
		}
		return fault(CodeInit, nil, "初始化 %s 没有收到应答（%s），不再重发：它可能已经生效", ae.Name, ae.Detail)
	case ActionNotSent:
		if ae.Exec {
			return fault(CodeInit, nil, "初始化 %s：%s", ae.Name, ae.account())
		}
		return fault(CodeInit, nil, "初始化 %s 失败：%s", ae.Name, ae.Detail)
	case ActionStatus:
		f := fault(CodeInit, nil, "初始化 %s 返回 HTTP %d", ae.Name, ae.Status)
		if ae.Detail != "" {
			f.Message += "（" + ae.Detail + "）"
		}
		f.answered = true
		return f
	case ActionExpect:
		f := fault(CodeInit, nil, "初始化 %s 的应答和预期不符：%s", ae.Name, ae.AppMessage)
		f.answered = true
		return f
	case ActionUnsupported:
		return fault(CodeUnsupported, nil, "初始化 %s：%s", ae.Name, ae.Detail)
	case ActionRunnerOld:
		return fault(CodeInternal, nil, "初始化 %s：%s", ae.Name, ae.account())
	case ActionExit:
		f := fault(CodeInit, nil, "初始化 %s：%s", ae.Name, ae.account())
		f.answered = true
		return f
	case ActionTimeout, ActionTooLarge, ActionNotRunning, ActionExecFailed:
		return fault(CodeInit, nil, "初始化 %s：%s", ae.Name, ae.account())
	case ActionLimited:
		return fault(CodeInit, nil, "初始化 %s 时，这个应用限制了请求次数（HTTP %d），过一会儿再试；没有自动重发", ae.Name, ae.Status)
	}
	return fault(CodeInit, nil, "初始化 %s：%s", ae.Name, ae.Detail)
}
