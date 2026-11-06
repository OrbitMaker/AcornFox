package runner

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClientLogTailRoundTrip(t *testing.T) {
	data := []byte("2026-11-03T01:02:03.000000004Z <ok> & \xff raw\n")
	api := &fakeAPI{logTailResp: LogTailResult{Data: data, Truncated: true, Env: []string{"A=1", "SECRET=<s&>"}}}
	_, cli := newTestServer(t, api)

	res, err := cli.LogTail(context.Background(), "demo", "af-demo-0123456789ab", 5<<20)
	if err != nil {
		t.Fatalf("LogTail: %v", err)
	}
	if string(res.Data) != string(data) || !res.Truncated || len(res.Env) != 2 || res.Env[1] != "SECRET=<s&>" {
		t.Fatalf("unexpected result: %+v", res)
	}
	api.mu.Lock()
	req := api.logTailReq
	api.mu.Unlock()
	if req.App != "demo" || req.Name != "af-demo-0123456789ab" || req.MaxBytes != LogTailMaxBytes {
		t.Fatalf("server passed %+v (max_bytes must be clamped to %d)", req, LogTailMaxBytes)
	}
}

func TestClientLogTailEmptyAndDefaults(t *testing.T) {
	api := &fakeAPI{}
	_, cli := newTestServer(t, api)
	res, err := cli.LogTail(context.Background(), "demo", "af-demo-0123456789ab", 0)
	if err != nil {
		t.Fatalf("LogTail: %v", err)
	}
	if res.Data == nil || len(res.Data) != 0 || res.Env == nil || res.Truncated {
		t.Fatalf("empty result not normalized: %+v", res)
	}
	if api.logTailReq.MaxBytes != LogTailMaxBytes {
		t.Fatalf("default max_bytes = %d", api.logTailReq.MaxBytes)
	}
}

func TestClientLogTailFullSizeFitsBound(t *testing.T) {
	// 1 MiB of bytes that would be expensive as a JSON string still fits the
	// large bound as base64.
	data := []byte(strings.Repeat("\x01<\"\\\n", LogTailMaxBytes/5))
	env := make([]string, 200)
	for i := range env {
		env[i] = "K=" + strings.Repeat("v", 1000)
	}
	api := &fakeAPI{logTailResp: LogTailResult{Data: data, Env: env}}
	_, cli := newTestServer(t, api)
	res, err := cli.LogTail(context.Background(), "demo", "af-demo-0123456789ab", LogTailMaxBytes)
	if err != nil {
		t.Fatalf("LogTail: %v", err)
	}
	if len(res.Data) != len(data) || len(res.Env) != len(env) {
		t.Fatalf("lost data: %d bytes, %d env", len(res.Data), len(res.Env))
	}
}

func TestClientLogTailErrors(t *testing.T) {
	_, cli := newTestServer(t, &fakeAPI{logTailErr: ErrNotFound})
	if _, err := cli.LogTail(context.Background(), "demo", "af-demo-0123456789ab", 0); !errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnsupported) {
		t.Fatalf("missing container: %v", err)
	}
	_, cli = newTestServer(t, &fakeAPI{logTailErr: refusedContainer("af-demo-0123456789ab", "demo")})
	_, err := cli.LogTail(context.Background(), "demo", "af-demo-0123456789ab", 0)
	var re *RemoteError
	if !errors.As(err, &re) || re.Code != "refused" || errors.Is(err, ErrUnsupported) {
		t.Fatalf("refused: %v", err)
	}
	// Server-side validation happens before the capability check.
	_, err = cli.LogTail(context.Background(), "demo", "af-other-0123456789ab", 0)
	assertInvalidRequest(t, err)
}

// apiOnly hides every optional capability of the wrapped implementation.
type apiOnly struct{ API }

func TestServerLogTailUnsupported(t *testing.T) {
	_, cli := newTestServer(t, apiOnly{&fakeAPI{}})
	_, err := cli.LogTail(context.Background(), "demo", "af-demo-0123456789ab", 0)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
	var re *RemoteError
	if !errors.As(err, &re) || re.Status != http.StatusNotImplemented || re.Code != "unsupported" {
		t.Fatalf("want 501 unsupported, got %v", err)
	}
}

// An older runner has no such route: the mux answers a plain 404.
func TestClientLogTailOldRunner(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+PathPing, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, PingResponse{})
	})
	cli := startTestServer(t, &Server{api: &fakeAPI{}, mux: mux})
	_, err := cli.LogTail(context.Background(), "demo", "af-demo-0123456789ab", 0)
	if !errors.Is(err, ErrUnsupported) || errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

func TestClientResponseTooLarge(t *testing.T) {
	api := &fakeAPI{pingResp: PingResponse{ServerVersion: strings.Repeat("v", clientDefaultBound+10)}}
	_, cli := newTestServer(t, api)
	_, err := cli.Ping(context.Background())
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("want ErrResponseTooLarge, got %v", err)
	}
	if errors.Is(err, ErrUnavailable) {
		t.Fatalf("too large must not look like unavailable: %v", err)
	}
}

func TestClientBuildCarriesLog(t *testing.T) {
	log := omittedHeader(3<<10) + "Step 1/1 : RUN echo <hi> && true\n<hi>\n"
	api := &fakeAPI{buildResp: BuildResponse{OK: true, Image: &ImageInfo{ID: "sha256:abc"}, Log: log}}
	uploadDir := t.TempDir()
	cli := startTestServer(t, NewServer(api, uploadDir))
	resp, err := cli.Build(context.Background(), BuildRequest{App: "demo", DeploymentID: "0123456789ab", ContextPath: uploadDir + "/x.tar"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if resp.Log != log {
		t.Fatalf("log = %q", resp.Log)
	}
}

func TestClientBuildHugeLogShrunkByServer(t *testing.T) {
	// The server keeps the encoded reply within buildResponseMaxBytes, so even
	// a log of escape-heavy bytes never trips the client's bound.
	var b strings.Builder
	for b.Len() < BuildLogMaxBytes {
		b.WriteString(strings.Repeat("\x02", 127) + "\n")
	}
	api := &fakeAPI{buildResp: BuildResponse{OK: false, Failure: &Failure{Stage: "build", Code: "build_failed"}, Log: b.String()}}
	uploadDir := t.TempDir()
	cli := startTestServer(t, NewServer(api, uploadDir))
	resp, err := cli.Build(context.Background(), BuildRequest{App: "demo", DeploymentID: "0123456789ab", ContextPath: uploadDir + "/x.tar"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !omittedPattern.MatchString(resp.Log) || len(resp.Log) >= b.Len() {
		t.Fatalf("log not shrunk: %d bytes", len(resp.Log))
	}
	if resp.Failure == nil || resp.Failure.Code != "build_failed" {
		t.Fatalf("failure lost: %+v", resp)
	}
}

func TestLogTailResult(t *testing.T) {
	out := newTailBuffer(32)
	out.Write([]byte("2026-11-03T00:00:00Z first line\n2026-11-03T00:00:01Z 第二行\n"))
	res := logTailResult(out, nil)
	if !res.Truncated || !strings.HasPrefix(string(res.Data), "2026-11-03T00:00:01Z") || !utf8.Valid(res.Data) {
		t.Fatalf("truncated tail: %+v %q", res, res.Data)
	}
	if res.Env == nil {
		t.Fatalf("nil env")
	}

	small := newTailBuffer(1 << 10)
	small.Write([]byte("a\nb\n"))
	if res := logTailResult(small, []string{"X=1"}); res.Truncated || string(res.Data) != "a\nb\n" || res.Env[0] != "X=1" {
		t.Fatalf("untruncated: %+v", res)
	}

	// Docker's line cap reached: Truncated even though no byte was dropped.
	many := newTailBuffer(LogTailMaxBytes)
	many.Write([]byte(strings.Repeat("x\n", logTailLines)))
	if res := logTailResult(many, nil); !res.Truncated {
		t.Fatalf("line cap not reported")
	}
}

func TestClampLogTailBytes(t *testing.T) {
	for in, want := range map[int]int{-1: LogTailMaxBytes, 0: LogTailMaxBytes, 10: 10, LogTailMaxBytes: LogTailMaxBytes, LogTailMaxBytes + 1: LogTailMaxBytes} {
		if got := clampLogTailBytes(in); got != want {
			t.Errorf("clamp(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestTimestampedLines(t *testing.T) {
	data := []byte("2026-11-03T00:00:02Z b \x1b[1mbold\x1b[0m\r\n" +
		"2026-11-03T00:00:01Z a\n" +
		"continuation without stamp\n" +
		"2026-11-03T00:00:02Z c\n" +
		"2026-11-03T00:00:03Z \n")
	got := timestampedLines(data, 100)
	want := []string{"a", "continuation without stamp", "b bold", "c", ""}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	if got := timestampedLines(data, 2); strings.Join(got, "|") != "c|" {
		t.Fatalf("tail 2 = %q", got)
	}
	if got := timestampedLines(nil, 10); len(got) != 0 {
		t.Fatalf("empty = %q", got)
	}
}
