package edgeadapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
)

// storeFile is the file under the state directory that keeps the last
// configuration the adapter applied (edge.StoreFile in the container).
var storeFile = filepath.Base(edge.StoreFile)

// Store keeps the last applied configuration on the container's settings
// volume, so a restarted container serves every route again before the
// acornfox program says anything.
type Store struct{ dir string }

// NewStore returns the store in dir (created on the first Save).
func NewStore(dir string) *Store { return &Store{dir: dir} }

// stored is the file format, shared with the runner (edge.Stored). Load
// recomputes the version from Config.
type stored = edge.Stored

// Path is the file the store reads and writes.
func (s *Store) Path() string { return filepath.Join(s.dir, storeFile) }

// Load returns the stored configuration; ok is false when there is none yet.
func (s *Store) Load() (cfg edge.Config, ok bool, err error) {
	b, err := os.ReadFile(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		return edge.Config{}, false, nil
	}
	if err != nil {
		return edge.Config{}, false, err
	}
	var st stored
	if err := json.Unmarshal(b, &st); err != nil {
		return edge.Config{}, false, fmt.Errorf("读不懂保存的设置 %s：%w", s.Path(), err)
	}
	return st.Config, true, nil
}

// Save replaces the stored configuration atomically: a crash leaves either
// the old or the new file, never half of one.
func (s *Store) Save(cfg edge.Config) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(stored{Version: cfg.Version(), SavedAt: time.Now().UTC(), Config: cfg}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, storeFile+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.Path()); err != nil {
		return err
	}
	if d, err := os.Open(s.dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
