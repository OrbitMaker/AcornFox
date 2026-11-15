package appinstall

// Connecting apps to services at install time, and taking back what was
// handed out when an app goes (needs written the new way; see needs.go for
// how a need is matched).
//
// A provider: once an app whose manifest provides a service (the new way)
// is installed, each provides entry becomes one service of the registry
// (registerProvides), named by DefaultServiceName. Its items written as a
// template are worked out then: self.internal_url is how another container
// reaches the entry's endpoint (http://<app name>:<port>, no trailing
// slash), self.host and self.port its parts. Items written { ask: … } stay
// empty until the user fills them in; items written { issued: … } are never
// in the service: each consumer gets its own.
//
// A consumer: when its app is created, every need that was matched is
// plugged in (connectNeeds) and a binding is written. A service an app
// provides with issue steps hands out a credential: the issue steps run
// against the endpoint the provides entry names (its own port on the
// providing app, see providerOf), with consumer.name / consumer.id the
// consuming app's name and a fresh issue number (see action.go). The issue
// number is recorded in the task (AppResult.Issuing) before the steps run;
// steps that fail half-way are followed by the revoke steps with what was
// saved so far and that number (with the number alone after a restart cut
// them short), and the user is told to look when that does not work. What is kept of what the issue steps saved
// is only what is used later (credentialKeys): the keys the items written
// { issued: k } name and the keys the revoke and check steps read as
// issued.<k>, plus the issue number; anything else (a login's access token)
// was for those steps only and is dropped. The credential is kept encrypted
// in the binding only. The consumer sees the service's own items plus, for
// every item written { issued: k }, the credential's k. A provider app also
// joins the consumer's container network (its name goes into the
// consumer's links). A user's own service is handed over as it is.
//
// Those items are what uses.<need>.<item> reads in env "from", config file
// templates and init steps. They live in the install's values only (usesKey),
// never in the saved parameters: the binding is the one place a credential
// is kept.
//
// When a binding of the app exists already (it was deleted with its data
// kept, and is installed again): plugged into the same service, the old
// credential is tried with the provider's check steps and used again when
// they pass. When they do not pass, a new one is issued and the old one is
// left where it is: the user is told (a CAT-INST-35 line in carried) to look
// at it in the provider and remove it if it is of no use (the check may
// have failed because the user turned it off on purpose). A provider
// without check steps cannot tell: the old credential is taken back and a
// new one issued. Plugged into another service, the old credential is
// taken back. None of this happens before the whole task has succeeded:
// the old binding is moved aside meanwhile (ReplacedRef, finishReplaced),
// so a rollback puts it back with its credential as it was. A kept binding
// of a need this install does not plug in (an optional one whose service
// cannot be used now, or a need the new manifest no longer has) is taken
// back and removed then too (dropStaleBindings).
//
// Taking back (revoke steps, with issued.<key> the credential): a failed
// install takes back what it issued and removes the bindings and services it
// made (cleanup); deleting an app with its data does the same
// (ReleaseServices); deleting it with its data kept keeps its bindings, so
// an install on that data finds them again.
//
// A failed install also deletes what the provider made for the credentials
// it issued itself (purge steps, after the revoke steps): the database it
// had just made, empty but for what this install wrote. Only those: the
// task record says which (BindingRef.Issued with its IssueID, IssuingRef for
// an issue under way), and a binding used again (BindingRef.Reused, an
// install on data kept) is never touched; and only when the provider's
// purge steps find what they delete by the issue's number alone
// (purgeOwnsIssue: names made from {{issue.id}}, never from the consumer's
// name or from what a step found), else the user is told (leftDataNote).
// The same goes for an install
// that lost a service only one app may have to another install at the same
// time (CAT-INST-38): what it was issued before it lost is taken back and
// purged. A purge that does not work does not stop the rollback: the
// CAT-INST-35 line says what to delete by hand (rollbackPurgeWarning).
//
// What an app provides follows its data: deleted with its data, its services
// go, with the bindings that apps deleted before (data kept) still had to
// them (their credentials lived in the provider's data, gone now; the
// delete says that those apps have to be connected anew when they are
// installed again); deleted with its data kept, its services stay (the
// provider is gone: not usable) with those bindings, and an install on
// that data takes the same services over, items the user filled in by hand
// included. Either way it cannot be deleted while installed apps use them
// (ServiceUsers).
//
// Deleted with its data, a consumer's credential is taken back first
// (revoke) and then what the provider made for it is deleted (purge, when
// the entry has such steps); a purge that does not work is a CAT-INST-35
// warning too, saying what to delete by hand. Deleted with its data kept,
// neither runs.
//
// An entry written on_check_fail: stop does not have an old credential that
// fails the check replaced: the install stops (CAT-INST-37) and the old
// binding stays as it was; a new credential would be a new, empty database.
//
// Who may be plugged into a service (uses[].dedicated, the provider's
// max_consumers) is the rule of state.ServiceTakenError: matched against in
// needs.go, and checked again in the transaction that writes the binding,
// so two installs at once cannot both take a service only one may have
// (CAT-INST-38 for the one that comes second).
//
// A credential that cannot be taken back is a warning (CAT-INST-35) that
// says what to remove by hand: in the delete's warnings, or in the task's
// apps[].carried for an install (a rollback, a credential replaced). It
// never stops a delete or a rollback, and the binding goes anyway: the
// warning is then the only record of it.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// usesKey is where Values keep, during an install, the items of the service
// each need is plugged into ({need: {item: value}}); usesSecretKey which of
// them are secret ({"need.item": true}). Neither is saved.
const (
	usesKey       = "@uses"
	usesSecretKey = "@uses_secret"
)

// errNotConnected is the error of uses.<need>.<item> for a need that is not
// plugged into anything (an optional one this machine does not have). It is
// a catalog.ErrNoValue: a default transform stands in for it, a condition
// on it is false.
var errNotConnected error = noValueError("这个需要没有接上服务")

// noValueError is an error that counts as catalog.ErrNoValue.
type noValueError string

func (e noValueError) Error() string        { return string(e) }
func (e noValueError) Is(target error) bool { return target == catalog.ErrNoValue }

// lookupUse resolves "<need>.<item>". A need that is plugged in but has no
// value for the item gives "".
func (v Values) lookupUse(rest string) (string, error) {
	name, item, ok := strings.Cut(rest, ".")
	if !ok || name == "" || item == "" {
		return "", fmt.Errorf("uses 的引用要写成 uses.需要名.项名：uses.%s", rest)
	}
	m, _ := v[usesKey].(map[string]map[string]string)
	got, ok := m[name]
	if !ok {
		return "", fmt.Errorf("uses.%s：%w", rest, errNotConnected)
	}
	return got[item], nil
}

// useSecret reports whether a uses.<need>.<item> reference is a secret item.
func (v Values) useSecret(ref string) bool {
	rest, ok := strings.CutPrefix(ref, "uses.")
	if !ok {
		return false
	}
	m, _ := v[usesSecretKey].(map[string]bool)
	return m[rest]
}

// secretRefs is secretSettingRefs of m that also knows the secret items of
// the services the app's needs are plugged into.
func (p *planned) secretRefs() func(ref string) bool {
	base := secretSettingRefs(p.app.Manifest)
	return func(ref string) bool {
		if strings.HasPrefix(ref, "uses.") {
			return p.vals.useSecret(ref)
		}
		return base(ref)
	}
}

// BindingRef is a need this install plugged in (no credential in it): it is
// in the task record so a failed install, also after a restart, takes back
// exactly what it handed out. Issued: this install issued the credential;
// Reused: the binding was there before and is left as it is.
type BindingRef struct {
	Use     string `json:"use"`
	Service string `json:"service"`
	Issued  bool   `json:"issued,omitempty"`
	Reused  bool   `json:"reused,omitempty"`
	// IssueID is the number of the credential this install issued (with
	// Issued): a rollback purges what the provider made for the binding
	// only when the binding still holds that very credential. Tasks
	// recorded before it was there have none: nothing is purged for them,
	// the user is told instead (leftDataNote).
	IssueID string `json:"issue_id,omitempty"`
}

// IssuingRef is an issue this install started and that has not ended: the
// need, the service, the providing app and the issue's number. It goes into
// the task record before the provider's issue steps run and out again when
// they have ended (passed, or failed and what they made was dealt with).
type IssuingRef struct {
	Use      string `json:"use"`
	Service  string `json:"service"`
	Provider string `json:"provider"`
	IssueID  string `json:"issue_id"`
}

// ReplacedRef is a binding the app had from before this install (it was
// deleted with its data kept) that this install plugs into something else:
// another service, or a new credential of the same one. It is moved aside
// (state.ParkedUseName), recorded here first, so that the old binding and
// its credential stay as they were until the whole task has succeeded:
// a failed install, also after a restart, puts it back. Once the task has
// succeeded its credential is taken back, unless Keep: it did not pass the
// provider's check and stays there (the user may have turned it off on
// purpose), and the user is told to look.
type ReplacedRef struct {
	Use     string `json:"use"`
	Service string `json:"service"`
	Keep    bool   `json:"keep,omitempty"`
}

// providerConn is how to run a provided service's steps: the service, the
// providing app, the manifest it was installed with, the provides entry, its
// saved parameters, the address of its endpoint from this server (HTTP
// steps), its container (command steps) and which keys of a credential are
// secret.
type providerConn struct {
	svc    string
	app    state.App
	m      *catalog.Manifest
	pv     catalog.Provide
	vals   Values
	base   string
	exec   ExecTarget
	secret map[string]bool
}

// providerOf finds what sv's steps need. The provider must be installed and
// running. HTTP steps go to the endpoint the provides entry names (the
// app's main one when it names none): this server reaches an app through
// the one port it publishes on 127.0.0.1, so an entry on another port of
// the app cannot be reached from here and is said to be so. Command steps
// run in the provider's container: the one of its current deployment, as
// the store says (the app is sv's, from the registry; nothing else names
// it). An entry with command steps only needs no published port.
func (e *Engine) providerOf(ctx context.Context, sv Service) (providerConn, error) {
	if sv.Source != state.ServiceFromApp {
		return providerConn{}, fmt.Errorf("服务 %s 不是应用提供的", sv.Name)
	}
	prov, err := e.cfg.Store.GetApp(ctx, sv.App)
	if errors.Is(err, state.ErrNotFound) {
		return providerConn{}, fmt.Errorf("提供它的应用 %s 已经删了", sv.App)
	}
	if err != nil {
		return providerConn{}, err
	}
	if prov.Desired == state.DesiredStopped {
		return providerConn{}, fmt.Errorf("提供它的应用 %s 停着", sv.App)
	}
	m := e.appManifest(ctx, sv.App)
	if m == nil {
		return providerConn{}, fmt.Errorf("读不到 %s 安装时的清单", sv.App)
	}
	if sv.ProvideIndex < 0 || sv.ProvideIndex >= len(m.Provides) || m.Provides[sv.ProvideIndex].Kind != sv.Kind {
		return providerConn{}, fmt.Errorf("%s 的清单里没有登记的这条服务（第 %d 条 %s）", sv.App, sv.ProvideIndex+1, sv.Kind)
	}
	pv := m.Provides[sv.ProvideIndex]
	ep := primaryEndpoint(m)
	for i := range m.Endpoints {
		if pv.Endpoint != "" && m.Endpoints[i].Name == pv.Endpoint {
			ep = &m.Endpoints[i]
		}
	}
	if ep == nil {
		return providerConn{}, fmt.Errorf("%s 的清单里没有这条服务的入口 %s", sv.App, pv.Endpoint)
	}
	saved, err := e.savedParams(ctx, sv.App)
	if err != nil {
		return providerConn{}, fmt.Errorf("读不到 %s 保存的参数：%w", sv.App, err)
	}
	pc := providerConn{svc: sv.Name, app: prov, m: m, pv: pv, vals: Values(saved),
		secret: issuedSecretKeys(pv, m, e.planKinds()[sv.Kind])}
	if stepsSendRequests(pv.Issue, pv.Revoke, pv.Purge, pv.Check, pv.ReadySteps()) {
		base, err := e.endpointBase(ctx, prov, ep.Port)
		if err != nil {
			return providerConn{}, fmt.Errorf("提供它的应用 %s 的入口 %s：%w", sv.App, ep.Name, err)
		}
		pc.base = base
	}
	if stepsRunCommands(pv.Issue, pv.Revoke, pv.Purge, pv.Check, pv.ReadySteps()) {
		if prov.CurrentDeployment == "" {
			return providerConn{}, fmt.Errorf("提供它的应用 %s 还没有部署好的容器", sv.App)
		}
		pc.exec = ExecTarget{App: prov.Name, Container: runner.ContainerName(prov.Name, prov.CurrentDeployment)}
	}
	return pc, nil
}

// endpointBase is http://127.0.0.1:<host port> for container port port of
// app a's live container: the one port it publishes here must be that one.
func (e *Engine) endpointBase(ctx context.Context, a state.App, port int) (string, error) {
	cs, err := e.cfg.Runner.ListContainers(ctx, a.Name)
	if err != nil {
		return "", err
	}
	for _, c := range cs {
		if c.DeploymentID != a.CurrentDeployment || !c.Running || c.HostPort <= 0 {
			continue
		}
		published := c.Port
		if published == 0 {
			published = a.Port // a runner that does not say: the app's port
		}
		if published != port {
			return "", fmt.Errorf("它的容器端口 %d 在这台机器上没有映射出来（只映射了 %d），平台连不到这个入口", port, published)
		}
		return "http://127.0.0.1:" + strconv.Itoa(c.HostPort), nil
	}
	return "", errors.New("没有在运行的容器")
}

// credentialKeys are the keys of what pv's issue steps save (and of its
// issue_vars) that make up a credential: the keys the items written
// { issued: k } name, the keys item templates read as issued.<k> and the
// keys the revoke, purge and check steps (HTTP or command) read as
// issued.<k>, plus
// the issue number. Whatever else was for the steps of that one issue only
// (a login's session, say) and is not kept.
func credentialKeys(pv catalog.Provide) map[string]bool {
	keep := map[string]bool{catalog.IssueIDKey: true}
	for _, src := range pv.Values {
		if src.Kind() == catalog.ValueIssued {
			keep[src.Issued] = true
		}
		for _, k := range src.IssuedRefs() {
			keep[k] = true
		}
	}
	for _, steps := range [][]catalog.Step{pv.Revoke, pv.Purge, pv.Check} {
		for _, st := range steps {
			for _, ref := range actionRefs(stepTexts(st)) {
				if k, ok := strings.CutPrefix(ref, "issued."); ok {
					keep[k] = true
				}
			}
		}
	}
	return keep
}

// lookup resolves what the provider's steps refer to besides the step
// values: self.* (the provider on the app network: selfValues), params.*
// and platform.* (its settings).
func (pc providerConn) lookup(ref string) (string, error) {
	if strings.HasPrefix(ref, "self.") && pc.m != nil {
		if v, ok := selfValues(pc.m, pc.pv, pc.app.Name)[ref]; ok {
			return v, nil
		}
		return "", fmt.Errorf("没有 %s：只有 self.internal_url（http 入口）、self.host、self.port", ref)
	}
	return pc.vals.lookup(ref)
}

// env is what the provider's steps of group can read, for consumer ("":
// none, the readiness check), and where its command steps run.
func (pc providerConn) env(group, consumer string, issued map[string]string, issueID string) ActionEnv {
	env := ActionEnv{Lookup: pc.lookup, SecretRef: secretSettingRefs(pc.m), IssueID: issueID, Group: group, Exec: pc.exec}
	if consumer != "" {
		env.Consumer = &ActionConsumer{Name: consumer, ID: consumer}
	}
	if issued != nil {
		env.Issued = make(map[string]SavedValue, len(issued))
		for k, v := range issued {
			env.Issued[k] = SavedValue{Value: v, Secret: pc.secret[k]}
		}
	}
	return env
}

// issuedSecretKeys are the keys of a credential that are secret, by the one
// rule lint uses too (catalog.Provide.IssuedSecretKeys): generated issue
// variables and those made of a secret setting, values an issue step saves as
// secret, and keys behind a secret item written { issued: k } (secret in the
// manifest or in the service type t).
func issuedSecretKeys(pv catalog.Provide, m *catalog.Manifest, t catalog.ServiceType) map[string]bool {
	var secretRef func(string) bool
	if m != nil {
		secretRef = secretSettingRefs(m)
	}
	return pv.IssuedSecretKeys(secretRef, &t)
}

// issueVars works out the entry's issue_vars for one issue: a template with
// the issue number, the consumer and the provider's settings in it; a
// generated one drawn now (crypto/rand). Secret: generated ones, and those
// the one rule says (pc.secret). With generate false, generated ones are
// left out (taking back an issue cut short: they are lost, the templates can
// be worked out again). Its error names the variable, never a value.
func (pc providerConn) issueVars(consumer, id string, generate bool) (map[string]SavedValue, error) {
	if len(pc.pv.IssueVars) == 0 {
		return nil, nil
	}
	out := make(map[string]SavedValue, len(pc.pv.IssueVars))
	for _, k := range pc.pv.IssueVarKeys() {
		iv := pc.pv.IssueVars[k]
		if iv.Generated() {
			if !generate {
				continue
			}
			v, err := generateSecret(iv.Generate.Length, iv.Generate.Charset)
			if err != nil {
				return nil, fmt.Errorf("issue_vars.%s 生成失败：%w", k, err)
			}
			out[k] = SavedValue{Value: v, Secret: true}
			continue
		}
		v, err := catalog.RenderTemplate(iv.Template, func(ref string) (string, error) {
			switch ref {
			case catalog.IssueIDRef:
				return id, nil
			case "consumer.name", "consumer.id":
				return consumer, nil
			}
			return pc.vals.lookup(ref)
		}, nil)
		if err != nil {
			return nil, fmt.Errorf("issue_vars.%s 的模板：%w", k, err)
		}
		out[k] = SavedValue{Value: v, Secret: iv.Secret || pc.secret[k]}
	}
	return out, nil
}

func flatCredential(c map[string]SavedValue) map[string]string {
	out := make(map[string]string, len(c))
	for k, v := range c {
		out[k] = v.Value
	}
	return out
}

// issue runs the issue steps for consumer with issue number id and returns
// the credential (only its keys that are used later, credentialKeys). When
// a step fails (unless none of them reached the provider: ActionError's
// NothingDone), the revoke steps and then the purge steps (when the entry
// has them) are run with what the earlier steps saved and the number
// (issued.issue_id), which is enough for steps that find what was made by
// its name: what was made half-way does not stay behind.
// When they do not work, or the entry has none, the CAT-INST-35 warning for
// it comes back with the error: whether something was left cannot be told.
func (e *Engine) issue(ctx context.Context, pc providerConn, consumer, id string) (map[string]string, string, error) {
	vars, err := pc.issueVars(consumer, id, true)
	if err != nil {
		// Nothing was done in the provider yet.
		return nil, "", &ActionError{Kind: ActionTemplate, Detail: err.Error(), err: err}
	}
	env := pc.env(GroupIssue, consumer, nil, id)
	env.IssueVars = vars
	saved, err := e.actions().run(ctx, pc.base, pc.pv.Issue, env)
	if err != nil {
		var ae *ActionError
		if errors.As(err, &ae) && ae.NothingDone() {
			// No step reached the provider (refused before sending, or the
			// first one never ran): nothing was made, nothing to take back.
			return nil, "", err
		}
		warning := ""
		// The revoke steps find what was made by the names the issue_vars
		// gave it, or by what the steps saved until then.
		half := flatCredential(credentialOf(saved, vars, id))
		rerr := e.runRevoke(ctx, pc, consumer, half)
		if rerr != nil {
			e.cfg.Log.Warn("take back a half-issued credential", "provider", pc.app.Name, "consumer", consumer, "err", rerr)
		}
		// Made by this issue (number id, fresh): what it made half-way (a
		// database, say) goes too, also when taking it back did not work;
		// only when the purge steps find it by the number (purgeOwnsIssue).
		var perr error
		owned := purgeOwnsIssue(pc.pv)
		if owned {
			perr = e.runPurge(ctx, pc, consumer, half)
		}
		if perr != nil {
			e.cfg.Log.Warn("delete what a half-issued credential made", "provider", pc.app.Name, "consumer", consumer, "err", perr)
		}
		switch {
		case rerr != nil || perr != nil:
			if rerr == nil {
				rerr = perr
			}
			warning = halfIssuedWarning(consumer, pc.app.Name, id, rerr)
		case !owned && len(pc.pv.Purge) > 0:
			warning = leftDataNote(consumer, "", pc.app.Name, half, leftNotOwned)
		}
		return nil, warning, err
	}
	keep := credentialKeys(pc.pv)
	out := map[string]string{}
	for k, v := range flatCredential(credentialOf(saved, vars, id)) {
		if keep[k] {
			out[k] = v
		}
	}
	return out, "", nil
}

// errNoRevoke is the error of taking back a credential of a provides entry
// that has no revoke steps: nothing can take it back but the user.
var errNoRevoke = errors.New("提供它的应用的清单没有写收回凭据的步骤")

// runRevoke runs the revoke steps with what was issued; nothing to do when
// nothing was. An entry without revoke steps cannot take it back
// (errNoRevoke): the callers say so, never silently.
func (e *Engine) runRevoke(ctx context.Context, pc providerConn, consumer string, issued map[string]string) error {
	if len(issued) == 0 {
		return nil
	}
	if len(pc.pv.Revoke) == 0 {
		return errNoRevoke
	}
	_, err := e.actions().run(ctx, pc.base, pc.pv.Revoke, pc.env(GroupRevoke, consumer, issued, ""))
	return err
}

// stillGood reports whether an old credential passes the check steps. An
// entry without check steps cannot tell: false.
func (e *Engine) stillGood(ctx context.Context, pc providerConn, consumer string, issued map[string]string) bool {
	return e.checkOld(ctx, pc, consumer, issued) == nil
}

// errNoCheck: the entry has no check steps, or there is no credential to
// check.
var errNoCheck = errors.New("提供它的应用的清单没有写核对凭据的步骤")

// checkOld runs the check steps on an old credential: nil when they pass,
// otherwise why not (an *ActionError: no secret in it).
func (e *Engine) checkOld(ctx context.Context, pc providerConn, consumer string, issued map[string]string) error {
	if len(pc.pv.Check) == 0 || len(issued) == 0 {
		return errNoCheck
	}
	_, err := e.actions().run(ctx, pc.base, pc.pv.Check, pc.env(GroupCheck, consumer, issued, ""))
	return err
}

// errMissingKeys: an old credential lacks a key the entry hands out now.
var errMissingKeys = errors.New("以前发的凭据里缺了提供方现在要发的项（提供方换了清单版本）")

// hasIssuedKeys reports whether an old credential has every key the items
// handed out per consumer read now (written { issued: k }, or a template
// that reads issued.<k>): a provider installed again with another manifest
// may hand out more, and an old credential without it would put an empty
// value where one is needed.
func hasIssuedKeys(pv catalog.Provide, issued map[string]string) bool {
	for _, src := range pv.Values {
		keys := src.IssuedRefs()
		if src.Kind() == catalog.ValueIssued {
			keys = append(keys, src.Issued)
		}
		for _, k := range keys {
			if _, ok := issued[k]; !ok {
				return false
			}
		}
	}
	return true
}

// revokeBinding takes back what b holds. A user's service, or a binding
// that holds nothing, has nothing to take back.
func (e *Engine) revokeBinding(ctx context.Context, reg *Registry, b Binding) error {
	if len(b.Issued) == 0 {
		return nil
	}
	sv, err := reg.GetService(ctx, b.Service)
	if errors.Is(err, state.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if sv.Source != state.ServiceFromApp {
		return nil
	}
	pc, err := e.providerOf(ctx, sv)
	if err != nil {
		return err
	}
	return e.runRevoke(ctx, pc, b.App, b.Issued)
}

// purgeBinding deletes what the provider made for the consumer of b (its
// database, say), with the purge steps of the entry, once b's credential
// was taken back. Nothing to do for a user's service, a binding that holds
// nothing, a service that is gone or an entry without purge steps.
func (e *Engine) purgeBinding(ctx context.Context, reg *Registry, b Binding) error {
	if len(b.Issued) == 0 {
		return nil
	}
	sv, err := reg.GetService(ctx, b.Service)
	if errors.Is(err, state.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if sv.Source != state.ServiceFromApp {
		return nil
	}
	m := e.appManifest(ctx, sv.App)
	if m != nil && sv.ProvideIndex >= 0 && sv.ProvideIndex < len(m.Provides) && len(m.Provides[sv.ProvideIndex].Purge) == 0 {
		return nil
	}
	pc, err := e.providerOf(ctx, sv)
	if err != nil {
		return err
	}
	return e.runPurge(ctx, pc, b.App, b.Issued)
}

// runPurge runs the purge steps of pc's entry with what was issued to
// consumer: nothing to do when the entry has none or nothing was issued.
func (e *Engine) runPurge(ctx context.Context, pc providerConn, consumer string, issued map[string]string) error {
	if len(issued) == 0 || len(pc.pv.Purge) == 0 {
		return nil
	}
	_, err := e.actions().run(ctx, pc.base, pc.pv.Purge, pc.env(GroupPurge, consumer, issued, ""))
	return err
}

// purgeEntry is the entry service sv comes from, by the manifest its
// provider was installed with, and whether it has purge steps (it makes
// data per consumer: a database, say).
func (e *Engine) purgeEntry(ctx context.Context, sv Service) (catalog.Provide, bool) {
	if sv.Source != state.ServiceFromApp {
		return catalog.Provide{}, false
	}
	m := e.appManifest(ctx, sv.App)
	if m == nil || sv.ProvideIndex < 0 || sv.ProvideIndex >= len(m.Provides) || len(m.Provides[sv.ProvideIndex].Purge) == 0 {
		return catalog.Provide{}, false
	}
	return m.Provides[sv.ProvideIndex], true
}

// purgeOwnsIssue reports whether pv's purge steps find what they delete
// only by what one issue made under its own number, so that a failed
// install's rollback may run them: every issued.<k> they read is the number
// itself (issue_id), an issue variable written as a template with
// {{issue.id}} in it, or a generated one (a password: it names nothing);
// at least one of the first two; and they read neither consumer.* nor a key
// an issue step saved (that may be something the step found, not made).
// Otherwise what they would delete may be data that was there before the
// install (a database named after the consumer, say, which an issue step
// that makes it only "if not there yet" took over): a rollback then only
// takes the credential back and tells the user (leftDataNote). Deleting a
// consumer with its data (purgeBinding) does not ask this: there the user
// said the data goes.
func purgeOwnsIssue(pv catalog.Provide) bool {
	var refs []string
	for _, st := range pv.Purge {
		refs = append(refs, actionRefs(stepTexts(st))...)
	}
	anchored := false
	for _, ref := range refs {
		if strings.HasPrefix(ref, "consumer.") {
			return false
		}
		k, ok := strings.CutPrefix(ref, "issued.")
		if !ok {
			continue // self.*, params.*, platform.*, saved.* of the purge steps themselves
		}
		if k == catalog.IssueIDKey {
			anchored = true
			continue
		}
		iv, ok := pv.IssueVars[k]
		switch {
		case !ok:
			return false
		case iv.Generated():
		case slices.Contains(actionRefs([]any{iv.Template}), catalog.IssueIDRef):
			anchored = true
		default:
			return false
		}
	}
	return anchored
}

// Why a rollback left what the provider made for a failed install
// (leftDataNote).
const (
	// leftNoRecord: the task record (written by a version before it had
	// the issue number) does not say which credential the install issued.
	leftNoRecord = "这次安装的记录里没有写明是哪一份"
	// leftNotOwned: the provider's purge steps do not find what they
	// delete by the issue's number alone (purgeOwnsIssue).
	leftNotOwned = "提供方清单里删数据的步骤不是只按这次发凭据的编号找要删的东西"
)

// errPurgeNotOwned is said for an interrupted issue whose provider's purge
// steps the platform does not run on a rollback (purgeOwnsIssue).
var errPurgeNotOwned = errors.New(leftNotOwned + "，平台没有删")

// leftDataNote is the CAT-INST-35 line for what provider may have made for
// consumer with a credential of a failed install, which the rollback did
// not delete (why: leftNoRecord or leftNotOwned): the platform cannot be
// sure it is this install's, so the credential is taken back (or said to
// be not) and the data left. The user is told where, never silently left
// with it, and looks before deleting.
func leftDataNote(consumer, service, provider string, issued map[string]string, why string) string {
	which := ""
	if id := issued[catalog.IssueIDKey]; id != "" {
		which = "（名字里带编号 " + id + "）"
	}
	where := provider
	if where == "" {
		where = "提供服务 " + service + " 的应用"
	}
	return fmt.Sprintf("%s %s 没装成，%s 里给它建的库或数据%s没有删（%s，平台不能确认是这次新建的，所以只收回了凭据）：请到 %s 里看一下，确认是这次新建的、不要了再手工删掉；不删的话它会一直占着空间", CodeRevoke, consumer, where, which, why, where)
}

// rollbackPurgeWarning is the CAT-INST-35 line for what provider made for
// consumer with a credential this failed install had just issued, which
// the rollback tried to delete with the entry's purge steps and could not
// (err): the rollback went on; the user deletes it by hand.
func rollbackPurgeWarning(consumer, service, provider string, issued map[string]string, err error) string {
	which := ""
	if id := issued[catalog.IssueIDKey]; id != "" {
		which = "（名字里带编号 " + id + "）"
	}
	where := provider
	if where == "" {
		where = "提供服务 " + service + " 的应用"
	}
	return fmt.Sprintf("%s %s 没装成，平台想删掉 %s 这次给它新建的库或数据%s，没删成（%v）：里面只有这次安装写进去的东西，请到 %s 里手工删掉；不删的话它会一直占着空间", CodeRevoke, consumer, where, which, err, where)
}

// purgeWarning is the CAT-INST-35 warning for the data provider made for
// consumer that could not be deleted when the consumer was deleted with its
// data: the user deletes it by hand.
func purgeWarning(consumer, service, provider string, issued map[string]string, err error) string {
	which := ""
	if id := issued[catalog.IssueIDKey]; id != "" {
		which = "（名字里带编号 " + id + "）"
	}
	where := provider
	if where == "" {
		where = "提供服务 " + service + " 的应用"
	}
	return fmt.Sprintf("%s %s 连数据一起删了，但没能删掉 %s 里给它建的数据（%v）：请到 %s 里手工删掉给 %s 建的库或数据%s；不删的话它会一直占着空间", CodeRevoke, consumer, where, err, where, consumer, which)
}

// revokeWarning is the CAT-INST-35 warning for a credential of consumer in
// the service's provider that could not be taken back.
func revokeWarning(consumer, service, provider string, issued map[string]string, err error) string {
	which := ""
	if id := issued[catalog.IssueIDKey]; id != "" {
		which = "（名字或备注里带编号 " + id + "）"
	}
	where := provider
	if where == "" {
		where = "提供服务 " + service + " 的应用"
	}
	return fmt.Sprintf("%s 没能收回发给 %s 的凭据（%v）：请到 %s 里手工删掉给 %s 建的那一项%s", CodeRevoke, consumer, err, where, consumer, which)
}

// halfIssuedWarning is the CAT-INST-35 warning for issue steps that failed
// half-way and whose revoke steps did not work either: whether something was
// made for consumer in the provider cannot be told, so the user looks.
func halfIssuedWarning(consumer, provider, issueID string, err error) string {
	return fmt.Sprintf("%s %s 给 %s 发凭据发到一半失败了，平台没能确认 %s 里有没有留下建了一半的那一项（%v）：请到 %s 里看一下，有给 %s 建的、名字或备注里带编号 %s 的那一项就手工删掉", CodeRevoke, provider, consumer, provider, err, provider, consumer, issueID)
}

// checkFailedNote is the line (a CAT-INST-35 one: the user must hear it)
// for an old credential that did not pass the provider's check steps and was
// replaced by a new one, left where it is in the provider.
func checkFailedNote(consumer, use, provider string, old map[string]string) string {
	which := ""
	if id := old[catalog.IssueIDKey]; id != "" {
		which = "（名字或备注里带编号 " + id + "）"
	}
	return fmt.Sprintf("%s %s 的“%s”：以前 %s 发给它的那份凭据没通过检查，已经换了一份新的；旧的那份%s没有删，要不要删请到 %s 里看", CodeRevoke, consumer, use, provider, which, provider)
}

// kindItems is the items of kind t: defaults filled in for the ones with no
// value, and which are secret.
func kindItems(t catalog.ServiceType, values map[string]string) (map[string]string, map[string]bool) {
	out := make(map[string]string, len(values))
	for k, v := range values {
		out[k] = v
	}
	secret := map[string]bool{}
	for _, f := range t.Fields {
		if f.Secret {
			secret[f.Key] = true
		}
		if out[f.Key] == "" {
			if d, ok := f.DefaultText(); ok {
				out[f.Key] = d
			}
		}
	}
	return out, secret
}

// connectNeeds plugs every matched need of p into its service: writes the
// binding, issues a credential where the provider hands them out, and puts
// the items into p.vals (usesKey). It returns the providing apps whose
// network p's container joins. What it does is recorded in the task first
// (AppResult.Bindings), so a failure takes it back.
func (t *taskRun) connectNeeds(ctx context.Context, p *planned) ([]string, error) {
	if len(p.needs) == 0 {
		return nil, nil
	}
	e := t.e
	reg, err := e.Registry()
	if err != nil {
		return nil, fault(CodeInternal, err, "读不了服务登记表")
	}
	kinds := e.planKinds()
	items := map[string]map[string]string{}
	secrets := map[string]bool{}
	var links []string
	for _, n := range p.needs {
		svName := n.service
		switch {
		case n.status == NeedOptionalMissing:
			continue
		case n.status != NeedMatched:
			return nil, fault(CodeInternal, nil, "%s 的“%s”没有匹配上服务（%s）", p.name, n.use.Name, n.status)
		case n.from != nil:
			// Registered by an earlier app of this recipe, just now.
			name, err := e.providedService(ctx, reg, n.from.name, n.fromIndex)
			if err != nil {
				return nil, err
			}
			svName = name
		}
		sv, err := reg.GetService(ctx, svName)
		if err != nil {
			return nil, fault(CodeBadUse, err, "%s 的“%s”要接的服务 %s 读不到了", p.name, n.use.Name, svName)
		}
		// Usable when the request was accepted; it may have changed since.
		if h := e.serviceHealth(ctx, sv, kinds); !h.Usable() {
			return nil, fault(CodeBadUse, nil, "%s 的“%s”要接的服务 %s 现在不能用（%s）", p.name, n.use.Name, svName, h.StatusLabel)
		}
		vals, secret, link, carried, err := t.plugIn(ctx, reg, p, n, sv, kinds[n.use.Kind])
		if err != nil {
			return nil, err
		}
		if link != "" && !contains(links, link) {
			links = append(links, link)
		}
		p.carried = append(p.carried, carried...)
		items[n.use.Name] = vals
		for k := range secret {
			secrets[n.use.Name+"."+k] = true
		}
	}
	p.vals[usesKey] = items
	p.vals[usesSecretKey] = secrets
	return links, nil
}

// providedService is the name of the service app registered for entry
// index of its provides.
func (e *Engine) providedService(ctx context.Context, reg *Registry, app string, index int) (string, error) {
	svs, err := reg.ListServices(ctx, state.ServiceFilter{App: app})
	if err != nil {
		return "", fault(CodeInternal, err, "读服务登记表失败")
	}
	for _, sv := range svs {
		if sv.Source == state.ServiceFromApp && sv.ProvideIndex == index {
			return sv.Name, nil
		}
	}
	return "", fault(CodeInternal, nil, "%s 装好了，但登记表里没有它提供的第 %d 条服务", app, index+1)
}

// plugIn connects need n of p to sv and returns the items p gets, which of
// them are secret, the provider app to link to ("" for a user's service) and
// lines for the task's carried.
func (t *taskRun) plugIn(ctx context.Context, reg *Registry, p *planned, n *needMatch, sv Service, kind catalog.ServiceType) (map[string]string, map[string]bool, string, []string, error) {
	e := t.e
	vals, secret := kindItems(kind, sv.Values)
	what := fmt.Sprintf("%s 的“%s”", p.name, n.use.Name)
	old, oerr := reg.GetBinding(ctx, p.name, n.use.Name)
	haveOld := oerr == nil
	// carried: lines for the task (CAT-INST-35 warnings among them). On a
	// failure they go to p.carried, which the failed create step puts into
	// the task record.
	var carried []string
	failing := func(f *Fault) (map[string]string, map[string]bool, string, []string, error) {
		p.carried = append(p.carried, carried...)
		return nil, nil, "", nil, f
	}
	// park moves the old binding aside until the task ends (ReplacedRef):
	// put back as it was when the install fails; once it has succeeded, what
	// it holds is taken back, or (keep: it did not pass the check) left in
	// the provider with a word to the user, and it goes (finishReplaced).
	park := func(keep bool) *Fault {
		if !haveOld {
			return nil
		}
		if f := t.clearParked(ctx, reg, p.name, n.use.Name); f != nil {
			return f
		}
		ref := ReplacedRef{Use: n.use.Name, Service: old.Service, Keep: keep}
		t.setApp(p.name, func(r *AppResult) { r.Replaced = append(r.Replaced, ref) })
		t.save(ctx)
		if err := reg.ParkBinding(ctx, p.name, n.use.Name); err != nil {
			return fault(CodeInternal, err, "把 %s 以前接的服务 %s 先放到一边失败", what, old.Service)
		}
		haveOld = false
		return nil
	}
	record := func(ref BindingRef) {
		t.setApp(p.name, func(r *AppResult) { r.Bindings = append(r.Bindings, ref) })
		t.save(ctx)
	}
	// reused: plugged into the same service as before, and given the same
	// thing (its credential, when it has one, passed the check).
	reused := func() {
		if p.reusedUses == nil {
			p.reusedUses = map[string]bool{}
		}
		p.reusedUses[n.use.Name] = true
	}
	// The same service, and nothing issued to take back: the binding stays.
	same := haveOld && old.Service == sv.Name && len(old.Issued) == 0
	if sv.Source != state.ServiceFromApp {
		if !same {
			if f := park(false); f != nil {
				return failing(f)
			}
		}
		if _, err := reg.SetBinding(ctx, Binding{App: p.name, UseName: n.use.Name, Service: sv.Name, Dedicated: n.use.Dedicated}); err != nil {
			return failing(bindFault(what, sv.Name, err))
		}
		record(BindingRef{Use: n.use.Name, Service: sv.Name, Reused: same})
		if same {
			reused()
		}
		return vals, secret, "", carried, nil
	}
	pc, err := e.providerOf(ctx, sv)
	if err != nil {
		return failing(fault(CodeIssue, err, "%s 要接 %s 提供的服务 %s：%v", what, sv.App, sv.Name, err))
	}
	for item, src := range pc.pv.Values {
		// Secret in the manifest, or what an issue step saved as secret (or
		// a template made of such a value: lint wants it marked anyway).
		if src.Secret || (src.Kind() == catalog.ValueIssued && pc.secret[src.Issued]) {
			secret[item] = true
		}
		for _, k := range src.IssuedRefs() {
			if pc.secret[k] {
				secret[item] = true
			}
		}
	}
	if len(pc.pv.Issue) == 0 {
		if !same {
			if f := park(false); f != nil {
				return failing(f)
			}
		}
		if _, err := reg.SetBinding(ctx, Binding{App: p.name, UseName: n.use.Name, Service: sv.Name, Dedicated: n.use.Dedicated, MaxConsumers: pc.pv.MaxConsumers}); err != nil {
			return failing(bindFault(what, sv.Name, err))
		}
		record(BindingRef{Use: n.use.Name, Service: sv.Name, Reused: same})
		if same {
			reused()
		}
		return vals, secret, sv.App, carried, nil
	}
	var cred map[string]string
	sameService := haveOld && old.Service == sv.Name && len(old.Issued) > 0
	if sameService {
		// What the old app was given, should a new credential replace it.
		was := make(map[string]string, len(vals))
		for k, v := range vals {
			was[k] = v
		}
		for item, v := range pc.perConsumer(old.Issued) {
			was[item] = v
		}
		if p.oldUses == nil {
			p.oldUses = map[string]map[string]string{}
		}
		p.oldUses[n.use.Name] = was
	}
	var checkErr error
	if sameService && len(pc.pv.Check) > 0 {
		checkErr = errMissingKeys
		if hasIssuedKeys(pc.pv, old.Issued) {
			checkErr = e.checkOld(ctx, pc, p.name, old.Issued)
		}
	}
	switch {
	case sameService && len(pc.pv.Check) > 0 && checkErr == nil:
		cred = old.Issued
		if old.Dedicated != n.use.Dedicated {
			// The manifest changed whether the need takes the service for
			// itself: the binding says so now (under the same rule).
			if _, err := reg.SetBinding(ctx, Binding{App: p.name, UseName: n.use.Name, Service: sv.Name, Issued: old.Issued, Dedicated: n.use.Dedicated, MaxConsumers: pc.pv.MaxConsumers}); err != nil {
				return failing(bindFault(what, sv.Name, err))
			}
		}
		record(BindingRef{Use: n.use.Name, Service: sv.Name, Reused: true})
		reused()
		carried = append(carried, fmt.Sprintf("%s 接着用以前 %s 发给它的凭据（检查过还能用）", what, sv.App))
	case sameService && len(pc.pv.Check) > 0 && pc.pv.CheckFailAction() == catalog.OnCheckFailStop:
		// The provider holds the consumer's data under that credential: a
		// new one would be a new, empty place. Stop; the old binding stays
		// as it was (it was not moved aside), and the user decides.
		return failing(checkStopFault(what, n.use.Name, sv, checkErr, false))
	case sameService && len(pc.pv.Check) > 0:
		// It did not pass (or lacks a key the provider hands out now): a
		// new one is issued, the old one stays in the provider (the user may
		// have turned it off on purpose) and, once the install has
		// succeeded, the user is told.
		if f := park(true); f != nil {
			return failing(f)
		}
	default:
		// Another service, or a provider that cannot check it: taken back
		// once the install has succeeded.
		if f := park(false); f != nil {
			return failing(f)
		}
	}
	if cred == nil {
		// Who is plugged in already may leave no room (another app holds the
		// service for itself, the provider's limit is reached): said before
		// anything is issued. Writing the binding checks again.
		if err := reg.CheckPlug(ctx, Binding{App: p.name, UseName: n.use.Name, Service: sv.Name, Dedicated: n.use.Dedicated, MaxConsumers: pc.pv.MaxConsumers}); err != nil {
			return failing(bindFault(what, sv.Name, err))
		}
		id := newIssueID()
		ref := IssuingRef{Use: n.use.Name, Service: sv.Name, Provider: sv.App, IssueID: id}
		t.setApp(p.name, func(r *AppResult) { r.Issuing = append(r.Issuing, ref) })
		t.save(ctx)
		got, warning, err := e.issue(ctx, pc, p.name, id)
		// Over, either way: what it made was dealt with (or is said below).
		t.setApp(p.name, func(r *AppResult) { r.Issuing = dropIssuing(r.Issuing, id) })
		t.save(ctx)
		if warning != "" {
			carried = append(carried, warning)
		}
		if err != nil {
			return failing(issueFault(what, sv, err))
		}
		if _, err := reg.SetBinding(ctx, Binding{App: p.name, UseName: n.use.Name, Service: sv.Name, Issued: got, Dedicated: n.use.Dedicated, MaxConsumers: pc.pv.MaxConsumers}); err != nil {
			// Not recorded anywhere: take it back now (another install took
			// the service meanwhile, or the store failed).
			if rerr := e.runRevoke(ctx, pc, p.name, got); rerr != nil {
				e.cfg.Log.Warn("take back a credential not recorded", "app", p.name, "service", sv.Name, "err", rerr)
				carried = append(carried, revokeWarning(p.name, sv.Name, sv.App, got, rerr))
			}
			// got was issued just now, by this install (number id): what
			// the provider made for it goes too (tried also when taking it
			// back did not work), when the purge steps find it by the
			// number (purgeOwnsIssue).
			switch {
			case purgeOwnsIssue(pc.pv):
				if perr := e.runPurge(ctx, pc, p.name, got); perr != nil {
					e.cfg.Log.Warn("delete what was made for a credential not recorded", "app", p.name, "service", sv.Name, "err", perr)
					carried = append(carried, rollbackPurgeWarning(p.name, sv.Name, sv.App, got, perr))
				}
			case len(pc.pv.Purge) > 0:
				carried = append(carried, leftDataNote(p.name, sv.Name, sv.App, got, leftNotOwned))
			}
			var taken *state.ServiceTakenError
			if errors.As(err, &taken) {
				return failing(bindFault(what, sv.Name, err))
			}
			return failing(fault(CodeInternal, err, "记下 %s 拿到的凭据失败", what))
		}
		record(BindingRef{Use: n.use.Name, Service: sv.Name, Issued: true, IssueID: id})
		cred = got
	}
	for item, v := range pc.perConsumer(cred) {
		vals[item] = v
	}
	return vals, secret, sv.App, carried, nil
}

// perConsumer works out the items pc's entry hands each consumer from its
// credential: an item written { issued: k } is the credential's k; a
// template that reads issued.<k> (a connection string of the consumer's own,
// say) is filled in with it, self.*, params.* and platform.* as for the
// service's other items. A template that cannot be filled in (a setting the
// provider no longer has) is left empty, as an issued key it lacks is; one
// is left out when pc has no manifest.
func (pc providerConn) perConsumer(cred map[string]string) map[string]string {
	out := map[string]string{}
	for _, item := range pc.pv.ValueKeys() {
		src := pc.pv.Values[item]
		switch {
		case src.Kind() == catalog.ValueIssued:
			out[item] = cred[src.Issued]
		case src.PerConsumer() && pc.m != nil:
			v, err := providedTemplate(pc.m, pc.pv, pc.app.Name, pc.vals, cred, src.Template)
			if err != nil {
				v = ""
			}
			out[item] = v
		}
	}
	return out
}

// dropIssuing is list without the issue numbered id.
func dropIssuing(list []IssuingRef, id string) []IssuingRef {
	out := list[:0]
	for _, r := range list {
		if r.IssueID != id {
			out = append(out, r)
		}
	}
	return out
}

// interruptedIssueWarning is the CAT-INST-35 line for an issue that was
// under way when the server stopped and that the revoke steps, run with its
// number alone, did not clear (err): whether it made something in the
// provider cannot be told, so the user looks.
func interruptedIssueWarning(consumer string, r IssuingRef, err error) string {
	return fmt.Sprintf("%s %s 正在给 %s 发凭据（服务 %s）时安装中断了，平台没能确认 %s 里有没有留下建了一半的那一项（%v）：请到 %s 里看一下，有给 %s 建的、名字或备注里带编号 %s 的那一项就手工删掉",
		CodeRevoke, r.Provider, consumer, r.Service, r.Provider, err, r.Provider, consumer, r.IssueID)
}

// bindFault is the fault of writing (or checking) a binding: CAT-INST-38
// when the apps plugged into the service already leave no room for it
// (state.ServiceTakenError), CAT-INST-20 otherwise.
func bindFault(what, service string, err error) *Fault {
	var taken *state.ServiceTakenError
	if errors.As(err, &taken) {
		return fault(CodeTaken, nil, "%s 接不了服务 %s：%s（可能是同时在装的别的应用先接上了）。acornfox install 应用 --plan 看一次现在能接哪条", what, service, takenWhy(taken, nil))
	}
	return fault(CodeInternal, err, "记下 %s 接的服务失败", what)
}

// checkStopFault is CAT-INST-37: the old credential of need use, in a
// service whose provider wrote on_check_fail: stop, did not pass the check
// (why: checkOld's error, or errMissingKeys); nothing new was issued. plan:
// said by a plan, before anything is installed.
func checkStopFault(what, use string, sv Service, why error, plan bool) *Fault {
	reason := "核对没通过"
	if why != nil {
		reason = why.Error()
	}
	outcome := "所以这次安装停下了，什么都没换，原来的接入原样留着"
	if plan {
		outcome = "所以照现在的样子装会停下、不会换新的（这是看计划时先核对的，什么都没装、没改）"
	}
	return fault(CodeCheckStop, nil, "%s 以前从服务 %s 拿到的凭据这次核对没通过（%s）。提供它的应用 %s 的清单写了核对不过就停下、不自动换一份（换一份等于接到一个空的新库，原来的数据就接不上了），%s。先看 %s 是否正常（acornfox status %s，acornfox logs %s --tail 200）、原来的数据还在不在；提供方好了再装一次。确定不要原来的数据、要改接别的服务时，问过用户后加 --use %s=服务名 再装",
		what, sv.Name, reason, sv.App, outcome, sv.App, sv.App, sv.App, use)
}

// issueFault is CAT-INST-34 for issue steps that did not pass (CAT-INST-20
// when the runner is too old to run the provider's commands: upgrading is
// the way out, not the provider).
func issueFault(what string, sv Service, err error) *Fault {
	var ae *ActionError
	if errors.As(err, &ae) {
		code := CodeIssue
		if ae.Kind == ActionRunnerOld {
			code = CodeInternal
		}
		return fault(code, nil, "%s 要 %s 发一个凭据，%s 没给（%s）", what, sv.App, sv.App, ae.Error())
	}
	return fault(CodeIssue, nil, "%s 要 %s 发一个凭据，%s 没给（%v）", what, sv.App, sv.App, err)
}

// registerProvides puts the services p provides (the new way) into the
// registry, once p is up and initialized. An entry that has a service
// already (the app was deleted with its data kept and is installed again)
// keeps it, with the bindings to it: its items are worked out anew; items
// the user filled in by hand ({ ask: … }) stay when the install is on the
// old data (they describe something done in that data).
func (t *taskRun) registerProvides(ctx context.Context, p *planned) error {
	m := p.app.Manifest
	if len(m.Provides) == 0 {
		return nil
	}
	kinds := make([]string, len(m.Provides))
	for i, pv := range m.Provides {
		kinds[i] = pv.Kind
	}
	e := t.e
	reg, err := e.Registry()
	if err != nil {
		return fault(CodeInternal, err, "读不了服务登记表")
	}
	names, err := reg.AppServiceNames(ctx, p.name, kinds)
	if err != nil {
		return fault(CodeInternal, err, "给 %s 提供的服务起名字失败：%v", p.name, err)
	}
	for i, pv := range m.Provides {
		values, err := provideValues(m, pv, p.name, p.vals)
		if err != nil {
			return fault(CodeBadParams, err, "%s 提供的服务 %s：%v", p.name, pv.Kind, err)
		}
		if old, err := reg.GetService(ctx, names[i]); err == nil && old.Source == state.ServiceFromApp && old.App == p.name && old.ProvideIndex == i {
			if _, err := reg.UpdateService(ctx, names[i], func(sv *Service) error {
				for k, src := range pv.Values {
					if p.reuseData && src.Kind() == catalog.ValueAsk && sv.Values[k] != "" {
						values[k] = sv.Values[k]
					}
				}
				sv.Values = values
				return nil
			}); err != nil {
				return fault(CodeInternal, err, "更新服务 %s 失败", names[i])
			}
			continue
		}
		// Recorded before it is made: removing one that was never made is
		// harmless, leaving one behind is not.
		t.setApp(p.name, func(r *AppResult) { r.Services = append(r.Services, names[i]) })
		t.save(ctx)
		if _, err := reg.CreateService(ctx, Service{Name: names[i], Kind: pv.Kind, Source: state.ServiceFromApp, App: p.name, ProvideIndex: i, Values: values}); err != nil {
			return fault(CodeInternal, err, "登记 %s 提供的服务 %s 失败", p.name, names[i])
		}
	}
	return nil
}

// provideValues works out the items of a provides entry that are written as
// templates (self.*, params.*, platform.*). The others are not stored, nor
// are templates that read issued.<k>: those are each consumer's own
// (providerConn.perConsumer).
func provideValues(m *catalog.Manifest, pv catalog.Provide, app string, vals Values) (map[string]string, error) {
	out := map[string]string{}
	for _, k := range pv.ValueKeys() {
		src := pv.Values[k]
		if src.Kind() != catalog.ValueTemplate || src.PerConsumer() {
			continue
		}
		v, err := providedTemplate(m, pv, app, vals, nil, src.Template)
		if err != nil {
			return nil, fmt.Errorf("%s 的模板：%w", k, err)
		}
		out[k] = v
	}
	return out, nil
}

// selfValues are the self.* of provides entry pv of app: how another
// container reaches its endpoint (the app's main one when it names none):
// self.host (the app's name on the app network), self.port and, for an
// http endpoint, self.internal_url (http://<host>:<port>, no trailing
// slash). The same in values and in the entry's revoke, purge, check and
// ready steps.
func selfValues(m *catalog.Manifest, pv catalog.Provide, app string) map[string]string {
	port, proto := 0, ""
	if ep := primaryEndpoint(m); ep != nil {
		port, proto = ep.Port, ep.Protocol
	}
	for _, ep := range m.Endpoints {
		if pv.Endpoint != "" && ep.Name == pv.Endpoint {
			port, proto = ep.Port, ep.Protocol
		}
	}
	self := map[string]string{
		"self.internal_url": "http://" + app + ":" + strconv.Itoa(port),
		"self.host":         app,
		"self.port":         strconv.Itoa(port),
	}
	if proto == catalog.ProtocolTCP {
		// A tcp entry has no http:// address (lint refuses it too).
		delete(self, "self.internal_url")
	}
	return self
}

// providedTemplate fills in a template item of a provides entry of app:
// self.internal_url (http://<app>:<port of the entry's endpoint>; not for a
// tcp entry, which has no http:// address), self.host, self.port,
// issued.<k> from issued (a consumer's credential; nil when there is none),
// and params.* and platform.* from vals.
func providedTemplate(m *catalog.Manifest, pv catalog.Provide, app string, vals Values, issued map[string]string, tmpl string) (string, error) {
	self := selfValues(m, pv, app)
	return catalog.RenderTemplate(tmpl, func(ref string) (string, error) {
		if s, ok := self[ref]; ok {
			return s, nil
		}
		if k, ok := strings.CutPrefix(ref, "issued."); ok {
			s, have := issued[k]
			if !have {
				return "", fmt.Errorf("凭据里没有 %s", k)
			}
			return s, nil
		}
		return vals.lookup(ref)
	}, nil)
}

// releaseCreated takes back, for a failed install, what this task handed out
// to app r and registered for it: credentials it issued (revoke steps) and
// the bindings it wrote, then the services it registered. Bindings that were
// there before are left alone.
func (t *taskRun) releaseCreated(ctx context.Context, r AppResult) {
	if len(r.Bindings) == 0 && len(r.Services) == 0 && len(r.Issuing) == 0 && len(r.Replaced) == 0 {
		return
	}
	e := t.e
	reg, err := e.Registry()
	if err != nil {
		e.cfg.Log.Error("registry on cleanup", "app", r.App, "err", err)
		for _, is := range r.Issuing {
			t.warnApp(ctx, r.App, interruptedIssueWarning(r.App, is, err))
		}
		return
	}
	// An issue still under way here was cut short by a restart: what its
	// steps saved is lost, but the revoke steps can be tried with its
	// number.
	for _, is := range r.Issuing {
		if err := e.revokeInterrupted(ctx, reg, r.App, is); err != nil {
			e.cfg.Log.Warn("take back an interrupted issue", "app", r.App, "service", is.Service, "err", err)
			t.warnApp(ctx, r.App, interruptedIssueWarning(r.App, is, err))
		}
	}
	for _, ref := range r.Bindings {
		if ref.Reused {
			continue
		}
		b, err := reg.GetBinding(ctx, r.App, ref.Use)
		if errors.Is(err, state.ErrNotFound) {
			continue
		}
		if err != nil {
			e.cfg.Log.Error("read binding on cleanup", "app", r.App, "use", ref.Use, "err", err)
			continue
		}
		if ref.Issued {
			sv, _ := reg.GetService(ctx, b.Service)
			if err := e.revokeBinding(ctx, reg, b); err != nil {
				e.cfg.Log.Warn("take back a credential of a failed install", "app", r.App, "use", ref.Use, "service", b.Service, "err", err)
				t.warnApp(ctx, r.App, revokeWarning(r.App, b.Service, sv.App, b.Issued, err))
			}
			// What the provider made for it (a new database, empty but for
			// what this install wrote) goes too, but only when the binding
			// holds the very credential the task record says this install
			// issued and the purge steps find it by the number
			// (purgeOwnsIssue); tried also when taking it back did not work.
			if pv, ok := e.purgeEntry(ctx, sv); ok {
				switch {
				case ref.IssueID == "" || b.Issued[catalog.IssueIDKey] != ref.IssueID:
					t.warnApp(ctx, r.App, leftDataNote(r.App, b.Service, sv.App, b.Issued, leftNoRecord))
				case !purgeOwnsIssue(pv):
					t.warnApp(ctx, r.App, leftDataNote(r.App, b.Service, sv.App, b.Issued, leftNotOwned))
				default:
					if err := e.purgeBinding(ctx, reg, b); err != nil {
						e.cfg.Log.Warn("delete what was made for a failed install", "app", r.App, "use", ref.Use, "service", b.Service, "err", err)
						t.warnApp(ctx, r.App, rollbackPurgeWarning(r.App, b.Service, sv.App, b.Issued, err))
					}
				}
			}
		}
		if _, err := reg.DeleteBinding(ctx, r.App, ref.Use); err != nil {
			e.cfg.Log.Error("delete binding on cleanup", "app", r.App, "use", ref.Use, "err", err)
		}
	}
	// What the app had from before goes back as it was.
	for _, ref := range r.Replaced {
		err := reg.UnparkBinding(ctx, r.App, ref.Use)
		if err == nil || errors.Is(err, state.ErrNotFound) { // not moved yet when the task stopped
			continue
		}
		e.cfg.Log.Error("put back an old binding on cleanup", "app", r.App, "use", ref.Use, "err", err)
		t.warnApp(ctx, r.App, fmt.Sprintf("%s 的“%s”以前接的服务 %s 没能恢复原样（%v）：装回来时会重新接服务", r.App, ref.Use, ref.Service, err))
	}
	for _, name := range r.Services {
		if err := reg.DeleteService(ctx, name); err != nil && !errors.Is(err, state.ErrNotFound) {
			e.cfg.Log.Error("delete service on cleanup", "app", r.App, "service", name, "err", err)
		}
	}
}

// revokeInterrupted runs, for an issue cut short by a restart, the revoke
// steps of its provider with the issue number alone, then its purge steps
// (the issue was this install's: whatever it made is deleted). Its error is
// the first that did not work.
func (e *Engine) revokeInterrupted(ctx context.Context, reg *Registry, app string, is IssuingRef) error {
	sv, err := reg.GetService(ctx, is.Service)
	if err != nil {
		return err
	}
	pc, err := e.providerOf(ctx, sv)
	if err != nil {
		return err
	}
	// What the steps saved is lost; the issue_vars written as templates
	// (names made of the issue number, say) can be worked out again.
	issued := map[string]string{}
	if vars, err := pc.issueVars(app, is.IssueID, false); err == nil {
		issued = flatCredential(vars)
	}
	issued[catalog.IssueIDKey] = is.IssueID
	rerr := e.runRevoke(ctx, pc, app, issued)
	// It was this install's (the task recorded its number before the
	// steps ran): what it may have made goes too, also when taking it back
	// did not work, when the purge steps find it by the number
	// (purgeOwnsIssue); otherwise the user is told to look.
	if len(pc.pv.Purge) > 0 && !purgeOwnsIssue(pc.pv) {
		if rerr == nil {
			return errPurgeNotOwned
		}
		return rerr
	}
	if perr := e.runPurge(ctx, pc, app, issued); perr != nil && rerr == nil {
		return perr
	}
	return rerr
}

// clearParked removes what an earlier install of app left moved aside for
// need use (it stopped between moving it and dealing with it): taken back
// first, or said when that does not work. Nothing to do as a rule.
func (t *taskRun) clearParked(ctx context.Context, reg *Registry, app, use string) *Fault {
	b, err := reg.GetBinding(ctx, app, state.ParkedUseName(use))
	if errors.Is(err, state.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fault(CodeInternal, err, "读 %s 以前留下的接入失败", app)
	}
	if note := t.e.letGo(ctx, reg, app, b); note != "" {
		t.warnApp(ctx, app, note)
	}
	if _, err := reg.DeleteBinding(ctx, app, b.UseName); err != nil {
		return fault(CodeInternal, err, "删 %s 以前留下的接入失败", app)
	}
	return nil
}

// letGo takes back what binding b of app holds and returns the CAT-INST-35
// warning when that did not work ("" when it did, or there was nothing).
func (e *Engine) letGo(ctx context.Context, reg *Registry, app string, b Binding) string {
	if err := e.revokeBinding(ctx, reg, b); err != nil {
		e.cfg.Log.Warn("take back a credential", "app", app, "use", b.UseName, "service", b.Service, "err", err)
		sv, _ := reg.GetService(ctx, b.Service)
		return revokeWarning(app, b.Service, sv.App, b.Issued, err)
	}
	return ""
}

// finishReplaced deals, once the whole task has succeeded, with the
// bindings app p had from before that this install moved aside
// (ReplacedRef): the credential is taken back (or the user told why not),
// or, for one that did not pass the check, left in the provider with a word
// to the user; then the binding goes.
func (t *taskRun) finishReplaced(ctx context.Context, p *planned) {
	var refs []ReplacedRef
	for _, r := range t.snapshot().Apps {
		if r.App == p.name {
			refs = r.Replaced
		}
	}
	if len(refs) == 0 {
		return
	}
	e := t.e
	reg, err := e.Registry()
	if err != nil {
		return
	}
	for _, ref := range refs {
		parked := state.ParkedUseName(ref.Use)
		b, err := reg.GetBinding(ctx, p.name, parked)
		if errors.Is(err, state.ErrNotFound) {
			continue
		}
		if err != nil {
			e.cfg.Log.Warn("read an old binding after install", "app", p.name, "use", ref.Use, "err", err)
			continue
		}
		what := fmt.Sprintf("%s 的“%s”", p.name, ref.Use)
		switch {
		case ref.Keep:
			sv, _ := reg.GetService(ctx, b.Service)
			t.warnApp(ctx, p.name, checkFailedNote(p.name, ref.Use, sv.App, b.Issued))
		case len(b.Issued) > 0:
			if note := e.letGo(ctx, reg, p.name, b); note != "" {
				t.warnApp(ctx, p.name, note)
			} else {
				t.warnApp(ctx, p.name, fmt.Sprintf("%s 以前从服务 %s 拿到的凭据这次不用了，已经收回", what, b.Service))
			}
		}
		if _, err := reg.DeleteBinding(ctx, p.name, parked); err != nil {
			e.cfg.Log.Warn("delete an old binding after install", "app", p.name, "use", ref.Use, "err", err)
		}
	}
}

// warnApp adds a line (a CAT-INST-35 warning) to what the task says about
// app (apps[].carried), at once.
func (t *taskRun) warnApp(ctx context.Context, app, line string) {
	t.setApp(app, func(r *AppResult) {
		if !contains(r.Carried, line) {
			r.Carried = append(r.Carried, line)
		}
	})
	t.save(ctx)
}

// dropStaleBindings takes back and removes the bindings app p kept from
// before (deleted with its data kept) for needs this install did not plug
// in: an optional one whose service cannot be used now, or one the new
// manifest no longer has. Otherwise the app would show as plugged into a
// service it does not use, and keep that service and its provider from
// being deleted. It runs once the whole task has succeeded, so a rollback
// leaves them as they were.
func (t *taskRun) dropStaleBindings(ctx context.Context, p *planned) {
	e := t.e
	reg, err := e.Registry()
	if err != nil {
		return // a store without a registry has no bindings
	}
	bs, err := reg.ListBindings(ctx, state.BindingFilter{App: p.name})
	if err != nil {
		e.cfg.Log.Warn("read bindings after install", "app", p.name, "err", err)
		return
	}
	plugged := map[string]bool{}
	for _, n := range p.needs {
		if n.status == NeedMatched {
			plugged[n.use.Name] = true
		}
	}
	var notes []string
	for _, b := range bs {
		use := b.UseName
		if base, parked := state.ParkedUse(use); parked {
			// One finishReplaced has not removed: left by an install that
			// stopped half-way, or one it could not remove. Not used.
			use = base
		} else if plugged[use] {
			continue
		}
		if note := e.letGo(ctx, reg, p.name, b); note != "" {
			notes = append(notes, note)
		}
		if _, err := reg.DeleteBinding(ctx, p.name, b.UseName); err != nil {
			e.cfg.Log.Warn("delete a binding not used any more", "app", p.name, "use", b.UseName, "err", err)
			continue
		}
		notes = append(notes, fmt.Sprintf("%s 以前接的服务 %s（“%s”）这次没有接上，已经断开", p.name, b.Service, use))
	}
	if len(notes) == 0 {
		return
	}
	for _, n := range notes {
		t.warnApp(ctx, p.name, n)
	}
	t.noteStep(ctx, p.name, "init", strings.Join(notes, "；"))
}

// ServiceUser is an installed app plugged into a service another app
// provides.
type ServiceUser struct {
	App     string `json:"app"`
	Use     string `json:"use"`
	Service string `json:"service"`
}

// ServiceUsers lists the installed apps (other than app itself) plugged into
// the services app provides. While there are any, app must not be deleted:
// they would lose the service. Bindings of apps that are gone (deleted with
// their data kept) do not count.
func (e *Engine) ServiceUsers(ctx context.Context, app string) ([]ServiceUser, error) {
	reg, err := e.Registry()
	if err != nil {
		return nil, nil
	}
	svs, err := reg.ListServices(ctx, state.ServiceFilter{App: app})
	if err != nil || len(svs) == 0 {
		return nil, err
	}
	var out []ServiceUser
	for _, sv := range svs {
		bs, err := reg.ListBindings(ctx, state.BindingFilter{Service: sv.Name})
		if err != nil {
			return nil, err
		}
		for _, b := range bs {
			if b.App == app {
				continue
			}
			if _, err := e.cfg.Store.GetApp(ctx, b.App); err != nil {
				continue
			}
			out = append(out, ServiceUser{App: b.App, Use: b.UseName, Service: sv.Name})
		}
	}
	return out, nil
}

// ServiceUsersText is the refusal for deleting app while others use what it
// provides.
func ServiceUsersText(app string, users []ServiceUser) string {
	var parts []string
	for _, u := range users {
		parts = append(parts, fmt.Sprintf("%s（用它的服务 %s）", u.App, u.Service))
	}
	return fmt.Sprintf("应用 %s 提供的服务还有应用在用：%s。先删掉这些应用，或者让它们改用别的服务，再删 %s", app, strings.Join(parts, "、"), app)
}

// ReleaseServices runs once app has been deleted.
//
// What app was handed: deleted with its data, every credential is taken back
// and then, where the provider's entry has purge steps, what it made for
// app is deleted (warnings for what could not be, CAT-INST-35), and its
// bindings go;
// deleted with its data kept, they stay, so an install on that data finds
// them again (and checks the old credentials).
//
// What app provided (nobody installed uses it: ServiceUsers refused the
// delete otherwise): deleted with its data, its services go, with the
// bindings apps that are gone already (deleted with their data kept) still
// had to them: what was issued to them lived in the provider's data, which
// is gone, so there is nothing to take back; an install of theirs is matched
// anew. Deleted with its data kept, its services stay (provider gone: not
// usable) with those bindings: the provider's data still holds what was
// issued, and an install on that data takes the services over again
// (registerProvides), so a consumer installed on its own data again finds
// its credential and checks it.
func (e *Engine) ReleaseServices(ctx context.Context, app string, deleteData bool) []string {
	if !deleteData {
		return nil
	}
	reg, err := e.Registry()
	if err != nil {
		return nil
	}
	var warnings []string
	bs, err := reg.ListBindings(ctx, state.BindingFilter{App: app})
	if err != nil {
		e.cfg.Log.Warn("read bindings of a deleted app", "app", app, "err", err)
	}
	for _, b := range bs {
		if err := e.revokeBinding(ctx, reg, b); err != nil {
			sv, _ := reg.GetService(ctx, b.Service)
			warnings = append(warnings, revokeWarning(app, b.Service, sv.App, b.Issued, err))
		}
		// Deleted with its data: what the provider made for it goes too
		// (purge), after the credential was taken back. Tried also when
		// taking it back did not work: the user asked for the data to go.
		if err := e.purgeBinding(ctx, reg, b); err != nil {
			e.cfg.Log.Warn("delete a deleted consumer's data in its provider", "app", app, "use", b.UseName, "service", b.Service, "err", err)
			sv, _ := reg.GetService(ctx, b.Service)
			warnings = append(warnings, purgeWarning(app, b.Service, sv.App, b.Issued, err))
		}
		if _, err := reg.DeleteBinding(ctx, app, b.UseName); err != nil {
			e.cfg.Log.Warn("delete binding of a deleted app", "app", app, "use", b.UseName, "err", err)
		}
	}
	svs, err := reg.ListServices(ctx, state.ServiceFilter{App: app})
	if err != nil {
		e.cfg.Log.Warn("read services of a deleted app", "app", app, "err", err)
		return warnings
	}
	for _, sv := range svs {
		users, _ := reg.ListBindings(ctx, state.BindingFilter{Service: sv.Name})
		var gone []string
		for _, b := range users {
			if _, err := e.cfg.Store.GetApp(ctx, b.App); errors.Is(err, state.ErrNotFound) {
				if _, err := reg.DeleteBinding(ctx, b.App, b.UseName); err != nil {
					e.cfg.Log.Warn("delete binding of a deleted consumer", "app", b.App, "use", b.UseName, "err", err)
					continue
				}
				gone = append(gone, b.App)
			}
		}
		if len(gone) > 0 {
			warnings = append(warnings, goneConsumersNote(sv.Name, gone))
		}
	}
	if _, err := reg.DeleteAppServices(ctx, app); err != nil {
		e.cfg.Log.Warn("delete services of a deleted app", "app", app, "err", err)
		warnings = append(warnings, fmt.Sprintf("%s 提供的服务没能从登记表里删掉（%v）：acornfox services list 看一下，用不上的用 acornfox services remove 删", app, err))
	}
	return warnings
}

// goneConsumersNote says that the bindings apps deleted with their data
// kept had to a service that is going away went with it: installed again,
// those apps have to be connected to a service anew. Nothing is revoked:
// when the provider's data went too, so did what it issued; when it is
// kept, keptDataWarning says what to remove there.
func goneConsumersNote(service string, apps []string) string {
	return fmt.Sprintf("服务 %s 删了，以前接它的 %s（已经删了、数据还留着）的接入也一并删了：这些应用以后装回来时要重新接服务", service, strings.Join(dedupe(apps), "、"))
}

// keptDataWarning is the CAT-INST-35 warning for a credential provider
// issued to consumer, both deleted with their data kept, whose binding goes
// with the service: the credential is still in the provider's data, and
// nothing will take it back once the provider is installed on that data
// again.
func keptDataWarning(consumer, service, provider string, issued map[string]string) string {
	which := ""
	if id := issued[catalog.IssueIDKey]; id != "" {
		which = "（名字或备注里带编号 " + id + "）"
	}
	return fmt.Sprintf("%s 服务 %s 删了，但 %s 发给 %s 的凭据还在 %s 留下的数据里：以后用这份数据把 %s 装回来时，请到 %s 里手工删掉给 %s 建的那一项%s",
		CodeRevoke, service, provider, consumer, provider, provider, provider, consumer, which)
}

func dedupe(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// AppServiceUse is one need of an installed app (written the new way) and
// the service it is plugged into ("" when none: an optional one this machine
// did not have). Status is the service's, or not_connected. It is the
// status without the readiness check (never not_ready): an app's detail is
// read often and must not wait for its providers' checks; whether a service
// can really be used now is in the services view (GET /v1/services), which
// runs them.
type AppServiceUse struct {
	Use         string `json:"use"`
	Kind        string `json:"kind"`
	KindLabel   string `json:"kind_label"`
	Required    bool   `json:"required"`
	Service     string `json:"service"`
	Status      string `json:"status"`
	StatusLabel string `json:"status_label"`
}

// Status of a need that is not plugged into any service.
const (
	UseNotConnected      = "not_connected"
	useNotConnectedLabel = "没接上"
)

// AppServices lists the needs of app (written the new way) and what each is
// plugged into. nil when app has none (or was not installed from the
// catalog).
func (e *Engine) AppServices(ctx context.Context, app string) ([]AppServiceUse, error) {
	m := e.appManifest(ctx, app)
	if m == nil {
		return nil, nil
	}
	var out []AppServiceUse
	reg, err := e.Registry()
	if err != nil {
		return nil, err
	}
	kinds := e.planKinds()
	a := &assessment{kinds: kinds}
	for _, u := range m.Uses {
		v := AppServiceUse{Use: u.Name, Kind: u.Kind, KindLabel: a.kindLabel(u.Kind), Required: !u.Optional, Status: UseNotConnected, StatusLabel: useNotConnectedLabel}
		if b, err := reg.GetBinding(ctx, app, u.Name); err == nil {
			v.Service = b.Service
			if sv, err := reg.GetService(ctx, b.Service); err == nil {
				h := e.serviceHealth(ctx, sv, kinds)
				v.Status, v.StatusLabel = string(h.Status), h.StatusLabel
			} else {
				v.Status, v.StatusLabel = string(ServiceProviderGone), "这条服务已经不在了"
			}
		}
		out = append(out, v)
	}
	return out, nil
}
