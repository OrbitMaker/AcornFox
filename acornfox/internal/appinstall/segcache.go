package appinstall

// The download cache of the segmented download (segdl.go).
//
// What it keeps: for one pinned file (named by its sha256) a sparse file
// <dir>/<sha256>.part of the file's full size and a small record
// <dir>/<sha256>.json that says how many bytes of each segment are in it.
// A download that stops (the server restarts, the network breaks, the task
// is cancelled) leaves both; the next download of the same file (same sha256,
// size and segment size) goes on from them. A finished and verified download
// is renamed out of the cache, so a successful one leaves nothing behind.
//
// Where: <data>/uploads/dl-cache (the downloaded image archives' folder; the
// uploads clean-up only touches files directly in <data>/uploads, never this
// folder). Entries not touched for dlCacheMaxAge are removed when a
// download starts.
//
// Safe against a crash: the record is written only after the file was
// synced, and a record is never trusted blindly: the finished file is
// checked against the catalog's sha256, and a file that was resumed from the
// cache and does not match is thrown away and fetched again from nothing,
// once.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// dlCacheMaxAge is how long an unfinished download is kept.
const dlCacheMaxAge = 3 * 24 * time.Hour

// dlCacheDir is the cache folder inside the downloads folder.
func dlCacheDir(uploadDir string) string { return filepath.Join(uploadDir, "dl-cache") }

// dlCacheEntry is one file's cache: the data file and its record.
type dlCacheEntry struct {
	dir, key string
	size     int64
	segSize  int64
}

type dlRecord struct {
	SHA256  string  `json:"sha256"`
	Size    int64   `json:"size"`
	SegSize int64   `json:"seg_size"`
	Got     []int64 `json:"got"` // bytes in the file per segment, counted from the segment's start
	Updated string  `json:"updated"`
}

func newDLCacheEntry(dir, sha string, size, segSize int64) *dlCacheEntry {
	return &dlCacheEntry{dir: dir, key: strings.ToLower(sha), size: size, segSize: segSize}
}

func (c *dlCacheEntry) partPath() string { return filepath.Join(c.dir, c.key+".part") }
func (c *dlCacheEntry) recPath() string  { return filepath.Join(c.dir, c.key+".json") }

// segments is how many segments the file has.
func (c *dlCacheEntry) segments() int { return int((c.size + c.segSize - 1) / c.segSize) }

// segLen is segment i's length.
func (c *dlCacheEntry) segLen(i int) int64 {
	start := int64(i) * c.segSize
	n := c.size - start
	if n > c.segSize {
		n = c.segSize
	}
	return n
}

// open returns the data file and the bytes each segment already has. When
// there is no usable earlier download (no record, another size or segment
// size, a data file of the wrong size) it starts from nothing.
func (c *dlCacheEntry) open() (f *os.File, got []int64, resumed int64, err error) {
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return nil, nil, 0, err
	}
	got = make([]int64, c.segments())
	if rec, ok := c.load(); ok {
		if st, serr := os.Stat(c.partPath()); serr == nil && st.Size() == c.size {
			f, err = os.OpenFile(c.partPath(), os.O_RDWR, 0o600)
			if err == nil {
				for i, g := range rec.Got {
					if g < 0 || g > c.segLen(i) {
						g = 0
					}
					got[i] = g
					resumed += g
				}
				return f, got, resumed, nil
			}
		}
	}
	c.remove()
	f, err = os.OpenFile(c.partPath(), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, 0, err
	}
	if err := f.Truncate(c.size); err != nil {
		f.Close()
		c.remove()
		return nil, nil, 0, err
	}
	return f, got, 0, nil
}

// load reads the record when it describes this file.
func (c *dlCacheEntry) load() (dlRecord, bool) {
	b, err := os.ReadFile(c.recPath())
	if err != nil {
		return dlRecord{}, false
	}
	var rec dlRecord
	if json.Unmarshal(b, &rec) != nil || !strings.EqualFold(rec.SHA256, c.key) || rec.Size != c.size ||
		rec.SegSize != c.segSize || len(rec.Got) != c.segments() {
		return dlRecord{}, false
	}
	return rec, true
}

// save writes the record (after the caller synced the data file).
func (c *dlCacheEntry) save(got []int64) error {
	rec := dlRecord{SHA256: c.key, Size: c.size, SegSize: c.segSize, Got: got, Updated: time.Now().UTC().Format(time.RFC3339)}
	b, _ := json.Marshal(rec)
	tmp := c.recPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.recPath())
}

// remove deletes the data file and the record.
func (c *dlCacheEntry) remove() {
	_ = os.Remove(c.partPath())
	_ = os.Remove(c.recPath())
	_ = os.Remove(c.recPath() + ".tmp")
}

// finish moves the verified data file to dest (mode 0644 so the runner can
// read it) and drops the record.
func (c *dlCacheEntry) finish(dest string) error {
	if err := os.Chmod(c.partPath(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(c.partPath(), dest); err != nil {
		return err
	}
	_ = os.Remove(c.recPath())
	_ = os.Remove(c.recPath() + ".tmp")
	return nil
}

// pruneDLCache removes entries nobody touched for dlCacheMaxAge.
func pruneDLCache(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() {
			continue
		}
		if now.Sub(info.ModTime()) > dlCacheMaxAge {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// dlLocks keeps two downloads of the same file in one process from using one
// cache entry at the same time: the second waits for the first.
var dlLocks = struct {
	mu sync.Mutex
	m  map[string]chan struct{}
}{m: map[string]chan struct{}{}}

// lockDLKey waits for key's turn (or ctx ending) and returns the release.
func lockDLKey(ctx context.Context, key string) (func(), error) {
	for {
		dlLocks.mu.Lock()
		ch, busy := dlLocks.m[key]
		if !busy {
			mine := make(chan struct{})
			dlLocks.m[key] = mine
			dlLocks.mu.Unlock()
			return func() {
				dlLocks.mu.Lock()
				delete(dlLocks.m, key)
				dlLocks.mu.Unlock()
				close(mine)
			}, nil
		}
		dlLocks.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

var errNoCacheDir = errors.New("没有下载缓存目录")
