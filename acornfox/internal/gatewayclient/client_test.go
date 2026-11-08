package gatewayclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/identity"
)

// fakeGateway is a minimal gateway: it checks signatures the way the
// contract says (headers, timestamp window, nonce reuse, install ID derived
// from the registered key) and answers from a script.
type fakeGateway struct {
	mu         sync.Mutex
	keys       map[string]string // install id -> pubkey
	nonces     map[string]bool
	registers  int
	issues     int
	lastIssue  map[string]any
	lastClient map[string]any
	// next errors to return from /v1/issue (code, status, retry_after); popped in order
	issueErrs []fakeErr
	sha       string
	// catalog answers /v1/catalog/* after the signature checked out
	catalog func(path string, body []byte) (int, any)
}

type fakeErr struct {
	code   string
	status int
	retry  int
}

func newFake() *fakeGateway {
	return &fakeGateway{keys: map[string]string{}, nonces: map[string]bool{}, sha: strings.Repeat("ab", 32)}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func gwErr(w http.ResponseWriter, status int, code string, retry int) {
	m := map[string]any{"ok": false, "code": code, "message": "测试：" + code}
	if retry > 0 {
		m["retry_after"] = retry
	}
	writeJSON(w, status, m)
}

func (g *fakeGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	iid := r.Header.Get("X-AFX-Install")
	ts, err := strconv.ParseInt(r.Header.Get("X-AFX-Timestamp"), 10, 64)
	nonce := r.Header.Get("X-AFX-Nonce")
	sig := r.Header.Get("X-AFX-Signature")
	if r.Method != http.MethodPost || err != nil || len(nonce) != 22 || r.Header.Get("Content-Type") != "application/json" {
		gwErr(w, 400, "GW-REQ-01", 0)
		return
	}
	if d := time.Now().Unix() - ts; d > 300 || d < -300 {
		gwErr(w, 401, "GW-AUTH-01", 0)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.nonces[iid+nonce] {
		gwErr(w, 401, "GW-AUTH-01", 0)
		return
	}
	g.nonces[iid+nonce] = true
	var pubB64 string
	switch r.URL.Path {
	case "/v1/register":
		var req struct {
			PubKey string         `json:"pubkey"`
			Client map[string]any `json:"client"`
		}
		if json.Unmarshal(body, &req) != nil {
			gwErr(w, 400, "GW-REQ-01", 0)
			return
		}
		want, _ := identity.InstallID(req.PubKey)
		if want != iid {
			gwErr(w, 401, "GW-AUTH-01", 0)
			return
		}
		pubB64 = req.PubKey
		g.lastClient = req.Client
	case "/v1/issue", "/v1/catalog/connect", "/v1/catalog/index":
		pubB64 = g.keys[iid]
		if pubB64 == "" {
			gwErr(w, 401, "GW-AUTH-02", 0)
			return
		}
	default:
		gwErr(w, 404, "GW-REQ-01", 0)
		return
	}
	pub, err := identity.DecodePublicKey(pubB64)
	if err != nil || !identity.Verify(pub, r.Method, r.URL.Path, ts, nonce, body, sig) {
		gwErr(w, 401, "GW-AUTH-01", 0)
		return
	}
	if r.URL.Path == "/v1/register" {
		g.keys[iid] = pubB64
		g.registers++
		writeJSON(w, 200, map[string]any{"ok": true, "install_id": iid, "limits": map[string]any{"per_day": 20}})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/catalog/") {
		status, v := 404, any(map[string]any{"ok": false, "code": "GW-REQ-01", "message": "没有"})
		if g.catalog != nil {
			status, v = g.catalog(r.URL.Path, body)
		}
		writeJSON(w, status, v)
		return
	}
	g.issues++
	_ = json.Unmarshal(body, &g.lastIssue)
	if len(g.issueErrs) > 0 {
		e := g.issueErrs[0]
		g.issueErrs = g.issueErrs[1:]
		gwErr(w, e.status, e.code, e.retry)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "file_id": g.lastIssue["file_id"], "url": "https://s3.example.invalid/f?sig=x",
		"expires_at": "2026-11-05T02:24:27+08:00", "sha256": g.sha, "size": 123, "backend": "senluopan",
		"quota": map[string]any{"day_used": 3, "day_limit": 20}})
}

func newClient(t *testing.T, url string) *Client {
	t.Helper()
	id, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Client{BaseURL: url + "/", Identity: id, Info: DefaultInfo("0.4.0-test")}
}

func TestIssueRegistersOnceThenIssues(t *testing.T) {
	g := newFake()
	srv := httptest.NewServer(g)
	defer srv.Close()
	c := newClient(t, srv.URL)
	iss, err := c.Issue(context.Background(), "wsl-x64-3.0.3", "runtime")
	if err != nil {
		t.Fatal(err)
	}
	if iss.URL == "" || iss.SHA256 != g.sha || iss.Size != 123 || iss.Backend != "senluopan" || iss.DayLimit != 20 || iss.ExpiresAt.IsZero() {
		t.Fatalf("%+v", iss)
	}
	if g.registers != 1 || g.issues != 1 {
		t.Fatalf("registers %d issues %d (the first issue is refused before counting)", g.registers, g.issues)
	}
	if g.lastIssue["file_id"] != "wsl-x64-3.0.3" || g.lastIssue["purpose"] != "runtime" {
		t.Fatalf("issue body %v", g.lastIssue)
	}
	if v, ok := g.lastIssue["app"]; !ok || v != nil {
		t.Fatal("app must be sent as null")
	}
	if g.lastClient["via"] != "acornfox" || g.lastClient["version"] != "0.4.0-test" {
		t.Fatalf("client info %v", g.lastClient)
	}
	// Registered now: no second registration.
	if _, err := c.Issue(context.Background(), "wsl-x64-3.0.3", "runtime"); err != nil {
		t.Fatal(err)
	}
	if g.registers != 1 {
		t.Fatal("registered again")
	}
}

func TestIssueErrorsAreTyped(t *testing.T) {
	for _, e := range []fakeErr{
		{"GW-ART-01", 404, 0}, {"GW-OFF-01", 503, 0}, {"GW-LIM-01", 429, 0}, {"GW-AUTH-03", 403, 0},
		{"GW-BE-01", 502, 0}, {"GW-REG-01", 429, 0}, {"GW-LIM-02", 429, 3600},
	} {
		g := newFake()
		srv := httptest.NewServer(g)
		c := newClient(t, srv.URL)
		g.issueErrs = []fakeErr{e}
		_, err := c.Issue(context.Background(), "x", "runtime")
		var ge *Error
		if !errors.As(err, &ge) || ge.Code != e.code || ge.HTTPStatus != e.status {
			t.Fatalf("%s: got %v", e.code, err)
		}
		if e.code == "GW-LIM-02" && ge.RetryAfter != time.Hour {
			t.Fatalf("retry after %v", ge.RetryAfter)
		}
		if CodeOf(err) != e.code {
			t.Fatal("CodeOf")
		}
		srv.Close()
	}
}

func TestLim02ShortWaitRetriesOnce(t *testing.T) {
	g := newFake()
	srv := httptest.NewServer(g)
	defer srv.Close()
	c := newClient(t, srv.URL)
	var waited []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error { waited = append(waited, d); return nil }
	g.issueErrs = []fakeErr{{"GW-LIM-02", 429, 2}}
	if _, err := c.Issue(context.Background(), "x", "runtime"); err != nil {
		t.Fatal(err)
	}
	if len(waited) != 1 || waited[0] != 2*time.Second {
		t.Fatalf("waited %v", waited)
	}
	// Twice in a row: give up after one retry.
	g.issueErrs = []fakeErr{{"GW-LIM-02", 429, 1}, {"GW-LIM-02", 429, 1}}
	if _, err := c.Issue(context.Background(), "x", "runtime"); CodeOf(err) != "GW-LIM-02" {
		t.Fatalf("got %v", err)
	}
}

// A clock an hour behind: the gateway refuses (GW-AUTH-01), its Date header
// gives its time, and the request signed again with the corrected timestamp
// goes through; the correction is kept for the next requests.
func TestBadClockCorrectedFromDate(t *testing.T) {
	g := newFake()
	srv := httptest.NewServer(g)
	defer srv.Close()
	c := newClient(t, srv.URL)
	c.now = func() time.Time { return time.Now().Add(-time.Hour) }
	if _, err := c.Issue(context.Background(), "x", "runtime"); err != nil {
		t.Fatalf("should be corrected: %v", err)
	}
	if off := c.ClockOffset(); off < 59*time.Minute || off > 61*time.Minute {
		t.Fatalf("offset %v", off)
	}
	g.mu.Lock()
	n := len(g.nonces)
	g.mu.Unlock()
	if _, err := c.Issue(context.Background(), "x", "runtime"); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.nonces)-n != 1 {
		t.Fatalf("the second issue took %d requests; the correction should be kept", len(g.nonces)-n)
	}
}

// Without a Date header the client cannot know the gateway's time: the
// GW-AUTH-01 is returned as it is, without a retry.
func TestBadClockWithoutDateNotRetried(t *testing.T) {
	g := newFake()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Date"] = nil // net/http adds one otherwise
		g.ServeHTTP(w, r)
	}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	c.now = func() time.Time { return time.Now().Add(-time.Hour) }
	_, err := c.Issue(context.Background(), "x", "runtime")
	var ge *Error
	if !errors.As(err, &ge) || ge.Code != "GW-AUTH-01" || ge.ClockSkew != 0 || c.ClockOffset() != 0 {
		t.Fatalf("got %v (offset %v)", err, c.ClockOffset())
	}
}

// A gateway that still refuses after the correction: the error says how far
// the clocks are apart and how to fix it.
func TestBadClockStillRefusedSaysSkew(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
		gwErr(w, 401, "GW-AUTH-01", 0)
	}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	c.now = func() time.Time { return time.Now().Add(-1000 * time.Second) }
	_, err := c.Register(context.Background(), c.Info)
	var ge *Error
	if !errors.As(err, &ge) || ge.Code != "GW-AUTH-01" {
		t.Fatalf("got %v", err)
	}
	if calls != 2 {
		t.Fatalf("%d requests, want the first and one corrected retry", calls)
	}
	if ge.ClockSkew < 998*time.Second || ge.ClockSkew > 1002*time.Second ||
		!(strings.Contains(ge.Message, "时钟和网关差了约 999 秒") || strings.Contains(ge.Message, "时钟和网关差了约 1000 秒")) ||
		!strings.Contains(ge.Message, "timedatectl set-ntp true") {
		t.Fatalf("skew %v message %q", ge.ClockSkew, ge.Message)
	}
}

// A small difference is not what made the gateway refuse (it allows 300
// seconds): no retry, no clock hint.
func TestAuth01WithGoodClockNotRetried(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gwErr(w, 401, "GW-AUTH-01", 0)
	}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	_, err := c.Register(context.Background(), c.Info)
	if CodeOf(err) != "GW-AUTH-01" || calls != 1 || strings.Contains(err.Error(), "时钟") {
		t.Fatalf("calls %d err %v", calls, err)
	}
}

func TestUnreachableAndUnreadable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := newClient(t, url)
	if _, err := c.Issue(context.Background(), "x", "runtime"); CodeOf(err) != "GW-NET-01" {
		t.Fatalf("closed: %v", err)
	}
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>captive portal</html>")) }))
	defer html.Close()
	c.BaseURL = html.URL
	if _, err := c.Issue(context.Background(), "x", "runtime"); CodeOf(err) != "GW-RESP-01" {
		t.Fatalf("html: %v", err)
	}
	// A hanging gateway is cut off by the client timeout.
	// (The body is read first: the server only notices a closed connection
	// once the request body is consumed.)
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer hang.Close()
	c.BaseURL = hang.URL
	c.HTTP = &http.Client{Timeout: 200 * time.Millisecond}
	start := time.Now()
	if _, err := c.Issue(context.Background(), "x", "runtime"); CodeOf(err) != "GW-NET-01" {
		t.Fatalf("hang: %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout not applied")
	}
	c.BaseURL = ""
	if _, err := c.Issue(context.Background(), "x", "runtime"); CodeOf(err) != "GW-NET-01" {
		t.Fatalf("empty base: %v", err)
	}
}

func TestBadIssuedURLRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "url": "file:///etc/passwd", "sha256": strings.Repeat("a", 64)})
	}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	if _, err := c.Issue(context.Background(), "x", "runtime"); CodeOf(err) != "GW-RESP-01" {
		t.Fatalf("got %v", err)
	}
}

func TestRegisterIdempotent(t *testing.T) {
	g := newFake()
	srv := httptest.NewServer(g)
	defer srv.Close()
	c := newClient(t, srv.URL)
	for i := 0; i < 2; i++ {
		r, err := c.Register(context.Background(), c.Info)
		if err != nil || r.InstallID != c.Identity.InstallID || r.PerDay != 20 {
			t.Fatalf("%+v %v", r, err)
		}
	}
}

func TestCodesCoverContract(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Codes {
		if seen[c.Code] || !strings.HasPrefix(c.Code, "GW-") || c.Meaning == "" || c.Client == "" {
			t.Fatalf("bad entry %+v", c)
		}
		seen[c.Code] = true
	}
	for _, c := range []string{"GW-REQ-01", "GW-REG-01", "GW-AUTH-01", "GW-AUTH-02", "GW-AUTH-03", "GW-LIM-01", "GW-LIM-02", "GW-OFF-01", "GW-ART-01", "GW-BE-01", "GW-NIMP-01", "GW-NET-01", "GW-RESP-01"} {
		if !seen[c] {
			t.Fatalf("missing %s", c)
		}
	}
}
