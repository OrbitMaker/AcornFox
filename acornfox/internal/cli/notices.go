package cli

// 命令行发现服务器比自己新时提醒用户更新（故障编号 CLI-UPDATE-01）。
//
// 做什么：连上服务器后，问一次服务器的版本（GET /v1/status 的 version）；服务器的 x.y.z 比这个命令行的高，就提醒
// 更新命令行和 Skill。不打断命令：人看的输出写到标准错误，一行标题加几行命令；--json 时放进结果的 notices 字段
// （emitJSON、emitDiagnosisJSON 都带），标准输出的其余内容不变。
// 频率：每台电脑每天最多提醒一次；问版本本身一小时最多一次（记在配置目录的 update-notice.json，只有时间、日期和版本号）。
// 不提醒的情况：连的是这台电脑自己的服务（unix 套接字：命令行和服务是同一个程序）、服务器不报版本（旧服务器）、
// 版本号看不懂、问版本出错或超过 4 秒（不拖慢命令）、设了环境变量 ACORNFOX_NO_UPDATE_NOTICE。
// 副作用：多一次只读请求；写 update-notice.json。读不到、写不进配置目录都不报错，只是少一次记录。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

// CodeCLIUpdate is the fault code of "the server is newer than this command line".
const CodeCLIUpdate = "CLI-UPDATE-01"

const (
	noticeStampFile  = "update-notice.json"
	noticeCheckEvery = time.Hour
	noticeAskTimeout = 4 * time.Second
	// NoUpdateNoticeEnv switches the notice off.
	NoUpdateNoticeEnv = "ACORNFOX_NO_UPDATE_NOTICE"

	cliUpdateGitee  = "curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash -s -- cli"
	cliUpdateGitHub = "curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | bash -s -- cli"
)

// notice is one non-fatal message that travels with a command's result.
type notice struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Hint     string   `json:"hint,omitempty"`
	Commands []string `json:"commands,omitempty"`
}

// noticeStamp is update-notice.json.
type noticeStamp struct {
	CheckedAt     time.Time `json:"checked_at"`
	NotifiedDay   string    `json:"notified_day,omitempty"` // local date of the last notice
	ServerVersion string    `json:"server_version,omitempty"`
}

func noticeStampPath(configDir string) string {
	dir, _, err := configPaths(configDir)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, noticeStampFile)
}

func loadNoticeStamp(configDir string) noticeStamp {
	var st noticeStamp
	if p := noticeStampPath(configDir); p != "" {
		if data, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(data, &st)
		}
	}
	return st
}

func saveNoticeStamp(configDir string, st noticeStamp) {
	p := noticeStampPath(configDir)
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(st)
	if err != nil {
		return
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, p)
}

// cliUpdateNotice builds the notice for a server at serverVer.
func cliUpdateNotice(targetName, serverVer string) notice {
	who := "服务器"
	if targetName != "" {
		who = "服务器 " + targetName + " "
	}
	n := notice{
		Code:    CodeCLIUpdate,
		Message: who + "上的 AcornFox 是 " + serverVer + " 版，这台电脑上的命令行是 " + Version + " 版，比服务器旧",
	}
	if runtime.GOOS == "windows" {
		n.Commands = []string{"acornfox windows upgrade", "acornfox skill install"}
		n.Hint = "更新命令行：acornfox windows upgrade（或重新运行安装命令，末尾加 -Action upgrade）；再更新 AI 工作台里的 Skill：acornfox skill install"
		return n
	}
	n.Commands = []string{cliUpdateGitee, cliUpdateGitHub, "acornfox skill install"}
	n.Hint = "更新命令行：在这台电脑上运行 " + cliUpdateGitee + "（海外用 " + cliUpdateGitHub + "）；再更新 AI 工作台里的 Skill：acornfox skill install"
	return n
}

// printNotice writes a notice for people to standard error.
func (o *outputWriter) printNotice(n notice) {
	o.humanErr("提醒：%s（%s，运行 acornfox faults %s 看详情）", n.Message, n.Code, n.Code)
	if runtime.GOOS == "windows" {
		o.humanErr("  更新命令行：acornfox windows upgrade（或重新运行安装命令，末尾加 -Action upgrade）")
		o.humanErr("  再更新 AI 工作台里的 Skill：acornfox skill install")
	} else {
		o.humanErr("  更新命令行（国内）：%s", cliUpdateGitee)
		o.humanErr("  更新命令行（海外）：%s", cliUpdateGitHub)
		o.humanErr("  再更新 AI 工作台里的 Skill：acornfox skill install")
	}
	o.humanErr("  这条提醒每天最多出现一次；不想看到就设环境变量 %s=1", NoUpdateNoticeEnv)
}

// addNotice records a notice: in --json mode it joins the result (notices),
// otherwise it is written to standard error at once.
func (o *outputWriter) addNotice(n notice) {
	if o.json {
		o.notices = append(o.notices, n)
		return
	}
	o.printNotice(n)
}

// checkServerNewer is called once a connection to a server is open. See the
// file comment for when it says nothing.
func (a *app) checkServerNewer(ctx context.Context, t client.Target, api client.API) {
	if a.noticeAsked {
		return
	}
	a.noticeAsked = true
	if a.getenv != nil && a.getenv(NoUpdateNoticeEnv) != "" {
		return
	}
	if strings.HasPrefix(t.URL, "unix:") {
		return
	}
	now := time.Now()
	if a.now != nil {
		now = a.now()
	}
	today := now.Format("2006-01-02")
	st := loadNoticeStamp(a.configDir)
	if st.NotifiedDay == today {
		return
	}
	if !st.CheckedAt.IsZero() && now.After(st.CheckedAt) && now.Sub(st.CheckedAt) < noticeCheckEvery {
		return
	}
	askCtx, cancel := context.WithTimeout(ctx, noticeAskTimeout)
	status, err := api.Status(askCtx)
	cancel()
	st.CheckedAt = now
	if err == nil && versionNewer(status.Version, Version) {
		st.NotifiedDay = today
		st.ServerVersion = status.Version
		saveNoticeStamp(a.configDir, st)
		a.out.addNotice(cliUpdateNotice(t.Name, status.Version))
		return
	}
	saveNoticeStamp(a.configDir, st)
}
