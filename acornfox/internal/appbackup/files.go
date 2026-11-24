package appbackup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Meta is meta.json: what a backup is.
type Meta struct {
	Format    int       `json:"format"`
	ID        string    `json:"id"`
	App       string    `json:"app"`
	Kind      string    `json:"kind"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Platform  string    `json:"platform,omitempty"` // the AcornFox version that took it
	// WasRunning: the app was running and was stopped for the copy;
	// DowntimeMS how long it was down.
	WasRunning bool          `json:"was_running"`
	DowntimeMS int64         `json:"downtime_ms"`
	Version    Version       `json:"version"`
	Volumes    []VolumeEntry `json:"volumes"`
	Bytes      int64         `json:"bytes"`     // all files of the backup
	RawBytes   int64         `json:"raw_bytes"` // the volumes' tar streams before compressing
}

// Version is what the app ran when the backup was taken.
type Version struct {
	DeploymentID   string `json:"deployment_id,omitempty"`
	Seq            int    `json:"seq,omitempty"`
	ImageID        string `json:"image_id,omitempty"`
	CatalogID      string `json:"catalog_id,omitempty"`
	CatalogVersion string `json:"catalog_version,omitempty"`
}

// VolumeEntry is one volume of a backup.
type VolumeEntry struct {
	Role     string `json:"role"`            // app | addon
	Path     string `json:"path,omitempty"`  // app: where the app mounts it
	Addon    string `json:"addon,omitempty"` // addon: its kind
	Volume   string `json:"volume"`          // Docker volume name when it was taken
	File     string `json:"file"`            // vol-N.tar.gz
	Bytes    int64  `json:"bytes"`           // the file's size
	RawBytes int64  `json:"raw_bytes"`       // the tar stream's size
	Entries  int    `json:"entries"`         // files and directories in it
	SHA256   string `json:"sha256"`          // of the file
}

// Volume roles.
const (
	RoleApp   = "app"
	RoleAddon = "addon"
)

const (
	metaFile    = "meta.json"
	recordsFile = "records.json"
	partialExt  = ".partial"
)

var idPattern = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{4}$`)

// ValidID reports whether id has the form of a backup id.
func ValidID(id string) bool { return idPattern.MatchString(id) }

func (m *Manager) newID() string {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return m.now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

func (m *Manager) backupDir(app, id string) string { return filepath.Join(m.appDir(app), id) }

// writeJSONFile writes v to path (0600) and syncs it.
func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// readMeta reads the meta.json of a backup directory.
func readMeta(dir string) (Meta, error) {
	var meta Meta
	b, err := os.ReadFile(filepath.Join(dir, metaFile))
	if err != nil {
		return meta, err
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return meta, fmt.Errorf("meta.json: %w", err)
	}
	if meta.Format != FormatVersion || !ValidID(meta.ID) {
		return meta, fmt.Errorf("meta.json: format %d id %q", meta.Format, meta.ID)
	}
	return meta, nil
}

// Get returns one backup's meta; BKP-04 when there is none.
func (m *Manager) Get(app, id string) (Meta, error) {
	if !ValidID(id) {
		return Meta{}, fault(CodeBadRequest, fmt.Sprintf("备份编号 %q 的写法不对（形如 20261124-153012-ab12）", id))
	}
	meta, err := readMeta(m.backupDir(app, id))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Meta{}, fault(CodeNotFound, fmt.Sprintf("应用 %s 没有编号 %s 的备份", app, id))
		}
		return Meta{}, fault(CodeCorrupt, fmt.Sprintf("备份 %s 的清单读不了：%v", id, err))
	}
	if meta.App != app {
		return Meta{}, fault(CodeCorrupt, fmt.Sprintf("备份 %s 的清单写的是应用 %s，不是 %s", id, meta.App, app))
	}
	return meta, nil
}

// List returns app's backups, newest first. A directory whose meta cannot
// be read is left out (doctor lists those).
func (m *Manager) List(app string) ([]Meta, error) {
	entries, err := os.ReadDir(m.appDir(app))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Meta
	for _, e := range entries {
		if !e.IsDir() || !ValidID(e.Name()) {
			continue
		}
		meta, err := readMeta(filepath.Join(m.appDir(app), e.Name()))
		if err != nil || meta.App != app {
			continue
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

// Latest returns app's newest backup of kind ("" any kind).
func (m *Manager) Latest(app, kind string) (Meta, bool, error) {
	all, err := m.List(app)
	if err != nil {
		return Meta{}, false, err
	}
	for _, b := range all {
		if kind == "" || b.Kind == kind {
			return b, true, nil
		}
	}
	return Meta{}, false, nil
}

// Delete removes one backup's files. It is not undone, so it needs the
// user's yes (BKP-06 without confirm). A missing backup is not an error
// (deleted false), so repeating it is safe.
func (m *Manager) Delete(ctx context.Context, app, id string, confirm bool) (deleted bool, err error) {
	if !confirm {
		return false, fault(CodeConfirm, "删除备份后找不回来，需要用户先同意（加 --yes）")
	}
	if !ValidID(id) {
		return false, fault(CodeBadRequest, fmt.Sprintf("备份编号 %q 的写法不对", id))
	}
	if t := m.Busy(app); t != "" {
		return false, fault(CodeBusy, fmt.Sprintf("应用 %s 正在做另一件备份或恢复的事（任务 %s），做完再删", app, t))
	}
	dir := m.backupDir(app, id)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, fault(CodeDirNotWritable, fmt.Sprintf("删不掉备份 %s：%v", id, err))
	}
	m.log(ctx, app, "删除备份 "+id)
	return true, nil
}

// prune removes the backups of kind the count rule no longer keeps (see
// pruneIDs), never the one named keepID.
func (m *Manager) prune(ctx context.Context, app, kind, keepID string) []string {
	all, err := m.List(app)
	if err != nil {
		return nil
	}
	var removed []string
	for _, id := range m.pruneIDs(all, kind, keepID, false) {
		if err := os.RemoveAll(m.backupDir(app, id)); err != nil {
			m.cfg.Log.Warn("prune backup", "app", app, "id", id, "err", err)
			continue
		}
		removed = append(removed, id)
		m.log(ctx, app, "按保留份数清掉旧备份 "+id+"（"+kind+"）")
	}
	return removed
}

// removePartials removes the half-written backups of app.
func (m *Manager) removePartials(app string) []string {
	entries, err := os.ReadDir(m.appDir(app))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), partialExt) {
			if os.RemoveAll(filepath.Join(m.appDir(app), e.Name())) == nil {
				out = append(out, e.Name())
			}
		}
	}
	return out
}

// log appends one line to operations.log and does nothing else.
func (m *Manager) log(ctx context.Context, app, msg string) {
	m.opLog(app, msg)
	m.event(ctx, app, msg)
}

// opLog appends one JSON line to <dir>/operations.log (0600).
func (m *Manager) opLog(app, msg string) {
	if err := os.MkdirAll(m.cfg.Dir, 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(m.cfg.Dir, "operations.log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	line, _ := json.Marshal(map[string]string{"at": m.now().Format(time.RFC3339), "app": app, "msg": msg})
	_, _ = f.Write(append(line, '\n'))
}

// countingWriter counts what passes.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// scanArchive reads a volume file once: its sha256, the size of the tar
// stream inside, how many entries it has. It fails when the gzip or tar
// framing is broken (a cut-off file).
func scanArchive(path string) (sum string, raw int64, entries int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, 0, err
	}
	defer f.Close()
	h := sha256.New()
	tee := io.TeeReader(f, h)
	gz, err := gzip.NewReader(tee)
	if err != nil {
		return "", 0, 0, err
	}
	cw := &countingWriter{w: io.Discard}
	tr := tar.NewReader(io.TeeReader(gz, cw))
	for {
		_, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", 0, 0, err
		}
		entries++
	}
	// Drain what follows the tar's end marker so the gzip checksum is read
	// and the whole file is hashed.
	if _, err := io.Copy(io.Discard, io.TeeReader(gz, cw)); err != nil {
		return "", 0, 0, err
	}
	if _, err := io.Copy(io.Discard, tee); err != nil {
		return "", 0, 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), cw.n, entries, nil
}

// verify checks that every file of a backup is there with the size and
// sha256 meta.json says. BKP-05 when not.
func (m *Manager) verify(app string, meta Meta) error {
	dir := m.backupDir(app, meta.ID)
	if len(meta.Volumes) == 0 {
		return fault(CodeCorrupt, fmt.Sprintf("备份 %s 的清单里没有数据卷", meta.ID))
	}
	for _, v := range meta.Volumes {
		if v.File != filepath.Base(v.File) || !strings.HasPrefix(v.File, "vol-") {
			return fault(CodeCorrupt, fmt.Sprintf("备份 %s 的清单里文件名 %q 不对", meta.ID, v.File))
		}
		st, err := os.Stat(filepath.Join(dir, v.File))
		if err != nil {
			return fault(CodeCorrupt, fmt.Sprintf("备份 %s 缺文件 %s", meta.ID, v.File))
		}
		if st.Size() != v.Bytes {
			return fault(CodeCorrupt, fmt.Sprintf("备份 %s 的文件 %s 大小不对（清单 %d 字节，实际 %d 字节）", meta.ID, v.File, v.Bytes, st.Size()))
		}
		sum, _, _, err := scanArchive(filepath.Join(dir, v.File))
		if err != nil {
			return fault(CodeCorrupt, fmt.Sprintf("备份 %s 的文件 %s 读不完整：%v", meta.ID, v.File, err))
		}
		if sum != v.SHA256 {
			return fault(CodeCorrupt, fmt.Sprintf("备份 %s 的文件 %s 校验不对（被改过或损坏）", meta.ID, v.File))
		}
	}
	return nil
}

// dirBytes is the size of all regular files under dir.
func dirBytes(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}
