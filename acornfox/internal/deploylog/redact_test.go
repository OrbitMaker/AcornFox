package deploylog

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/state"
)

func TestRedactorLongestFirstAndDedup(t *testing.T) {
	r := NewRedactor("abcd", "abcdefgh", "abcd", "abc", "")
	if !reflect.DeepEqual(r.values, []string{"abcdefgh", "abcd"}) {
		t.Fatalf("values=%q", r.values)
	}
	// The overlapping longer value is replaced whole, not leaving "efgh".
	if got := r.Redact("x=abcdefgh y=abcd z=abc"); got != "x=****** y=****** z=abc" {
		t.Fatalf("got=%q", got)
	}
}

func TestRedactorURLUserinfo(t *testing.T) {
	r := NewRedactor()
	cases := map[string]string{
		"git clone https://bob:pa55@github.com/x/y.git":    "git clone https://******@github.com/x/y.git",
		"INDEX=HTTP://u:p@mirror.example.com/simple":       "INDEX=HTTP://******@mirror.example.com/simple",
		"redis://:s3cretpw@redis:6379/0":                   "redis://******@redis:6379/0",
		"postgresql://acornfox_web:abc@db:5432/acornfox_w": "postgresql://******@db:5432/acornfox_w",
		"git+ssh://git:tok@host/repo":                      "git+ssh://******@host/repo",
		"https://example.com/path?a=b@c":                   "https://example.com/path?a=b@c",
		"mail me at bob@example.com":                       "mail me at bob@example.com",
		"https://user@host/no-password":                    "https://user@host/no-password",
	}
	for in, want := range cases {
		if got := r.Redact(in); got != want {
			t.Errorf("Redact(%q)=%q want %q", in, got, want)
		}
	}
	var nilR *Redactor
	if got := nilR.Redact("https://a:b@h"); got != "https://******@h" {
		t.Fatalf("nil redactor: %q", got)
	}
}

func TestRedactorValuesBeforeURLRule(t *testing.T) {
	r := NewRedactor("hunter22")
	got := r.Redact("ENV API_KEY=hunter22\nhttps://bob:hunter22@h/x")
	if strings.Contains(got, "hunter22") || strings.Contains(got, "bob") {
		t.Fatalf("got=%q", got)
	}
}

func TestContainerSecrets(t *testing.T) {
	current := []state.EnvVar{
		{Key: "API_KEY", Value: "new-secret", Secret: true},
		{Key: "SAME_KEY", Value: "unchanged-secret", Secret: true},
		{Key: "MODE", Value: "production"},
		{Key: "SHORT", Value: "abc", Secret: true},
		{Key: "DATABASE_URL", Value: "postgresql://u:now@db/x"},
	}
	env := []string{
		"API_KEY=old-secret",              // (a) secret changed → old value
		"SAME_KEY=unchanged-secret",       // unchanged secret: current rules cover it
		"MODE=staging",                    // non-secret changed: not included
		"SHORT=xyz",                       // (a) but too short
		"REMOVED_TOKEN=removed-token-val", // (b) key gone
		"DATABASE_URL=postgresql://u:old@db/x",
		"REDIS_URL=redis://:oldpw@cache:6379/0", // (c) addon key, no longer in current env
		"PATH=/usr/local/sbin:/usr/local/bin",   // OS/base-image variable
		"HOME=/root",
		"NODE_VERSION=20.11.1",
		"MALFORMED",
		"=novalue",
		"DUP=removed-token-val", // duplicate value
	}
	got := ContainerSecrets(env, current, []string{"DATABASE_URL", "REDIS_URL"})
	sort.Strings(got)
	want := []string{"old-secret", "postgresql://u:old@db/x", "redis://:oldpw@cache:6379/0", "removed-token-val"}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%q\nwant=%q", got, want)
	}
}

type fakeSource struct {
	env              []state.EnvVar
	active, removed  []state.Addon
	envErr, addonErr error
	removedErr       error
}

func (f *fakeSource) ListEnv(context.Context, string) ([]state.EnvVar, error) {
	return f.env, f.envErr
}
func (f *fakeSource) ListAddons(context.Context, string) ([]state.Addon, error) {
	return f.active, f.addonErr
}
func (f *fakeSource) ListRemovedAddons(context.Context, string) ([]state.Addon, error) {
	return f.removed, f.removedErr
}

func addon(t *testing.T, kind string, creds state.AddonCredentials) state.Addon {
	t.Helper()
	raw, err := json.Marshal(creds)
	if err != nil {
		t.Fatal(err)
	}
	return state.Addon{App: "web", Kind: kind, Credentials: raw}
}

func TestSecretsForApp(t *testing.T) {
	src := &fakeSource{
		env: []state.EnvVar{
			{Key: "API_KEY", Value: "k-123456", Secret: true},
			{Key: "PIN", Value: "123", Secret: true},
			{Key: "MODE", Value: "production"},
		},
		active:  []state.Addon{addon(t, state.AddonMySQL, state.AddonCredentials{URL: "mysql://u:pw-active@db/x", Password: "pw-active", RootPassword: "root-pw-1"})},
		removed: []state.Addon{addon(t, state.AddonRedis, state.AddonCredentials{URL: "redis://:pw-gone@r/0", Password: "pw-gone"})},
	}
	got, err := SecretsForApp(context.Background(), src, "web")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"k-123456": true, "mysql://u:pw-active@db/x": true, "pw-active": true, "root-pw-1": true, "redis://:pw-gone@r/0": true, "pw-gone": true}
	if len(got) != len(want) {
		t.Fatalf("got=%q", got)
	}
	for i, v := range got {
		if !want[v] {
			t.Fatalf("unexpected %q", v)
		}
		if i > 0 && len(got[i-1]) < len(v) {
			t.Fatalf("not longest first: %q", got)
		}
	}
}

func TestSecretsForAppErrors(t *testing.T) {
	boom := errors.New("boom")
	cases := map[string]*fakeSource{
		"env":     {envErr: boom},
		"addons":  {addonErr: boom},
		"removed": {removedErr: boom},
	}
	for name, src := range cases {
		if _, err := SecretsForApp(context.Background(), src, "web"); !errors.Is(err, boom) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	bad := &fakeSource{removed: []state.Addon{{App: "web", Kind: state.AddonPostgres, Credentials: json.RawMessage(`{`)}}}
	if _, err := SecretsForApp(context.Background(), bad, "web"); err == nil {
		t.Error("undecodable credentials accepted")
	}
	empty := &fakeSource{active: []state.Addon{{App: "web", Kind: state.AddonPostgres}}}
	if _, err := SecretsForApp(context.Background(), empty, "web"); err == nil {
		t.Error("missing credentials accepted")
	}
}

func TestRedactorMultiLineSecretWithTimestamps(t *testing.T) {
	key := "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASC\n-----END PRIVATE KEY-----"
	r := NewRedactor(key)
	// Docker prefixes every line, so the whole value never appears.
	logged := "2026-11-03T02:10:31.1Z -----BEGIN PRIVATE KEY-----\n" +
		"2026-11-03T02:10:31.2Z MIIEvQIBADANBgkqhkiG9w0BAQEFAASC\n" +
		"2026-11-03T02:10:31.3Z -----END PRIVATE KEY-----\n"
	got := r.Redact(logged)
	for _, part := range strings.Split(key, "\n") {
		if strings.Contains(got, part) {
			t.Fatalf("line %q of a multi-line secret left in:\n%s", part, got)
		}
	}
	if strings.Count(got, "\n") != 3 {
		t.Fatalf("line structure changed:\n%s", got)
	}
}
