package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
)

// fakeInstallAPI records the install (and plan) requests the install command
// sends; the catalog has app "demo" (a secret and a plain parameter) and a
// recipe "bundle" of "demo" and "other".
type fakeInstallAPI struct {
	*fakeAPI
	got     []client.InstallRequest
	planned []client.InstallRequest
	planRaw string // the plan to answer, when set
}

func (f *fakeInstallAPI) Catalog(context.Context) (client.CatalogList, error) {
	var v client.CatalogList
	_ = json.Unmarshal([]byte(`{"recipes":[{"id":"bundle","apps":[{"app":"demo"},{"app":"other"}]}]}`), &v)
	return v, nil
}

func (f *fakeInstallAPI) CatalogApp(_ context.Context, id string) (client.CatalogApp, error) {
	var v client.CatalogApp
	switch id {
	case "demo":
		_ = json.Unmarshal([]byte(`{"id":"demo","params":[{"key":"admin_password","type":"secret"},{"key":"sep","type":"string"}]}`), &v)
	case "other":
		_ = json.Unmarshal([]byte(`{"id":"other","params":[{"key":"root_password","type":"secret"}]}`), &v)
	}
	return v, nil
}
func (f *fakeInstallAPI) CatalogSync(context.Context) (client.CatalogSync, error) {
	return client.CatalogSync{}, nil
}
func (f *fakeInstallAPI) Install(_ context.Context, req client.InstallRequest) (client.InstallTask, error) {
	f.got = append(f.got, req)
	return client.InstallTask{ID: "t1", Status: "running"}, nil
}
func (f *fakeInstallAPI) InstallTask(context.Context, string) (client.InstallTask, error) {
	return client.InstallTask{}, nil
}
func (f *fakeInstallAPI) FirstLogin(context.Context, string, bool) (client.FirstLogin, error) {
	return client.FirstLogin{}, nil
}
func (f *fakeInstallAPI) InstallPlan(_ context.Context, req client.InstallRequest) (client.InstallPlan, error) {
	f.planned = append(f.planned, req)
	raw := json.RawMessage(`{"can_install":true,"say":"可以安装 demo。","steps":[],"apps":[],"problems":[]}`)
	if f.planRaw != "" {
		raw = json.RawMessage(f.planRaw)
	}
	var p client.InstallPlan
	_ = json.Unmarshal(raw, &p)
	p.Raw = raw
	return p, nil
}

func installHarness(t *testing.T) (*harness, *fakeInstallAPI) {
	h := newHarness(t)
	h.addTarget("prod", "https://203.0.113.10:8443")
	f := &fakeInstallAPI{fakeAPI: h.api}
	h.connect = func(context.Context, client.Target) (client.API, error) { return f, nil }
	return h, f
}

func (h *harness) runStdin(stdin string, args ...string) (int, string, string) {
	h.t.Helper()
	var out, errBuf bytes.Buffer
	code := MainWithConnector(context.Background(), args, strings.NewReader(stdin), &out, &errBuf, func(string) string { return "" }, h.connect, h.configDir, h.workDir)
	return code, out.String(), errBuf.String()
}

// --set 参数=- for a secret parameter reads one line of standard input (never
// installs "-" as the password); for a plain parameter "-" is the value.
// What was read never shows in the output.
func TestInstallSecretParamFromStdin(t *testing.T) {
	h, f := installHarness(t)
	const pw = "pw-from-stdin-4f2a"
	code, out, errOut := h.runStdin(pw+"\n", "--json", "install", "demo", "--no-wait", "--set", "admin_password=-", "--set", "sep=-")
	if code != exitOK || len(f.got) != 1 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if p := f.got[0].Params; p["admin_password"] != pw || p["sep"] != "-" {
		t.Fatalf("params %v", p)
	}
	noSecret(t, pw, out, errOut)

	// A recipe: the prefix names the app; in the order written.
	f.got = nil
	code, out, errOut = h.runStdin("first-secret-1\nsecond-secret-2\n", "install", "--recipe", "bundle", "--no-wait",
		"--set", "other.root_password=-", "--set", "demo.admin_password=-")
	if code != exitOK || len(f.got) != 1 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if p := f.got[0].Params; p["other.root_password"] != "first-secret-1" || p["demo.admin_password"] != "second-secret-2" {
		t.Fatalf("params %v", p)
	}
	noSecret(t, "first-secret-1", out, errOut)

	// A bare key of a recipe: secret in one of its apps.
	f.got = nil
	if code, out, errOut := h.runStdin("bare-secret-3\n", "install", "--recipe", "bundle", "--no-wait", "--set", "root_password=-"); code != exitOK || f.got[0].Params["root_password"] != "bare-secret-3" {
		t.Fatalf("exit %d %v\n%s\n%s", code, f.got, out, errOut)
	}

	// Nothing on standard input: refused before anything is sent.
	f.got = nil
	code, _, errOut = h.runStdin("", "install", "demo", "--no-wait", "--set", "admin_password=-")
	if code != exitUsage || len(f.got) != 0 || !strings.Contains(errOut, "标准输入") {
		t.Fatalf("exit %d sent %v\n%s", code, f.got, errOut)
	}

	// --plan reads nothing: the secret counts as not filled in yet.
	code, out, errOut = h.runStdin("", "install", "demo", "--plan", "--set", "admin_password=-", "--set", "sep=-")
	if code != exitOK || len(f.planned) != 1 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if p := f.planned[0].Params; p["sep"] != "-" {
		t.Fatalf("plan params %v", p)
	} else if _, given := p["admin_password"]; given {
		t.Fatalf("plan sent the secret: %v", p)
	}

	// The help says so.
	if _, out, _ := h.run("install", "-h"); !strings.Contains(out, "--set 参数=-") || !strings.Contains(out, "标准输入") {
		t.Fatalf("help: %s", out)
	}
}
