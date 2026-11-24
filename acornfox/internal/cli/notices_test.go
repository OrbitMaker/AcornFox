package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
)

// runWithEnv is harness.run with an environment lookup.
func runWithEnv(h *harness, getenv func(string) string, args ...string) (int, string, string) {
	var out, errBuf bytes.Buffer
	code := MainWithConnector(context.Background(), args, io.NopCloser(bytes.NewReader(nil)), &out, &errBuf, getenv, h.connect, h.configDir, h.workDir)
	return code, out.String(), errBuf.String()
}

// serverVersion makes the fake server report a version.
func (h *harness) serverVersion(v string) {
	h.api.statusFn = func(ctx context.Context) (client.Status, error) {
		return client.Status{APIVersion: client.APIVersion, Runner: "ok", Version: v}, nil
	}
}

func noticesOf(t *testing.T, out string) []any {
	t.Helper()
	m := decodeJSON(t, out)
	n, _ := m["notices"].([]any)
	return n
}

// The server is newer: a human run writes the notice to stderr and leaves
// stdout alone; --json puts it in the result's notices; it comes once a day.
func TestNoticeServerNewer(t *testing.T) {
	withVersion(t, "0.6.3")
	h := newHarness(t)
	h.addTarget("n7", "http://n7")
	h.serverVersion("0.6.4")

	code, out, errOut := h.run("apps")
	if code != exitOK {
		t.Fatalf("exit %d %s", code, errOut)
	}
	for _, want := range []string{"提醒：服务器 n7 上的 AcornFox 是 0.6.4 版", "这台电脑上的命令行是 0.6.3 版", "CLI-UPDATE-01", "get.sh | bash -s -- cli", "acornfox skill install"} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, errOut)
		}
	}
	if strings.Contains(out, "提醒") {
		t.Fatalf("the notice must not go to stdout:\n%s", out)
	}

	// Same day: nothing more, in either mode.
	if _, out, errOut = h.run("apps"); strings.Contains(errOut, "CLI-UPDATE-01") {
		t.Fatalf("a second notice on the same day:\n%s", errOut)
	}
	if _, out, _ = h.run("--json", "apps"); len(noticesOf(t, out)) != 0 {
		t.Fatalf("notices on the same day: %s", out)
	}

	// Next day (the stamp is dated yesterday): --json carries it.
	st := loadNoticeStamp(h.configDir)
	st.NotifiedDay, st.CheckedAt = "2000-01-01", st.CheckedAt.AddDate(0, 0, -1)
	saveNoticeStamp(h.configDir, st)
	code, out, errOut = h.run("--json", "apps")
	if code != exitOK {
		t.Fatalf("exit %d %s", code, errOut)
	}
	ns := noticesOf(t, out)
	if len(ns) != 1 {
		t.Fatalf("notices = %v\n%s", ns, out)
	}
	n := ns[0].(map[string]any)
	if n["code"] != CodeCLIUpdate || !strings.Contains(n["message"].(string), "0.6.4") || n["hint"] == "" {
		t.Fatalf("notice = %v", n)
	}
	if cmds, _ := n["commands"].([]any); len(cmds) < 2 {
		t.Fatalf("commands = %v", n["commands"])
	}
	if strings.Contains(errOut, "提醒") {
		t.Fatalf("--json must not also write the human notice:\n%s", errOut)
	}
	if m := decodeJSON(t, out); m["ok"] != true {
		t.Fatalf("result changed: %s", out)
	}
}

// A failing command carries the notice in its diagnosis JSON too.
func TestNoticeOnFailureJSON(t *testing.T) {
	withVersion(t, "0.6.3")
	h := newHarness(t)
	h.addTarget("n7", "http://n7")
	h.serverVersion("0.7.0")
	h.api.appsFn = func(ctx context.Context) ([]client.App, error) {
		return nil, &client.Error{Status: 500, Diag: client.Diagnosis{Stage: "server", Code: "internal", Message: "boom"}}
	}
	code, out, _ := h.run("--json", "apps")
	if code != exitServerError {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if m := decodeJSON(t, out); m["ok"] != false || len(noticesOf(t, out)) != 1 {
		t.Fatalf("failure JSON: %s", out)
	}
}

// Nothing to say: same version, older server, no version reported, a local
// socket target, the switch, a Status error. The check is also asked at most
// once an hour.
func TestNoticeQuietCases(t *testing.T) {
	withVersion(t, "0.6.3")
	cases := []struct {
		name   string
		server string
		url    string
		env    string
		err    error
	}{
		{name: "same", server: "0.6.3"},
		{name: "same-with-suffix", server: "0.6.3-dev"},
		{name: "older", server: "0.6.2"},
		{name: "no-version", server: ""},
		{name: "unparsable", server: "weird"},
		{name: "local-socket", server: "9.9.9", url: "unix:///run/acornfox/api.sock"},
		{name: "switched-off", server: "9.9.9", env: "1"},
		{name: "status-error", server: "9.9.9", err: errors.New("down")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			url := c.url
			if url == "" {
				url = "http://n7"
			}
			h.addTarget("n7", url)
			h.api.statusFn = func(ctx context.Context) (client.Status, error) {
				if c.err != nil {
					return client.Status{}, c.err
				}
				return client.Status{APIVersion: client.APIVersion, Version: c.server}, nil
			}
			getenv := func(k string) string {
				if k == NoUpdateNoticeEnv {
					return c.env
				}
				return ""
			}
			code, _, errOut := runWithEnv(h, getenv, "apps")
			if code != exitOK || strings.Contains(errOut, "提醒") {
				t.Fatalf("exit %d stderr:\n%s", code, errOut)
			}
		})
	}

	// The hourly check: a newer server seen within the hour of a quiet check
	// is not asked about again (no extra request), a day later it is.
	h := newHarness(t)
	h.addTarget("n7", "http://n7")
	calls := 0
	h.api.statusFn = func(ctx context.Context) (client.Status, error) {
		calls++
		return client.Status{APIVersion: client.APIVersion, Version: "0.6.3"}, nil
	}
	h.run("apps")
	h.run("apps")
	if calls != 1 {
		t.Fatalf("Status asked %d times within the hour, want 1", calls)
	}
	st := loadNoticeStamp(h.configDir)
	st.CheckedAt = st.CheckedAt.Add(-2 * noticeCheckEvery)
	saveNoticeStamp(h.configDir, st)
	h.run("apps")
	if calls != 2 {
		t.Fatalf("Status asked %d times after the hour, want 2", calls)
	}
}

// The stamp holds only times, a date and a version.
func TestNoticeStampContent(t *testing.T) {
	withVersion(t, "0.6.3")
	h := newHarness(t)
	h.addTarget("n7", "http://n7")
	h.serverVersion("0.6.4")
	h.run("apps")
	p := noticeStampPath(h.configDir)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(data); !strings.Contains(s, "checked_at") || !strings.Contains(s, "0.6.4") || strings.Contains(s, "n7") {
		t.Fatalf("stamp = %s", s)
	}
}
