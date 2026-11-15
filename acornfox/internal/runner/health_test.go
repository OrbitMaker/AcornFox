package runner

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestProbeAddr(t *testing.T) {
	cases := []struct {
		name     string
		ci       ContainerInfo
		port     int
		internal bool
		want     string
	}{
		{"published port", ContainerInfo{HostPort: 41000, Port: 8080, IP: "172.18.0.2"}, 8080, false, "127.0.0.1:41000"},
		{"published, port not said", ContainerInfo{HostPort: 41000, IP: "172.18.0.2"}, 8080, false, "127.0.0.1:41000"},
		{"published, any port", ContainerInfo{HostPort: 41000, Port: 8080}, 0, false, "127.0.0.1:41000"},
		// Not internal: the published port, as before, whatever port is asked.
		{"published, other port", ContainerInfo{HostPort: 41000, Port: 8080, IP: "172.18.0.2"}, 9000, false, "127.0.0.1:41000"},
		// Not internal and nothing published: not up (no route), never its
		// address on the app network.
		{"not internal, not published", ContainerInfo{IP: "172.18.0.2", Port: 8080}, 8080, false, ""},
		// An internal app publishes nothing: its address on the app network.
		{"internal", ContainerInfo{IP: "172.18.0.2"}, 5432, true, "172.18.0.2:5432"},
		{"internal, other port", ContainerInfo{IP: "172.18.0.2"}, 9000, true, "172.18.0.2:9000"},
		{"ipv6", ContainerInfo{IP: "fd00::2"}, 5432, true, "[fd00::2]:5432"},
		{"no address yet", ContainerInfo{}, 5432, true, ""},
		{"no port", ContainerInfo{IP: "172.18.0.2"}, 0, true, ""},
	}
	for _, c := range cases {
		if got := ProbeAddr(c.ci, c.port, c.internal); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestProbeTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ProbeTCP(context.Background(), addr); err != nil {
		t.Fatalf("listening: %v", err)
	}
	ln.Close()
	if err := ProbeTCP(context.Background(), addr); err == nil || !strings.Contains(err.Error(), "连不上") {
		t.Fatalf("closed: %v", err)
	}
	if err := ProbeTCP(context.Background(), ""); err == nil {
		t.Fatal("no address must fail")
	}
}

// fakeExec answers health commands.
type fakeExec struct {
	res  ExecResult
	err  error
	reqs []ExecRequest
}

func (f *fakeExec) Exec(_ context.Context, req ExecRequest) (ExecResult, error) {
	f.reqs = append(f.reqs, req)
	return f.res, f.err
}

func TestProbeExec(t *testing.T) {
	ctx := context.Background()
	cmd := []string{"demo-ping", "--host", "127.0.0.1"}
	if err := ProbeExec(ctx, nil, "db", "af-db-aaaaaaaaaaaa", cmd, ""); !errors.Is(err, ErrNoExec) {
		t.Fatalf("no ExecAPI: %v", err)
	}
	f := &fakeExec{res: ExecResult{ExitCode: 0, Stdout: []byte("  PONG\n")}}
	if err := ProbeExec(ctx, f, "db", "af-db-aaaaaaaaaaaa", cmd, ""); err != nil {
		t.Fatalf("exit 0: %v", err)
	}
	r := f.reqs[0]
	if r.App != "db" || r.Name != "af-db-aaaaaaaaaaaa" || strings.Join(r.Command, " ") != "demo-ping --host 127.0.0.1" || r.TimeoutS != HealthExecTimeoutS || r.User != "" || len(r.Env) != 0 || len(r.Stdin) != 0 {
		t.Fatalf("request %+v", r)
	}
	if err := ValidateExec(r); err != nil {
		t.Fatalf("the request must be one the runner takes: %v", err)
	}
	// expect_stdout: the trimmed output must be equal.
	if err := ProbeExec(ctx, f, "db", "af-db-aaaaaaaaaaaa", cmd, "PONG"); err != nil {
		t.Fatalf("expected output: %v", err)
	}
	if err := ProbeExec(ctx, f, "db", "af-db-aaaaaaaaaaaa", cmd, "LOADING"); err == nil || !strings.Contains(err.Error(), "预期不符") || strings.Contains(err.Error(), "PONG") {
		t.Fatalf("other output: %v", err)
	}
	f.res = ExecResult{ExitCode: 0, Stdout: []byte("PONG"), StdoutTruncated: true, Truncated: true}
	if err := ProbeExec(ctx, f, "db", "af-db-aaaaaaaaaaaa", cmd, "PONG"); err == nil {
		t.Fatal("cut output must not pass an expected output")
	}
	f.res = ExecResult{ExitCode: 2, Stderr: []byte("no server on 127.0.0.1")}
	if err := ProbeExec(ctx, f, "db", "af-db-aaaaaaaaaaaa", cmd, ""); err == nil || !strings.Contains(err.Error(), "退出码 2") || strings.Contains(err.Error(), "no server") {
		t.Fatalf("exit 2: %v", err)
	}
	f.res = ExecResult{ExitCode: -1, TimedOut: true}
	if err := ProbeExec(ctx, f, "db", "af-db-aaaaaaaaaaaa", cmd, ""); err == nil || !strings.Contains(err.Error(), "没结束") {
		t.Fatalf("timed out: %v", err)
	}
	for in, want := range map[error]string{
		ErrUnsupported: ErrNoExec.Error(),
		ErrStopped:     "没在运行",
		ErrNotFound:    "没在运行",
		&RemoteError{Status: 500, ErrorResponse: ErrorResponse{Code: CodeExecFailed, Message: "no such program"}}: "没能在容器里运行",
	} {
		f.err = in
		if err := ProbeExec(ctx, f, "db", "af-db-aaaaaaaaaaaa", cmd, ""); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: %v, want %q", in, err, want)
		}
	}
	if err := ProbeExec(ctx, f, "db", "af-db-aaaaaaaaaaaa", nil, ""); err == nil {
		t.Fatal("no command must fail")
	}
}
