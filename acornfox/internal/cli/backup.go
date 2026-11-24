package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

const backupUsage = `用法：acornfox backup 子命令 …   [--json]

应用数据备份和恢复：把一个应用的全部数据（数据卷里的文件）连同它的变量和设置打包存在这台服务器上，
出问题时恢复回来。备份时应用会暂停几十秒到几分钟，打完包自动继续运行。备份存在服务器的数据目录
app-backups/应用名/ 下，不删应用时也不会丢；恢复只能恢复到同一台服务器的同名应用。

子命令
  list [应用]                         看备份：带应用名列出这个应用的所有备份（新的在前），不带则看整个备份目录
                                      的情况（每个应用几份、占多大、磁盘剩多少、有没有问题）。只读
  create [应用] [--note 话] [--plan]   做一份手动备份（默认每个应用留最近 5 份，旧的自动清掉）。--plan 只看
                                      会怎样、够不够磁盘，什么都不动。默认等到做完才返回，--no-wait 只提交
  show 应用 编号 [--verify]            看一份备份的清单；--verify 把每个文件的校验和都核对一遍（只读）
  restore 应用 编号 [--plan] [--yes]   用这份备份的数据覆盖应用现在的数据（编号写 latest 是最新的一份）。
                                      会先把现在的数据另存一份（恢复前自动备份），恢复失败自动退回；必须先 --plan 看过、
                                      告诉用户、用户同意后加 --yes 才会真做。自动另存的（升级前、退回前、恢复前）
                                      至少留 7 天、每种最新 3 份也留着，不会被别的操作自动清；只有磁盘放不下时才清最旧的，
                                      --plan 里会写明清哪几份，做完返回的 pruned 列出实际清掉的
  rm 应用 编号 [--yes]                删掉一份备份的文件（删了找不回来，要用户同意后加 --yes；不加只提示）
  task 任务编号                        看一件备份或恢复做到哪一步了（只读）

选项
  --note 话            给这份备份写几个字（最多 200 字）
  --plan               只看会怎样，什么都不动
  --yes                用户已经同意（恢复要覆盖现在的数据；rm 删了找不回来）
  --allow-current-version   备份时的程序版本已不在本机（BKP-09）时，改用现在的版本继续恢复（要先问用户）
  --no-wait            只提交，不等做完；用 task 看进度
  --timeout 30m        等多久（默认 30 分钟）

例子
  acornfox backup create hello --note 升级之前
  acornfox backup list hello
  acornfox backup restore hello latest --plan
  acornfox backup restore hello 20261124-153012-ab12 --yes
  acornfox backup rm hello 20261124-153012-ab12 --yes

出错先看：失败时会打故障编号（BKP-xx），运行 acornfox faults BKP-xx 看原因和处理办法；
服务器上 acornfox doctor 会检查备份目录和磁盘。
`

// backupPoll is how often a waiting backup command asks the server how its
// task is going (tests make it short).
var backupPoll = time.Second

var backupKindName = map[string]string{"manual": "手动", "pre-upgrade": "升级前", "pre-restore": "恢复前"}

func (a *app) backupAPI(ctx context.Context, res resolution) (client.API, client.BackupAPI, int) {
	api, derr := a.dial(ctx, res.target)
	if derr != nil {
		return nil, nil, a.out.fail(derr)
	}
	b, ok := api.(client.BackupAPI)
	if !ok {
		api.Close()
		return nil, nil, a.out.fail(client.ServerTooOld(backupsWhatCLI))
	}
	return api, b, exitOK
}

const backupsWhatCLI = "应用数据备份"

// cmdBackup implements `backup list|create|show|restore|rm|task`.
func (a *app) cmdBackup(ctx context.Context, args []string) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") && args[0] != "-h" && args[0] != "--help" {
		return a.out.usageError("backup 需要一个子命令：list、create、show、restore、rm、task（acornfox backup --help 看说明）")
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "help", "-h", "--help":
		fmt.Fprint(a.out.stdout, backupUsage)
		return exitOK
	case "list", "ls":
		return a.backupList(ctx, args)
	case "create", "new":
		return a.backupCreate(ctx, args)
	case "show":
		return a.backupShow(ctx, args)
	case "restore":
		return a.backupRestore(ctx, args)
	case "rm", "remove", "delete":
		return a.backupRm(ctx, args)
	case "task":
		return a.backupTaskCmd(ctx, args)
	}
	return a.out.usageError("不认识的子命令 %q。可用：list、create、show、restore、rm、task（acornfox backup --help）", sub)
}

func humanSize(n int64) string {
	const k = 1024
	switch {
	case n >= k*k*k:
		return fmt.Sprintf("%.1f GB", float64(n)/(k*k*k))
	case n >= k*k:
		return fmt.Sprintf("%.1f MB", float64(n)/(k*k))
	case n >= k:
		return fmt.Sprintf("%.1f KB", float64(n)/k)
	}
	return fmt.Sprintf("%d B", n)
}

func versionLine(m client.BackupMeta) string {
	v := m.Version
	switch {
	case v.CatalogVersion != "" && v.Seq > 0:
		return fmt.Sprintf("%s（第 %d 版）", v.CatalogVersion, v.Seq)
	case v.Seq > 0:
		return fmt.Sprintf("第 %d 版", v.Seq)
	}
	return "-"
}

func (a *app) backupFlags(name string) (*flag.FlagSet, *backupFlagVals) {
	f := a.catalogFlags("backup "+name, backupUsage)
	v := &backupFlagVals{}
	v.note = f.String("note", "", "给这份备份写几个字")
	v.plan = f.Bool("plan", false, "只看会怎样")
	v.yes = f.Bool("yes", false, "用户已经同意")
	v.verify = f.Bool("verify", false, "核对每个文件的校验和")
	v.allow = f.Bool("allow-current-version", false, "备份时的版本不在了，用现在的版本")
	v.noWait = f.Bool("no-wait", false, "只提交，不等做完")
	v.timeout = f.Duration("timeout", 30*time.Minute, "等做完的最长时间")
	return f, v
}

type backupFlagVals struct {
	note    *string
	plan    *bool
	yes     *bool
	verify  *bool
	allow   *bool
	noWait  *bool
	timeout *time.Duration
}

// appAndID reads "APP ID" (or only ID with the app from --app or the
// directory).
func (a *app) appAndID(cmd string, pos []string) (resolution, string, error) {
	switch len(pos) {
	case 2:
		res, err := a.resolveAppArg(cmd, pos[:1])
		return res, pos[1], err
	case 1:
		res, err := a.resolveAppArg(cmd, nil)
		return res, pos[0], err
	}
	return resolution{}, "", fmt.Errorf("用法：acornfox backup %s 应用 编号", cmd)
}

func (a *app) backupList(ctx context.Context, args []string) int {
	fs, _ := a.backupFlags("list")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) > 1 {
		return a.out.usageError("backup list 最多一个应用名")
	}
	t, tname, terr := a.resolveTarget()
	if terr != nil {
		return a.out.usageError("%s", terr.Error())
	}
	res := resolution{target: t, targetName: tname}
	if len(pos) == 1 {
		r, rerr := a.resolveAppArg("backup list", pos)
		if rerr != nil {
			return a.out.usageError("%s", rerr.Error())
		}
		res = r
	}
	api, b, code := a.backupAPI(ctx, res)
	if b == nil {
		return code
	}
	defer api.Close()
	if len(pos) == 0 {
		ov, err := b.Backups(ctx)
		if err != nil {
			return a.out.fail(err)
		}
		if a.out.json {
			a.emitRaw(ov.Raw, nil)
			return exitOK
		}
		a.out.human("备份目录 %s，现有备份共 %s", ov.Dir, humanSize(ov.TotalBytes))
		if ov.FreeKnown {
			a.out.human("所在磁盘剩 %s", humanSize(int64(ov.FreeBytes)))
		}
		a.out.human("每个应用留：手动 %d 份；自动另存的（升级前、恢复前）至少留 %d 天、每种最新 %d 份也留着", ov.Keep.Manual, autoDays(ov.Keep.AutoDays), ov.Keep.PreUpgrade)
		for _, ap := range ov.Apps {
			gone := ""
			if !ap.Installed {
				gone = "（应用已不在这台机器上，备份还留着）"
			}
			latest := ""
			if ap.Latest != nil {
				latest = "，最近一份 " + ap.Latest.Local().Format("2006-01-02 15:04")
			}
			a.out.human("  %s：%d 份，%s%s%s", ap.App, ap.Count, humanSize(ap.Bytes), latest, gone)
		}
		if len(ov.Apps) == 0 {
			a.out.human("  还没有备份。给应用做一份：acornfox backup create 应用名")
		}
		for _, f := range ov.Findings {
			mark := map[string]string{"ok": "✓", "warn": "!", "fail": "✗"}[f.Severity]
			a.out.human("%s %s（%s）", mark, f.Text, f.Code)
		}
		for _, t := range ov.Running {
			a.out.human("正在做：应用 %s 的%s，任务 %s（acornfox backup task %s）", t.App, map[string]string{"backup": "备份", "restore": "恢复"}[t.Op], t.ID, t.ID)
		}
		return exitOK
	}
	l, err := b.AppBackups(ctx, res.app)
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(l.Raw, nil)
		return exitOK
	}
	if !l.Installed {
		a.out.human("应用 %s 已经不在这台机器上，下面是它留下的备份（恢复要先重新安装一个同名应用）。", res.app)
	}
	if len(l.Backups) == 0 {
		a.out.human("应用 %s 还没有备份。做一份：acornfox backup create %s", res.app, res.app)
	} else {
		a.out.human("应用 %s 的备份（共 %d 份，新的在前）：", res.app, len(l.Backups))
		for _, m := range l.Backups {
			note := ""
			if m.Note != "" {
				note = "  " + m.Note
			}
			a.out.human("  %s  %-4s  %s  %s  版本 %s%s", m.ID, backupKindName[m.Kind], m.CreatedAt.Local().Format("2006-01-02 15:04"), humanSize(m.Bytes), versionLine(m), note)
		}
		a.out.human("每个应用留：手动 %d 份，超出的旧备份自动清掉；自动另存的（升级前、恢复前）7 天内的不自动清，每种最新 %d 份也留着，只有磁盘放不下新备份时才提前清最旧的（计划里会写明）", l.Keep.Manual, l.Keep.PreUpgrade)
	}
	if l.Busy != "" {
		a.out.human("正在做：任务 %s（acornfox backup task %s）", l.Busy, l.Busy)
	}
	return exitOK
}

func (a *app) backupCreate(ctx context.Context, args []string) int {
	fs, v := a.backupFlags("create")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	res, rerr := a.resolveAppArg("backup create", pos)
	if rerr != nil {
		return a.out.usageError("%s", rerr.Error())
	}
	api, b, code := a.backupAPI(ctx, res)
	if b == nil {
		return code
	}
	defer api.Close()
	ans, err := b.CreateBackup(ctx, res.app, "manual", *v.note, *v.plan)
	if err != nil {
		return a.out.fail(err)
	}
	if ans.Plan != nil {
		if a.out.json {
			a.emitRaw(ans.Raw, nil)
			return exitOK
		}
		a.out.human("只是看看，什么都没动。")
		a.out.human("%s。", strings.TrimSuffix(ans.Plan.Say, "。"))
		if ans.Plan.Keep > 0 {
			a.out.human("手动备份留最近 %d 份%s。", ans.Plan.Keep, pruneText(ans.Plan.WillPrune))
		}
		if len(ans.Plan.WillPruneForSpace) > 0 {
			a.out.human("磁盘放不下，会先清掉自动另存的旧备份：%s（找不回来）。", strings.Join(ans.Plan.WillPruneForSpace, "、"))
		}
		return exitOK
	}
	return a.followTask(ctx, b, ans, *v.noWait, *v.timeout, "备份")
}

func autoDays(n int) int {
	if n <= 0 {
		return 7
	}
	return n
}

func pruneText(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return "，这次会清掉最旧的 " + strings.Join(ids, "、")
}

// followTask prints a task's steps as they change until it ends (unless
// noWait) and turns its end into the exit code.
func (a *app) followTask(ctx context.Context, b client.BackupAPI, ans client.BackupAnswer, noWait bool, timeout time.Duration, what string) int {
	t := ans.Task
	if t == nil {
		return a.out.fail(fmt.Errorf("服务器没有返回任务"))
	}
	if noWait {
		if a.out.json {
			a.emitRaw(ans.Raw, nil)
			return exitOK
		}
		a.out.human("已开始%s，任务 %s。进度：acornfox backup task %s", what, t.ID, t.ID)
		return exitOK
	}
	if !a.out.json {
		a.out.human("已开始%s，任务 %s。", what, t.ID)
	}
	printed := map[string]string{}
	deadline := time.Now().Add(timeout)
	cur := *t
	for {
		if !a.out.json {
			a.printSteps(cur, printed)
		}
		if cur.Done() {
			break
		}
		if time.Now().After(deadline) {
			if a.out.json {
				a.emitRaw(cur.Raw, nil)
			} else {
				a.out.humanErr("等了 %s 还没做完，它在服务器上还会继续：acornfox backup task %s 看进度", timeout, cur.ID)
			}
			return exitOpFailed
		}
		select {
		case <-ctx.Done():
			return exitOpFailed
		case <-time.After(backupPoll):
		}
		next, err := b.BackupTask(ctx, cur.ID)
		if err != nil {
			return a.out.fail(err)
		}
		cur = next
	}
	return a.reportBackupTask(cur, what)
}

func (a *app) printSteps(t client.BackupTask, printed map[string]string) {
	for _, s := range t.Steps {
		sig := s.Status + "|" + s.Detail
		if s.Status == "pending" || printed[s.Key] == sig {
			continue
		}
		printed[s.Key] = sig
		mark := map[string]string{"running": "…", "done": "✓", "failed": "✗", "skipped": "-"}[s.Status]
		line := fmt.Sprintf("  %s %s", mark, s.Text)
		if s.Detail != "" && s.Status != "pending" {
			line += "：" + s.Detail
		}
		a.out.human("%s", line)
	}
}

// reportTask prints the end of a task and returns the exit code.
func (a *app) reportBackupTask(t client.BackupTask, what string) int {
	if t.Status == "failed" {
		f := t.Fault
		d := client.Diagnosis{Stage: "backup", Code: "unknown", Message: what + "没做成"}
		if f != nil {
			d.Code, d.Message, d.Hint = f.Code, f.Message, f.Hint
		}
		if a.out.json {
			var m map[string]any
			_ = json.Unmarshal(t.Raw, &m)
			a.out.encodeRedacted(a.out.stdout, map[string]any{"ok": false, "diagnosis": map[string]any{"stage": d.Stage, "code": d.Code, "message": a.out.redact(d.Message), "hint": a.out.redact(d.Hint)}, "task": m})
		} else {
			a.out.printDiagnosis(d)
			if t.Result != nil && t.Result.PreRestoreID != "" {
				a.out.humanErr("恢复之前的数据另存在备份 %s（acornfox backup list %s）", t.Result.PreRestoreID, t.App)
			}
		}
		return exitOpFailed
	}
	if a.out.json {
		a.emitRaw(t.Raw, nil)
		return exitOK
	}
	r := t.Result
	if r == nil {
		a.out.human("%s完成。", what)
		return exitOK
	}
	switch t.Op {
	case "backup":
		a.out.human("%s完成：备份 %s（%s，%s）%s", what, r.BackupID, backupKindName[r.Kind], humanSize(r.Bytes), downtimeText(r.DowntimeMS))
		if len(r.Pruned) > 0 {
			a.out.human("按保留份数清掉了旧备份：%s", strings.Join(r.Pruned, "、"))
		}
	case "restore":
		a.out.human("%s完成：应用 %s 已恢复到备份 %s，现在运行的是 %s。", what, t.App, r.BackupID, r.Version)
		a.out.human("恢复之前的数据另存在备份 %s，不想要可以 acornfox backup rm %s %s --yes", r.PreRestoreID, t.App, r.PreRestoreID)
	}
	for _, w := range r.Warnings {
		a.out.human("要留意：%s", w)
	}
	return exitOK
}

func downtimeText(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return fmt.Sprintf("，应用暂停了 %.1f 秒", float64(ms)/1000)
}

func (a *app) backupShow(ctx context.Context, args []string) int {
	fs, v := a.backupFlags("show")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	res, id, rerr := a.appAndID("show", pos)
	if rerr != nil {
		return a.out.usageError("%s", rerr.Error())
	}
	api, b, code := a.backupAPI(ctx, res)
	if b == nil {
		return code
	}
	defer api.Close()
	id, ierr := a.resolveBackupID(ctx, b, res.app, id)
	if ierr != nil {
		return a.out.fail(ierr)
	}
	d, err := b.BackupDetail(ctx, res.app, id, *v.verify)
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(d.Raw, nil)
		return exitOK
	}
	m := d.Backup
	a.out.human("备份 %s（应用 %s，%s）", m.ID, m.App, backupKindName[m.Kind])
	a.out.human("  时间：%s，%s", m.CreatedAt.Local().Format("2006-01-02 15:04:05"), map[bool]string{true: "做的时候应用在运行，暂停了一下", false: "做的时候应用没在运行"}[m.WasRunning])
	a.out.human("  版本：%s；大小 %s（数据 %s）", versionLine(m), humanSize(m.Bytes), humanSize(m.RawBytes))
	if m.Note != "" {
		a.out.human("  备注：%s", m.Note)
	}
	for _, vol := range m.Volumes {
		where := vol.Path
		if vol.Role == "addon" {
			where = "附加数据库 " + vol.Addon
		}
		a.out.human("  数据：%s（%s，%d 个文件和目录）校验 %s…", where, humanSize(vol.Bytes), vol.Entries, short(vol.SHA256))
	}
	if p := d.RestorePlan; p != nil {
		state := "文件齐全"
		if !p.Intact {
			state = "文件不齐或大小不对（BKP-05），不能用来恢复"
		} else if p.Verified {
			state = "文件齐全，校验和全部核对过"
		}
		a.out.human("  状态：%s", state)
		if !p.VersionAvailable {
			a.out.human("  注意：这份备份时的程序版本已不在本机（BKP-09），恢复要加 --allow-current-version")
		}
	}
	if !d.Installed {
		a.out.human("  应用已不在这台机器上，恢复要先重新安装一个同名应用")
	}
	return exitOK
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// resolveBackupID turns "latest" into the id of the app's newest backup.
func (a *app) resolveBackupID(ctx context.Context, b client.BackupAPI, app, id string) (string, error) {
	if id != "latest" {
		return id, nil
	}
	l, err := b.AppBackups(ctx, app)
	if err != nil {
		return "", err
	}
	if len(l.Backups) == 0 {
		return "", &client.Error{Diag: client.Diagnosis{Stage: "backup", Code: "BKP-04", Message: "应用 " + app + " 还没有备份", Hint: "先做一份：acornfox backup create " + app}}
	}
	return l.Backups[0].ID, nil
}

func (a *app) backupRestore(ctx context.Context, args []string) int {
	fs, v := a.backupFlags("restore")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	res, id, rerr := a.appAndID("restore", pos)
	if rerr != nil {
		return a.out.usageError("%s", rerr.Error())
	}
	api, b, code := a.backupAPI(ctx, res)
	if b == nil {
		return code
	}
	defer api.Close()
	id, ierr := a.resolveBackupID(ctx, b, res.app, id)
	if ierr != nil {
		return a.out.fail(ierr)
	}
	ans, err := b.RestoreBackup(ctx, res.app, id, *v.yes, *v.plan, *v.allow)
	if err != nil {
		return a.out.fail(err)
	}
	if ans.Plan != nil {
		if a.out.json {
			a.emitRaw(ans.Raw, nil)
			return exitOK
		}
		p := ans.Plan
		a.out.human("只是看看，什么都没动。")
		a.out.human("%s。", strings.TrimSuffix(p.Say, "。"))
		if !p.Intact {
			a.out.human("✗ 这份备份的文件不齐或校验不对（BKP-05），不能用来恢复。")
		} else {
			a.out.human("✓ 备份文件齐全，校验和核对过。")
		}
		if !p.VersionAvailable {
			a.out.human("! 备份时的程序版本（%s）已不在本机（BKP-09）；要在现在的版本（%s）上用这份数据，加 --allow-current-version。", p.VersionThen, p.VersionNow)
		}
		if !p.Enough {
			a.out.human("✗ 磁盘空间不够（BKP-01）：恢复前备份约需 %s，恢复约需 %s 的数据空间。", humanSize(p.BackupNeedBytes), humanSize(p.DockerNeedBytes))
		}
		a.out.human("这一步会覆盖现在的数据，要用户同意。同意后去掉 --plan、加 --yes 再运行。")
		return exitOK
	}
	return a.followTask(ctx, b, ans, *v.noWait, *v.timeout, "恢复")
}

func (a *app) backupRm(ctx context.Context, args []string) int {
	fs, v := a.backupFlags("rm")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	res, id, rerr := a.appAndID("rm", pos)
	if rerr != nil {
		return a.out.usageError("%s", rerr.Error())
	}
	if !*v.yes {
		msg := fmt.Sprintf("会删掉应用 %s 的备份 %s 的全部文件，删了找不回来。这一步要用户同意；同意后加 --yes 再运行。", res.app, id)
		if a.out.json {
			a.out.emitJSON(map[string]any{"plan": true, "deleted": false, "say": msg})
		} else {
			a.out.human("只是看看，什么都没删。%s", msg)
		}
		return exitOK
	}
	api, b, code := a.backupAPI(ctx, res)
	if b == nil {
		return code
	}
	defer api.Close()
	id, ierr := a.resolveBackupID(ctx, b, res.app, id)
	if ierr != nil {
		return a.out.fail(ierr)
	}
	d, err := b.DeleteBackup(ctx, res.app, id, true)
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(d.Raw, nil)
		return exitOK
	}
	if d.Deleted {
		a.out.human("已删掉备份 %s。", id)
	} else {
		a.out.human("备份 %s 本来就不在（没有删任何东西）。", id)
	}
	return exitOK
}

func (a *app) backupTaskCmd(ctx context.Context, args []string) int {
	fs, v := a.backupFlags("task")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) != 1 {
		return a.out.usageError("用法：acornfox backup task 任务编号")
	}
	t, tname, terr := a.resolveTarget()
	if terr != nil {
		return a.out.usageError("%s", terr.Error())
	}
	_ = v
	api, b, code := a.backupAPI(ctx, resolution{target: t, targetName: tname})
	if b == nil {
		return code
	}
	defer api.Close()
	task, err := b.BackupTask(ctx, pos[0])
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(task.Raw, nil)
		return exitOK
	}
	op := map[string]string{"backup": "备份", "restore": "恢复"}[task.Op]
	a.out.human("任务 %s：应用 %s 的%s，%s", task.ID, task.App, op, map[string]string{"running": "进行中", "succeeded": "已完成", "failed": "没成功"}[task.Status])
	a.printSteps(task, map[string]string{})
	if task.Done() {
		return a.reportBackupTask(task, op)
	}
	return exitOK
}
