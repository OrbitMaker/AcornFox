package deploylog

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/acornfox/acornfox/internal/state"
)

// Mask replaces every redacted value.
const Mask = "******"

// minSecretLen matches the API's rule: shorter values are too likely to
// collide with ordinary log text to be masked.
const minSecretLen = 4

// urlUserinfo matches "scheme://user:password@" (user may be empty, as in
// redis://:password@host). Credentials written into build files can only be
// caught by patterns like this one.
var urlUserinfo = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s:@]*:[^/\s@]+@`)

// Redactor masks known secret values and URL credentials in log text.
type Redactor struct {
	values []string // deduplicated, len >= minSecretLen, longest first
}

// NewRedactor keeps the distinct values of at least four bytes, ordered so a
// longer value is replaced before any shorter value it contains.
func NewRedactor(values ...string) *Redactor {
	seen := make(map[string]bool, len(values))
	r := &Redactor{}
	addValue := func(v string) {
		if len(v) < minSecretLen || seen[v] {
			return
		}
		seen[v] = true
		r.values = append(r.values, v)
	}
	for _, v := range values {
		addValue(v)
		// Docker logs prefix every line with a timestamp, so a multi-line
		// value (a PEM key, JSON credentials) never appears whole in
		// stored logs; mask each of its lines on its own too.
		if strings.ContainsAny(v, "\r\n") {
			for _, line := range strings.FieldsFunc(v, func(c rune) bool { return c == '\n' || c == '\r' }) {
				addValue(strings.TrimSpace(line))
			}
		}
	}
	sort.Slice(r.values, func(i, j int) bool {
		if len(r.values[i]) != len(r.values[j]) {
			return len(r.values[i]) > len(r.values[j])
		}
		return r.values[i] < r.values[j]
	})
	return r
}

// Redact replaces the known values with Mask, then masks URL userinfo
// ("https://u:p@host" becomes "https://******@host"). A nil Redactor only
// applies the URL rule.
func (r *Redactor) Redact(s string) string {
	if r != nil {
		for _, v := range r.values {
			s = strings.ReplaceAll(s, v, Mask)
		}
	}
	return urlUserinfo.ReplaceAllString(s, "${1}"+Mask+"@")
}

// SecretSource is the subset of the state store SecretsForApp reads.
type SecretSource interface {
	ListEnv(ctx context.Context, app string) ([]state.EnvVar, error)
	ListAddons(ctx context.Context, app string) ([]state.Addon, error)
	ListRemovedAddons(ctx context.Context, app string) ([]state.Addon, error)
}

// SecretsForApp returns every value to mask in app's logs: secret
// environment variables, and the URL, password and root password of current
// and removed add-ons. Values shorter than four bytes are skipped; the result
// is ordered longest first. Any read or decode error is returned, so callers
// can refuse to show or store logs instead of leaking plaintext.
func SecretsForApp(ctx context.Context, src SecretSource, app string) ([]string, error) {
	vars, err := src.ListEnv(ctx, app)
	if err != nil {
		return nil, err
	}
	var secrets []string
	for _, v := range vars {
		if v.Secret && len(v.Value) >= minSecretLen {
			secrets = append(secrets, v.Value)
		}
	}
	active, err := src.ListAddons(ctx, app)
	if err != nil {
		return nil, err
	}
	removed, err := src.ListRemovedAddons(ctx, app)
	if err != nil {
		return nil, err
	}
	for _, addon := range append(active, removed...) {
		creds, err := addon.DecodeCredentials()
		if err != nil {
			return nil, err
		}
		for _, value := range []string{creds.URL, creds.Password, creds.RootPassword} {
			if len(value) >= minSecretLen {
				secrets = append(secrets, value)
			}
		}
	}
	sort.SliceStable(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return secrets, nil
}

// nonSecretEnv lists variables that base images and the OS set in every
// container. They are never user secrets, and masking values like "/root"
// or "C.UTF-8" would only garble the log.
var nonSecretEnv = map[string]bool{
	"PATH": true, "HOME": true, "HOSTNAME": true, "PWD": true, "OLDPWD": true,
	"SHLVL": true, "TERM": true, "USER": true, "SHELL": true, "TZ": true,
	"LANG": true, "LANGUAGE": true, "LC_ALL": true, "LC_CTYPE": true,
	"container": true,
}

// ContainerSecrets returns values from a container's environment (KEY=VALUE
// entries) that the current configuration no longer covers:
//
//	(a) the old value of a key currently marked secret whose value changed;
//	(b) the value of a key that no longer exists (except the common OS and
//	    base-image variables in nonSecretEnv and *_VERSION keys);
//	(c) the value of any key an add-on injects (addonEnvKeys, e.g.
//	    DATABASE_URL, REDIS_URL).
//
// Values shorter than four bytes are skipped; duplicates are removed.
func ContainerSecrets(containerEnv []string, current []state.EnvVar, addonEnvKeys []string) []string {
	cur := make(map[string]state.EnvVar, len(current))
	for _, e := range current {
		cur[e.Key] = e
	}
	addonKeys := make(map[string]bool, len(addonEnvKeys))
	for _, k := range addonEnvKeys {
		addonKeys[k] = true
	}
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		if len(v) >= minSecretLen && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, kv := range containerEnv {
		key, value, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			continue
		}
		c, exists := cur[key]
		switch {
		case addonKeys[key]:
			add(value)
		case exists && c.Secret && c.Value != value:
			add(value)
		case !exists && !nonSecretEnv[key] && !strings.HasSuffix(key, "_VERSION"):
			add(value)
		}
	}
	return out
}
