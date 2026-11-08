package hostcheck

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type line struct {
	ok    bool
	level string
	code  string
	text  string
}

type recorder struct{ lines []line }

func (r *recorder) Add(ok bool, code, format string, a ...any) {
	lvl := "ok"
	if !ok {
		lvl = "fail"
	}
	r.lines = append(r.lines, line{ok, lvl, code, fmt.Sprintf(format, a...)})
}

func (r *recorder) Warn(code, format string, a ...any) {
	r.lines = append(r.lines, line{true, "warn", code, fmt.Sprintf(format, a...)})
}

func TestCheckClock(t *testing.T) {
	now := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	synced := func(context.Context) (SyncStatus, error) {
		return SyncStatus{NTPKnown: true, NTP: true, SyncKnown: true, Synchronized: true}, nil
	}
	ntpOff := func(context.Context) (SyncStatus, error) {
		return SyncStatus{NTPKnown: true, NTP: false, SyncKnown: true, Synchronized: false}, nil
	}
	notYet := func(context.Context) (SyncStatus, error) {
		return SyncStatus{NTPKnown: true, NTP: true, SyncKnown: true, Synchronized: false}, nil
	}
	unknown := func(context.Context) (SyncStatus, error) { return SyncStatus{}, errors.New("no timedatectl") }
	ref := func(off time.Duration) func(context.Context) (time.Time, error) {
		return func(context.Context) (time.Time, error) { return now.Add(off), nil }
	}
	noNet := func(context.Context) (time.Time, error) { return time.Time{}, errors.New("dial tcp: no route") }
	for _, c := range []struct {
		name      string
		sync      func(context.Context) (SyncStatus, error)
		ref       func(context.Context) (time.Time, error)
		level     string
		code      string
		contains  []string
		forbidden []string
	}{
		{"synced and close", synced, ref(2 * time.Second), "ok", "SETUP-CLOCK-01", []string{"已同步", "相差 2 秒"}, nil},
		{"synced, no gateway", synced, nil, "ok", "SETUP-CLOCK-01", []string{"已同步"}, []string{"相差"}},
		{"ten minutes behind", synced, ref(10 * time.Minute), "fail", "SETUP-CLOCK-01", []string{"慢了约 600 秒", "timedatectl set-ntp true", "下载网关会拒绝"}, nil},
		{"ten minutes ahead", synced, ref(-10 * time.Minute), "fail", "SETUP-CLOCK-01", []string{"快了约 600 秒"}, nil},
		{"two minutes off", synced, ref(2 * time.Minute), "warn", "SETUP-CLOCK-01", []string{"慢了约 120 秒"}, nil},
		{"sync off", ntpOff, ref(time.Second), "warn", "SETUP-CLOCK-01", []string{"没开网络对时", "timedatectl set-ntp true"}, nil},
		{"not synced yet", notYet, nil, "warn", "SETUP-CLOCK-01", []string{"还没同步上"}, nil},
		{"no timedatectl, close", unknown, ref(time.Second), "ok", "SETUP-CLOCK-01", []string{"查不到对时状态", "相差 1 秒"}, nil},
		{"no timedatectl, no network", unknown, noNet, "ok", "SETUP-CLOCK-01", []string{"跳过"}, nil},
		{"no network but synced", synced, noNet, "ok", "SETUP-CLOCK-01", []string{"已同步"}, []string{"相差"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := &recorder{}
			CheckClock(context.Background(), r, ClockProbe{Now: func() time.Time { return now }, Sync: c.sync, Reference: c.ref})
			if len(r.lines) != 1 {
				t.Fatalf("want one line: %+v", r.lines)
			}
			l := r.lines[0]
			if l.level != c.level || l.code != c.code {
				t.Fatalf("got %+v", l)
			}
			for _, s := range c.contains {
				if !strings.Contains(l.text, s) {
					t.Fatalf("%q missing %q", l.text, s)
				}
			}
			for _, s := range c.forbidden {
				if strings.Contains(l.text, s) {
					t.Fatalf("%q should not have %q", l.text, s)
				}
			}
		})
	}
}

// A reference that hangs is cut off after ClockProbeTimeout: doctor never
// waits on the network.
func TestCheckClockReferenceTimeout(t *testing.T) {
	r := &recorder{}
	start := time.Now()
	CheckClock(context.Background(), r, ClockProbe{
		Sync: func(context.Context) (SyncStatus, error) { return SyncStatus{SyncKnown: true, Synchronized: true}, nil },
		Reference: func(ctx context.Context) (time.Time, error) {
			<-ctx.Done()
			return time.Time{}, ctx.Err()
		},
	})
	if d := time.Since(start); d > ClockProbeTimeout+2*time.Second {
		t.Fatalf("took %v", d)
	}
	if len(r.lines) != 1 || r.lines[0].level != "ok" {
		t.Fatalf("%+v", r.lines)
	}
}

func TestParseTimedatectl(t *testing.T) {
	for _, c := range []struct {
		out  string
		want SyncStatus
		ok   bool
	}{
		{"NTP=yes\nNTPSynchronized=yes\n", SyncStatus{NTPKnown: true, NTP: true, SyncKnown: true, Synchronized: true}, true},
		{"NTP=no\nNTPSynchronized=no\n", SyncStatus{NTPKnown: true, SyncKnown: true}, true},
		{"               Local time: Sun 2026-11-08 10:00:00 CST\n           Universal time: Sun 2026-11-08 02:00:00 UTC\n                RTC time: Sun 2026-11-08 02:00:00\n                Time zone: Asia/Shanghai (CST, +0800)\nSystem clock synchronized: yes\n              NTP service: active\n          RTC in local TZ: no\n",
			SyncStatus{NTPKnown: true, NTP: true, SyncKnown: true, Synchronized: true}, true},
		{"      Local time: Sun 2026-11-08 10:00:00 CST\n     NTP enabled: no\nNTP synchronized: no\n RTC in local TZ: no\n", SyncStatus{NTPKnown: true, SyncKnown: true}, true},
		{"NTP service: n/a\nSystem clock synchronized: yes\n", SyncStatus{NTPKnown: true, SyncKnown: true, Synchronized: true}, true},
		{"Failed to connect to bus\n", SyncStatus{}, false},
	} {
		got, ok := parseTimedatectl([]byte(c.out))
		if ok != c.ok || got != c.want {
			t.Fatalf("%q: got %+v %v", c.out, got, ok)
		}
	}
}

func TestReadSyncStatusFallbacks(t *testing.T) {
	oldT, oldF := timedatectl, timesyncFile
	t.Cleanup(func() { timedatectl, timesyncFile = oldT, oldF })
	// Old systemd: "show" fails, "status" works.
	var calls []string
	timedatectl = func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args[0])
		if args[0] == "show" {
			return nil, errors.New("Unknown operation show")
		}
		return []byte("     NTP enabled: yes\nNTP synchronized: no\n"), nil
	}
	st, err := readSyncStatus(context.Background())
	if err != nil || !st.NTP || st.Synchronized || strings.Join(calls, ",") != "show,status" {
		t.Fatalf("%+v %v %v", st, err, calls)
	}
	// No timedatectl at all: timesyncd's file.
	timedatectl = func(ctx context.Context, args ...string) ([]byte, error) { return nil, errors.New("not found") }
	timesyncFile = filepath.Join(t.TempDir(), "synchronized")
	if _, err := readSyncStatus(context.Background()); err == nil {
		t.Fatal("nothing known should be an error")
	}
	if err := os.WriteFile(timesyncFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err = readSyncStatus(context.Background())
	if err != nil || !st.SyncKnown || !st.Synchronized || st.NTPKnown {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestHeadDate(t *testing.T) {
	when := time.Date(2026, 11, 8, 2, 0, 0, 0, time.UTC)
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.Header().Set("Date", when.Format(http.TimeFormat))
	}))
	defer srv.Close()
	got, err := headDate(context.Background(), srv.URL+"/")
	if err != nil || !got.Equal(when) || method != http.MethodHead || path != "/v1/health" {
		t.Fatalf("%v %v %s %s", got, err, method, path)
	}
	// DefaultClockProbe without an address compares nothing.
	if p := DefaultClockProbe(" "); p.Reference != nil || p.Sync == nil {
		t.Fatal("no gateway: no reference")
	}
	noDate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header()["Date"] = nil }))
	defer noDate.Close()
	if _, err := headDate(context.Background(), noDate.URL); err == nil {
		t.Fatal("no Date should be an error (the check then skips the comparison)")
	}
}
