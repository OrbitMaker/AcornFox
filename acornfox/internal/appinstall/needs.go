package appinstall

// Matching the services an app needs (its manifest's uses) with what this
// machine has, before anything is installed, and the plan of an install.
//
// Apps do not depend on each other: the platform never decides on its own
// that another app has to be installed first, never looks in the catalog for
// an app that could provide a service, and never installs one. Apps are
// installed together only when the user installs a published bundle (a
// recipe), which lists them in order and says which is wired to which.
//
// The rule (matchNeed), the same for the plan and for the install itself:
//
//  1. The request names the service for this need ("use": {"<need>": …} or
//     {"<app>.<need>": …}): it must exist, be of the kind and be usable now
//     (CAT-INST-33 otherwise). The need is matched.
//  2. A recipe link (from, to, uses: the need's name) wires the need to the
//     service of that kind the "from" app provides.
//  3. The candidates are the usable services of the kind on this machine
//     (status ok) and, in a recipe, the services the apps installed earlier
//     by the same recipe will register (they do not exist yet when the plan
//     is made: their names are worked out the way registration names them).
//     Exactly one: it is used. Several: the one this app was plugged into
//     before (a binding kept when it was deleted with its data) is used,
//     otherwise the user chooses (choose, CAT-INST-32).
//  4. None: an optional need is skipped (optional_missing); a required one
//     stops the install (required_missing, CAT-INST-31), and the user is told
//     to register one they already have (acornfox services add …).
//
// Who is plugged in already counts at every step (the rule of
// state.ServiceTakenError, the one the store checks again when it writes
// the binding): a service another app holds for itself (uses[].dedicated),
// one this need wants for itself while another app uses it, and one whose
// provider's max_consumers is reached are not candidates; they are listed in
// the need's how with the reason, and a request that names one is refused
// (CAT-INST-38). An app the same request matched earlier counts as plugged
// in. The app's own bindings (it was deleted with its data kept) never
// count. A user's service is under the same rule (it has no limit of its
// own: max_consumers is a provider's).
//
// In a plan (not an install), once everything is matched (planChecks, all
// side by side), the readiness check (ready.go) of every matched service an
// app provides is run, once per service; one that is not ready does not
// stop anything: its provider's sentence goes into the tell of every need
// matched to it. And an app installed again on its data, whose kept binding
// is to the matched service of a provider that wrote on_check_fail: stop,
// has its old credential checked (read-only, as the install would): when it
// clearly does not pass, the plan says CAT-INST-37 before anything is
// installed; when it could not be done in time or the provider did not
// answer, the need's tell says the install checks again (precheckOld).
//
// Nothing here names a service type or an app: kinds, labels and the items
// of a kind come from the catalog's service type definitions.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

// What became of one need (PlanNeed.Status).
const (
	NeedMatched         = "matched"          // plugged into Service (or one an earlier app of the recipe registers)
	NeedOptionalMissing = "optional_missing" // optional, none here: skipped
	NeedRequiredMissing = "required_missing" // required, none here: the install cannot go on
	NeedChoose          = "choose"           // several usable: one of Candidates must be chosen
)

// needMatch is the outcome of matching one need of one planned app.
type needMatch struct {
	use     catalog.Use
	label   string
	status  string
	service string   // NeedMatched
	cands   []string // NeedChoose
	// from is, when the matched service does not exist yet, the app of the
	// same recipe, installed earlier in the same task, that registers it
	// (fromIndex: its provides entry).
	from      *planned
	fromIndex int
	how       []string
	// tell (NeedMatched): what the provider of the service wrote for the
	// user, the ask sentences of its provides entry, as they are (tellOf).
	tell []string
}

// svcState is one service of the registry with its status right now and
// its provider's limit on consumers (max_consumers; 0: none).
type svcState struct {
	sv     Service
	health ServiceHealth
	max    int
}

// assessment is what checking a request found out: the apps in install
// order and, for a plan, every problem instead of the first.
type assessment struct {
	req       Request
	kind      string
	target    string
	title     string // the app's or the recipe's name, for sentences
	plan      []*planned
	userSteps []string
	// collect: list every problem (a plan) instead of stopping at the first
	// (an install).
	collect  bool
	problems []*Fault
	missing  map[*planned][]ParamMissing

	kinds   map[string]catalog.ServiceType
	svcs    []svcState
	svcRead bool
	// bindings this machine has, by consuming app.
	bindings map[string][]Binding
	// usedPicks: the keys of req.Use that named a need.
	usedPicks map[string]bool
	// pendingNames: the names the services of the apps this request
	// installs will be registered under, by app (worked out once).
	pendingNames map[*planned][]string
	// claims: the needs of this request matched so far, by service: they
	// count as plugged in for the needs matched after them.
	claims map[string][]state.PlugBinding
	// ready (a plan): the readiness checks planChecks ran, by service name
	// (only the ones that ran), each service once.
	ready map[string]readyResult
}

// taken is why need u of p cannot be plugged into service svc (whose
// provider takes at most max consumers, 0: no limit) given who is plugged in
// already and what this request matched before it (state.PlugRule); nil
// when it can.
func (a *assessment) taken(p *planned, u catalog.Use, svc string, max int) *state.ServiceTakenError {
	var existing []state.PlugBinding
	for app, bs := range a.bindings {
		for _, b := range bs {
			if b.Service == svc {
				existing = append(existing, state.PlugBinding{App: app, Dedicated: b.Dedicated})
			}
		}
	}
	existing = append(existing, a.claims[svc]...)
	err := state.PlugRule(existing, state.ServiceBinding{App: p.name, UseName: u.Name, Service: svc, Dedicated: u.Dedicated, MaxConsumers: max})
	var t *state.ServiceTakenError
	if errors.As(err, &t) {
		return t
	}
	return nil
}

// claim records that need u of p was matched to svc.
func (a *assessment) claim(p *planned, u catalog.Use, svc string) {
	if a.claims == nil {
		a.claims = map[string][]state.PlugBinding{}
	}
	a.claims[svc] = append(a.claims[svc], state.PlugBinding{App: p.name, Dedicated: u.Dedicated})
}

// takenWhy says in the user's words why a service cannot take one more
// app (t). gone reports an app deleted with its data kept (nil: not said).
func takenWhy(t *state.ServiceTakenError, gone func(app string) bool) string {
	names := make([]string, 0, len(t.Apps))
	for _, app := range t.Apps {
		if gone != nil && gone(app) {
			app += "（已删、数据还留着，装回来时要接回它）"
		}
		names = append(names, app)
	}
	who := strings.Join(names, "、")
	switch t.Reason {
	case state.TakenHeld:
		return "已经被应用 " + who + " 独占（它的清单写了只接没有别的应用在用的服务），别的应用不能再接"
	case state.TakenInUse:
		return "已经有应用 " + who + " 在用，而这个需要要独占一条没有别的应用在用的服务"
	case state.TakenFull:
		return fmt.Sprintf("已经接满：提供它的应用只让同时接 %d 个，现在接着 %s", t.Max, who)
	}
	return "现在接不了（" + who + "）"
}

// goneApp reports whether app is deleted (with its data kept: it still has
// bindings).
func (e *Engine) goneApp(ctx context.Context) func(app string) bool {
	return func(app string) bool {
		_, err := e.cfg.Store.GetApp(ctx, app)
		return errors.Is(err, state.ErrNotFound)
	}
}

// providerMax is the max_consumers of the provides entry sv comes from (0:
// a user's service, no limit written, or the manifest cannot be read).
func (e *Engine) providerMax(ctx context.Context, sv Service) int {
	if sv.Source != state.ServiceFromApp {
		return 0
	}
	if m := e.appManifest(ctx, sv.App); m != nil && sv.ProvideIndex >= 0 && sv.ProvideIndex < len(m.Provides) {
		return m.Provides[sv.ProvideIndex].MaxConsumers
	}
	return 0
}

// fail records err; it reports true when the caller must stop (an install
// stops at the first problem).
func (a *assessment) fail(err error) bool {
	if !a.collect {
		return true
	}
	a.problems = append(a.problems, AsFault(err))
	return false
}

// kindLabel is the service type's label, or the kind itself when the catalog
// does not define it.
func (a *assessment) kindLabel(kind string) string {
	if t, ok := a.kinds[kind]; ok && t.Label != "" {
		return t.Label
	}
	return kind
}

// planKinds returns the service types the catalogs in use define, by kind
// (public catalog first; see catalog.KnownServiceTypes).
func (e *Engine) planKinds() map[string]catalog.ServiceType {
	out := map[string]catalog.ServiceType{}
	if e.cfg.Catalog == nil {
		return out
	}
	vs, err := e.cfg.Catalog.views()
	if err != nil {
		return out
	}
	idx := make([]*catalog.Index, 0, len(vs))
	for _, v := range vs {
		idx = append(idx, v.idx)
	}
	for _, t := range catalog.KnownServiceTypes(idx...) {
		out[t.Kind] = t
	}
	return out
}

// kindFields is what the registry needs to know about the items of a kind.
func kindFields(t catalog.ServiceType) []ServiceField {
	out := make([]ServiceField, 0, len(t.Fields))
	for _, f := range t.Fields {
		out = append(out, ServiceField{Key: f.Key, Required: f.Required, Secret: f.Secret})
	}
	return out
}

// issuedItems are the items of a provides entry handed out per consumer:
// written { issued: k }, or a template that reads issued.<k>.
func issuedItems(pv catalog.Provide) []string {
	var out []string
	for _, k := range pv.ValueKeys() {
		if pv.Values[k].PerConsumer() {
			out = append(out, k)
		}
	}
	return out
}

// appManifest is the manifest app was installed with, nil when none of the
// current format can be read: not from the catalog, or installed with a
// manifest of a former format (installedWith, see legacy.go: such an app
// takes no part in services).
func (e *Engine) appManifest(ctx context.Context, app string) *catalog.Manifest {
	m, _ := e.installedWith(ctx, app)
	return m
}

// serviceHealth works out the status of sv now: the providing app's row and
// the items it issues per consumer come from the store.
func (e *Engine) serviceHealth(ctx context.Context, sv Service, kinds map[string]catalog.ServiceType) ServiceHealth {
	var perConsumer []string
	var prov *state.App
	if sv.Source == state.ServiceFromApp {
		if a, err := e.cfg.Store.GetApp(ctx, sv.App); err == nil {
			prov = &a
		}
		if m := e.appManifest(ctx, sv.App); m != nil && sv.ProvideIndex < len(m.Provides) {
			perConsumer = issuedItems(m.Provides[sv.ProvideIndex])
		}
	}
	return ServiceStatusOf(sv, kindFields(kinds[sv.Kind]), perConsumer, ProviderStateOf(sv, prov))
}

// services reads the registry once per assessment.
func (e *Engine) loadServices(ctx context.Context, a *assessment) error {
	if a.svcRead {
		return nil
	}
	reg, err := e.Registry()
	if err != nil {
		return fault(CodeInternal, err, "读不了服务登记表")
	}
	svs, err := reg.ListServices(ctx, state.ServiceFilter{})
	if err != nil {
		return fault(CodeInternal, err, "读服务登记表失败：%v", err)
	}
	bs, err := reg.ListBindings(ctx, state.BindingFilter{})
	if err != nil {
		return fault(CodeInternal, err, "读服务接入记录失败：%v", err)
	}
	a.svcs = make([]svcState, 0, len(svs))
	for _, sv := range svs {
		a.svcs = append(a.svcs, svcState{sv: sv, health: e.serviceHealth(ctx, sv, a.kinds), max: e.providerMax(ctx, sv)})
	}
	a.bindings = map[string][]Binding{}
	for _, b := range bs {
		a.bindings[b.App] = append(a.bindings[b.App], b)
	}
	a.svcRead = true
	return nil
}

func (a *assessment) service(name string) *svcState {
	for i := range a.svcs {
		if a.svcs[i].sv.Name == name {
			return &a.svcs[i]
		}
	}
	return nil
}

// pickFor is the service the request names for need use of p, if any. A key
// "<app>.<need>" always counts; a bare "<need>" counts for the app of a
// one-app request, and in a recipe for the one app that has such a need.
func (a *assessment) pickFor(p *planned, use string) (string, bool) {
	if v, ok := a.req.Use[p.name+"."+use]; ok {
		a.usedPicks[p.name+"."+use] = true
		return strings.TrimSpace(v), true
	}
	v, ok := a.req.Use[use]
	if !ok {
		return "", false
	}
	if a.kind == "app" && p != a.plan[0] {
		return "", false
	}
	if a.kind == "recipe" && len(a.appsWithNeed(use)) != 1 {
		return "", false // ambiguous: badPick says so
	}
	a.usedPicks[use] = true
	return strings.TrimSpace(v), true
}

// appsWithNeed are the apps of the request (not installed already) whose
// manifest has a need named use.
func (a *assessment) appsWithNeed(use string) []*planned {
	var out []*planned
	for _, q := range a.plan {
		if q.reuse {
			continue
		}
		for _, u := range q.app.Manifest.Uses {
			if u.Name == use {
				out = append(out, q)
				break
			}
		}
	}
	return out
}

// badPick is the CAT-INST-33 for a key of the request's use that named no
// need it matched; nil when there is nothing to say: the app it names was
// not matched at all (a plan that refused its manifest already says why).
func (a *assessment) badPick(key string) *Fault {
	app, use, qualified := strings.Cut(key, ".")
	if !qualified {
		app, use = "", key
	}
	var have []*planned
	for _, q := range a.appsWithNeed(use) {
		if app == "" || q.name == app {
			have = append(have, q)
		}
	}
	switch {
	case !qualified && len(have) > 1:
		names := make([]string, 0, len(have))
		for _, q := range have {
			names = append(names, q.name+"."+use)
		}
		return fault(CodeBadUse, nil, "指定的 --use %s：这次要装的几个应用都有叫“%s”的需要，写成 %s 指明是哪一个", key, use, strings.Join(names, " 或 "))
	case len(have) == 1 && !have[0].needsMatched:
		return nil
	}
	return fault(CodeBadUse, nil, "指定的 --use %s：这次要装的应用里没有叫这个名字的需要（写成 应用名.需要名，只装一个应用时可以只写需要名；acornfox install 应用 --plan 看有哪些需要）", key)
}

// providesKind reports whether m provides kind, and at which entry.
func providesKind(m *catalog.Manifest, kind string) (int, bool) {
	for i, pv := range m.Provides {
		if pv.Kind == kind {
			return i, true
		}
	}
	return 0, false
}

// pendingService is a service an app of this request will register once it
// is installed (it does not exist yet).
type pendingService struct {
	app   *planned
	index int
	name  string
}

// max is the provides entry's max_consumers.
func (ps pendingService) max() int { return ps.app.app.Manifest.Provides[ps.index].MaxConsumers }

// coversRequired reports whether a provides entry will give every required
// item of kind t without the user doing a step (a template or an issued
// item, not ask): only then is the service usable as soon as it is there.
func coversRequired(pv catalog.Provide, t catalog.ServiceType) bool {
	for _, k := range t.RequiredKeys() {
		src, ok := pv.Values[k]
		if !ok || src.Kind() == catalog.ValueAsk {
			return false
		}
	}
	return true
}

// pendingName is the name the service of entry index of q will be
// registered under (the registry's naming, worked out now).
func (e *Engine) pendingName(ctx context.Context, a *assessment, q *planned, index int) (string, error) {
	if names, ok := a.pendingNames[q]; ok {
		return names[index], nil
	}
	reg, err := e.Registry()
	if err != nil {
		return "", fault(CodeInternal, err, "读不了服务登记表")
	}
	kinds := make([]string, len(q.app.Manifest.Provides))
	for i, pv := range q.app.Manifest.Provides {
		kinds[i] = pv.Kind
	}
	names, err := reg.AppServiceNames(ctx, q.name, kinds)
	if err != nil {
		return "", fault(CodeInternal, err, "给 %s 提供的服务起名字失败：%v", q.name, err)
	}
	if a.pendingNames == nil {
		a.pendingNames = map[*planned][]string{}
	}
	a.pendingNames[q] = names
	return names[index], nil
}

// earlierPending are the services of kind the apps of this request
// installed before p will register, usable as soon as they are there.
func (e *Engine) earlierPending(ctx context.Context, a *assessment, p *planned, kind string) ([]pendingService, error) {
	var out []pendingService
	for _, q := range a.plan {
		if q == p {
			break
		}
		if q.reuse {
			continue // installed already: its services are in the registry
		}
		i, ok := providesKind(q.app.Manifest, kind)
		if !ok || !coversRequired(q.app.Manifest.Provides[i], a.kinds[kind]) {
			continue
		}
		name, err := e.pendingName(ctx, a, q, i)
		if err != nil {
			return nil, err
		}
		out = append(out, pendingService{app: q, index: i, name: name})
	}
	return out, nil
}

// matchNeeds matches every need of p. An install stops at the first
// problem (returned); a plan records them all.
func (e *Engine) matchNeeds(ctx context.Context, a *assessment, p *planned) error {
	p.needsMatched = true
	for _, u := range p.app.Manifest.Uses {
		n := &needMatch{use: u, label: a.kindLabel(u.Kind)}
		p.needs = append(p.needs, n)
		if err := e.matchNeed(ctx, a, p, n); err != nil && a.fail(err) {
			return err
		}
		if n.status == NeedMatched {
			a.claim(p, u, n.service)
			n.tell = e.tellOf(ctx, a, p, n)
		}
	}
	return nil
}

// planChecks runs, for a plan, once every need of every app is matched, the
// read-only checks a plan does on the matched services, all of them side by
// side (each with its own time limit, readyTimeout), so a plan with many
// needs or a slow provider takes about one time limit, not one per need:
//
//   - the readiness check of every matched service an app provides whose
//     status is ok, once per service (a.ready keeps the outcome: every need
//     matched to a service that is not ready gets its sentence in tell, the
//     second one too);
//   - precheckOld for every need of an app installed again on its data.
//
// The outcomes go to the needs in plan order (problems and tell as before).
func (e *Engine) planChecks(ctx context.Context, a *assessment) {
	type job struct {
		p *planned
		n *needMatch
	}
	var jobs []job
	var svs []Service
	seen := map[string]bool{}
	for _, p := range a.plan {
		for _, n := range p.needs {
			if n.status != NeedMatched || n.from != nil {
				continue
			}
			jobs = append(jobs, job{p, n})
			if s := a.service(n.service); s != nil && s.health.Status == ServiceOK && !seen[n.service] {
				seen[n.service] = true
				svs = append(svs, s.sv)
			}
		}
	}
	if len(jobs) == 0 {
		return
	}
	pre := make([]precheckOutcome, len(jobs))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		a.ready = e.readyMany(ctx, svs)
	}()
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			pre[i] = e.precheckOld(ctx, a, j.p, j.n)
		}(i, j)
	}
	wg.Wait()
	for name, rr := range a.ready {
		if s := a.service(name); s != nil && !rr.ready {
			s.health.Status, s.health.StatusLabel = ServiceNotReady, ServiceNotReady.Label()
		}
	}
	for i, j := range jobs {
		if rr, ok := a.ready[j.n.service]; ok && !rr.ready {
			if t := readyTell(j.n.service, rr.say); !contains(j.n.tell, t) {
				j.n.tell = append(j.n.tell, t)
			}
		}
		if pre[i].tell != "" {
			j.n.tell = append(j.n.tell, pre[i].tell)
		}
		if pre[i].fault != nil {
			a.fail(pre[i].fault)
		}
	}
}

// precheckOutcome is what precheckOld found: fault, CAT-INST-37 when the
// check clearly did not pass; tell, a sentence for the user when it could
// not be done now (too slow, the provider not reachable for the moment):
// the install checks again, with its own retries and no such limit.
type precheckOutcome struct {
	fault *Fault
	tell  string
}

// errPrecheckLate: the plan's check of an old credential was not over in
// time (readyTimeout).
var errPrecheckLate = errors.New("late")

// passingMayFollow reports whether err, from checking an old credential,
// says nothing about the credential: the check did not get an answer from
// the provider in time or at all (an install, which waits and tries again,
// may well get through). A step that ran and answered no (a status, an exit
// code, an answer that does not match), or a credential that lacks a key,
// is a clear no.
func passingMayFollow(err error) bool {
	if errors.Is(err, errPrecheckLate) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var ae *ActionError
	if errors.As(err, &ae) {
		switch ae.Kind {
		case ActionNotSent, ActionNoAnswer, ActionCanceled, ActionTimeout, ActionNotRunning, ActionLimited:
			return true
		}
	}
	return false
}

// precheckOld checks, for a plan, the old credential of p's need n when p
// has a kept binding to the very service n is matched to and its provider
// wrote on_check_fail: stop: the install would stop on it (CAT-INST-37), so
// the plan says so first. Read-only (the check steps), one try, bounded
// like a readiness check. A check that clearly did not pass is CAT-INST-37;
// one that could not be done now (passingMayFollow) is only told: the
// install checks again (and stops then if it does not pass). Nothing when
// there is nothing to check or it passes. Safe to run for several needs at
// once (it only reads a).
func (e *Engine) precheckOld(ctx context.Context, a *assessment, p *planned, n *needMatch) precheckOutcome {
	if n.from != nil {
		return precheckOutcome{}
	}
	var old *Binding
	for i, b := range a.bindings[p.name] {
		if b.UseName == n.use.Name && b.Service == n.service && len(b.Issued) > 0 {
			old = &a.bindings[p.name][i]
		}
	}
	s := a.service(n.service)
	if old == nil || s == nil || s.sv.Source != state.ServiceFromApp || !s.health.Usable() {
		return precheckOutcome{}
	}
	pc, err := e.providerOf(ctx, s.sv)
	if err != nil || len(pc.pv.Check) == 0 || pc.pv.CheckFailAction() != catalog.OnCheckFailStop {
		return precheckOutcome{}
	}
	var why error = errMissingKeys
	if hasIssuedKeys(pc.pv, old.Issued) {
		cctx, cancel := context.WithTimeout(ctx, readyTimeout)
		defer cancel()
		r := e.actions()
		r.attempts = 1
		done := make(chan error, 1)
		go func() {
			_, err := r.run(cctx, pc.base, pc.pv.Check, pc.env(GroupCheck, p.name, old.Issued, ""))
			done <- err
		}()
		select {
		case why = <-done:
		case <-cctx.Done():
			why = errPrecheckLate
		}
	}
	if why == nil {
		return precheckOutcome{}
	}
	what := fmt.Sprintf("%s 的“%s”", p.name, n.use.Name)
	if passingMayFollow(why) {
		reason := why.Error()
		if errors.Is(why, errPrecheckLate) {
			reason = "在 " + waitText(readyTimeout) + "内没做完"
		}
		return precheckOutcome{tell: fmt.Sprintf("看计划时没能核对完 %s 以前从服务 %s 拿到的凭据（%s），这不算没通过：装的时候会再核对一次（会等它、会重试）；那时还核对不过，安装会停下（%s），不会换一份新的，原来的接入原样留着。先看 %s 是否正常：acornfox status %s",
			what, s.sv.Name, reason, CodeCheckStop, s.sv.App, s.sv.App)}
	}
	return precheckOutcome{fault: checkStopFault(what, n.use.Name, s.sv, why, true)}
}

// tellOf are the sentences for the user about the service n is matched to:
// one for each item of its provides entry written ask (the user does a step
// in that app and fills the item in) that is still empty and that p's
// manifest refers to (uses.<need>.<item>); an item p does not use is none
// of this install's business. In the order of the entry's keys, each once.
// None for a service the user registered. The same for every app and kind:
// the words come from the provider's manifest and the kind's definition.
func (e *Engine) tellOf(ctx context.Context, a *assessment, p *planned, n *needMatch) []string {
	var pv catalog.Provide
	var filled map[string]string // the values of the registered service
	switch {
	case n.from != nil:
		if n.fromIndex >= len(n.from.app.Manifest.Provides) {
			return nil
		}
		pv = n.from.app.Manifest.Provides[n.fromIndex]
	default:
		s := a.service(n.service)
		if s == nil || s.sv.Source != state.ServiceFromApp {
			return nil
		}
		m := e.appManifest(ctx, s.sv.App)
		if m == nil || s.sv.ProvideIndex >= len(m.Provides) {
			return nil
		}
		pv = m.Provides[s.sv.ProvideIndex]
		filled = s.sv.Values
	}
	used := catalog.UsedItems(p.app.Manifest, n.use.Name)
	kind := a.kinds[n.use.Kind]
	var out []string
	for _, k := range pv.ValueKeys() {
		src := pv.Values[k]
		if src.Kind() != catalog.ValueAsk || !used[k] || strings.TrimSpace(filled[k]) != "" {
			continue
		}
		ask := strings.TrimSpace(src.Ask)
		if ask == "" {
			continue
		}
		if t := askTell(n.service, k, kind, ask); !contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// askTell is the sentence for the user about item key of service svc that
// is still empty: which service and item, the provider's words as they
// are, and the command that fills it in (a secret item read from standard
// input, never on the command line).
func askTell(svc, key string, kind catalog.ServiceType, ask string) string {
	label, value := key, key+"=值"
	if f, ok := kind.Field(key); ok {
		if f.Label != "" {
			label = f.Label
		}
		if f.Secret {
			value = key + "=-"
		}
	}
	ask = strings.TrimRight(ask, "。.；; ")
	return fmt.Sprintf("服务 %s 的“%s”还没填：%s。填法：acornfox services set %s %s", svc, label, ask, svc, value)
}

// tell is what the providers of the services p's needs are matched to wrote
// for the user (tellOf), each sentence once.
func (p *planned) tell() []string {
	var out []string
	for _, n := range p.needs {
		for _, t := range n.tell {
			if !contains(out, t) {
				out = append(out, t)
			}
		}
	}
	return out
}

func (e *Engine) matchNeed(ctx context.Context, a *assessment, p *planned, n *needMatch) error {
	u := n.use
	what := fmt.Sprintf("%s 需要的“%s”（%s）", p.name, u.Name, n.label)
	gone := e.goneApp(ctx)
	if _, ok := a.kinds[u.Kind]; !ok {
		n.status = NeedRequiredMissing
		if u.Optional {
			n.status = NeedOptionalMissing
			return nil
		}
		return fault(CodeUnsupported, nil, "%s 是服务类型 %s，这台 AcornFox 的应用目录里没有它的定义：先 acornfox catalog sync，还不行就要先升级 AcornFox", what, u.Kind)
	}
	if err := e.loadServices(ctx, a); err != nil {
		return err
	}
	pend, err := e.earlierPending(ctx, a, p, u.Kind)
	if err != nil {
		return err
	}
	if pick, ok := a.pickFor(p, u.Name); ok {
		for _, ps := range pend {
			if ps.name == pick {
				if t := a.taken(p, u, ps.name, ps.max()); t != nil {
					n.status = NeedRequiredMissing
					return fault(CodeTaken, nil, "%s 指定用服务 %s（由这次先装的 %s 提供），但它%s", what, pick, ps.app.name, takenWhy(t, gone))
				}
				n.status, n.service, n.from, n.fromIndex = NeedMatched, ps.name, ps.app, ps.index
				return nil
			}
		}
		s := a.service(pick)
		switch {
		case s == nil:
			n.status = NeedRequiredMissing
			return fault(CodeBadUse, nil, "%s 指定用服务 %s，这台机器上没有叫这个名字的服务（acornfox services list 看有哪些）", what, pick)
		case s.sv.Kind != u.Kind:
			n.status = NeedRequiredMissing
			return fault(CodeBadUse, nil, "%s 指定用服务 %s，它是%s，不是%s", what, pick, a.kindLabel(s.sv.Kind), n.label)
		case !s.health.Usable():
			n.status = NeedRequiredMissing
			return fault(CodeBadUse, nil, "%s 指定用服务 %s，它现在不能用（%s）", what, pick, s.health.StatusLabel)
		}
		if t := a.taken(p, u, pick, s.max); t != nil {
			n.status = NeedRequiredMissing
			return fault(CodeTaken, nil, "%s 指定用服务 %s，但它%s。acornfox services list 看每条服务被谁占着；有别的同类服务就改用它，没有就登记一条你已有的", what, pick, takenWhy(t, gone))
		}
		n.status, n.service = NeedMatched, pick
		return nil
	}
	if q := p.wired[u.Name]; q != nil {
		return e.matchWired(ctx, a, p, n, q, pend)
	}
	var usable []string
	for _, s := range a.svcs {
		if s.sv.Kind != u.Kind {
			continue
		}
		if !s.health.Usable() {
			n.how = append(n.how, unusableHow(s, a.kinds[s.sv.Kind])...)
			continue
		}
		if t := a.taken(p, u, s.sv.Name, s.max); t != nil {
			n.how = append(n.how, fmt.Sprintf("服务 %s %s", s.sv.Name, takenWhy(t, gone)))
			continue
		}
		usable = append(usable, s.sv.Name)
	}
	cands := append([]string(nil), usable...)
	var pendOK []pendingService
	for _, ps := range pend {
		if t := a.taken(p, u, ps.name, ps.max()); t != nil {
			n.how = append(n.how, fmt.Sprintf("这次先装的 %s 提供的服务 %s %s", ps.app.name, ps.name, takenWhy(t, gone)))
			continue
		}
		pendOK = append(pendOK, ps)
		cands = append(cands, ps.name)
	}
	pend = pendOK
	switch {
	case len(cands) == 1 && len(pend) == 1:
		n.status, n.service, n.from, n.fromIndex, n.how = NeedMatched, pend[0].name, pend[0].app, pend[0].index, nil
		return nil
	case len(cands) == 1:
		n.status, n.service, n.how = NeedMatched, usable[0], nil
		return nil
	case len(cands) > 1:
		for _, b := range a.bindings[p.name] {
			if b.UseName == u.Name && contains(usable, b.Service) {
				n.status, n.service, n.how = NeedMatched, b.Service, nil
				return nil
			}
		}
		sort.Strings(cands)
		n.status, n.cands = NeedChoose, cands
		n.how = []string{fmt.Sprintf("加 --use %s.%s=服务名 选一条（只装这一个应用时可以写 --use %s=服务名）", p.name, u.Name, u.Name)}
		return fault(CodeChooseService, nil, "%s 这台机器上有 %d 条能用：%s，要选一条：安装时加 --use %s=服务名（页面上在“需要的服务”里选）", what, len(cands), strings.Join(cands, "、"), u.Name)
	}
	if u.Optional {
		n.status = NeedOptionalMissing
		return nil
	}
	n.status = NeedRequiredMissing
	how := registerHow(a.kinds[u.Kind])
	// One there that cannot be used now is said first, with how it becomes
	// usable (starting its provider, filling in an item): no app is named
	// to be installed.
	there := ""
	if len(n.how) > 0 {
		there = "；这台机器上有，但现在不能用：" + strings.Join(n.how, "；")
	}
	n.how = append(n.how, how)
	return fault(CodeNoService, nil, "必须用到的服务“%s”这台机器上没有能用的，先登记一条你已有的：%s%s；登记好以后先运行 acornfox services test 名字，通了再看一次计划%s", n.label, how, registerNote(a.kinds[u.Kind]), there)
}

// matchWired matches a need a recipe link wires to app q of the same
// recipe: q's service of the kind (one that exists when q is installed
// already, the one it will register otherwise).
func (e *Engine) matchWired(ctx context.Context, a *assessment, p *planned, n *needMatch, q *planned, pend []pendingService) error {
	what := fmt.Sprintf("%s 需要的“%s”（%s）", p.name, n.use.Name, n.label)
	tooFull := func(svc string, t *state.ServiceTakenError) error {
		n.status = NeedRequiredMissing
		if n.use.Optional {
			n.status = NeedOptionalMissing
		}
		return fault(CodeTaken, nil, "配方把 %s 接到 %s 上，但 %s 的服务 %s %s", what, q.name, q.name, svc, takenWhy(t, e.goneApp(ctx)))
	}
	for _, ps := range pend {
		if ps.app == q {
			if t := a.taken(p, n.use, ps.name, ps.max()); t != nil {
				return tooFull(ps.name, t)
			}
			n.status, n.service, n.from, n.fromIndex = NeedMatched, ps.name, ps.app, ps.index
			return nil
		}
	}
	if q.reuse {
		for _, s := range a.svcs {
			if s.sv.Source == state.ServiceFromApp && s.sv.App == q.name && s.sv.Kind == n.use.Kind && s.health.Usable() {
				if t := a.taken(p, n.use, s.sv.Name, s.max); t != nil {
					return tooFull(s.sv.Name, t)
				}
				n.status, n.service = NeedMatched, s.sv.Name
				return nil
			}
		}
	}
	n.status = NeedRequiredMissing
	if n.use.Optional {
		n.status = NeedOptionalMissing
	}
	return fault(CodeBadUse, nil, "配方把 %s 接到 %s 上，但 %s 没有提供能用的%s", what, q.name, q.name, n.label)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// unusableHow says how a service of kind t that is there but cannot be used
// now becomes usable. A secret item still to fill in is written 项=- (read
// from standard input), never with its value on the command line.
func unusableHow(s svcState, t catalog.ServiceType) []string {
	switch s.health.Status {
	case ServiceProviderStopped:
		return []string{fmt.Sprintf("服务 %s 的提供方 %s 停着：先问用户，同意后 acornfox start %s", s.sv.Name, s.sv.App, s.sv.App)}
	case ServiceProviderGone:
		// The provider was deleted with its data kept (deleting it with its
		// data removes its services): installing that app instance again on
		// its data brings the service back. Said by the instance's name: the
		// command names no catalog app, the platform finds which one it was
		// from what was kept (reuseTarget).
		return []string{fmt.Sprintf("服务 %s 原来由应用 %s 提供，%s 已经删了、数据还留着：用旧数据把它装回来，装好后这条服务就能用了：%s", s.sv.Name, s.sv.App, s.sv.App, ReuseCommand(s.sv.App))}
	case ServiceIncomplete:
		var sets []string
		for _, k := range s.health.Missing {
			if f, ok := t.Field(k); ok && f.Secret {
				sets = append(sets, k+"=-")
				continue
			}
			sets = append(sets, k+"=…")
		}
		return []string{fmt.Sprintf("服务 %s 还差 %s：acornfox services set %s %s", s.sv.Name, strings.Join(s.health.Missing, "、"), s.sv.Name, strings.Join(sets, " "))}
	}
	return nil
}

// registerHow is the command that registers a service of kind t the user
// already has, with every required item: a secret one is written 项=-, so
// that it is read from standard input (one line each, in the order given)
// and never typed on the command line. Copied as it is, the command works
// once the "…" are filled in.
func registerHow(t catalog.ServiceType) string {
	parts := []string{"acornfox services add " + t.Kind + " --name 名字"}
	for _, f := range t.Fields {
		if !f.Required {
			continue
		}
		if f.Secret {
			parts = append(parts, "--set "+f.Key+"=-")
			continue
		}
		parts = append(parts, "--set "+f.Key+"=…")
	}
	return strings.Join(parts, " ")
}

// registerNote is said with registerHow when the command reads secret items
// from standard input.
func registerNote(t catalog.ServiceType) string {
	for _, f := range t.Fields {
		if f.Required && f.Secret {
			return "（写成 项=- 的是保密项，命令从标准输入读它的值，一项一行，按出现的顺序）"
		}
	}
	return ""
}

// PlanView is POST /v1/catalog/plan: what an install request would do,
// without doing anything.
type PlanView struct {
	CanInstall bool          `json:"can_install"`
	Say        string        `json:"say"`
	Steps      []PlanStep    `json:"steps"`
	Apps       []PlanApp     `json:"apps"`
	Problems   []PlanProblem `json:"problems"`
}

// PlanStep is one app the user asked for, in install order (one for an app,
// the recipe's apps for a recipe). Action is install, or reuse for an app of
// a recipe that is installed already. Because says, for an app of a recipe
// whose service a later app of it will use, which one.
type PlanStep struct {
	App     string `json:"app"`
	Name    string `json:"name"`
	Action  string `json:"action"`
	Because string `json:"because"`
}

// PlanApp is one app of the plan with what it needs and provides.
type PlanApp struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Version       string         `json:"version"`
	Needs         []PlanNeed     `json:"needs"`
	ParamsMissing []ParamMissing `json:"params_missing"`
	Provides      []PlanProvide  `json:"provides"`
}

// PlanNeed is one need and what became of it.
// PendingFrom is set when Service does not exist yet: the app of the same
// recipe, installed earlier in the same task, that registers it.
type PlanNeed struct {
	Use         string   `json:"use"`
	Kind        string   `json:"kind"`
	KindLabel   string   `json:"kind_label"`
	Required    bool     `json:"required"`
	Dedicated   bool     `json:"dedicated"`
	Status      string   `json:"status"`
	Service     string   `json:"service"`
	PendingFrom string   `json:"pending_from"`
	Candidates  []string `json:"candidates"`
	Without     string   `json:"without"`
	How         []string `json:"how"`
	// Tell (matched only, left out when there is none): what the provider
	// of Service wrote for the user, the ask sentences of its manifest's
	// provides entry, as they are. Pass them on before installing.
	Tell []string `json:"tell,omitempty"`
}

// PlanProvide is one service an app provides.
type PlanProvide struct {
	Kind      string `json:"kind"`
	KindLabel string `json:"kind_label"`
}

// PlanProblem is why the install cannot go ahead as asked.
type PlanProblem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// Plan works out what req would do, the way StartInstall checks it, but
// lists every problem instead of stopping at the first and creates nothing.
// A required parameter that has no value yet is listed in params_missing
// and does not stop it (the form may not be filled in yet).
func (e *Engine) Plan(ctx context.Context, req Request) (PlanView, error) {
	if e.cfg.Catalog == nil {
		return PlanView{}, fault(CodeInternal, nil, "这台服务器没有启用应用中心")
	}
	a, err := e.assess(ctx, req, true)
	if err != nil {
		a = &assessment{req: req, problems: []*Fault{AsFault(err)}}
	}
	if err := e.checkPlatformSets(req.Platform); err != nil {
		a.problems = append([]*Fault{AsFault(err)}, a.problems...)
	}
	return planView(a), nil
}

func planView(a *assessment) PlanView {
	v := PlanView{Steps: []PlanStep{}, Apps: []PlanApp{}, Problems: []PlanProblem{}}
	seen := map[string]bool{}
	for _, f := range a.problems {
		if seen[f.Code+f.Message] {
			continue
		}
		seen[f.Code+f.Message] = true
		v.Problems = append(v.Problems, PlanProblem{Code: f.Code, Message: f.Message, Hint: f.Hint})
	}
	for _, p := range a.plan {
		action := "install"
		if p.reuse {
			action = "reuse"
		}
		// Which later apps of the recipe use what this one registers.
		var forLater []string
		for _, q := range a.plan {
			for _, n := range q.needs {
				if n.from == p {
					forLater = append(forLater, fmt.Sprintf("%s 的“%s”", q.name, n.label))
				}
			}
		}
		because := ""
		if len(forLater) > 0 {
			because = "装好后登记的服务给后面的 " + strings.Join(forLater, "、") + " 用"
		}
		v.Steps = append(v.Steps, PlanStep{App: p.app.Manifest.ID, Name: p.name, Action: action, Because: because})
		pa := PlanApp{ID: p.app.Manifest.ID, Name: p.name, Version: p.app.Index.Version, Needs: []PlanNeed{}, ParamsMissing: []ParamMissing{}, Provides: []PlanProvide{}}
		if pa.Version == "" {
			pa.Version = p.app.Manifest.Release.Version
		}
		for _, n := range p.needs {
			pa.Needs = append(pa.Needs, n.view())
		}
		if ms := a.missing[p]; len(ms) > 0 {
			pa.ParamsMissing = ms
		}
		for _, pv := range p.app.Manifest.Provides {
			pa.Provides = append(pa.Provides, PlanProvide{Kind: pv.Kind, KindLabel: a.kindLabel(pv.Kind)})
		}
		v.Apps = append(v.Apps, pa)
	}
	v.CanInstall = len(v.Problems) == 0
	title := a.title
	if title == "" {
		title = a.req.App + a.req.Recipe
	}
	if !v.CanInstall {
		v.Say = "现在还不能装 " + title + "：" + v.Problems[0].Message
		return v
	}
	v.Say = "可以安装 " + title + "。"
	var matched, skip []string
	for _, p := range a.plan {
		for _, n := range p.needs {
			switch n.status {
			case NeedMatched:
				matched = append(matched, fmt.Sprintf("%s 接 %s", n.label, n.service))
			case NeedOptionalMissing:
				skip = append(skip, n.label)
			}
		}
	}
	if len(matched) > 0 {
		v.Say += "需要的服务：" + strings.Join(matched, "，") + "。"
	}
	if len(skip) > 0 {
		v.Say += "可选的" + strings.Join(skip, "、") + "这台机器上没有，先跳过，以后可以接上。"
	}
	return v
}

func (n *needMatch) view() PlanNeed {
	v := PlanNeed{Use: n.use.Name, Kind: n.use.Kind, KindLabel: n.label, Required: !n.use.Optional, Dedicated: n.use.Dedicated,
		Status: n.status, Service: n.service, Candidates: []string{}, Without: n.use.Without, How: []string{}}
	if n.cands != nil {
		v.Candidates = n.cands
	}
	if n.how != nil {
		v.How = n.how
	}
	if n.from != nil {
		v.PendingFrom = n.from.name
	}
	if len(n.tell) > 0 {
		v.Tell = n.tell
	}
	return v
}

// UseView is one need in GET /v1/catalog/apps/{id}: the manifest's entry
// plus what it is called, its kind's label and whether it is required.
type UseView struct {
	catalog.Use
	UseName   string `json:"use"`
	KindLabel string `json:"kind_label"`
	Required  bool   `json:"required"`
}

// useViews and provideViews are the needs and the provided services of m as
// the catalog detail shows them.
func useViews(m *catalog.Manifest, kinds map[string]catalog.ServiceType) []UseView {
	a := &assessment{kinds: kinds}
	out := []UseView{}
	for _, u := range m.Uses {
		out = append(out, UseView{Use: u, UseName: u.Name, KindLabel: a.kindLabel(u.Kind), Required: !u.Optional})
	}
	return out
}

func provideViews(m *catalog.Manifest, kinds map[string]catalog.ServiceType) []PlanProvide {
	a := &assessment{kinds: kinds}
	out := []PlanProvide{}
	for _, pv := range m.Provides {
		out = append(out, PlanProvide{Kind: pv.Kind, KindLabel: a.kindLabel(pv.Kind)})
	}
	return out
}
