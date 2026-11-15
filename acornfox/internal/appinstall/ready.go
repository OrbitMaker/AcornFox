package appinstall

// The readiness check of a provided service (its manifest's
// provides[].ready): read-only steps, run with no consumer and no
// credential, that tell whether the service can really be used now (a model
// gateway with no upstream yet, a database whose admin account is not
// there). Nothing names an app or a kind here: the steps and the sentence
// for the user are the manifest's.
//
// When it runs: whenever someone looks (acornfox services list / show, GET
// /v1/services…), for every matched service of a plan (POST
// /v1/catalog/plan), once an install has succeeded for the services its apps
// were plugged into, and so for doctor too (it reads GET /v1/services). The
// result is never stored: it is worked out again each time.
//
// It runs only for a service whose status is ok otherwise (its provider is
// running, nothing required is missing): a provider that is stopped or gone
// keeps saying so, and nothing is run in it. Each check has its own short
// time limit (readyTimeout) and the checks of several services run side by
// side, so one slow provider does not hold the others up.
//
// Not ready does not stop anything: the service's status is not_ready ("还
// 不能用") with the manifest's sentence (ready_say) and what failed
// (ready_detail, secrets masked); matching still takes it; a plan puts the
// sentence into the need's tell, a finished install into its must_tell, and
// doctor warns (CAT-SVC-09).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/state"
)

// readyTimeout bounds one readiness check, all its steps together (a
// variable for tests).
var readyTimeout = 3 * time.Second

// readyResult is what one readiness check found.
type readyResult struct {
	checked bool   // it ran: the service has one and its provider is up
	ready   bool   // every step passed
	say     string // not ready: the manifest's sentence, as written
	detail  string // not ready: what failed (no secret in it)
}

// hasReady reports whether sv comes from a provides entry with a readiness
// check, by the manifest its provider was installed with.
func (e *Engine) hasReady(ctx context.Context, sv Service) bool {
	if sv.Source != state.ServiceFromApp {
		return false
	}
	m := e.appManifest(ctx, sv.App)
	return m != nil && sv.ProvideIndex >= 0 && sv.ProvideIndex < len(m.Provides) &&
		m.Provides[sv.ProvideIndex].Kind == sv.Kind && len(m.Provides[sv.ProvideIndex].ReadySteps()) > 0
}

// readyOf runs sv's readiness check. Nothing runs (checked false) for a
// user's service, an entry without one, or a provider that is stopped, gone
// or has no container up (providerOf says why; the status says it already).
func (e *Engine) readyOf(ctx context.Context, sv Service) readyResult {
	if !e.hasReady(ctx, sv) {
		return readyResult{}
	}
	pc, err := e.providerOf(ctx, sv)
	if err != nil || pc.pv.Ready == nil || len(pc.pv.Ready.Steps) == 0 {
		return readyResult{}
	}
	cctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	r := e.actions()
	r.attempts = 1 // a check, not a change: no waiting to connect again
	if r.timeout > readyTimeout {
		r.timeout = readyTimeout
	}
	// The time limit holds whatever the runner does with the context: the
	// steps run on their own, and a check that is not over in time is not
	// ready (what is still running ends when its own timeout says).
	done := make(chan error, 1)
	go func() {
		_, err := r.run(cctx, pc.base, pc.pv.Ready.Steps, pc.env(GroupReady, "", nil, ""))
		done <- err
	}()
	say := strings.TrimSpace(pc.pv.Ready.Say)
	late := fmt.Sprintf("就绪检查在 %s内没做完", waitText(readyTimeout))
	select {
	case err = <-done:
	case <-cctx.Done():
		return readyResult{checked: true, say: say, detail: late}
	}
	if err == nil {
		return readyResult{checked: true, ready: true}
	}
	detail := err.Error()
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		detail = late + "（" + detail + "）"
	}
	return readyResult{checked: true, say: say, detail: detail}
}

// readyMany runs the readiness checks of svs side by side, each with its own
// time limit; the results are by service name (only the ones that ran).
func (e *Engine) readyMany(ctx context.Context, svs []Service) map[string]readyResult {
	out := map[string]readyResult{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, sv := range svs {
		if !e.hasReady(ctx, sv) {
			continue
		}
		wg.Add(1)
		go func(sv Service) {
			defer wg.Done()
			rr := e.readyOf(ctx, sv)
			if !rr.checked {
				return
			}
			mu.Lock()
			out[sv.Name] = rr
			mu.Unlock()
		}(sv)
	}
	wg.Wait()
	return out
}

// readyTell is the sentence for the user about service svc that is not ready
// (say: its provider's words, as written).
func readyTell(svc, say string) string {
	say = strings.TrimRight(strings.TrimSpace(say), "。.；; ")
	return fmt.Sprintf("服务 %s 现在还不能用：%s。用到它的功能要等它能用了才行（acornfox services show %s 看情况）", svc, say, svc)
}

// applyReady runs the readiness checks of the services in views whose status
// is ok and puts the outcome over it: not_ready with the sentence and what
// failed. svs are the services by name.
func (env *svcEnv) applyReady(ctx context.Context, views []ServiceView, svs map[string]Service) {
	var run []Service
	for _, v := range views {
		if v.Status == ServiceOK && v.Source == state.ServiceFromApp {
			run = append(run, svs[v.Name])
		}
	}
	if len(run) == 0 {
		return
	}
	got := env.e.readyMany(ctx, run)
	for i := range views {
		rr, ok := got[views[i].Name]
		if !ok || rr.ready {
			continue
		}
		views[i].Status, views[i].StatusLabel = ServiceNotReady, ServiceNotReady.Label()
		views[i].ReadySay, views[i].ReadyDetail = rr.say, rr.detail
	}
}

// tellNotReady adds, once the install of p has succeeded, a sentence to the
// task (apps[].tell, so must_tell) for each service p is plugged into that
// is not ready now.
func (t *taskRun) tellNotReady(ctx context.Context, p *planned) {
	e := t.e
	reg, err := e.Registry()
	if err != nil {
		return
	}
	bs, err := reg.ListBindings(ctx, state.BindingFilter{App: p.name})
	if err != nil {
		return
	}
	kinds := e.planKinds()
	seen := map[string]bool{}
	var svs []Service
	for _, b := range bs {
		if _, parked := state.ParkedUse(b.UseName); parked || seen[b.Service] {
			continue
		}
		seen[b.Service] = true
		sv, err := reg.GetService(ctx, b.Service)
		if err != nil || e.serviceHealth(ctx, sv, kinds).Status != ServiceOK {
			continue
		}
		svs = append(svs, sv)
	}
	got := e.readyMany(ctx, svs)
	var lines []string
	for _, sv := range svs {
		if rr, ok := got[sv.Name]; ok && !rr.ready {
			lines = append(lines, readyTell(sv.Name, rr.say))
		}
	}
	if len(lines) == 0 {
		return
	}
	t.setApp(p.name, func(r *AppResult) {
		for _, l := range lines {
			if !contains(r.Tell, l) {
				r.Tell = append(r.Tell, l)
			}
		}
	})
	t.save(ctx)
}
