package deploylog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const (
	id1 = "0123456789ab"
	id2 = "bbbbbbbbbbbb"
	id3 = "cccccccccccc"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "logs", "deployments"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOpenWritesSinceOnce(t *testing.T) {
	root := filepath.Join(t.TempDir(), "deployments")
	first := time.Date(2026, 11, 3, 1, 2, 3, 0, time.UTC)
	s, err := openAt(root, func() time.Time { return first })
	if err != nil {
		t.Fatal(err)
	}
	if !s.Since().Equal(first) {
		t.Fatalf("since=%v", s.Since())
	}
	raw, _ := os.ReadFile(filepath.Join(root, sinceFile))
	if strings.TrimSpace(string(raw)) != "2026-11-03T01:02:03Z" {
		t.Fatalf("marker=%q", raw)
	}
	later := first.Add(48 * time.Hour)
	s2, err := openAt(root, func() time.Time { return later })
	if err != nil {
		t.Fatal(err)
	}
	if !s2.Since().Equal(first) {
		t.Fatalf("since changed: %v", s2.Since())
	}
	// A corrupt marker is replaced rather than making the store unusable.
	if err := os.WriteFile(filepath.Join(root, sinceFile), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	s3, err := openAt(root, func() time.Time { return later })
	if err != nil {
		t.Fatal(err)
	}
	if !s3.Since().Equal(later) {
		t.Fatalf("since after corrupt=%v", s3.Since())
	}
}

func TestPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	s := openTest(t)
	if err := s.Write("web", id1, KindRun, []byte("hello\n"), NewRedactor()); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{s.Root(), filepath.Join(s.Root(), "web"), filepath.Join(s.Root(), "web", id1)} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Fatalf("%s perm=%o", p, perm)
		}
	}
	info, err := os.Stat(filepath.Join(s.Root(), "web", id1, "run.log"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file perm=%o", perm)
	}
	// No temp files are left behind.
	entries, _ := os.ReadDir(filepath.Join(s.Root(), "web", id1))
	if len(entries) != 1 {
		t.Fatalf("entries=%v", entries)
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	s := openTest(t)
	if err := s.Write("web", id1, KindBuild, []byte("step 1\r\nstep 2\nstep 3\n"), NewRedactor()); err != nil {
		t.Fatal(err)
	}
	lines, truncated, err := s.Read("web", id1, KindBuild, 0)
	if err != nil || truncated {
		t.Fatalf("err=%v truncated=%v", err, truncated)
	}
	if strings.Join(lines, "|") != "step 1|step 2|step 3" {
		t.Fatalf("lines=%q", lines)
	}
	lines, truncated, _ = s.Read("web", id1, KindBuild, 2)
	if !truncated || strings.Join(lines, "|") != "step 2|step 3" {
		t.Fatalf("tail: %q %v", lines, truncated)
	}
	// Overwrite (a retried build) replaces the file.
	if err := s.Write("web", id1, KindBuild, []byte("again\n"), NewRedactor()); err != nil {
		t.Fatal(err)
	}
	lines, _, _ = s.Read("web", id1, KindBuild, 0)
	if strings.Join(lines, "|") != "again" {
		t.Fatalf("overwrite: %q", lines)
	}
	if b, r := s.Has("web", id1); !b || r {
		t.Fatalf("has build=%v run=%v", b, r)
	}
	if _, _, err := s.Read("web", id1, KindRun, 0); !errors.Is(err, ErrNotKept) {
		t.Fatalf("missing run: %v", err)
	}
	if _, _, err := s.Read("web", id2, KindRun, 0); !errors.Is(err, ErrNotKept) {
		t.Fatalf("missing dir: %v", err)
	}
	// Empty log reads as no lines.
	if err := s.Write("web", id1, KindRun, nil, NewRedactor()); err != nil {
		t.Fatal(err)
	}
	lines, truncated, err = s.Read("web", id1, KindRun, 0)
	if err != nil || truncated || len(lines) != 0 || lines == nil {
		t.Fatalf("empty: %q %v %v", lines, truncated, err)
	}
}

func TestReadTailBounds(t *testing.T) {
	s := openTest(t)
	var b strings.Builder
	for i := 0; i < 2500; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	if err := s.Write("web", id1, KindRun, []byte(b.String()), NewRedactor()); err != nil {
		t.Fatal(err)
	}
	lines, truncated, _ := s.Read("web", id1, KindRun, 0)
	if len(lines) != DefaultTail || !truncated || lines[len(lines)-1] != "line 2499" {
		t.Fatalf("default: %d %v", len(lines), truncated)
	}
	lines, _, _ = s.Read("web", id1, KindRun, 999999)
	if len(lines) != MaxTail || lines[0] != "line 500" {
		t.Fatalf("max: %d %q", len(lines), lines[0])
	}
}

func TestWriteRequiresRedactor(t *testing.T) {
	s := openTest(t)
	if err := s.Write("web", id1, KindRun, []byte("secret"), nil); err == nil {
		t.Fatal("nil redactor accepted")
	}
	if b, r := s.Has("web", id1); b || r {
		t.Fatal("file written without redactor")
	}
}

func TestWriteRedacts(t *testing.T) {
	s := openTest(t)
	data := "token=supersecret\npip install -i https://bob:hunter22@pypi.example.com/simple x\n"
	if err := s.Write("web", id1, KindBuild, []byte(data), NewRedactor("supersecret")); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(s.Root(), "web", id1, "build.log"))
	got := string(raw)
	if strings.Contains(got, "supersecret") || strings.Contains(got, "hunter22") || strings.Contains(got, "bob") {
		t.Fatalf("plaintext kept: %q", got)
	}
	if !strings.Contains(got, "token=******") || !strings.Contains(got, "https://******@pypi.example.com") {
		t.Fatalf("redacted: %q", got)
	}
}

func TestPathValidation(t *testing.T) {
	s := openTest(t)
	r := NewRedactor()
	bad := []struct{ app, id string }{
		{"..", id1}, {"web/..", id1}, {"../x", id1}, {"Web", id1}, {"", id1},
		{"web", "../../etc"}, {"web", "0123456789AB"}, {"web", "0123456789a"}, {"web", "0123456789abc"},
		{"web", ""}, {"web", "../0123456789"},
	}
	for _, c := range bad {
		if err := s.Write(c.app, c.id, KindRun, []byte("x"), r); !errors.Is(err, ErrInvalid) {
			t.Errorf("write %q/%q: %v", c.app, c.id, err)
		}
		if _, _, err := s.Read(c.app, c.id, KindRun, 0); !errors.Is(err, ErrInvalid) {
			t.Errorf("read %q/%q: %v", c.app, c.id, err)
		}
		if b, rr := s.Has(c.app, c.id); b || rr {
			t.Errorf("has %q/%q", c.app, c.id)
		}
	}
	if err := s.Write("web", id1, Kind("../x"), []byte("x"), r); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad kind: %v", err)
	}
	if err := s.DeleteApp(".."); !errors.Is(err, ErrInvalid) {
		t.Errorf("delete ..: %v", err)
	}
	if err := s.PruneApp("../web", nil, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("prune ..: %v", err)
	}
}

func TestKeepTailAlignsToLineStart(t *testing.T) {
	var b strings.Builder
	for i := 0; b.Len() < MaxRunBytes+300_000; i++ {
		fmt.Fprintf(&b, "%06d some log output here\n", i)
	}
	data := []byte(b.String())
	out := keepTail(data, MaxRunBytes)
	if len(out) > MaxRunBytes {
		t.Fatalf("len=%d > limit", len(out))
	}
	first, rest, _ := strings.Cut(string(out), "\n")
	if !headerPattern.MatchString(first) {
		t.Fatalf("header=%q", first)
	}
	if !strings.HasSuffix(string(data), rest) {
		t.Fatal("tail is not a suffix of the input")
	}
	// The kept part starts at a full line.
	if data[len(data)-len(rest)-1] != '\n' {
		t.Fatalf("not aligned: %q", rest[:40])
	}
	omitted := len(data) - len(rest)
	if want := omittedHeader(omitted); first != want {
		t.Fatalf("header=%q want %q", first, want)
	}
}

func TestKeepTailCutExactlyAtLineStart(t *testing.T) {
	limit := 200
	lines := strings.Repeat("aaaaaaa\n", (limit-headerReserve)/8) // exactly the budget
	data := []byte(strings.Repeat("z", 100) + "\n" + lines)
	out := keepTail(data, limit)
	_, rest, _ := strings.Cut(string(out), "\n")
	if rest != lines {
		t.Fatalf("a whole first line was dropped: %q", rest)
	}
}

func TestKeepTailSingleHugeLineKeepsUTF8(t *testing.T) {
	data := []byte(strings.Repeat("日志", MaxBuildBytes)) // no newline, 3-byte runes
	out := keepTail(data, MaxBuildBytes)
	if len(out) > MaxBuildBytes {
		t.Fatalf("len=%d", len(out))
	}
	if !utf8.Valid(out) {
		t.Fatal("cut inside a UTF-8 sequence")
	}
	_, rest, _ := strings.Cut(string(out), "\n")
	if !strings.HasPrefix(rest, "日") && !strings.HasPrefix(rest, "志") {
		t.Fatalf("rest starts %q", rest[:6])
	}
}

func TestKeepTailSmallUnchanged(t *testing.T) {
	data := []byte("a\nb\n")
	if got := keepTail(data, MaxBuildBytes); string(got) != "a\nb\n" {
		t.Fatalf("got=%q", got)
	}
}

func TestReadReportsWriteTruncation(t *testing.T) {
	s := openTest(t)
	var b strings.Builder
	for i := 0; b.Len() < MaxBuildBytes*2; i++ {
		fmt.Fprintf(&b, "build line %d\n", i)
	}
	if err := s.Write("web", id1, KindBuild, []byte(b.String()), NewRedactor()); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(s.Root(), "web", id1, "build.log"))
	if info.Size() > MaxBuildBytes {
		t.Fatalf("size=%d", info.Size())
	}
	lines, truncated, err := s.Read("web", id1, KindBuild, MaxTail)
	if err != nil || !truncated {
		t.Fatalf("truncated=%v err=%v", truncated, err)
	}
	for _, l := range lines {
		if strings.Contains(l, "已省略") {
			t.Fatal("header returned as a log line")
		}
	}
	// A short file whose tail fits returns the header as truncated only.
	small := keepTail([]byte(strings.Repeat("x\n", 200)), 200)
	if err := os.WriteFile(filepath.Join(s.Root(), "web", id1, "run.log"), small, 0o600); err != nil {
		t.Fatal(err)
	}
	lines, truncated, _ = s.Read("web", id1, KindRun, 0)
	if !truncated || len(lines) == 0 || lines[0] != "x" {
		t.Fatalf("lines=%q truncated=%v", lines, truncated)
	}
}

func writeN(t *testing.T, s *Store, app string, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := s.Write(app, id, KindRun, []byte("log of "+id+"\n"), NewRedactor()); err != nil {
			t.Fatal(err)
		}
	}
}

func fakeID(i int) string { return fmt.Sprintf("%012x", i) }

func TestPruneApp(t *testing.T) {
	s := openTest(t)
	var recent []string // newest first
	for i := 30; i >= 1; i-- {
		recent = append(recent, fakeID(i))
	}
	for i := 1; i <= 30; i++ {
		writeN(t, s, "web", fakeID(i))
	}
	writeN(t, s, "web", "dddddddddddd") // in neither list → removed
	// A stray non-deployment entry is left alone.
	if err := os.WriteFile(filepath.Join(s.Root(), "web", "README"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	protected := map[string]bool{fakeID(2): true, fakeID(5): true}
	if err := s.PruneApp("web", recent, protected); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 30; i++ {
		_, run := s.Has("web", fakeID(i))
		want := i > 10 || i == 2 || i == 5
		if run != want {
			t.Errorf("id %d kept=%v want %v", i, run, want)
		}
	}
	if _, run := s.Has("web", "dddddddddddd"); run {
		t.Error("unknown id kept")
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "web", "README")); err != nil {
		t.Error("stray file removed")
	}
	if err := s.PruneApp("nolog", recent, nil); err != nil {
		t.Fatalf("missing app dir: %v", err)
	}
}

func TestPruneGlobalOrphans(t *testing.T) {
	s := openTest(t)
	writeN(t, s, "web", id1)
	writeN(t, s, "gone", id2)
	if err := s.PruneGlobal(nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, run := s.Has("gone", id2); !run {
		t.Fatal("nil apps must not delete anything")
	}
	if err := s.PruneGlobal(map[string]bool{"web": true}, nil); err != nil {
		t.Fatal(err)
	}
	if _, run := s.Has("gone", id2); run {
		t.Fatal("orphan app kept")
	}
	if _, run := s.Has("web", id1); !run {
		t.Fatal("live app removed")
	}
	if _, err := os.Stat(filepath.Join(s.Root(), sinceFile)); err != nil {
		t.Fatal("marker removed")
	}
}

func TestPruneGlobalCap(t *testing.T) {
	s := openTest(t)
	chunk := strings.Repeat("0123456789abcde\n", 64) // 1024 bytes
	base := time.Now().Add(-time.Hour)
	ids := []string{fakeID(1), fakeID(2), fakeID(3), fakeID(4), fakeID(5)}
	for i, id := range ids {
		if err := s.Write("web", id, KindRun, []byte(chunk), NewRedactor()); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(s.Root(), "web", id)
		mt := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(dir, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	// Total 5 KiB, cap 4 KiB → delete oldest until <= 3.2 KiB; the oldest is
	// protected, so the 2nd and 3rd go.
	protected := map[string]map[string]bool{"web": {fakeID(1): true}}
	if err := s.pruneGlobal(map[string]bool{"web": true}, protected, 4096); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{fakeID(1): true, fakeID(2): false, fakeID(3): false, fakeID(4): true, fakeID(5): true}
	for id, keep := range want {
		if _, run := s.Has("web", id); run != keep {
			t.Errorf("%s kept=%v want %v", id, run, keep)
		}
	}
	// Under the cap nothing is removed.
	if err := s.pruneGlobal(map[string]bool{"web": true}, nil, 1<<20); err != nil {
		t.Fatal(err)
	}
	if _, run := s.Has("web", fakeID(4)); !run {
		t.Fatal("removed under cap")
	}
}

func TestDeleteApp(t *testing.T) {
	s := openTest(t)
	writeN(t, s, "web", id1, id2)
	writeN(t, s, "api", id3)
	if err := s.DeleteApp("web"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "web")); !os.IsNotExist(err) {
		t.Fatalf("web dir: %v", err)
	}
	if _, run := s.Has("api", id3); !run {
		t.Fatal("other app removed")
	}
	if err := s.DeleteApp("web"); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}
