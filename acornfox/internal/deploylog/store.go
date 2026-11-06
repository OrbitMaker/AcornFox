// Package deploylog keeps per-deployment build and run logs as plain files so
// older versions' logs survive after their containers are removed.
//
// Layout: <root>/<app>/<deployment-id>/{build,run}.log, where root is
// <data dir>/logs/deployments. Directories are 0700 and files 0600. The
// server process is the only writer; the runner never stores anything.
//
// Limits (constants below): a build log keeps its last MaxBuildBytes, a run
// log its last MaxRunBytes, aligned to a line start and prefixed with a
// "（前面 N KB 已省略）" line. PruneApp keeps the newest KeepPerApp
// deployments of an app plus the protected ones (live, rollback targets, the
// latest failure); PruneGlobal removes directories of deleted apps and trims
// the whole tree to 80% of TotalCap when it grows past TotalCap.
//
// Every write is redacted first (see Redactor); without a redactor nothing is
// written, so a missing redaction config never leaves plaintext secrets on
// disk. Writes go to a temporary file renamed into place, so readers never
// see a half-written log.
//
// Safe to delete: removing the whole root only loses old logs; Open recreates
// it (and the .since marker, which then reports the later time).
package deploylog

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/acornfox/acornfox/internal/state"
)

// Kind selects which of the two logs of a deployment is meant.
type Kind string

const (
	KindBuild Kind = "build"
	KindRun   Kind = "run"
)

const (
	MaxBuildBytes = 512 << 10 // per build.log
	MaxRunBytes   = 1 << 20   // per run.log
	KeepPerApp    = 20        // newest deployments kept per app (besides protected ones)
	TotalCap      = 200 << 20 // whole tree; pruned down to 80% when exceeded

	// DefaultTail and MaxTail bound the number of lines Read returns. The
	// API and CLI use the same constants.
	DefaultTail = 500
	MaxTail     = 2000
)

var (
	// ErrNotKept means the log file does not exist (never written, or pruned).
	ErrNotKept = errors.New("deploylog: not kept")
	// ErrInvalid means an app name, deployment ID or kind failed validation.
	ErrInvalid = errors.New("deploylog: invalid name")
)

const (
	sinceFile = ".since"
	// headerReserve is the room kept for the "omitted" line so a truncated
	// file stays within its limit.
	headerReserve = 64
)

var (
	idPattern     = regexp.MustCompile(`^[0-9a-f]{12}$`)
	headerPattern = regexp.MustCompile(`^（前面 \d+ KB 已省略）$|^（更早的日志已省略）$`)
)

// EarlierOmitted is the first line of a log whose source already dropped
// older output (the runner keeps only a container's tail). Read treats it
// like its own "omitted" line.
const EarlierOmitted = "（更早的日志已省略）"

// Store reads and writes deployment logs under one root directory.
type Store struct {
	root  string
	since time.Time
	mu    sync.Mutex // serializes writes and pruning; reads do not lock
}

// Open creates root (0700) if needed and records the first time this feature
// was enabled in root/.since (RFC3339, UTC). An existing marker is never
// changed; an unreadable one is rewritten with the current time.
func Open(root string) (*Store, error) {
	return openAt(root, time.Now)
}

func openAt(root string, now func() time.Time) (*Store, error) {
	if root == "" {
		return nil, fmt.Errorf("%w: empty log root", ErrInvalid)
	}
	root = filepath.Clean(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create log root: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("chmod log root: %w", err)
	}
	since, err := ensureSince(filepath.Join(root, sinceFile), now)
	if err != nil {
		return nil, err
	}
	return &Store{root: root, since: since}, nil
}

func ensureSince(path string, now func() time.Time) (time.Time, error) {
	if raw, err := os.ReadFile(path); err == nil {
		if t, perr := time.Parse(time.RFC3339, strings.TrimSpace(string(raw))); perr == nil {
			return t.UTC(), nil
		}
		// Corrupt marker (e.g. a crash mid-write): fall through and rewrite.
		if err := os.Remove(path); err != nil {
			return time.Time{}, fmt.Errorf("replace log marker: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return time.Time{}, fmt.Errorf("read log marker: %w", err)
	}
	t := now().UTC().Truncate(time.Second)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		// Another process won the race; use its value.
		return ensureSince(path, now)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("create log marker: %w", err)
	}
	_, werr := f.WriteString(t.Format(time.RFC3339) + "\n")
	cerr := f.Close()
	if werr != nil || cerr != nil {
		return time.Time{}, fmt.Errorf("write log marker: %w", errors.Join(werr, cerr))
	}
	return t, nil
}

// Root returns the directory the store writes under.
func (s *Store) Root() string { return s.root }

// Since is when deployment logs were first kept on this machine. Deployments
// created before it never had logs; later ones without logs were pruned.
func (s *Store) Since() time.Time { return s.since }

func (k Kind) valid() bool { return k == KindBuild || k == KindRun }

func (k Kind) limit() int {
	if k == KindBuild {
		return MaxBuildBytes
	}
	return MaxRunBytes
}

// ValidDeploymentID reports whether id has the 12 lowercase hex form.
func ValidDeploymentID(id string) bool { return idPattern.MatchString(id) }

// dir returns <root>/<app>[/<id>] after validating both names and checking
// the result is still inside root.
func (s *Store) dir(app, id string) (string, error) {
	if !state.ValidAppName(app) {
		return "", fmt.Errorf("%w: app %q", ErrInvalid, app)
	}
	parts := []string{s.root, app}
	if id != "" {
		if !ValidDeploymentID(id) {
			return "", fmt.Errorf("%w: deployment %q", ErrInvalid, id)
		}
		parts = append(parts, id)
	}
	p := filepath.Join(parts...)
	rel, err := filepath.Rel(s.root, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: path escapes log root", ErrInvalid)
	}
	return p, nil
}

func (s *Store) file(app, id string, kind Kind) (string, error) {
	if !kind.valid() {
		return "", fmt.Errorf("%w: kind %q", ErrInvalid, kind)
	}
	if id == "" {
		return "", fmt.Errorf("%w: empty deployment id", ErrInvalid)
	}
	d, err := s.dir(app, id)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, string(kind)+".log"), nil
}

// Write redacts data with r, keeps only the tail allowed for kind and
// atomically replaces <root>/<app>/<id>/<kind>.log. A nil r is an error and
// nothing is written.
func (s *Store) Write(app, deploymentID string, kind Kind, data []byte, r *Redactor) error {
	if r == nil {
		return errors.New("deploylog: refusing to write without a redactor")
	}
	path, err := s.file(app, deploymentID, kind)
	if err != nil {
		return err
	}
	body := keepTail([]byte(r.Redact(string(data))), kind.limit())

	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+string(kind)+".log.tmp-*")
	if err != nil {
		return fmt.Errorf("create temp log: %w", err)
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(body)
	cerr := tmp.Close()
	if werr == nil && cerr == nil {
		// CreateTemp already uses 0600 on Unix; be explicit for umask-free intent.
		werr = os.Chmod(tmpName, 0o600)
	}
	if werr != nil || cerr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("write log: %w", errors.Join(werr, cerr))
	}
	if err := renameRetry(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("install log: %w", err)
	}
	return nil
}

// renameRetry renames, retrying a few times on Windows where a reader holding
// the destination open makes the rename fail transiently.
func renameRetry(from, to string) error {
	err := os.Rename(from, to)
	if err == nil || runtime.GOOS != "windows" {
		return err
	}
	for attempt := 1; attempt <= 3; attempt++ {
		time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
		if err = os.Rename(from, to); err == nil {
			return nil
		}
	}
	return err
}

// keepTail returns data unchanged when it fits in limit; otherwise its last
// part starting at a line start (or, for a single huge line, at a UTF-8
// boundary), prefixed by an "omitted" line, with the total within limit.
func keepTail(data []byte, limit int) []byte {
	if len(data) <= limit {
		return data
	}
	budget := limit - headerReserve
	start := len(data) - budget
	tail := data[start:]
	if data[start-1] != '\n' {
		if i := bytes.IndexByte(tail, '\n'); i >= 0 && i+1 < len(tail) {
			tail = tail[i+1:]
		} else {
			for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
				tail = tail[1:]
			}
		}
	}
	omitted := len(data) - len(tail)
	header := omittedHeader(omitted)
	out := make([]byte, 0, len(header)+1+len(tail))
	out = append(out, header...)
	out = append(out, '\n')
	return append(out, tail...)
}

// omittedHeader is the first line of a truncated log.
func omittedHeader(omittedBytes int) string {
	return fmt.Sprintf("（前面 %d KB 已省略）", (omittedBytes+1023)/1024)
}

// Read returns the last tail lines (tail<=0 means DefaultTail, capped at
// MaxTail). truncated is true when the file was cut on write (its
// "omitted" line is not returned) or when tail dropped earlier lines. A
// missing file returns ErrNotKept.
func (s *Store) Read(app, deploymentID string, kind Kind, tail int) ([]string, bool, error) {
	path, err := s.file(app, deploymentID, kind)
	if err != nil {
		return nil, false, err
	}
	if tail <= 0 {
		tail = DefaultTail
	}
	if tail > MaxTail {
		tail = MaxTail
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, ErrNotKept
	}
	if err != nil {
		return nil, false, fmt.Errorf("read log: %w", err)
	}
	text := strings.TrimSuffix(string(raw), "\n")
	lines := []string{}
	if text != "" {
		lines = strings.Split(text, "\n")
	}
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	truncated := false
	if len(lines) > 0 && headerPattern.MatchString(lines[0]) {
		lines = lines[1:]
		truncated = true
	}
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
		truncated = true
	}
	return lines, truncated, nil
}

// Has reports which logs of a deployment exist. Invalid names report false.
func (s *Store) Has(app, deploymentID string) (build, run bool) {
	exists := func(kind Kind) bool {
		path, err := s.file(app, deploymentID, kind)
		if err != nil {
			return false
		}
		info, err := os.Stat(path)
		return err == nil && info.Mode().IsRegular()
	}
	return exists(KindBuild), exists(KindRun)
}

// PruneApp deletes the log directories of app that are neither protected nor
// among the first KeepPerApp IDs of recent (newest first). Entries that are
// not deployment directories are left alone.
func (s *Store) PruneApp(app string, recent []string, protected map[string]bool) error {
	appDir, err := s.dir(app, "")
	if err != nil {
		return err
	}
	keep := make(map[string]bool, KeepPerApp+len(protected))
	for id, ok := range protected {
		if ok {
			keep[id] = true
		}
	}
	for i, id := range recent {
		if i >= KeepPerApp {
			break
		}
		keep[id] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(appDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list app logs: %w", err)
	}
	var errs []error
	for _, e := range entries {
		if !e.IsDir() || !ValidDeploymentID(e.Name()) || keep[e.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(appDir, e.Name())); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type logDir struct {
	app, id string
	path    string
	size    int64
	mtime   time.Time
}

// PruneGlobal removes the directories of apps not in apps (orphans; skipped
// when apps is nil, i.e. unknown) and, when the remaining total exceeds
// TotalCap, deletes the oldest deployment directories (by directory mtime)
// until the total is at most 80% of TotalCap, never touching
// protected[app][id]. It lists two directory levels and stats the two log
// files of each deployment; it never walks recursively.
func (s *Store) PruneGlobal(apps map[string]bool, protected map[string]map[string]bool) error {
	return s.pruneGlobal(apps, protected, TotalCap)
}

func (s *Store) pruneGlobal(apps map[string]bool, protected map[string]map[string]bool, capBytes int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	appEntries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list log root: %w", err)
	}
	var errs []error
	var dirs []logDir
	var total int64
	for _, ae := range appEntries {
		app := ae.Name()
		if !ae.IsDir() || !state.ValidAppName(app) {
			continue
		}
		appDir := filepath.Join(s.root, app)
		if apps != nil && !apps[app] {
			if err := os.RemoveAll(appDir); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		idEntries, err := os.ReadDir(appDir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, ie := range idEntries {
			if !ie.IsDir() || !ValidDeploymentID(ie.Name()) {
				continue
			}
			d := logDir{app: app, id: ie.Name(), path: filepath.Join(appDir, ie.Name())}
			if info, err := ie.Info(); err == nil {
				d.mtime = info.ModTime()
			}
			for _, kind := range []Kind{KindBuild, KindRun} {
				if info, err := os.Stat(filepath.Join(d.path, string(kind)+".log")); err == nil {
					d.size += info.Size()
				}
			}
			total += d.size
			dirs = append(dirs, d)
		}
	}
	if total <= capBytes {
		return errors.Join(errs...)
	}
	target := capBytes * 8 / 10
	sort.SliceStable(dirs, func(i, j int) bool {
		if !dirs[i].mtime.Equal(dirs[j].mtime) {
			return dirs[i].mtime.Before(dirs[j].mtime)
		}
		return dirs[i].path < dirs[j].path
	})
	for _, d := range dirs {
		if total <= target {
			break
		}
		if protected[d.app][d.id] {
			continue
		}
		if err := os.RemoveAll(d.path); err != nil {
			errs = append(errs, err)
			continue
		}
		total -= d.size
	}
	return errors.Join(errs...)
}

// DeleteApp removes every log of app. A failure (e.g. a file still open on
// Windows) is returned for the caller to record; PruneGlobal retries later
// because the app is then an orphan.
func (s *Store) DeleteApp(app string) error {
	appDir, err := s.dir(app, "")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.RemoveAll(appDir); err != nil {
		return fmt.Errorf("delete app logs: %w", err)
	}
	return nil
}
