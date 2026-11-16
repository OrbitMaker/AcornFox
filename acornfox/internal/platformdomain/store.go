package platformdomain

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileName is the file in the data directory the platform domain is kept in.
const FileName = "platform-domain.json"

// maxChanges is how many past changes the file keeps (the operation record).
const maxChanges = 20

// saved is the file's content.
type saved struct {
	Domain  string    `json:"domain,omitempty"`
	SetAt   time.Time `json:"set_at,omitempty"`
	Changes []Change  `json:"changes,omitempty"`
}

// Change is one past change of the platform domain (the operation record):
// From "" is a first set, To "" a clear.
type Change struct {
	At   time.Time `json:"at"`
	From string    `json:"from,omitempty"`
	To   string    `json:"to,omitempty"`
}

func load(path string) (saved, error) {
	var s saved
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("读取平台域名设置失败：%w", err)
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return saved{}, fmt.Errorf("平台域名设置文件 %s 损坏：%w", path, err)
	}
	return s, nil
}

// store replaces the file atomically (0600).
func store(path string, s saved) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".platform-domain-*.json")
	if err != nil {
		return fmt.Errorf("写入平台域名设置失败：%w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("写入平台域名设置失败：%w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("写入平台域名设置失败：%w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("写入平台域名设置失败：%w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("写入平台域名设置失败：%w", err)
	}
	return nil
}
