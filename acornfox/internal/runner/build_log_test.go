package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// buildStream encodes BuilderV1 messages the way the daemon streams them.
func buildStream(t *testing.T, msgs ...buildMessage) io.Reader {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, m := range msgs {
		if err := enc.Encode(m); err != nil {
			t.Fatal(err)
		}
	}
	return &buf
}

func streamMsg(s string) buildMessage { return buildMessage{Stream: s} }

func TestDrainBuildSuccessKeepsLog(t *testing.T) {
	out, err := drainBuild(buildStream(t,
		streamMsg("Step 1/2 : FROM alpine\n"),
		streamMsg(" ---> abc123\n"),
		streamMsg("\x1b[32mgreen <b>&amp;</b>\x1b[0m\n"),
		streamMsg("Successfully built abc123\n"),
	))
	if err != nil {
		t.Fatalf("drainBuild: %v", err)
	}
	log := tailText(out)
	want := "Step 1/2 : FROM alpine\n ---> abc123\ngreen <b>&amp;</b>\nSuccessfully built abc123\n"
	if log != want {
		t.Fatalf("log = %q, want %q", log, want)
	}
}

func TestDrainBuildBoundsLargeOutput(t *testing.T) {
	var msgs []buildMessage
	msgs = append(msgs, streamMsg("Step 1/3 : FROM alpine\n"))
	line := strings.Repeat("x", 99) + "\n" // 100 bytes
	for i := 0; i < 8000; i++ {            // ~800 KB, more than BuildLogMaxBytes
		msgs = append(msgs, streamMsg(fmt.Sprintf("%05d", i)+line[5:]))
	}
	msgs = append(msgs, streamMsg("Step 3/3 : RUN false\n"), streamMsg("boom\n"),
		buildMessage{Error: "The command '/bin/sh -c false' returned a non-zero code: 1"})
	out, err := drainBuild(buildStream(t, msgs...))
	if err == nil {
		t.Fatalf("want build error")
	}
	f := classifyBuild(err, logLines(tailText(out)))
	if f.LogExcerpt != "Step 3/3 : RUN false\nboom" {
		t.Fatalf("excerpt = %q", f.LogExcerpt)
	}
	log := appendLogLine(tailText(out), err.Error())
	m := omittedPattern.FindString(log)
	if m == "" {
		t.Fatalf("no omitted header: %q", log[:60])
	}
	body := log[len(m):]
	if len(body) > BuildLogMaxBytes {
		t.Fatalf("log body %d bytes > %d", len(body), BuildLogMaxBytes)
	}
	if !strings.HasPrefix(body, "0") || len(strings.SplitN(body, "\n", 2)[0]) != 99 {
		t.Fatalf("log does not start at a whole line: %q", body[:40])
	}
	if !strings.HasSuffix(log, "boom\nThe command '/bin/sh -c false' returned a non-zero code: 1\n") {
		t.Fatalf("error not appended to log: %q", log[len(log)-120:])
	}
}

func TestDrainBuildHugeSingleLine(t *testing.T) {
	// A failing step printing a 300 KB line full of < > and color codes.
	huge := strings.Repeat("\x1b[31m<x>&\x1b[0m", 300<<10/len("<x>&"))
	out, err := drainBuild(buildStream(t,
		streamMsg("Step 2/2 : RUN ./gen\n"),
		streamMsg(huge+"\n"),
		buildMessage{Error: "returned a non-zero code: 2"},
	))
	if err == nil {
		t.Fatal("want error")
	}
	log := tailText(out)
	if strings.Contains(log, "\x1b[") {
		t.Fatalf("ANSI codes kept")
	}
	if len(log) > BuildLogMaxBytes+64 {
		t.Fatalf("log too large: %d", len(log))
	}
	f := classifyBuild(err, logLines(log))
	lines := strings.Split(f.LogExcerpt, "\n")
	if len(lines) != 2 {
		t.Fatalf("excerpt lines = %d: %q", len(lines), f.LogExcerpt)
	}
	if !strings.HasSuffix(lines[1], lineCutMarker) || len(lines[1]) > maxExcerptLineBytes+len(lineCutMarker) {
		t.Fatalf("huge line not capped: len %d", len(lines[1]))
	}

	// The reply carries it unescaped and within the bound.
	rec := httptest.NewRecorder()
	writeBuildResponse(rec, BuildResponse{OK: false, Failure: f, Log: log})
	if !strings.Contains(rec.Body.String(), "<x>&") {
		t.Fatalf("HTML characters escaped in reply")
	}
	if rec.Body.Len() > buildResponseMaxBytes {
		t.Fatalf("reply %d bytes", rec.Body.Len())
	}
}

func TestBuildExcerptRules(t *testing.T) {
	var lines []string
	lines = append(lines, "Step 1/2 : FROM alpine", " ---> 111")
	lines = append(lines, "Step 2/2 : RUN make", " ---> Running in 222", "")
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("out %d \x1b[1m", i))
	}
	got := strings.Split(buildExcerpt(lines), "\n")
	if len(got) != MaxLogExcerptLine {
		t.Fatalf("excerpt has %d lines", len(got))
	}
	if got[0] != "out 10" || got[39] != "out 49" {
		t.Fatalf("excerpt = %q ... %q", got[0], got[39])
	}
	short := buildExcerpt([]string{"Step 1/1 : RUN x", " ---> abc", "failed: " + strings.Repeat("字", 1000)})
	parts := strings.Split(short, "\n")
	if parts[0] != "Step 1/1 : RUN x" || !strings.HasSuffix(parts[1], lineCutMarker) || !utf8.ValidString(parts[1]) {
		t.Fatalf("excerpt = %q", short)
	}
}

func TestLogLinesSkipsHeaderAndBlanks(t *testing.T) {
	got := logLines(omittedHeader(2048) + "a\r\n\n  \nb\n")
	if strings.Join(got, "|") != "a|b" {
		t.Fatalf("logLines = %q", got)
	}
}

func TestWriteBuildResponseShrinksLog(t *testing.T) {
	old := buildResponseMaxBytes
	buildResponseMaxBytes = 64 << 10
	defer func() { buildResponseMaxBytes = old }()

	// Control bytes encode as \u00XX (6x): 40 KB raw is ~240 KB encoded.
	var b strings.Builder
	for b.Len() < 40<<10 {
		b.WriteString(strings.Repeat("\x01", 63) + "\n")
	}
	log := omittedHeader(100<<10) + b.String()
	rec := httptest.NewRecorder()
	writeBuildResponse(rec, BuildResponse{OK: true, Image: &ImageInfo{ID: "sha256:x"}, Log: log})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Body.Len() > buildResponseMaxBytes {
		t.Fatalf("reply %d > %d", rec.Body.Len(), buildResponseMaxBytes)
	}
	var resp BuildResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	m := omittedPattern.FindStringSubmatch(resp.Log)
	if m == nil {
		t.Fatalf("no header: %q", resp.Log[:min(40, len(resp.Log))])
	}
	if kb, _ := strconv.Atoi(m[1]); kb <= 100 {
		t.Fatalf("header not accumulated: %q", resp.Log[:min(40, len(resp.Log))])
	}
	if body := resp.Log[len(m[0]):]; !strings.HasPrefix(body, "\x01") || strings.Count(body, "\n")*64 != len(body) {
		t.Fatalf("shrunk log not aligned to whole lines")
	}
	if !resp.OK || resp.Image == nil {
		t.Fatalf("other fields lost: %+v", resp)
	}
}

func TestWriteBuildResponseSmallUntouched(t *testing.T) {
	rec := httptest.NewRecorder()
	writeBuildResponse(rec, BuildResponse{OK: true, Log: "a < b\n"})
	if got := rec.Body.String(); got != "{\"ok\":true,\"log\":\"a < b\\n\"}\n" {
		t.Fatalf("body = %q", got)
	}
}

// ---- build context scanning ----

type tarEntry struct {
	name string
	body string
	dir  bool
}

func makeTar(t *testing.T, gz bool, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	var w io.Writer = &buf
	var zw *gzip.Writer
	if gz {
		zw = gzip.NewWriter(&buf)
		w = zw
	}
	tw := tar.NewWriter(w)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body))}
		if e.dir {
			h = &tar.Header{Name: e.name, Mode: 0o755, Typeflag: tar.TypeDir}
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if !e.dir {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if zw != nil {
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

func TestScanContext(t *testing.T) {
	big := strings.Repeat("z", 64<<10)
	for _, gz := range []bool{false, true} {
		name := map[bool]string{false: "tar", true: "tar.gz"}[gz]
		cases := []struct {
			label   string
			data    []byte
			limit   int64
			want    string // "" = ok, else Failure.Code
			message string
		}{
			{"root Dockerfile", makeTar(t, gz, tarEntry{name: "app.py", body: "x"}, tarEntry{name: "Dockerfile", body: "FROM alpine"}), 0, "", ""},
			{"dot slash Dockerfile", makeTar(t, gz, tarEntry{name: "./", dir: true}, tarEntry{name: "./Dockerfile", body: "FROM alpine"}), 0, "", ""},
			{"Dockerfile only in subdir", makeTar(t, gz, tarEntry{name: "sub/Dockerfile", body: "FROM alpine"}), 0, "dockerfile_missing", ""},
			{"empty tar", makeTar(t, gz), 0, "dockerfile_missing", ""},
			{"too large after Dockerfile", makeTar(t, gz, tarEntry{name: "Dockerfile", body: "FROM alpine"}, tarEntry{name: "data.bin", body: big}), 32 << 10, "context_too_large", "项目包解压后超过 32 KB"},
			{"within limit", makeTar(t, gz, tarEntry{name: "Dockerfile", body: "FROM alpine"}, tarEntry{name: "data.bin", body: big}), 1 << 20, "", ""},
		}
		for _, c := range cases {
			old := maxContextBytes
			if c.limit > 0 {
				maxContextBytes = c.limit
			}
			f := scanContext(bytes.NewReader(c.data))
			maxContextBytes = old
			got := ""
			if f != nil {
				got = f.Code
				if f.Stage != "upload" {
					t.Errorf("%s/%s: stage %q", name, c.label, f.Stage)
				}
			}
			if got != c.want {
				t.Errorf("%s/%s: got %q, want %q", name, c.label, got, c.want)
			}
			if c.message != "" && (f == nil || f.Message != c.message) {
				t.Errorf("%s/%s: message %+v", name, c.label, f)
			}
		}
	}
}

func TestScanContextInvalid(t *testing.T) {
	for label, data := range map[string][]byte{
		"not a tar":     []byte("this is not an archive at all, just some text that is long enough............................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................"),
		"broken gzip":   {0x1f, 0x8b, 0x08, 0x00, 0x01},
		"truncated tar": makeTar(t, false, tarEntry{name: "Dockerfile", body: strings.Repeat("a", 4096)})[:1024],
		"gzip of junk":  gzipBytes(t, []byte(strings.Repeat("junk", 1000))),
	} {
		f := scanContext(bytes.NewReader(data))
		if f == nil || f.Code != "upload_invalid" {
			t.Errorf("%s: got %+v, want upload_invalid", label, f)
		}
	}
}

func gzipBytes(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(b)
	zw.Close()
	return buf.Bytes()
}

// runBuild rejects bad uploads before touching Docker (the zero Docker has no
// client, so reaching ImageBuild would panic).
func TestRunBuildRejectsBeforeDocker(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := maxContextBytes
	maxContextBytes = 16 << 10
	defer func() { maxContextBytes = old }()

	cases := map[string]struct {
		path string
		want string
	}{
		"missing file":  {filepath.Join(dir, "nope.tar.gz"), "upload_invalid"},
		"no Dockerfile": {write("a.tar.gz", makeTar(t, true, tarEntry{name: "main.go", body: "package main"})), "dockerfile_missing"},
		"too large":     {write("b.tar", makeTar(t, false, tarEntry{name: "Dockerfile", body: "FROM x"}, tarEntry{name: "blob", body: strings.Repeat("b", 32<<10)})), "context_too_large"},
	}
	d := &Docker{}
	for label, c := range cases {
		resp, err := d.runBuild(context.Background(), BuildRequest{App: "demo", DeploymentID: "0123456789ab", ContextPath: c.path}, ImageTag("demo", "0123456789ab"))
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if resp.OK || resp.Failure == nil || resp.Failure.Code != c.want || resp.Log != "" {
			t.Fatalf("%s: got %+v", label, resp)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{1 << 30: "1 GB", 3 << 30: "3 GB", 512 << 20: "512 MB", 32 << 10: "32 KB", 1: "1 KB"} {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestAppendLogLine(t *testing.T) {
	if got := appendLogLine("", "\x1b[31merr\x1b[0m"); got != "err\n" {
		t.Fatalf("empty log: %q", got)
	}
	if got := appendLogLine("a", "err"); got != "a\nerr\n" {
		t.Fatalf("no trailing newline: %q", got)
	}
	full := strings.Repeat(strings.Repeat("y", 1023)+"\n", BuildLogMaxBytes/1024)
	got := appendLogLine(full, "final error")
	m := omittedPattern.FindString(got)
	if m == "" || len(got)-len(m) > BuildLogMaxBytes || !strings.HasSuffix(got, "\nfinal error\n") {
		t.Fatalf("bound not kept: header %q len %d", m, len(got))
	}
}
