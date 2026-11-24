package appbackup

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// A task is one backup or restore as it runs: steps with a status, the
// fault when one failed, the result. It is the thing the API's 202 answer
// names and `acornfox backup … --wait`, the console and the Skill poll.

// Task operations and statuses.
const (
	OpBackup  = "backup"
	OpRestore = "restore"

	TaskRunning   = "running"
	TaskSucceeded = "succeeded"
	TaskFailed    = "failed"

	StepPending = "pending"
	StepRunning = "running"
	StepDone    = "done"
	StepFailed  = "failed"
	StepSkipped = "skipped"
)

// maxTasks is how many finished tasks are kept (memory and files).
const maxTasks = 50

var taskIDPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)

// ValidTaskID reports whether id has the form of a task id.
func ValidTaskID(id string) bool { return taskIDPattern.MatchString(id) }

// Step is one stage of a task.
type Step struct {
	Key    string `json:"key"`
	Text   string `json:"text"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// TaskFault is the fault a failed task ended with.
type TaskFault struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// TaskResult is what a task that succeeded (or failed after doing something)
// left: the backup it made, the pre-restore backup, the version it ended on.
type TaskResult struct {
	BackupID     string   `json:"backup_id,omitempty"` // backup: the new one; restore: the one restored
	Kind         string   `json:"kind,omitempty"`
	Bytes        int64    `json:"bytes,omitempty"`
	PreRestoreID string   `json:"pre_restore_id,omitempty"` // restore: the backup taken just before
	RolledBack   bool     `json:"rolled_back,omitempty"`    // restore: failed and went back
	Version      string   `json:"version,omitempty"`        // restore: what the app runs now
	DowntimeMS   int64    `json:"downtime_ms,omitempty"`
	Pruned       []string `json:"pruned,omitempty"` // older backups removed to keep the count
	Warnings     []string `json:"warnings,omitempty"`
}

// TaskView is the public form of a task (the API's answer, the persisted file).
type TaskView struct {
	ID         string      `json:"id"`
	App        string      `json:"app"`
	Op         string      `json:"op"`
	Kind       string      `json:"kind,omitempty"` // backup: its kind
	Status     string      `json:"status"`
	Steps      []Step      `json:"steps"`
	Fault      *TaskFault  `json:"fault,omitempty"`
	Result     *TaskResult `json:"result,omitempty"`
	StartedAt  time.Time   `json:"started_at"`
	FinishedAt *time.Time  `json:"finished_at,omitempty"`
}

// taskRun is a task while it runs.
type taskRun struct {
	m  *Manager
	mu sync.Mutex
	v  TaskView
}

func (m *Manager) newTask(app, op, kind string, steps []Step) *taskRun {
	var b [6]byte
	_, _ = rand.Read(b[:])
	t := &taskRun{m: m, v: TaskView{ID: hex.EncodeToString(b[:]), App: app, Op: op, Kind: kind, Status: TaskRunning, Steps: steps, StartedAt: m.now()}}
	for i := range t.v.Steps {
		if t.v.Steps[i].Status == "" {
			t.v.Steps[i].Status = StepPending
		}
	}
	m.mu.Lock()
	m.tasks[t.v.ID] = t
	m.order = append(m.order, t.v.ID)
	m.trimTasksLocked()
	m.mu.Unlock()
	t.save()
	return t
}

// dropTask forgets a task that never started (the app was busy): it was
// refused, there is nothing to look at.
func (m *Manager) dropTask(t *taskRun) {
	m.mu.Lock()
	delete(m.tasks, t.v.ID)
	for i, id := range m.order {
		if id == t.v.ID {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	_ = os.Remove(filepath.Join(m.cfg.Dir, ".tasks", t.v.ID+".json"))
}

// trimTasksLocked forgets the oldest finished tasks beyond maxTasks.
func (m *Manager) trimTasksLocked() {
	if len(m.order) <= maxTasks {
		return
	}
	keep := m.order[:0:0]
	over := len(m.order) - maxTasks
	for _, id := range m.order {
		t := m.tasks[id]
		if over > 0 && t != nil && t.snapshot().Status != TaskRunning {
			delete(m.tasks, id)
			_ = os.Remove(filepath.Join(m.cfg.Dir, ".tasks", id+".json"))
			over--
			continue
		}
		keep = append(keep, id)
	}
	m.order = keep
}

func (t *taskRun) snapshot() TaskView {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.v
	v.Steps = append([]Step(nil), t.v.Steps...)
	if t.v.Result != nil {
		r := *t.v.Result
		v.Result = &r
	}
	return v
}

// step sets one step's status (and its detail when given).
func (t *taskRun) step(key, status, detail string) {
	t.mu.Lock()
	for i := range t.v.Steps {
		if t.v.Steps[i].Key == key {
			t.v.Steps[i].Status = status
			if detail != "" {
				t.v.Steps[i].Detail = detail
			}
		}
	}
	t.mu.Unlock()
	t.save()
}

// addStep appends a step the task did not plan for (a rollback).
func (t *taskRun) addStep(s Step) {
	t.mu.Lock()
	t.v.Steps = append(t.v.Steps, s)
	t.mu.Unlock()
	t.save()
}

func (t *taskRun) setResult(fn func(*TaskResult)) {
	t.mu.Lock()
	if t.v.Result == nil {
		t.v.Result = &TaskResult{}
	}
	fn(t.v.Result)
	t.mu.Unlock()
}

// finish ends the task: err nil is success. A step still running is marked
// failed, the ones not reached skipped.
func (t *taskRun) finish(err error) {
	t.mu.Lock()
	now := t.m.now()
	t.v.FinishedAt = &now
	if err == nil {
		t.v.Status = TaskSucceeded
	} else {
		t.v.Status = TaskFailed
		f := &TaskFault{Message: err.Error()}
		if e, ok := asFault(err); ok {
			f.Code, f.Message, f.Hint = e.Code, e.Message, e.Hint
		}
		t.v.Fault = f
		for i := range t.v.Steps {
			switch t.v.Steps[i].Status {
			case StepRunning:
				t.v.Steps[i].Status = StepFailed
			case StepPending:
				t.v.Steps[i].Status = StepSkipped
			}
		}
	}
	t.mu.Unlock()
	t.save()
}

// save writes the task to <dir>/.tasks/<id>.json (a failure only goes to
// the log: the task still runs).
func (t *taskRun) save() {
	v := t.snapshot()
	dir := filepath.Join(t.m.cfg.Dir, ".tasks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	if err := writeJSONFile(filepath.Join(dir, v.ID+".json"), v); err != nil {
		t.m.cfg.Log.Warn("save backup task", "task", v.ID, "err", err)
	}
}

// loadTasks reads the tasks of earlier runs; one that was running then is
// failed with BKP-10 (this server is the only one that runs them).
func (m *Manager) loadTasks() {
	dir := filepath.Join(m.cfg.Dir, ".tasks")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var views []TaskView
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var v TaskView
		if json.Unmarshal(b, &v) != nil || !ValidTaskID(v.ID) {
			continue
		}
		views = append(views, v)
	}
	sort.Slice(views, func(i, j int) bool { return views[i].StartedAt.Before(views[j].StartedAt) })
	for _, v := range views {
		t := &taskRun{m: m, v: v}
		if v.Status == TaskRunning {
			t.finish(fault(CodeInterrupted, "服务重启了，这件备份或恢复中途被打断"))
		}
		m.tasks[v.ID] = t
		m.order = append(m.order, v.ID)
	}
	m.trimTasksLocked()
}

// Task returns one task; ErrNoTask when there is none.
func (m *Manager) Task(id string) (TaskView, error) {
	m.mu.Lock()
	t := m.tasks[id]
	m.mu.Unlock()
	if t == nil {
		return TaskView{}, ErrNoTask
	}
	return t.snapshot(), nil
}

// ErrNoTask: no such task.
var ErrNoTask = errors.New("appbackup: no such task")

// Tasks returns the tasks of app (all apps when empty), newest first.
func (m *Manager) Tasks(app string) []TaskView {
	m.mu.Lock()
	ids := append([]string(nil), m.order...)
	m.mu.Unlock()
	var out []TaskView
	for i := len(ids) - 1; i >= 0; i-- {
		m.mu.Lock()
		t := m.tasks[ids[i]]
		m.mu.Unlock()
		if t == nil {
			continue
		}
		v := t.snapshot()
		if app == "" || v.App == app {
			out = append(out, v)
		}
	}
	return out
}

// Standard steps.
func backupSteps() []Step {
	return []Step{
		{Key: "check", Text: "检查应用和磁盘空间"},
		{Key: "stop", Text: "暂停应用（保证数据是完整的一份）"},
		{Key: "pack", Text: "打包数据"},
		{Key: "start", Text: "让应用继续运行"},
		{Key: "save", Text: "保存备份清单"},
		{Key: "prune", Text: "清掉超出份数的旧备份"},
	}
}

func restoreSteps() []Step {
	return []Step{
		{Key: "check", Text: "核对这份备份、版本和磁盘空间"},
		{Key: "snapshot", Text: "先给现在的数据做一份备份（恢复前）"},
		{Key: "swap", Text: "换上备份里的数据"},
		{Key: "records", Text: "恢复变量和设置"},
		{Key: "deploy", Text: "按备份时的版本启动，等应用就绪"},
		{Key: "verify", Text: "核对服务连接"},
	}
}

func stepText(steps []Step, key string) string {
	for _, s := range steps {
		if s.Key == key {
			return s.Text
		}
	}
	return key
}

var _ = fmt.Sprintf
