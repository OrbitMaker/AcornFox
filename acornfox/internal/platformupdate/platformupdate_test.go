package platformupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 假的发行版服务：Gitee 的列表没有 html_url，GitHub 的有。
const giteeList = `[
 {"id":3,"tag_name":"v0.7.1-rc.1","prerelease":true,"name":"v0.7.1-rc.1"},
 {"id":2,"tag_name":"v0.7.0","prerelease":false,"name":"v0.7.0"},
 {"id":1,"tag_name":"v0.6.3","prerelease":false,"name":"v0.6.3"}
]`

const githubList = `[
 {"tag_name":"v0.7.2","draft":true,"prerelease":false,"html_url":"https://github.com/o/r/releases/tag/v0.7.2"},
 {"tag_name":"v0.7.1","draft":false,"prerelease":false,"html_url":"https://github.com/o/r/releases/tag/v0.7.1"},
 {"tag_name":"v0.8.0-rc.1","draft":false,"prerelease":true,"html_url":"https://github.com/o/r/releases/tag/v0.8.0-rc.1"},
 {"tag_name":"v0.7.0","draft":false,"prerelease":false,"html_url":"https://github.com/o/r/releases/tag/v0.7.0"}
]`

// serve 起一个返回固定内容的假服务，hits 记被问了几次。
func serve(t *testing.T, code int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "AcornFox/") {
			t.Errorf("User-Agent 应以 AcornFox/ 开头，现在是 %q", r.Header.Get("User-Agent"))
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func newChecker(t *testing.T, cur string, china bool, gitee, github string, dir string) *Checker {
	t.Helper()
	return New(Config{Current: cur, DataDir: dir, China: func() bool { return china }, GiteeURL: gitee, GitHubURL: github})
}

func TestNewerVersionAvailable(t *testing.T) {
	gitee, _ := serve(t, 200, giteeList)
	github, _ := serve(t, 200, githubList)
	c := newChecker(t, "0.7.0", false, gitee.URL, github.URL, "")
	if st := c.Status(); st.Available || st.CheckedAt != "" || st.Latest != "" || !st.Enabled {
		t.Fatalf("没查过时不该有结果：%+v", st)
	}
	st := c.CheckNow(context.Background())
	if !st.OK || st.Latest != "0.7.1" || !st.Available || st.Source != SourceGitHub || st.Code != CodeAvailable {
		t.Fatalf("境外先 GitHub，草稿 0.7.2 和预发布 0.8.0-rc.1 要忽略，得到 0.7.1：%+v", st)
	}
	if st.NotesURL != "https://github.com/o/r/releases/tag/v0.7.1" || st.CheckedAt == "" || st.LastOKAt == "" {
		t.Fatalf("发行说明地址或时间不对：%+v", st)
	}
	if !strings.Contains(st.UpgradeCommand, "github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh") || !strings.Contains(st.UpgradeCommand, "sudo bash -s -- upgrade") {
		t.Fatalf("境外 Linux 的升级命令不对：%q", st.UpgradeCommand)
	}
}

func TestAlreadyLatest(t *testing.T) {
	gitee, _ := serve(t, 200, giteeList)
	github, _ := serve(t, 200, githubList)
	c := newChecker(t, "0.7.1", false, gitee.URL, github.URL, "")
	st := c.CheckNow(context.Background())
	if !st.OK || st.Available || st.Latest != "0.7.1" || st.Code != "" {
		t.Fatalf("已是最新时不该提醒：%+v", st)
	}
	// 当前版本比线上还新（自己编的）：也不提醒。
	c = newChecker(t, "0.9.0", false, gitee.URL, github.URL, "")
	if st := c.CheckNow(context.Background()); st.Available {
		t.Fatalf("比线上新不该提醒：%+v", st)
	}
}

func TestPrereleaseIgnoredGitee(t *testing.T) {
	gitee, _ := serve(t, 200, giteeList) // 里面 v0.7.1-rc.1 是预发布，正式版最高是 0.7.0
	github, ghHits := serve(t, 200, githubList)
	c := newChecker(t, "0.6.3", true, gitee.URL, github.URL, "")
	st := c.CheckNow(context.Background())
	if st.Latest != "0.7.0" || st.Source != SourceGitee || !st.Available {
		t.Fatalf("国内先 Gitee，预发布要忽略：%+v", st)
	}
	if ghHits.Load() != 0 {
		t.Fatalf("Gitee 成功就不该再问 GitHub，问了 %d 次", ghHits.Load())
	}
	if st.NotesURL != "https://gitee.com/VIP13390/AcornFox/releases/tag/v0.7.0" {
		t.Fatalf("Gitee 没有 html_url，要自己拼发行说明地址：%q", st.NotesURL)
	}
	if !strings.Contains(st.UpgradeCommand, "gitee.com/VIP13390/AcornFox/raw/main/get.sh") {
		t.Fatalf("国内的升级命令要用 Gitee：%q", st.UpgradeCommand)
	}
}

func TestPrereleaseTagWithoutFlagIgnored(t *testing.T) {
	// 标签带“-”但 prerelease 标志没打：也当预发布。
	gitee, _ := serve(t, 200, `[{"tag_name":"v0.9.0-rc.2","prerelease":false},{"tag_name":"v0.7.0","prerelease":false}]`)
	c := newChecker(t, "0.6.3", true, gitee.URL, "http://127.0.0.1:1/", "")
	if st := c.CheckNow(context.Background()); st.Latest != "0.7.0" {
		t.Fatalf("带 - 的标签要忽略：%+v", st)
	}
}

func TestFallbackToOtherSource(t *testing.T) {
	bad, badHits := serve(t, 500, "boom")
	github, _ := serve(t, 200, githubList)
	// 国内：Gitee 坏了，换 GitHub。
	c := newChecker(t, "0.7.0", true, bad.URL, github.URL, "")
	st := c.CheckNow(context.Background())
	if !st.OK || st.Source != SourceGitHub || st.Latest != "0.7.1" || badHits.Load() != 1 {
		t.Fatalf("国内 Gitee 失败要换 GitHub：%+v（Gitee 被问 %d 次）", st, badHits.Load())
	}
	// 境外：GitHub 连不上，换 Gitee。
	gitee, _ := serve(t, 200, giteeList)
	c = newChecker(t, "0.6.3", false, gitee.URL, "http://127.0.0.1:1/", "")
	if st := c.CheckNow(context.Background()); !st.OK || st.Source != SourceGitee || st.Latest != "0.7.0" {
		t.Fatalf("境外 GitHub 失败要换 Gitee：%+v", st)
	}
}

func TestSingleReleaseObject(t *testing.T) {
	one, _ := serve(t, 200, `{"tag_name":"v0.7.3","prerelease":false,"html_url":"https://example.com/r"}`)
	c := newChecker(t, "0.7.0", false, "http://127.0.0.1:1/", one.URL, "")
	if st := c.CheckNow(context.Background()); st.Latest != "0.7.3" || st.NotesURL != "https://example.com/r" {
		t.Fatalf("单个发行版对象也要认：%+v", st)
	}
}

func TestBothFail(t *testing.T) {
	bad1, _ := serve(t, 404, "")
	bad2, _ := serve(t, 200, "<html>不是 JSON</html>")
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	clock := now
	c := New(Config{Current: "0.7.0", GiteeURL: bad1.URL, GitHubURL: bad2.URL, Now: func() time.Time { return clock }})
	st := c.CheckNow(context.Background())
	if st.OK || st.Failures != 1 || st.Error == "" || st.Available || st.Unreachable || st.Code != "" {
		t.Fatalf("都失败：只记失败、不报不可达（才一次）：%+v", st)
	}
	if !strings.Contains(st.Error, "gitee") || !strings.Contains(st.Error, "github") {
		t.Fatalf("失败原因要写两边：%q", st.Error)
	}
	// 3 天以后还在失败：算一直查不到。
	clock = now.Add(StaleAfter + time.Hour)
	c.mu.Lock()
	c.st.CheckedAt = time.Time{}
	c.mu.Unlock()
	st = c.CheckNow(context.Background())
	if !st.Unreachable || st.Code != CodeUnreachable || st.Failures != 2 {
		t.Fatalf("失败超过 3 天要标一直查不到：%+v", st)
	}
}

func TestFailureKeepsLastGoodResult(t *testing.T) {
	gitee, _ := serve(t, 200, giteeList)
	var fail atomic.Bool
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		_, _ = w.Write([]byte(githubList))
	}))
	defer flaky.Close()
	clock := time.Now()
	c := New(Config{Current: "0.7.0", GiteeURL: gitee.URL, GitHubURL: flaky.URL, Now: func() time.Time { return clock }})
	c.CheckNow(context.Background())
	fail.Store(true)
	clock = clock.Add(time.Minute)
	// GitHub 坏了会换 Gitee，Gitee 给的是 0.7.0，所以这次是成功的“已是最新”。
	st := c.CheckNow(context.Background())
	if !st.OK || st.Latest != "0.7.0" || st.Available {
		t.Fatalf("换到 Gitee 以后以 Gitee 为准：%+v", st)
	}
	// 两边都坏：保留上次的结果，只记失败。
	c2 := New(Config{Current: "0.7.0", GiteeURL: "http://127.0.0.1:1/", GitHubURL: flaky.URL, Now: func() time.Time { return clock }})
	fail.Store(false)
	c2.CheckNow(context.Background())
	fail.Store(true)
	clock = clock.Add(time.Minute)
	st = c2.CheckNow(context.Background())
	if st.OK || st.Latest != "0.7.1" || !st.Available || st.Failures != 1 {
		t.Fatalf("失败时要保留上次查到的 0.7.1：%+v", st)
	}
}

func TestSavedAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	github, _ := serve(t, 200, githubList)
	c := newChecker(t, "0.7.0", false, "http://127.0.0.1:1/", github.URL, dir)
	c.CheckNow(context.Background())
	if _, err := os.Stat(filepath.Join(dir, "platform-update.json")); err != nil {
		t.Fatalf("要写状态文件：%v", err)
	}
	// 重启：新对象没联网就读到上次的结果。
	c2 := newChecker(t, "0.7.0", false, "http://127.0.0.1:1/", "http://127.0.0.1:1/", dir)
	st := c2.Status()
	if st.Latest != "0.7.1" || !st.Available || !st.OK || st.CheckedAt == "" || st.Source != SourceGitHub {
		t.Fatalf("重启后要读到上次的结果：%+v", st)
	}
	// 升级到 0.7.1 以后重启：同一份结果变成“已是最新”。
	c3 := newChecker(t, "0.7.1", false, "http://127.0.0.1:1/", "http://127.0.0.1:1/", dir)
	if st := c3.Status(); st.Available || st.Latest != "0.7.1" {
		t.Fatalf("升级后不该再提醒：%+v", st)
	}
	// 状态文件坏了：当作没有，不报错。
	if err := os.WriteFile(filepath.Join(dir, "platform-update.json"), []byte("{坏"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := newChecker(t, "0.7.0", false, "", "", dir).Status(); st.Latest != "" || st.Available {
		t.Fatalf("坏文件要当作没有：%+v", st)
	}
}

func TestManualCheckIsIdempotent(t *testing.T) {
	github, hits := serve(t, 200, githubList)
	c := newChecker(t, "0.7.0", false, "http://127.0.0.1:1/", github.URL, "")
	a := c.CheckNow(context.Background())
	b := c.CheckNow(context.Background())
	if hits.Load() != 1 || a.CheckedAt != b.CheckedAt || b.Latest != "0.7.1" {
		t.Fatalf("几秒内再点“现在查一次”不该重复请求（请求 %d 次）：%+v %+v", hits.Load(), a, b)
	}
}

func TestDisabled(t *testing.T) {
	github, hits := serve(t, 200, githubList)
	var cfg Config
	cfg.FromEnv(func(k string) string {
		if k == EnvCheck {
			return "off"
		}
		return ""
	})
	cfg.Current, cfg.GitHubURL = "0.7.0", github.URL
	c := New(cfg)
	st := c.CheckNow(context.Background())
	if st.Enabled || st.Available || st.Latest != "" || hits.Load() != 0 {
		t.Fatalf("关掉以后不联网、不提醒：%+v（请求 %d 次）", st, hits.Load())
	}
	done := make(chan struct{})
	go func() { c.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("关掉以后 Run 要马上返回")
	}
}

func TestRunChecksAfterDelayAndStops(t *testing.T) {
	github, hits := serve(t, 200, githubList)
	c := New(Config{Current: "0.7.0", GitHubURL: github.URL, GiteeURL: "http://127.0.0.1:1/", FirstDelay: 20 * time.Millisecond, Interval: time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hits.Load() != 1 {
		t.Fatalf("启动后稍等一会儿要查一次，实际 %d 次", hits.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ctx 结束后 Run 要返回")
	}
	if st := c.Status(); st.Latest != "0.7.1" {
		t.Fatalf("Run 查到的结果要记下：%+v", st)
	}
}

func TestConfigFromEnv(t *testing.T) {
	env := map[string]string{EnvGiteeURL: "http://g/", EnvGitHubURL: "http://h/", EnvInterval: "2h"}
	var cfg Config
	cfg.FromEnv(func(k string) string { return env[k] })
	if cfg.GiteeURL != "http://g/" || cfg.GitHubURL != "http://h/" || cfg.Interval != 2*time.Hour || cfg.Disabled {
		t.Fatalf("环境变量没读对：%+v", cfg)
	}
	env[EnvInterval] = "5s" // 小于 1 分钟：不采用
	var c2 Config
	c2.FromEnv(func(k string) string { return env[k] })
	if c2.Interval != 0 {
		t.Fatalf("间隔不能小于 1 分钟：%v", c2.Interval)
	}
	for _, v := range []string{"off", "OFF", "0", "false", "no", "disabled"} {
		var c3 Config
		c3.FromEnv(func(k string) string {
			if k == EnvCheck {
				return v
			}
			return ""
		})
		if !c3.Disabled {
			t.Errorf("%s 应该是关掉", v)
		}
	}
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"0.7.1", "0.7.0", true},
		{"0.7.0", "0.7.1", false},
		{"0.7.0", "0.7.0", false},
		{"v0.8.0", "0.7.9", true},
		{"0.10.0", "0.9.0", true},
		{"1.0.0", "0.99.99", true},
		{"0.7.1", "0.7.1-rc.1", true},
		{"0.7.1-rc.1", "0.7.1", false},
		{"0.7.0", "0.6.0-dev", true},
		{"0.7.0", "0.7.0-dev", true},
		{"0.7.1", "dev", false},
		{"", "0.7.0", false},
	} {
		if got := Newer(tc.a, tc.b); got != tc.want {
			t.Errorf("Newer(%q,%q)=%v，应该是 %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestUpgradeCommandPerHost(t *testing.T) {
	lin, where := UpgradeCommand(HostLinux, true)
	if lin != "curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash -s -- upgrade" || where == "" {
		t.Errorf("Linux 国内：%q", lin)
	}
	lin, _ = UpgradeCommand(HostLinux, false)
	if lin != "curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | sudo bash -s -- upgrade" {
		t.Errorf("Linux 境外：%q", lin)
	}
	mac, where := UpgradeCommand(HostMac, true)
	if mac != "curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash -s -- upgrade" || strings.Contains(mac, "sudo") || !strings.Contains(where, "不要加 sudo") {
		t.Errorf("Mac 不加 sudo：%q / %q", mac, where)
	}
	win, where := UpgradeCommand(HostWindows, false)
	if !strings.HasPrefix(win, "powershell -NoProfile -ExecutionPolicy Bypass -Command ") || !strings.HasSuffix(win, `))) -Action upgrade"`) ||
		!strings.Contains(win, "https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.ps1") || !strings.Contains(where, "PowerShell") {
		t.Errorf("Windows：%q / %q", win, where)
	}
	if win, _ := UpgradeCommand(HostWindows, true); !strings.Contains(win, "https://gitee.com/VIP13390/AcornFox/raw/main/get.ps1") {
		t.Errorf("Windows 国内要用 Gitee：%q", win)
	}
}

func TestDetectHostKind(t *testing.T) {
	none := func(string) string { return "" }
	if k := DetectHostKind(false, "Linux version 6.1", none); k != HostLinux {
		t.Errorf("普通 Linux：%s", k)
	}
	if k := DetectHostKind(true, "Linux version 5.15.153.1-microsoft-standard-WSL2", none); k != HostWindows {
		t.Errorf("WSL：%s", k)
	}
	if k := DetectHostKind(true, "Linux version 6.18.56 (kata)", none); k != HostMac {
		t.Errorf("Mac 虚拟机：%s", k)
	}
	if k := DetectHostKind(true, "", func(k string) string {
		if k == "WSL_DISTRO_NAME" {
			return "AcornFox"
		}
		return ""
	}); k != HostWindows {
		t.Errorf("有 WSL_DISTRO_NAME：%s", k)
	}
	if k := DetectHostKind(false, "", func(k string) string {
		if k == EnvHostKind {
			return "mac"
		}
		return ""
	}); k != HostMac {
		t.Errorf("环境变量强制：%s", k)
	}
}
