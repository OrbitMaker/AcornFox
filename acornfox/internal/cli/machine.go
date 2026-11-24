package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/machine"
)

// `acornfox machine`, `acornfox skill refresh` and the Markdown part (分册)
// kept in the Skill directory for each registered server.
//
// The truth is `acornfox --json machine`: the server works it out when asked
// (GET /v1/machine, internal/apiserver/machine.go). The file
// machine-<server>.md next to SKILL.md is a copy for an AI workbench that
// reads files before it runs commands. It is written by `skill install` and
// `skill refresh`, and again after a command that changes what is installed
// or its settings succeeded (autoRefreshMachine). It holds no password, key
// or token (the view has none).

const machineUsage = `用法：acornfox machine [--json]

用途：这台服务器现在能做什么：装了哪些应用和解决方案、各自什么状态、怎么打开、有没有手册（acornfox catalog skill 应用）、
      有哪些服务和谁在用、平台本身能备份、升级、让应用跟上设置吗。AI 工作台开始做事前先看它；
      缓存超过一天或用户刚在页面上动过应用，先重新取一次。
副作用：无，只读；现场算，不存。里面没有密码、Key、令牌的值，也不会有。
输出（--json）：
  {"ok":true,"schema":1,"generated_at":"…","machine":{"name":"登记的服务器名","version":"…","region":"cn|global"},
   "apps":[{"id":"目录里的编号","instance":"这台机器上的名字","name","tagline","source":"catalog|deploy",
            "state":"running|stopped|not_running|unknown","url","exposure":"local|lan|public|internal","version",
            "manual":{"has","kind":"app","version"},"provides":["服务类型"],"uses":[{"name","kind","required","service"}]}],
   "solutions":[{"id","name","apps":["应用编号"],"manual":{…}}],
   "services":[{"type","name","service":"登记名","provider":"提供它的应用（自己登记的是空）","status","used_by":["应用"],"registered_by_user"}],
   "platform":{"backup","upgrade","reapply"},"warnings":[{"code":"MACHINE-01","part","message"}]}
  warnings 不空是有一块没算全（其余照常给），照 acornfox faults MACHINE-01 做。
退出码：0 正常；2 参数不对；3 连不上服务器；4 服务器拒绝（服务器版本太旧没有这个接口是 server_too_old）。
例子：
  acornfox machine
  acornfox --json machine
  acornfox --target 家里的服务器 --json machine`

const skillRefreshUsage = `用法：acornfox skill refresh [--dir DIR]

用途：重新写 Skill 目录里的“本机能力”分册：每台登记的服务器一份 machine-服务器名.md（文件头写生成时间、服务器、版本）。
      在命令行里装、删、升级、退回、恢复应用和改设置之后它会自己刷新；在页面上动过应用之后，或者缓存超过一天，手动运行一次。
      给 --target 只刷新那一台。已经不在登记里的服务器的旧分册会删掉。
前提：已经运行过 acornfox skill install（Skill 目录里有 acornfox/SKILL.md）；--dir 指的是 skill install --dir 用过的目录。
副作用：写 Skill 目录里的 machine-*.md（只这些文件，重复运行不出错）；会连每台服务器读一次（只读）。里面没有密码、Key、令牌的值。
输出（--json）：{"ok":true,"refreshed":[{"server","path"}],"failed":[{"server","code":"MACHINE-03","message"}],"removed":["旧分册"]}
退出码：0 至少刷新了一台，或没有登记的服务器；1 所有服务器都没刷新成功（MACHINE-03）或写不进目录（MACHINE-02）；2 参数不对。
例子：
  acornfox skill refresh
  acornfox --target 家里的服务器 skill refresh`

// machineTimeout bounds one server's connect and read when writing the
// parts: one unreachable server must not hold up the others for long.
const machineTimeout = 25 * time.Second

func (a *app) cmdMachine(ctx context.Context, args []string) int {
	fs := a.catalogFlags("machine", machineUsage)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) > 0 {
		return a.out.usageError("machine 不带参数；看某台服务器用 --target 服务器名")
	}
	t, name, rerr := a.resolveTarget()
	if rerr != nil {
		return a.out.usageError("%s", rerr.Error())
	}
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()
	ma, ok := api.(client.MachineAPI)
	if !ok {
		return a.out.fail(client.ServerTooOld(machineWhat))
	}
	v, merr := ma.Machine(ctx)
	if merr != nil {
		return a.out.fail(merr)
	}
	v.Machine.Name = name
	if a.out.json {
		a.out.emitJSON(machineFields(v.View))
		return exitOK
	}
	a.out.human("%s", strings.TrimRight(machine.Text(v.View), "\n"))
	return exitOK
}

const machineWhat = "本机能力（machine）"

// machineFields is the view as the fields of the JSON answer.
func machineFields(v machine.View) map[string]any {
	b, _ := json.Marshal(v)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	return m
}

// ---- the part kept in the Skill directory ----

// skillAcornfoxDirs are the directories (…/skills/acornfox) of the Skill
// installed on this computer, one per workbench that has it.
func skillAcornfoxDirs(home string) []string {
	var out []string
	for _, wb := range detectWorkbenches(home) {
		dir := filepath.Join(wb.SkillsDir, skillName)
		if fi, err := os.Stat(filepath.Join(dir, "SKILL.md")); err == nil && !fi.IsDir() {
			out = append(out, dir)
		}
	}
	return out
}

// writeMachinePart writes the part of server into every dir; it returns the
// paths written. The first write error stops it (MACHINE-02).
func writeMachinePart(dirs []string, server string, v machine.View) ([]string, error) {
	text := machine.Markdown(v, server)
	var paths []string
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return paths, fmt.Errorf("创建目录 %s 失败：%w", dir, err)
		}
		path := filepath.Join(dir, machine.CacheFileName(server))
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
			return paths, fmt.Errorf("写入 %s 失败：%w", path, err)
		}
		if err := os.Rename(tmp, path); err != nil {
			_ = os.Remove(tmp)
			return paths, fmt.Errorf("写入 %s 失败：%w", path, err)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// removeStaleMachineParts deletes the machine-*.md files in dirs that this
// command line wrote (their first line says so) for a server that is no
// longer registered. keep holds the file names of the registered servers.
func removeStaleMachineParts(dirs []string, keep map[string]bool) []string {
	var removed []string
	for _, dir := range dirs {
		matches, _ := filepath.Glob(filepath.Join(dir, machine.CacheFilePrefix+"*"+machine.CacheFileExt))
		for _, p := range matches {
			if keep[filepath.Base(p)] {
				continue
			}
			data, err := os.ReadFile(p)
			if err != nil || !strings.HasPrefix(string(data), "<!-- 生成时间：") {
				continue
			}
			if os.Remove(p) == nil {
				removed = append(removed, p)
			}
		}
	}
	return removed
}

// machineResult is what one server's part came to.
type machineResult struct {
	Server  string `json:"server"`
	Path    string `json:"path,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// fetchMachine reads the view of a server without the "server is newer"
// notice (this is a side job, not what the user asked for).
func (a *app) fetchMachine(ctx context.Context, t client.Target, name string) (machine.View, error) {
	if a.connect == nil {
		return machine.View{}, errors.New("内部错误：未配置连接器")
	}
	ctx, cancel := context.WithTimeout(ctx, machineTimeout)
	defer cancel()
	api, err := a.connect(ctx, t)
	if err != nil {
		return machine.View{}, err
	}
	defer api.Close()
	ma, ok := api.(client.MachineAPI)
	if !ok {
		return machine.View{}, client.ServerTooOld(machineWhat)
	}
	v, err := ma.Machine(ctx)
	if err != nil {
		return machine.View{}, err
	}
	v.Machine.Name = name
	return v.View, nil
}

// refreshMachineParts writes the part of each named server into dirs (all
// registered servers when names is empty). It never prints.
func (a *app) refreshMachineParts(ctx context.Context, dirs []string, names []string) (done []machineResult, failed []machineResult, removed []string) {
	tf, err := loadTargets(a.configDir)
	if err != nil {
		return nil, []machineResult{{Server: "", Code: machine.CodeNoServer, Message: "读取 targets.json 失败：" + err.Error()}}, nil
	}
	all := make([]string, 0, len(tf.Targets))
	for n := range tf.Targets {
		all = append(all, n)
	}
	sort.Strings(all)
	if len(names) == 0 {
		names = all
	}
	for _, n := range names {
		t := tf.Targets[n]
		if t == nil {
			failed = append(failed, machineResult{Server: n, Code: machine.CodeNoServer, Message: "找不到服务器 " + n + "；用 acornfox target list 查看"})
			continue
		}
		tt := *t
		tt.Name = n
		if tt.RemoteCommand == "" {
			tt.RemoteCommand = defaultRemote
		}
		v, ferr := a.fetchMachine(ctx, tt, n)
		if ferr != nil {
			_, d := exitCodeForError(ferr)
			failed = append(failed, machineResult{Server: n, Code: machine.CodeNoServer, Message: strings.TrimSpace(d.Message + " " + d.Hint)})
			continue
		}
		paths, werr := writeMachinePart(dirs, n, v)
		if werr != nil {
			failed = append(failed, machineResult{Server: n, Code: machine.CodeCacheWrite, Message: werr.Error()})
			continue
		}
		for _, p := range paths {
			done = append(done, machineResult{Server: n, Path: p})
		}
	}
	if len(names) == len(all) {
		keep := map[string]bool{}
		for _, n := range all {
			keep[machine.CacheFileName(n)] = true
		}
		removed = removeStaleMachineParts(dirs, keep)
	}
	return done, failed, removed
}

// cmdSkillRefresh implements `skill refresh [--dir DIR]`.
func (a *app) cmdSkillRefresh(ctx context.Context, args []string) int {
	fs := a.catalogFlags("skill refresh", skillRefreshUsage)
	dir := fs.String("dir", "", "写到指定的 skills 目录（skill install --dir 用过的那个）")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) > 0 {
		return a.out.usageError("用法：skill refresh [--dir DIR]；只刷新一台用 --target 服务器名")
	}
	var dirs []string
	if *dir != "" {
		d := filepath.Join(*dir, skillName)
		if fi, serr := os.Stat(filepath.Join(d, "SKILL.md")); serr != nil || fi.IsDir() {
			return a.out.usageError("%s 里还没有 Skill；先运行 acornfox skill install --dir %s", d, *dir)
		}
		dirs = []string{d}
	} else {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return a.out.usageError("无法确定用户主目录：%v", herr)
		}
		if dirs = skillAcornfoxDirs(home); len(dirs) == 0 {
			return a.out.usageError("这台电脑上还没装 Skill（找不到 ~/.claude/skills/acornfox、~/.codex/skills/acornfox）；先运行 acornfox skill install")
		}
	}
	var names []string
	if a.flagTarget != "" {
		names = []string{a.flagTarget}
	}
	done, failed, removed := a.refreshMachineParts(ctx, dirs, names)
	if a.out.json {
		a.out.emitJSON(map[string]any{"refreshed": nonNil(done), "failed": nonNil(failed), "removed": nonNilStrings(removed)})
	} else {
		for _, d := range done {
			a.out.human("✓ %s：%s", d.Server, d.Path)
		}
		for _, f := range failed {
			a.out.humanErr("✗ %s：%s（%s）", f.Server, f.Message, f.Code)
		}
		for _, r := range removed {
			a.out.human("已删除不再登记的服务器的旧分册：%s", r)
		}
		if len(done) == 0 && len(failed) == 0 {
			a.out.human("还没有登记的服务器（acornfox target add），没有分册要写。")
		}
	}
	if len(done) == 0 && len(failed) > 0 {
		return exitOpFailed
	}
	return exitOK
}

func nonNil(v []machineResult) []machineResult {
	if v == nil {
		return []machineResult{}
	}
	return v
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// ---- after a command that changed something ----

// machineChangingCommands are the commands (first word, then the second
// word that makes it one, "" for any) after which the part of the server is
// written again when they succeeded. Trial runs (--plan) change nothing and
// do not refresh.
var machineChangingCommands = map[string][]string{
	"install":  {""},
	"delete":   {""},
	"deploy":   {""},
	"rollback": {""},
	"redeploy": {""},
	"app":      {"set"},
	"apps":     {"set", "reapply", "upgrade", "rollback"},
	"backup":   {"restore"},
	"services": {"add", "set", "remove", "rm"},
	"domain":   {"add", "remove"},
	"add":      {""},
	"remove":   {""},
}

// changesMachine reports whether cmd with args is one of them (and not a
// trial run or a look at a task).
func changesMachine(cmd string, args []string) bool {
	seconds, ok := machineChangingCommands[cmd]
	if !ok {
		return false
	}
	for _, x := range args {
		if x == "--plan" || x == "-plan" || x == "-h" || x == "--help" {
			return false
		}
	}
	if cmd == "install" && len(args) > 0 && args[0] == "status" {
		return false
	}
	second := ""
	if len(args) > 0 {
		second = args[0]
	}
	for _, s := range seconds {
		if s == "" || s == second {
			return true
		}
	}
	return false
}

// autoRefreshMachine writes the part of the server the command worked on,
// into every Skill directory that exists on this computer. It is silent and
// never changes the exit code: the command already succeeded, the part is a
// copy. It only runs for a real run of the command line (configDir is the
// user's own: the tests give a temporary one), and does nothing when the
// Skill is not installed here.
func (a *app) autoRefreshMachine(ctx context.Context, cmd string, args []string) {
	if a.configDir != "" || !changesMachine(cmd, args) {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dirs := skillAcornfoxDirs(home)
	if len(dirs) == 0 {
		return
	}
	t, name, rerr := a.resolveTarget()
	if rerr != nil {
		return
	}
	v, ferr := a.fetchMachine(ctx, t, name)
	if ferr != nil {
		return
	}
	_, _ = writeMachinePart(dirs, name, v)
}
