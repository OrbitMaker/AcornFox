package reconcile

// Deployment logs: the reconciler keeps each deployment's build output and,
// before removing a deployment's container, the tail of its run output, so
// older versions' logs survive. Files are written through
// Config.DeployLogs (internal/deploylog); with DeployLogs nil nothing here
// runs and the reconciler behaves as before.
//
// Redaction uses deploylog.SecretsForApp, the same rule as the log API. When
// that configuration cannot be read the log is not saved at all (an event
// says so): a missing log is better than a plaintext secret on disk.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/acornfox/acornfox/internal/deploylog"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

const (
	// runLogTimeout bounds reading a container's log tail before removal.
	runLogTimeout = 10 * time.Second
	// runLogHeaderRoom is left free under the run log limit for the
	// "earlier output omitted" first line.
	runLogHeaderRoom = 256
	// gitLogLines is how much of the git output a build log keeps.
	gitLogLines = 200
	// excerptLineMax caps one line of a diagnosis log excerpt.
	excerptLineMax = 2 << 10
)

// Build log lines written when there is no build output to keep.
const (
	buildLogImageExists  = "镜像已存在，未重新构建"
	buildLogNoOutput     = "runner 未返回构建输出"
	buildLogTooLarge     = "构建输出过大，runner 无法返回"
	buildLogPullPrefix   = "拉取 "
	buildLogLocalPattern = "镜像 %s 已在本机，未重新拉取"
)

// excerptANSI matches ANSI escape sequences (colors, cursor moves).
var excerptANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// logRedactor returns a redactor with SecretsForApp plus extra values. On
// error the caller must not save the log; an event records why.
func (r *Reconciler) logRedactor(ctx context.Context, app string, d state.Deployment, extra ...string) (*deploylog.Redactor, error) {
	secrets, err := deploylog.SecretsForApp(ctx, r.cfg.Store, app)
	if err != nil {
		r.log.Error("deploy log secrets", "app", app, "deployment", d.ID, "err", err)
		r.event(ctx, app, d.ID, "logs", fmt.Sprintf("%s的日志没有保存：读不到脱敏配置", versionLabel(d)))
		return nil, err
	}
	return deploylog.NewRedactor(append(secrets, extra...)...), nil
}

// versionLabel is "第 N 版" for a known seq, "这一版" otherwise.
func versionLabel(d state.Deployment) string {
	if d.Seq > 0 {
		return fmt.Sprintf("第 %d 版", d.Seq)
	}
	return "这一版"
}

// saveBuildLog writes text as d's build log (redacted). Failures are recorded
// and never affect the deployment.
func (r *Reconciler) saveBuildLog(ctx context.Context, app string, d state.Deployment, text string) {
	if r.cfg.DeployLogs == nil {
		return
	}
	red, err := r.logRedactor(ctx, app, d)
	if err != nil {
		return
	}
	if err := r.cfg.DeployLogs.Write(app, d.ID, deploylog.KindBuild, []byte(text), red); err != nil {
		r.log.Error("write build log", "app", app, "deployment", d.ID, "err", err)
		r.event(ctx, app, d.ID, "logs", versionLabel(d)+"的构建日志没有保存：写入失败")
	}
}

// buildLogText joins the git output (last gitLogLines lines, may be empty)
// and the runner's build output. Without build output it writes one line:
// the image already existed (success), the failure message, or that the
// runner returned nothing.
func buildLogText(gitOutput string, resp runner.BuildResponse) string {
	var parts []string
	if g := lastLines(gitOutput, gitLogLines); g != "" {
		parts = append(parts, g)
	}
	switch {
	case resp.Log != "":
		parts = append(parts, strings.TrimRight(resp.Log, "\n"))
	case resp.OK:
		parts = append(parts, buildLogImageExists)
	case resp.Failure != nil && resp.Failure.Message != "":
		parts = append(parts, resp.Failure.Message)
	default:
		parts = append(parts, buildLogNoOutput)
	}
	return strings.Join(parts, "\n") + "\n"
}

// lastLines returns the last n lines of s, trimmed of surrounding blank space.
func lastLines(s string, n int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// retireContainer is the one place a deployment's app container is removed
// by a reconcile round: it first saves the container's run log (when deploy
// logs are kept), then calls onCaptured, if any, with the last
// MaxLogExcerptLine lines of that log (redacted; falling back to tailLogs when
// nothing was saved), and finally removes the container. Failure paths pass
// onCaptured to record the diagnosis before the container goes away.
func (r *Reconciler) retireContainer(ctx context.Context, app, name string, d state.Deployment, env []state.EnvVar, onCaptured func(excerpt string)) {
	excerpt := r.archiveRunLog(ctx, app, name, d)
	if onCaptured != nil {
		if excerpt == "" {
			excerpt = r.tailLogs(ctx, app, name, env)
		}
		onCaptured(excerpt)
	}
	_ = r.cfg.Runner.RemoveContainer(ctx, app, name)
}

// archiveRunLog saves the tail of container name's output as d's run log and
// returns its excerpt, or "" when nothing was saved. It never fails the
// caller: a runner without LogTail is logged once, other problems become an
// event on the deployment.
func (r *Reconciler) archiveRunLog(ctx context.Context, app, name string, d state.Deployment) string {
	if r.cfg.DeployLogs == nil {
		return ""
	}
	lt, ok := r.cfg.Runner.(runner.LogTailer)
	if !ok {
		r.warnNoLogTail()
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, runLogTimeout)
	// Leave room for the "earlier output omitted" line so Write keeps it.
	res, err := lt.LogTail(cctx, app, name, deploylog.MaxRunBytes-runLogHeaderRoom)
	timedOut := errors.Is(cctx.Err(), context.DeadlineExceeded)
	cancel()
	if err != nil {
		switch {
		case errors.Is(err, runner.ErrUnsupported):
			r.warnNoLogTail()
		case errors.Is(err, runner.ErrNotFound):
			// The deployment never got a container (e.g. superseded while
			// queued): there is no output to keep.
		default:
			reason := "读取失败"
			if timedOut {
				reason = fmt.Sprintf("%d 秒内没有读完", int(runLogTimeout/time.Second))
			}
			r.log.Error("read run log", "app", app, "deployment", d.ID, "err", err)
			r.event(ctx, app, d.ID, "logs", versionLabel(d)+"的运行日志没有保存："+reason)
		}
		return ""
	}

	current, err := r.cfg.Store.ListEnv(ctx, app)
	if err != nil {
		r.log.Error("deploy log env", "app", app, "err", err)
		r.event(ctx, app, d.ID, "logs", fmt.Sprintf("%s的日志没有保存：读不到脱敏配置", versionLabel(d)))
		return ""
	}
	addonKeys, err := r.addonEnvKeys(ctx, app)
	if err != nil {
		r.log.Error("deploy log addon keys", "app", app, "err", err)
		r.event(ctx, app, d.ID, "logs", fmt.Sprintf("%s的日志没有保存：读不到脱敏配置", versionLabel(d)))
		return ""
	}
	red, err := r.logRedactor(ctx, app, d, deploylog.ContainerSecrets(res.Env, current, addonKeys)...)
	if err != nil {
		return ""
	}
	text := red.Redact(string(res.Data))
	stored := text
	if res.Truncated {
		// The runner only kept the tail; say so the way a write-side cut does.
		stored = deploylog.EarlierOmitted + "\n" + text
	}
	if err := r.cfg.DeployLogs.Write(app, d.ID, deploylog.KindRun, []byte(stored), red); err != nil {
		r.log.Error("write run log", "app", app, "deployment", d.ID, "err", err)
		r.event(ctx, app, d.ID, "logs", versionLabel(d)+"的运行日志没有保存：写入失败")
	}
	return runExcerpt(text)
}

// addonEnvKeys returns the variable names the app's current and removed
// add-ons inject (DATABASE_URL, REDIS_URL, ...).
func (r *Reconciler) addonEnvKeys(ctx context.Context, app string) ([]string, error) {
	active, err := r.cfg.Store.ListAddons(ctx, app)
	if err != nil {
		return nil, err
	}
	removed, err := r.cfg.Store.ListRemovedAddons(ctx, app)
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, a := range append(active, removed...) {
		key := a.EnvVar
		if key == "" {
			key = state.AddonEnvVar(a.Kind)
		}
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// warnNoLogTail logs once that the runner cannot return log tails.
func (r *Reconciler) warnNoLogTail() {
	r.noLogTailOnce.Do(func() {
		r.log.Warn("runner 版本较旧，暂不保存运行日志")
	})
}

// runExcerpt turns a saved run log ("<RFC3339Nano> <text>" lines) into a
// diagnosis excerpt: the last MaxLogExcerptLine lines, timestamps and ANSI
// codes removed, each at most excerptLineMax bytes.
func runExcerpt(text string) string {
	text = strings.TrimRight(text, "\r\n")
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) > runner.MaxLogExcerptLine {
		lines = lines[len(lines)-runner.MaxLogExcerptLine:]
	}
	for i, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		if ts, rest, ok := strings.Cut(l, " "); ok {
			if _, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				l = rest
			}
		}
		l = excerptANSI.ReplaceAllString(l, "")
		if len(l) > excerptLineMax {
			cut := excerptLineMax
			for cut > 0 && !utf8.RuneStart(l[cut]) {
				cut--
			}
			l = l[:cut] + "…（本行已截断）"
		}
		lines[i] = l
	}
	return strings.Join(lines, "\n")
}

// protectedLogIDs is the set of deployments whose logs are always kept: the
// live one, those a rollback can target (including older records of a kept
// image), pending ones, and the newest failed one.
func protectedLogIDs(deps []state.Deployment, liveID string) map[string]bool {
	out := map[string]bool{}
	if liveID != "" {
		out[liveID] = true
	}
	plan := state.ClassifyRollback(deps, liveID, nil)
	for id, st := range plan.ByID {
		if st.CanRollback || st.Blocked == state.BlockedOlderDuplicate || st.Blocked == state.BlockedPending {
			out[id] = true
		}
	}
	var newestFailed *state.Deployment
	for i := range deps {
		if deps[i].Status == state.StatusFailed && (newestFailed == nil || deps[i].Seq > newestFailed.Seq) {
			newestFailed = &deps[i]
		}
	}
	if newestFailed != nil {
		out[newestFailed.ID] = true
	}
	return out
}

// pruneAppLogs trims app's deployment logs to the newest KeepPerApp plus the
// protected ones. It runs in the app's own round.
func (r *Reconciler) pruneAppLogs(ctx context.Context, app string) {
	if r.cfg.DeployLogs == nil {
		return
	}
	a, err := r.cfg.Store.GetApp(ctx, app)
	if err != nil {
		return
	}
	deps, err := r.cfg.Store.ListDeployments(ctx, app, 0)
	if err != nil {
		r.log.Error("list deployments", "app", app, "err", err)
		return
	}
	recent := make([]string, 0, len(deps))
	for _, d := range deps { // newest first
		recent = append(recent, d.ID)
	}
	if err := r.cfg.DeployLogs.PruneApp(app, recent, protectedLogIDs(deps, a.CurrentDeployment)); err != nil {
		r.log.Error("prune deploy logs", "app", app, "err", err)
	}
}

// pruneGlobalLogs removes logs of deleted apps and enforces the total cap.
// The app list is read again here, right before pruning, so an app created
// during this global round is not treated as an orphan. Any read error skips
// the whole pass rather than prune with an incomplete protected set.
func (r *Reconciler) pruneGlobalLogs(ctx context.Context) {
	if r.cfg.DeployLogs == nil {
		return
	}
	apps, err := r.cfg.Store.ListApps(ctx)
	if err != nil {
		r.log.Error("list apps", "err", err)
		return
	}
	names := make(map[string]bool, len(apps))
	protected := make(map[string]map[string]bool, len(apps))
	for _, a := range apps {
		deps, err := r.cfg.Store.ListDeployments(ctx, a.Name, 0)
		if err != nil {
			r.log.Error("list deployments", "app", a.Name, "err", err)
			return
		}
		names[a.Name] = true
		protected[a.Name] = protectedLogIDs(deps, a.CurrentDeployment)
	}
	if err := r.cfg.DeployLogs.PruneGlobal(names, protected); err != nil {
		r.log.Error("prune deploy logs (global)", "err", err)
	}
}
