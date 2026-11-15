package appinstall

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

// Taking back and tidying up what was handed out, and the plan agreeing
// with the install in the corner cases (see needs_test.go for the main
// paths).

// carriedRevoke is the CAT-INST-35 lines of what the task says about app.
func carriedRevoke(tv TaskView, app string) []string {
	var out []string
	for _, r := range tv.Apps {
		if r.App != app {
			continue
		}
		for _, line := range r.Carried {
			if strings.HasPrefix(line, CodeRevoke+" ") {
				out = append(out, line)
			}
		}
	}
	return out
}

// TestStaleBindingDropped: installed again on its data, the consumer's
// optional mail is not plugged in this time (the service cannot be used
// now): the kept binding goes once the install has succeeded, so the app
// does not show as using it and the service can be removed. A rollback
// leaves the kept binding as it was.
func TestStaleBindingDropped(t *testing.T) {
	v := setupServices(t)
	ctx := context.Background()
	v.addMail(t, "mail-a")
	v.installOK(t, Request{App: "demo-provider"})
	v.installOK(t, Request{App: "demo-consumer"})
	reg := v.reg(t)
	if b, err := reg.GetBinding(ctx, "demo-consumer", "mail"); err != nil || b.Service != "mail-a" {
		t.Fatalf("mail binding %+v %v", b, err)
	}
	if err := v.w.delete(ctx, "demo-consumer", false); err != nil {
		t.Fatal(err)
	}
	// mail-a cannot be used now: its required host is gone.
	if _, err := reg.UpdateService(ctx, "mail-a", func(sv *Service) error { delete(sv.Values, "host"); return nil }); err != nil {
		t.Fatal(err)
	}
	if n := needOf(t, v.plan(t, Request{App: "demo-consumer", ReuseData: true}), "demo-consumer", "mail"); n.Status != NeedOptionalMissing {
		t.Fatalf("mail need %+v", n)
	}

	// A failed install on the data: the kept binding stays as it was.
	v.w.failDeploy["demo-consumer"] = true
	if tv := v.install(t, Request{App: "demo-consumer", ReuseData: true}); tv.Status != state.TaskFailed {
		t.Fatalf("task %+v", tv)
	}
	if b, err := reg.GetBinding(ctx, "demo-consumer", "mail"); err != nil || b.Service != "mail-a" {
		t.Fatalf("a rollback dropped the kept binding: %+v %v", b, err)
	}
	if b, err := reg.GetBinding(ctx, "demo-consumer", "demo"); err != nil || len(b.Issued) == 0 {
		t.Fatalf("a rollback dropped the kept token: %+v %v", b, err)
	}
	v.w.failDeploy["demo-consumer"] = false

	tv := v.installOK(t, Request{App: "demo-consumer", ReuseData: true})
	if _, err := reg.GetBinding(ctx, "demo-consumer", "mail"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("stale mail binding left: %v", err)
	}
	if _, have := envVars(t, v.env, "demo-consumer")["SMTP_HOST"]; have {
		t.Fatal("SMTP_HOST written for a need not plugged in")
	}
	uses, err := v.e.AppServices(ctx, "demo-consumer")
	if err != nil || len(uses) != 2 || uses[0].Service != "demo-provider" || uses[1].Service != "" || uses[1].Status != UseNotConnected {
		t.Fatalf("app services %+v %v", uses, err)
	}
	if !strings.Contains(strings.Join(tv.Apps[0].Carried, "；"), "mail-a") {
		t.Fatalf("carried does not say the mail was cut: %+v", tv.Apps[0].Carried)
	}
	if err := reg.DeleteService(ctx, "mail-a"); err != nil {
		t.Fatalf("mail-a still counted as used: %v", err)
	}
	// The token it still uses stays.
	if b, err := reg.GetBinding(ctx, "demo-consumer", "demo"); err != nil || len(b.Issued) == 0 || len(v.prov.tokenList()) != 1 {
		t.Fatalf("demo binding %+v %v", b, err)
	}
}

// TestRollbackRevokeFailWarns: the consumer fails after its token was
// issued and the provider will not delete it: the rollback still finishes,
// and the task says (CAT-INST-35) what to remove by hand, no secret in it.
func TestRollbackRevokeFailWarns(t *testing.T) {
	v := setupServices(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-provider"})
	v.prov.failDelete = true
	v.w.failDeploy["demo-consumer"] = true
	tv := v.install(t, Request{App: "demo-consumer"})
	if tv.Status != state.TaskFailed || tv.Fault.Code != CodeStart {
		t.Fatalf("task %+v", tv.Fault)
	}
	toks := v.prov.tokenList()
	if len(toks) != 1 {
		t.Fatalf("tokens %+v", toks)
	}
	if bs, _ := v.reg(t).ListBindings(ctx, state.BindingFilter{}); len(bs) != 0 {
		t.Fatalf("bindings left %+v", bs)
	}
	ws := carriedRevoke(tv, "demo-consumer")
	if len(ws) != 1 || !strings.Contains(ws[0], "demo-provider") || !strings.Contains(ws[0], "demo-consumer") {
		t.Fatalf("warnings %+v (apps %+v)", ws, tv.Apps)
	}
	raw, _ := v.w.st.GetInstallTask(ctx, tv.ID)
	if !strings.Contains(raw.Data, CodeRevoke) {
		t.Fatal("the warning is not in the task record")
	}
	for _, secret := range []string{toks[1].key, "session-secret-value-0001"} {
		if strings.Contains(raw.Data, secret) || strings.Contains(v.log.String(), secret) {
			t.Fatalf("a secret in the task record or the log: %s", secret)
		}
	}
}

// TestIssueHalfWayWarns: the issue steps fail after the login saved a
// session: the revoke steps cannot find a token (none was saved), so the
// task says to look in the provider; nothing secret in it.
func TestIssueHalfWayWarns(t *testing.T) {
	v := setupServices(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-provider"})
	v.prov.failMake = true
	tv := v.install(t, Request{App: "demo-consumer"})
	if tv.Status != state.TaskFailed || tv.Fault.Code != CodeIssue {
		t.Fatalf("task %+v", tv.Fault)
	}
	ws := carriedRevoke(tv, "demo-consumer")
	if len(ws) != 1 || !strings.Contains(ws[0], "demo-provider") {
		t.Fatalf("warnings %+v (apps %+v)", ws, tv.Apps)
	}
	raw, _ := v.w.st.GetInstallTask(ctx, tv.ID)
	if strings.Contains(raw.Data, "session-secret-value-0001") {
		t.Fatal("the task record holds the session")
	}
}

// TestIssueFailsBeforeAnythingSavedWarns: the issue steps fail at the
// first one (the login), so nothing is saved: the revoke steps are still
// tried with the issue number, and when they cannot clear it the task says
// to look in the provider, naming the number: never silence.
func TestIssueFailsBeforeAnythingSavedWarns(t *testing.T) {
	v := setupServices(t)
	v.installOK(t, Request{App: "demo-provider"})
	v.prov.mu.Lock()
	v.prov.failLogin = true
	v.prov.mu.Unlock()
	tv := v.install(t, Request{App: "demo-consumer"})
	if tv.Status != state.TaskFailed || tv.Fault.Code != CodeIssue {
		t.Fatalf("task %+v", tv.Fault)
	}
	ws := carriedRevoke(tv, "demo-consumer")
	if len(ws) != 1 || !strings.Contains(ws[0], "demo-provider") || !strings.Contains(ws[0], "编号") {
		t.Fatalf("warnings %+v (apps %+v)", ws, tv.Apps)
	}
	if r := tv.Apps[0]; len(r.Issuing) != 0 {
		t.Fatalf("the issue is still recorded as under way: %+v", r.Issuing)
	}
}

// A provides entry without revoke steps cannot take a credential back: that
// is an error the callers turn into a warning, never a silent success.
func TestRevokeWithoutStepsIsNotSilent(t *testing.T) {
	e := &Engine{}
	ctx := context.Background()
	if err := e.runRevoke(ctx, providerConn{}, "demo-app", map[string]string{catalog.IssueIDKey: "0a1b2c3d"}); !errors.Is(err, errNoRevoke) {
		t.Fatalf("no revoke steps: %v", err)
	}
	if err := e.runRevoke(ctx, providerConn{}, "demo-app", nil); err != nil {
		t.Fatalf("nothing issued: %v", err)
	}
}

// An old credential missing a key the provider hands out now (it was
// installed again with another manifest) is not used again, whatever the
// check says.
func TestHasIssuedKeys(t *testing.T) {
	pv := catalog.Provide{Values: map[string]catalog.ValueSource{"api_key": {Issued: "api_key"}, "base_url": {Template: "{{self.internal_url}}"}}}
	if !hasIssuedKeys(pv, map[string]string{"api_key": "k", catalog.IssueIDKey: "1"}) {
		t.Fatal("a complete credential")
	}
	if hasIssuedKeys(pv, map[string]string{"token_id": "1", catalog.IssueIDKey: "1"}) {
		t.Fatal("a credential without the key")
	}
}

// Installed again on its data and plugged into another service, the old
// credential is taken back only once the whole install has succeeded: a
// failed install puts the old binding back with its credential, nothing
// taken back, nothing left aside.
func TestReplacedBindingStaysUntilSuccess(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installHermes(t, v)
	addOwnService(t, v)
	reg := registry(t, v)
	old, _ := reg.GetBinding(ctx, "hermes-agent", "llm")
	keepData(t, v, "hermes-agent")
	v.w.mu.Lock()
	v.w.failDeploy["hermes-agent"] = true
	v.w.mu.Unlock()
	req := Request{App: "hermes-agent", ReuseData: true, Use: map[string]string{"llm": "my-upstream"}}
	tv := v.install(t, req)
	if tv.Status != state.TaskFailed || len(tv.Apps[0].Replaced) != 1 || tv.Apps[0].Replaced[0] != (ReplacedRef{Use: "llm", Service: "new-api"}) {
		t.Fatalf("task %+v", tv)
	}
	if len(v.tokens.deleted) != 0 || v.tokens.tokenCount() != 1 {
		t.Fatalf("the old token was taken back by a failed install: deleted %v", v.tokens.deleted)
	}
	b, err := reg.GetBinding(ctx, "hermes-agent", "llm")
	if err != nil || b.Service != "new-api" || b.Issued[catalog.IssueIDKey] != old.Issued[catalog.IssueIDKey] || b.Issued["api_key"] != old.Issued["api_key"] {
		t.Fatalf("old binding not put back: %+v %v", b, err)
	}
	if _, err := reg.GetBinding(ctx, "hermes-agent", state.ParkedUseName("llm")); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("something left aside: %v", err)
	}
	if c := strings.Join(tv.Apps[0].Carried, "；"); strings.Contains(c, "已经收回") {
		t.Fatalf("carried says it was taken back: %s", c)
	}

	v.w.mu.Lock()
	v.w.failDeploy["hermes-agent"] = false
	v.w.mu.Unlock()
	tv = v.install(t, req)
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	if len(v.tokens.deleted) != 1 || v.tokens.deleted[0] != 1 || v.tokens.tokenCount() != 0 {
		t.Fatalf("the old token not taken back after success: deleted %v", v.tokens.deleted)
	}
	if b, err := reg.GetBinding(ctx, "hermes-agent", "llm"); err != nil || b.Service != "my-upstream" {
		t.Fatalf("binding %+v %v", b, err)
	}
	if bs, _ := reg.ListBindings(ctx, state.BindingFilter{App: "hermes-agent"}); len(bs) != 1 {
		t.Fatalf("bindings %+v", bs)
	}
	if c := strings.Join(tv.Apps[0].Carried, "；"); !strings.Contains(c, "已经收回") {
		t.Fatalf("carried does not say it was taken back: %s", c)
	}
}

// The old credential does not pass the check (the check asks for it to be
// on; the user turned it off): a new one is issued. When the install then
// fails, the new one is taken back and the old binding is back as it was;
// once an install succeeds the old one stays in the provider and the user
// is told.
func TestCheckFailedOldStaysOnFailure(t *testing.T) {
	strict := goodManifest(t, "new-api")
	const sameToken = `expect_json: { success: true, data: { id: "{{issued.token_id}}", name: "acornfox-{{consumer.name}}-{{issued.issue_id}}" } }`
	idx := strings.LastIndex(strict, sameToken)
	if idx < 0 {
		t.Fatal("the check step to change is not in the material")
	}
	strict = strict[:idx] + `expect_json: { success: true, data: { id: "{{issued.token_id}}", name: "acornfox-{{consumer.name}}-{{issued.issue_id}}", status: 1 } }` + strict[idx+len(sameToken):]
	b := typedBundle(t, goodDir, nil, strict, goodManifest(t, "hermes-agent"))
	v, _ := setupSnapshot(t, bundleFS(b), nil)
	ctx := context.Background()
	installHermes(t, v)
	reg := registry(t, v)
	old, _ := reg.GetBinding(ctx, "hermes-agent", "llm")
	v.tokens.mu.Lock()
	v.tokens.disabled = map[int]bool{1: true}
	v.tokens.mu.Unlock()
	keepData(t, v, "hermes-agent")
	v.w.mu.Lock()
	v.w.failDeploy["hermes-agent"] = true
	v.w.mu.Unlock()
	tv := v.install(t, Request{App: "hermes-agent", ReuseData: true})
	if tv.Status != state.TaskFailed || len(tv.Apps[0].Replaced) != 1 || !tv.Apps[0].Replaced[0].Keep {
		t.Fatalf("task %+v", tv)
	}
	if len(v.tokens.deleted) != 1 || v.tokens.deleted[0] != 2 || v.tokens.tokenCount() != 1 {
		t.Fatalf("tokens %v deleted %v", v.tokens.tokens, v.tokens.deleted)
	}
	if got, err := reg.GetBinding(ctx, "hermes-agent", "llm"); err != nil || got.Issued["token_id"] != "1" || got.Issued[catalog.IssueIDKey] != old.Issued[catalog.IssueIDKey] {
		t.Fatalf("old binding not put back: %+v %v", got, err)
	}
	if c := strings.Join(tv.Apps[0].Carried, "；"); strings.Contains(c, "没通过检查") {
		t.Fatalf("carried speaks of a replacement that was rolled back: %s", c)
	}

	v.w.mu.Lock()
	v.w.failDeploy["hermes-agent"] = false
	v.w.mu.Unlock()
	tv = v.install(t, Request{App: "hermes-agent", ReuseData: true})
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	notes := carriedRevoke(tv, "hermes-agent")
	if len(notes) != 1 || !strings.Contains(notes[0], "没通过检查") || !strings.Contains(notes[0], old.Issued[catalog.IssueIDKey]) {
		t.Fatalf("carried %v", tv.Apps[0].Carried)
	}
	if len(v.tokens.deleted) != 1 || v.tokens.tokenCount() != 2 {
		t.Fatalf("the old token must stay: tokens %v deleted %v", v.tokens.tokens, v.tokens.deleted)
	}
	if got, _ := reg.GetBinding(ctx, "hermes-agent", "llm"); got.Issued["token_id"] != "3" {
		t.Fatalf("binding %+v", got)
	}
	if _, err := reg.GetBinding(ctx, "hermes-agent", state.ParkedUseName("llm")); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("something left aside: %v", err)
	}
}

// TestUsePickAmbiguous: in a recipe whose two apps both have a need called
// mail, a bare --use mail says it is ambiguous (plan and install alike).
func TestUsePickAmbiguous(t *testing.T) {
	second := strings.Replace(demoManifest(t, "demo-consumer"), "id: demo-consumer\n", "id: demo-consumer-2\n", 1)
	r := catalog.Recipe{ID: "demo-three", Schema: 1, Title: "样例三件套", Summary: "测试用",
		Apps:     []catalog.RecipeApp{{App: "demo-provider"}, {App: "demo-consumer"}, {App: "demo-consumer-2"}},
		Delivery: catalog.Delivery{Tier: "auto"}}
	v := setupServicesWith(t, []catalog.Recipe{r}, demoManifest(t, "demo-provider"), demoManifest(t, "demo-consumer"), second)
	v.addMail(t, "mail-a")
	pv, _, err := v.samePlan(t, Request{Recipe: "demo-three", Use: map[string]string{"mail": "mail-a"}})
	if code(err) != CodeBadUse || !strings.Contains(err.Error(), "demo-consumer.mail") || !strings.Contains(err.Error(), "demo-consumer-2.mail") {
		t.Fatalf("ambiguous pick: %v (plan %+v)", err, pv.Problems)
	}
	if len(pv.Problems) != 1 {
		t.Fatalf("problems %+v", pv.Problems)
	}
	// Named in full, it is fine.
	if pv := v.plan(t, Request{Recipe: "demo-three", Use: map[string]string{"demo-consumer-2.mail": "mail-a"}}); !pv.CanInstall {
		t.Fatalf("plan %+v", pv.Problems)
	}
}

// TestIssuedSecretFromKind: a credential key behind an item the service
// type marks secret is secret in the provider's steps, whatever the
// manifest says.
func TestIssuedSecretFromKind(t *testing.T) {
	raw := strings.Replace(demoManifest(t, "demo-provider"), `prefix: "dt-", secret: true }`, `prefix: "dt-" }`, 1)
	if raw == demoManifest(t, "demo-provider") {
		t.Fatal("test material changed: the token is not saved as secret any more")
	}
	m, err := catalog.ParseManifest([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	types, _, _ := catalog.ReadServiceTypes(servicesDir)
	var kind catalog.ServiceType
	for _, st := range types {
		if st.Kind == "demo_token" {
			kind = st
		}
	}
	if got := issuedSecretKeys(m.Provides[0], m, kind); !got["token"] {
		t.Fatalf("token not secret: %v", got)
	}
	if got := issuedSecretKeys(m.Provides[0], m, catalog.ServiceType{}); got["token"] {
		t.Fatalf("token secret without the type saying so: %v", got)
	}
}
