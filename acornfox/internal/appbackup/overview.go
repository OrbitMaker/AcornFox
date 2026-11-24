package appbackup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Overview is the state of the backup directory as a whole (GET
// /v1/backups): what `acornfox doctor` and the console's backup page read.
type Overview struct {
	Dir        string       `json:"dir"`
	Writable   bool         `json:"writable"`
	FreeBytes  uint64       `json:"free_bytes,omitempty"`
	FreeKnown  bool         `json:"free_known"`
	TotalBytes int64        `json:"total_bytes"`
	Keep       KeepCounts   `json:"keep"`
	Apps       []AppBackups `json:"apps"`
	Running    []TaskView   `json:"running,omitempty"`
	Findings   []Finding    `json:"findings"`
}

// KeepCounts is how many backups of each kind are kept per app.
type KeepCounts struct {
	Manual     int `json:"manual"`
	PreUpgrade int `json:"pre_upgrade"`
	PreRestore int `json:"pre_restore"`
	// AutoDays: the two automatic kinds are never removed to keep a count
	// while younger than this many days.
	AutoDays int `json:"auto_days"`
}

// AppBackups summarizes one app's backups.
type AppBackups struct {
	App       string     `json:"app"`
	Installed bool       `json:"installed"` // false: the app is gone, its backups are left
	Count     int        `json:"count"`
	Bytes     int64      `json:"bytes"`
	Latest    *time.Time `json:"latest,omitempty"`
}

// Finding is one thing the directory check found.
type Finding struct {
	Code string `json:"code"`
	// Severity: ok, warn or fail.
	Severity string `json:"severity"`
	Text     string `json:"text"`
}

// Finding severities.
const (
	SevOK   = "ok"
	SevWarn = "warn"
	SevFail = "fail"
)

// lowSpace is the free space under which doctor warns.
const lowSpace = 1 << 30

// Overview reads the backup directory. Read only (it makes the directory
// when there is none, to try that it can be written).
func (m *Manager) Overview(ctx context.Context) Overview {
	ov := Overview{Dir: m.cfg.Dir, Keep: KeepCounts{Manual: m.cfg.KeepManual, PreUpgrade: m.cfg.KeepPreUpgrade, PreRestore: m.cfg.KeepPreRestore, AutoDays: m.cfg.AutoKeepDays}}
	for _, t := range m.Tasks("") {
		if t.Status == TaskRunning {
			ov.Running = append(ov.Running, t)
		}
	}
	if err := os.MkdirAll(m.cfg.Dir, 0o700); err == nil {
		probe := filepath.Join(m.cfg.Dir, fmt.Sprintf(".write-test-%d", os.Getpid()))
		if f, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600); err == nil {
			_ = f.Close()
			_ = os.Remove(probe)
			ov.Writable = true
		}
	}
	if m.cfg.Free != nil {
		if sp, err := m.cfg.Free(ctx); err == nil {
			ov.FreeBytes, ov.FreeKnown = sp.BackupFree, true
		}
	}
	installed := map[string]bool{}
	if apps, err := m.cfg.Store.ListApps(ctx); err == nil {
		for _, a := range apps {
			installed[a.Name] = true
		}
	}
	var partials []string
	entries, _ := os.ReadDir(m.cfg.Dir)
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		app := e.Name()
		list, _ := m.List(app)
		sum := AppBackups{App: app, Installed: installed[app], Count: len(list)}
		for _, b := range list {
			sum.Bytes += b.Bytes
			if sum.Latest == nil {
				t := b.CreatedAt
				sum.Latest = &t
			}
		}
		ov.TotalBytes += sum.Bytes
		sub, _ := os.ReadDir(filepath.Join(m.cfg.Dir, app))
		for _, s := range sub {
			if s.IsDir() && strings.HasSuffix(s.Name(), partialExt) {
				partials = append(partials, app+"/"+s.Name())
			}
		}
		ov.Apps = append(ov.Apps, sum)
	}
	sort.Slice(ov.Apps, func(i, j int) bool { return ov.Apps[i].App < ov.Apps[j].App })

	switch {
	case !ov.Writable:
		ov.Findings = append(ov.Findings, Finding{Code: CodeDirNotWritable, Severity: SevFail, Text: fmt.Sprintf("备份目录 %s 写不进去（权限或磁盘问题），现在不能做备份", m.cfg.Dir)})
	case ov.FreeKnown && ov.FreeBytes < lowSpace:
		ov.Findings = append(ov.Findings, Finding{Code: CodeDisk, Severity: SevWarn, Text: fmt.Sprintf("备份目录所在磁盘只剩 %s，不够给大一点的应用做备份；清理磁盘或删掉不用的旧备份", humanBytes(int64(ov.FreeBytes)))})
	default:
		text := fmt.Sprintf("备份目录 %s 可写", m.cfg.Dir)
		if ov.FreeKnown {
			text += "，剩余 " + humanBytes(int64(ov.FreeBytes))
		}
		text += fmt.Sprintf("；现有 %d 份备份共 %s", totalCount(ov.Apps), humanBytes(ov.TotalBytes))
		ov.Findings = append(ov.Findings, Finding{Code: CodeDirNotWritable, Severity: SevOK, Text: text})
	}
	if len(partials) > 0 {
		sort.Strings(partials)
		ov.Findings = append(ov.Findings, Finding{Code: CodeInterrupted, Severity: SevWarn, Text: fmt.Sprintf("有 %d 份没做完的备份留在目录里（%s）：重新给这个应用做一次备份时会自己清掉", len(partials), strings.Join(partials, "、"))})
	}
	for _, t := range m.Tasks("") {
		if t.Status == TaskFailed && t.Fault != nil && t.Fault.Code == CodeInterrupted && t.FinishedAt != nil && m.now().Sub(*t.FinishedAt) < 24*time.Hour {
			ov.Findings = append(ov.Findings, Finding{Code: CodeInterrupted, Severity: SevWarn, Text: fmt.Sprintf("应用 %s 的一件备份或恢复被服务重启打断了（任务 %s）：看 acornfox backup list %s，数据没有损坏的话重新做一次即可", t.App, t.ID, t.App)})
		}
		if t.Status == TaskFailed && t.Fault != nil && t.Fault.Code == CodeRollbackFailed && t.FinishedAt != nil && m.now().Sub(*t.FinishedAt) < 7*24*time.Hour {
			ov.Findings = append(ov.Findings, Finding{Code: CodeRollbackFailed, Severity: SevFail, Text: fmt.Sprintf("应用 %s 的恢复没成功，自动退回也没成功（任务 %s）：恢复前的数据在备份 %s 里，别再动这个应用，马上找负责的人", t.App, t.ID, preID(t))})
		}
	}
	return ov
}

func totalCount(apps []AppBackups) int {
	n := 0
	for _, a := range apps {
		n += a.Count
	}
	return n
}

func preID(t TaskView) string {
	if t.Result != nil && t.Result.PreRestoreID != "" {
		return t.Result.PreRestoreID
	}
	return "（没有）"
}
