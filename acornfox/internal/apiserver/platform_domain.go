package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/acornfox/acornfox/internal/domainname"
	"github.com/acornfox/acornfox/internal/platformdomain"
	"github.com/acornfox/acornfox/internal/platformvar"
	"github.com/acornfox/acornfox/internal/state"
)

// The platform domain (package platformdomain): one domain for the whole
// machine; every public app also opens at https://<app>.<platform domain>.
//
//	GET    /v1/platform/domain         the domain, its three checks (the last ones when recent), every app's name under it
//	PUT    /v1/platform/domain         {"domain","confirm","plan"}: set or replace it; plan only says what would happen
//	DELETE /v1/platform/domain         {"confirm","plan"} or ?confirm=true&plan=true: clear it
//	POST   /v1/platform/domain/check   check again now
//
// Every answer is platformdomain.View plus "apps" (each app's name under
// the domain, whether it is in use, its address now) and, for a plan,
// "plan" (platformdomain.Plan; each app's "url_after"). Errors:
// {"error":{"code","message","hint"}} with a PDOMAIN-* code: PDOMAIN-05 not a
// domain name (400), PDOMAIN-02 a machine without a public address (409),
// PDOMAIN-07 replacing or clearing it without "confirm": true (409). A
// server without the platform domain answers 404 not_found (the console
// then shows no "domain" row). The routes are on the trusted socket and the
// logged-in console.

func (s *server) registerPlatformDomain(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/platform/domain", s.getPlatformDomain)
	mux.HandleFunc("PUT /v1/platform/domain", s.putPlatformDomain)
	mux.HandleFunc("DELETE /v1/platform/domain", s.deletePlatformDomain)
	mux.HandleFunc("POST /v1/platform/domain/check", s.checkPlatformDomain)
}

// platformDomainApp is one app's name under the platform domain.
type platformDomainApp struct {
	App      string `json:"app"`
	Host     string `json:"host"`
	Active   bool   `json:"active"`
	Note     string `json:"note,omitempty"`
	URL      string `json:"url"`
	URLAfter string `json:"url_after,omitempty"` // plan only: the address once the change is made
}

// platformDomainAnswer is the answer of every platform domain route.
type platformDomainAnswer struct {
	platformdomain.View
	Apps []platformDomainApp  `json:"apps"`
	Plan *platformdomain.Plan `json:"plan,omitempty"`
}

// appPlatformDomain is the app view's "platform_domain" (detail only; left
// out without a platform domain and for an internal app).
type appPlatformDomain struct {
	Host   string `json:"host"`
	Active bool   `json:"active"`
	Note   string `json:"note"`
}

// Notes of appPlatformDomain.
const (
	pdNoteNotPublic = "改成公网后自动生效"
	pdNoteOwnDomain = "地址用的是自己绑的域名，这个名字也能打开"
)

func (s *server) platformDomainOff(w http.ResponseWriter) bool {
	if s.platformDomain == nil {
		writeError(w, http.StatusNotFound, "not_found", "该服务端不支持平台域名")
		return true
	}
	return false
}

func writePlatformDomainError(w http.ResponseWriter, s *server, err error) {
	var pe *platformdomain.Error
	if errors.As(err, &pe) {
		var b serviceErrorBody
		b.Error.Code, b.Error.Message, b.Error.Hint = pe.Code, pe.Message, pe.Hint
		writeJSON(w, pe.Status, b)
		return
	}
	s.log.Error("platform domain", "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "内部错误")
}

// platformBase is the platform domain, "" when none.
func (s *server) platformBase() string {
	if s.platformDomain == nil {
		return ""
	}
	return s.platformDomain.Base()
}

// takenNames are the domain names already on a route of their own, with who
// holds them: the domains apps bound (the app's name) and the public
// console's ("公网控制台").
func (s *server) takenNames(ctx context.Context) map[string]string {
	out := map[string]string{}
	if ds, err := s.store.ListDomains(ctx, ""); err == nil {
		for _, d := range ds {
			out[d.Name] = d.App
		}
	}
	if d := s.publicConsoleDomain(ctx); d != "" {
		out[d] = "公网控制台"
	}
	return out
}

// platformDomainFor is the platform domain app a's address is worked out
// with (Where.PlatformDomain): "" when there is none, or when a's name under
// it is already held by another app or the public console (the entry proxy
// does not put it on a's route then; package reconcile).
func (s *server) platformDomainFor(ctx context.Context, a state.App, exposure string) string {
	base := s.platformBase()
	if base == "" {
		return ""
	}
	h := platformvar.PlatformHost(a.Name, a.Internal, exposure, base)
	if h == "" {
		return ""
	}
	if holder, ok := s.takenNames(ctx)[h]; ok && holder != a.Name {
		return ""
	}
	return base
}

// appPlatformDomainOf is a's "platform_domain" with platform domain base.
func appPlatformDomainOf(a state.App, exposure, base string, taken map[string]string, ownDomains bool) *appPlatformDomain {
	if base == "" || a.Internal {
		return nil
	}
	host := a.Name + "." + base
	if !domainname.Valid(host) {
		return &appPlatformDomain{Note: "应用名加上平台域名太长，用不了平台给的名字（" + platformdomain.CodeAppName + "）；可以给它绑一个自己的域名"}
	}
	if holder, ok := taken[host]; ok && holder != a.Name {
		return &appPlatformDomain{Host: host, Note: "这个名字已经被" + holderName(holder) + "用了（" + platformdomain.CodeAppName + "）；可以给它绑一个自己的域名"}
	}
	if platformvar.PlatformHost(a.Name, a.Internal, exposure, base) == "" {
		return &appPlatformDomain{Host: host, Note: pdNoteNotPublic}
	}
	v := &appPlatformDomain{Host: host, Active: true}
	if ownDomains {
		v.Note = pdNoteOwnDomain
	}
	return v
}

func holderName(h string) string {
	if h == "公网控制台" {
		return h
	}
	return "应用 " + h + " "
}

// platformDomainApps lists every app's name under the platform domain and
// its address now; with after (a plan), also the address once the
// platform domain is after.
func (s *server) platformDomainApps(ctx context.Context, plan *platformdomain.Plan) []platformDomainApp {
	out := []platformDomainApp{}
	apps, err := s.store.ListApps(ctx)
	if err != nil {
		return out
	}
	base := s.platformBase()
	taken := s.takenNames(ctx)
	for _, a := range apps {
		if a.Internal {
			continue
		}
		exposure := s.effectiveExposure(a)
		w := s.where(ctx, a)
		row := platformDomainApp{App: a.Name, URL: s.addresser.Address(platformvar.App{Name: a.Name, Created: true, Where: w}).URL}
		if pd := appPlatformDomainOf(a, exposure, base, taken, len(w.Domains) > 0); pd != nil {
			row.Host, row.Active, row.Note = pd.Host, pd.Active, pd.Note
		}
		if plan != nil {
			after := w
			after.PlatformDomain = ""
			if h := platformvar.PlatformHost(a.Name, false, exposure, plan.To); h != "" {
				if holder, ok := taken[h]; !ok || holder == a.Name {
					after.PlatformDomain = plan.To
				}
			}
			row.URLAfter = s.addresser.Address(platformvar.App{Name: a.Name, Created: true, Where: after}).URL
		}
		out = append(out, row)
	}
	return out
}

func (s *server) answerPlatformDomain(ctx context.Context, w http.ResponseWriter, v platformdomain.View, plan *platformdomain.Plan) {
	writeJSON(w, http.StatusOK, platformDomainAnswer{View: v, Apps: s.platformDomainApps(ctx, plan), Plan: plan})
}

func (s *server) getPlatformDomain(w http.ResponseWriter, r *http.Request) {
	if s.platformDomainOff(w) {
		return
	}
	s.answerPlatformDomain(r.Context(), w, s.platformDomain.View(r.Context(), false), nil)
}

func (s *server) checkPlatformDomain(w http.ResponseWriter, r *http.Request) {
	if s.platformDomainOff(w) {
		return
	}
	s.answerPlatformDomain(r.Context(), w, s.platformDomain.Check(r.Context()), nil)
}

// platformDomainRequest is the PUT and DELETE body.
type platformDomainRequest struct {
	Domain  string `json:"domain"`
	Confirm bool   `json:"confirm"`
	Plan    bool   `json:"plan"`
}

// readPlatformDomainRequest reads the body (may be empty for DELETE) and the
// confirm and plan query parameters.
func readPlatformDomainRequest(r *http.Request) (platformDomainRequest, bool) {
	var req platformDomainRequest
	b, err := io.ReadAll(io.LimitReader(r.Body, 4<<10))
	if err != nil {
		return req, false
	}
	if strings.TrimSpace(string(b)) != "" {
		if err := json.Unmarshal(b, &req); err != nil {
			return req, false
		}
	}
	q := r.URL.Query()
	if q.Get("confirm") == "true" || q.Get("confirm") == "1" {
		req.Confirm = true
	}
	if q.Get("plan") == "true" || q.Get("plan") == "1" {
		req.Plan = true
	}
	return req, true
}

func (s *server) putPlatformDomain(w http.ResponseWriter, r *http.Request) {
	if s.platformDomainOff(w) {
		return
	}
	ctx := r.Context()
	req, ok := readPlatformDomainRequest(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_body", "请求体不是合法 JSON")
		return
	}
	if req.Plan {
		p, err := s.platformDomain.PlanSet(ctx, req.Domain)
		if err != nil {
			writePlatformDomainError(w, s, err)
			return
		}
		s.answerPlatformDomain(ctx, w, s.platformDomain.View(ctx, false), &p)
		return
	}
	v, err := s.platformDomain.Set(ctx, req.Domain, req.Confirm)
	if err != nil {
		writePlatformDomainError(w, s, err)
		return
	}
	s.answerPlatformDomain(ctx, w, v, nil)
}

func (s *server) deletePlatformDomain(w http.ResponseWriter, r *http.Request) {
	if s.platformDomainOff(w) {
		return
	}
	ctx := r.Context()
	req, ok := readPlatformDomainRequest(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_body", "请求体不是合法 JSON")
		return
	}
	if req.Plan {
		p := s.platformDomain.PlanClear()
		s.answerPlatformDomain(ctx, w, s.platformDomain.View(ctx, false), &p)
		return
	}
	v, err := s.platformDomain.Clear(ctx, req.Confirm)
	if err != nil {
		writePlatformDomainError(w, s, err)
		return
	}
	s.answerPlatformDomain(ctx, w, v, nil)
}
