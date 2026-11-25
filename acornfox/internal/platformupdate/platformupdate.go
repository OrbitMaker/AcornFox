// Package platformupdate tells the user that a newer AcornFox is out (W12-3).
//
// 做什么：acornfox 服务每 6 小时查一次线上最新的正式版本号（启动后稍等一会儿先查一次），把结果记在内存和数据目录里的
// platform-update.json（重启后不丢上次结果）。GET /v1/status 带上 latest_version 和 platform_update，控制台“设置 › 这台机器”、
// 铃铛、acornfox update、acornfox doctor 都读同一份。只提醒，不升级：平台升级要在服务器上运行一条命令，由用户同意后做。
//
// 去哪里查（只读发行版列表，忽略带“-”的预发布版和草稿）：
//   - 这台机器在中国大陆：先 Gitee（VIP13390/AcornFox），连不上换 GitHub（OrbitMaker/AcornFox）；
//   - 其他地区：先 GitHub，连不上换 Gitee。
//     地域用平台已有的判断（Config.China，接的是 downloadSources 的 China）。
//
// 网络不通只记失败，不报错吵人：状态里有 failures、error，连续失败超过 StaleAfter（3 天）才算“一直查不到”（PLAT-UPD-02）。
//
// 环境变量（写进服务的环境里；都可以不设）：
//
//	ACORNFOX_UPDATE_CHECK=off        整个关掉：不联网、状态里 enabled=false（也认 0、false、no、disabled）
//	ACORNFOX_UPDATE_GITEE_URL=…      换掉 Gitee 的发行版列表地址（测试用；返回 JSON 数组或单个发行版对象）
//	ACORNFOX_UPDATE_GITHUB_URL=…     换掉 GitHub 的发行版列表地址（同上）
//	ACORNFOX_UPDATE_INTERVAL=1h      查询间隔（Go 的时间写法，不小于 1 分钟；默认 6h）
//	ACORNFOX_HOST_KIND=windows       强制指定这台机器是 linux、windows（WSL 里的）还是 mac（苹果 container 的虚拟机里的），
//	                                 决定给用户哪一条升级命令；不设就自己判断（见 DetectHostKind）
//
// 副作用：每次查询向 Gitee 或 GitHub 发一到两个只读的 HTTPS 请求（不带任何本机信息，User-Agent 只写程序名和版本）；
// 写数据目录里的 platform-update.json（读不到、写不进都不报错，只是重启后少一次记录）。
package platformupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// EnvCheck switches the whole check off ("off", "0", "false", "no", "disabled").
	EnvCheck = "ACORNFOX_UPDATE_CHECK"
	// EnvGiteeURL and EnvGitHubURL replace the release list addresses (tests).
	EnvGiteeURL  = "ACORNFOX_UPDATE_GITEE_URL"
	EnvGitHubURL = "ACORNFOX_UPDATE_GITHUB_URL"
	// EnvInterval changes the check interval (Go duration, at least a minute).
	EnvInterval = "ACORNFOX_UPDATE_INTERVAL"
	// EnvHostKind forces the host kind (linux, windows, mac).
	EnvHostKind = "ACORNFOX_HOST_KIND"

	// DefaultInterval is how often the server asks.
	DefaultInterval = 6 * time.Hour
	// DefaultFirstDelay is how long after start the first check waits.
	DefaultFirstDelay = 90 * time.Second
	// StaleAfter: failing this long without one success is "keeps failing".
	StaleAfter = 3 * 24 * time.Hour
	// manualGap: a second "check now" within this time answers with the
	// result just obtained (the manual route is idempotent and cheap).
	manualGap = 10 * time.Second

	// CodeAvailable is the fault code of "a newer AcornFox is out" (a hint, not a fault).
	CodeAvailable = "PLAT-UPD-01"
	// CodeUnreachable is the fault code of "the latest version cannot be read for days".
	CodeUnreachable = "PLAT-UPD-02"

	stateFile = "platform-update.json"

	// The release lists. The public repositories are fixed (AGENTS: two public repos).
	DefaultGiteeURL  = "https://gitee.com/api/v5/repos/VIP13390/AcornFox/releases?page=1&per_page=30&direction=desc"
	DefaultGitHubURL = "https://api.github.com/repos/OrbitMaker/AcornFox/releases?per_page=30"
	giteeTagPage     = "https://gitee.com/VIP13390/AcornFox/releases/tag/"
	githubTagPage    = "https://github.com/OrbitMaker/AcornFox/releases/tag/"

	// Source names in the status.
	SourceGitee  = "gitee"
	SourceGitHub = "github"

	maxBody = 2 << 20
)

// Host kinds: where this server runs, which decides the upgrade command.
const (
	HostLinux   = "linux"
	HostWindows = "windows"
	HostMac     = "mac"
)

// Status is what GET /v1/status (platform_update) and GET /v1/platform/update answer.
type Status struct {
	// Enabled is false when ACORNFOX_UPDATE_CHECK switched the check off.
	Enabled bool `json:"enabled"`
	// Current is the version this server runs.
	Current string `json:"current"`
	// Latest is the newest stable version seen at the last successful check ("" before one).
	Latest string `json:"latest,omitempty"`
	// Available: Latest is newer than Current.
	Available bool `json:"available"`
	// CheckedAt is when the last check ran (success or not), RFC 3339 UTC.
	CheckedAt string `json:"checked_at,omitempty"`
	// OK: the last check read a version.
	OK bool `json:"ok"`
	// Source is where Latest was read: gitee or github.
	Source string `json:"source,omitempty"`
	// NotesURL is the release page (the release notes).
	NotesURL string `json:"notes_url,omitempty"`
	// UpgradeCommand is the one command to upgrade this machine; UpgradeWhere says where to run it.
	UpgradeCommand string `json:"upgrade_command,omitempty"`
	UpgradeWhere   string `json:"upgrade_where,omitempty"`
	// HostKind is linux, windows or mac (see the package comment).
	HostKind string `json:"host_kind"`
	// LastOKAt is the last successful check.
	LastOKAt string `json:"last_ok_at,omitempty"`
	// Failures counts the checks that failed since the last success.
	Failures int `json:"failures,omitempty"`
	// Unreachable: failing for longer than StaleAfter (PLAT-UPD-02).
	Unreachable bool `json:"unreachable,omitempty"`
	// Error is the short reason of the last failure.
	Error string `json:"error,omitempty"`
	// Code is PLAT-UPD-01 while a newer version is out, PLAT-UPD-02 while Unreachable.
	Code string `json:"code,omitempty"`
}

// Config builds a Checker.
type Config struct {
	// Current is this program's version (cli.Version).
	Current string
	// DataDir holds platform-update.json; "" keeps the result in memory only.
	DataDir string
	// China says whether this machine counts as in mainland China (nil: no).
	China func() bool
	// HostKind is linux, windows or mac ("" is linux); decides the upgrade command.
	HostKind string
	// Disabled switches the check off.
	Disabled bool
	// GiteeURL, GitHubURL, Interval, FirstDelay default to the constants above.
	GiteeURL, GitHubURL string
	Interval            time.Duration
	FirstDelay          time.Duration
	// Client is the HTTP client (nil: a client with an 8 second timeout).
	Client *http.Client
	// Now is the clock (nil: time.Now).
	Now func() time.Time
	// Logf logs one line (nil: silent).
	Logf func(format string, args ...any)
}

// FromEnv fills what the environment says into c (see the package comment).
func (c *Config) FromEnv(getenv func(string) string) {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch strings.ToLower(strings.TrimSpace(getenv(EnvCheck))) {
	case "off", "0", "false", "no", "disabled":
		c.Disabled = true
	}
	if v := strings.TrimSpace(getenv(EnvGiteeURL)); v != "" {
		c.GiteeURL = v
	}
	if v := strings.TrimSpace(getenv(EnvGitHubURL)); v != "" {
		c.GitHubURL = v
	}
	if v := strings.TrimSpace(getenv(EnvInterval)); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= time.Minute {
			c.Interval = d
		}
	}
}

// Checker asks for the newest stable version now and then and remembers the answer.
type Checker struct {
	cfg Config

	mu   sync.Mutex // guards st and serializes the checks
	st   saved
	busy chan struct{}
}

// saved is platform-update.json.
type saved struct {
	CheckedAt   time.Time `json:"checked_at,omitempty"`
	OK          bool      `json:"ok"`
	Source      string    `json:"source,omitempty"`
	Latest      string    `json:"latest,omitempty"`
	NotesURL    string    `json:"notes_url,omitempty"`
	LastOKAt    time.Time `json:"last_ok_at,omitempty"`
	FirstFailAt time.Time `json:"first_fail_at,omitempty"`
	Failures    int       `json:"failures,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// New builds a Checker and reads the last saved result.
func New(cfg Config) *Checker {
	if cfg.GiteeURL == "" {
		cfg.GiteeURL = DefaultGiteeURL
	}
	if cfg.GitHubURL == "" {
		cfg.GitHubURL = DefaultGitHubURL
	}
	if cfg.Interval < time.Minute {
		cfg.Interval = DefaultInterval
	}
	if cfg.FirstDelay <= 0 {
		cfg.FirstDelay = DefaultFirstDelay
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 8 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.HostKind == "" {
		cfg.HostKind = HostLinux
	}
	c := &Checker{cfg: cfg, busy: make(chan struct{}, 1)}
	c.load()
	return c
}

func (c *Checker) logf(format string, args ...any) {
	if c.cfg.Logf != nil {
		c.cfg.Logf(format, args...)
	}
}

func (c *Checker) statePath() string {
	if c.cfg.DataDir == "" {
		return ""
	}
	return filepath.Join(c.cfg.DataDir, stateFile)
}

func (c *Checker) load() {
	p := c.statePath()
	if p == "" {
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var s saved
	if json.Unmarshal(data, &s) == nil {
		c.st = s
	}
}

func (c *Checker) save() {
	p := c.statePath()
	if p == "" {
		return
	}
	data, err := json.Marshal(c.st)
	if err != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, data, 0o644) != nil {
		return
	}
	if os.Rename(tmp, p) != nil {
		_ = os.Remove(tmp)
	}
}

// Status is the answer as of now. It never connects anywhere.
func (c *Checker) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statusLocked()
}

func (c *Checker) statusLocked() Status {
	china := c.cfg.China != nil && c.cfg.China()
	cmd, where := UpgradeCommand(c.cfg.HostKind, china)
	s := Status{
		Enabled: !c.cfg.Disabled, Current: c.cfg.Current, HostKind: c.cfg.HostKind,
		UpgradeCommand: cmd, UpgradeWhere: where,
	}
	if c.cfg.Disabled {
		return s
	}
	st := c.st
	if !st.CheckedAt.IsZero() {
		s.CheckedAt = st.CheckedAt.UTC().Format(time.RFC3339)
	}
	s.OK = st.OK
	if !st.LastOKAt.IsZero() {
		s.LastOKAt = st.LastOKAt.UTC().Format(time.RFC3339)
	}
	s.Failures = st.Failures
	s.Error = st.Error
	if st.Latest != "" {
		s.Latest, s.Source, s.NotesURL = st.Latest, st.Source, st.NotesURL
		s.Available = Newer(st.Latest, c.cfg.Current)
	}
	if s.Available {
		s.Code = CodeAvailable
	}
	if st.Failures > 0 {
		if since := st.FirstFailAt; !since.IsZero() && c.cfg.Now().Sub(since) > StaleAfter {
			s.Unreachable = true
			if s.Code == "" {
				s.Code = CodeUnreachable
			}
		}
	}
	return s
}

// Run checks now and then until ctx ends: the first check after FirstDelay
// (later if the saved result is still fresh), then every Interval. It returns
// at once when the check is switched off.
func (c *Checker) Run(ctx context.Context) {
	if c.cfg.Disabled {
		return
	}
	wait := c.cfg.FirstDelay
	c.mu.Lock()
	if last := c.st.CheckedAt; !last.IsZero() {
		if next := c.cfg.Interval - c.cfg.Now().Sub(last); next > wait {
			wait = next
		}
	}
	c.mu.Unlock()
	for {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		c.check(ctx)
		wait = c.cfg.Interval
	}
}

// CheckNow asks right away and returns the new status. Asking again within
// a few seconds answers with the result just obtained, so the manual
// "check now" is idempotent. When the check is switched off it only reports that.
func (c *Checker) CheckNow(ctx context.Context) Status {
	if c.cfg.Disabled {
		return c.Status()
	}
	c.mu.Lock()
	recent := !c.st.CheckedAt.IsZero() && c.cfg.Now().Sub(c.st.CheckedAt) < manualGap && c.st.OK
	c.mu.Unlock()
	if !recent {
		c.check(ctx)
	}
	return c.Status()
}

// check runs one check (one at a time) and records the result.
func (c *Checker) check(ctx context.Context) {
	select {
	case c.busy <- struct{}{}:
		defer func() { <-c.busy }()
	case <-ctx.Done():
		return
	}
	china := c.cfg.China != nil && c.cfg.China()
	latest, notes, source, err := c.fetch(ctx, china)
	now := c.cfg.Now()
	if err != nil && ctx.Err() != nil {
		return // the server is stopping: not a result
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.st.CheckedAt = now
	if err != nil {
		if c.st.Failures == 0 {
			c.st.FirstFailAt = now
		}
		c.st.OK = false
		c.st.Failures++
		c.st.Error = shortError(err)
		c.logf("platform update check failed (%d in a row): %v", c.st.Failures, err)
	} else {
		c.st.OK = true
		c.st.Failures, c.st.Error, c.st.FirstFailAt = 0, "", time.Time{}
		c.st.LastOKAt = now
		c.st.Latest, c.st.NotesURL, c.st.Source = latest, notes, source
	}
	c.save()
}

func shortError(err error) string {
	s := err.Error()
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// fetch reads the newest stable version from the first source that answers.
func (c *Checker) fetch(ctx context.Context, china bool) (latest, notes, source string, err error) {
	type src struct{ name, url string }
	order := []src{{SourceGitHub, c.cfg.GitHubURL}, {SourceGitee, c.cfg.GiteeURL}}
	if china {
		order = []src{{SourceGitee, c.cfg.GiteeURL}, {SourceGitHub, c.cfg.GitHubURL}}
	}
	var errs []string
	for _, s := range order {
		v, n, e := c.fetchOne(ctx, s.name, s.url)
		if e == nil {
			return v, n, s.name, nil
		}
		errs = append(errs, s.name+"："+e.Error())
		if ctx.Err() != nil {
			break
		}
	}
	return "", "", "", errors.New(strings.Join(errs, "；"))
}

// release is the part of a release both Gitee and GitHub send.
type release struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
	HTMLURL    string `json:"html_url"`
}

var stableTag = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

func (c *Checker) fetchOne(ctx context.Context, name, url string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "AcornFox/"+c.cfg.Current)
	resp, err := c.cfg.Client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("连不上")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("返回 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", "", fmt.Errorf("读不完")
	}
	rels, err := parseReleases(body)
	if err != nil {
		return "", "", err
	}
	best, bestURL := "", ""
	for _, r := range rels {
		if r.Draft || r.Prerelease || !stableTag.MatchString(strings.TrimSpace(r.TagName)) {
			continue
		}
		v := strings.TrimPrefix(strings.TrimSpace(r.TagName), "v")
		if best == "" || Newer(v, best) {
			best, bestURL = v, r.HTMLURL
		}
	}
	if best == "" {
		return "", "", fmt.Errorf("没有正式版")
	}
	if bestURL == "" {
		base := githubTagPage
		if name == SourceGitee {
			base = giteeTagPage
		}
		bestURL = base + "v" + best
	}
	return best, bestURL, nil
}

// parseReleases reads a JSON array of releases (Gitee's and GitHub's lists)
// or one release object (their "latest" answer).
func parseReleases(body []byte) ([]release, error) {
	b := strings.TrimSpace(string(body))
	if strings.HasPrefix(b, "{") {
		var r release
		if err := json.Unmarshal([]byte(b), &r); err != nil || r.TagName == "" {
			return nil, fmt.Errorf("返回的内容看不懂")
		}
		return []release{r}, nil
	}
	var rs []release
	if err := json.Unmarshal([]byte(b), &rs); err != nil {
		return nil, fmt.Errorf("返回的内容看不懂")
	}
	return rs, nil
}

// version is x.y.z and whether there is a suffix after it.
type version struct {
	n      [3]int
	suffix bool
}

var versionCore = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(.*)$`)

func parseVersion(s string) (version, bool) {
	m := versionCore.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return version{}, false
	}
	var v version
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return version{}, false
		}
		v.n[i] = n
	}
	v.suffix = m[4] != ""
	return v, true
}

// Newer reports whether version a is newer than b. A version with a
// suffix (0.7.1-rc.1, 0.7.1-dev, 0.7.1+abc) is older than the same x.y.z
// without one; versions that cannot be read are never newer.
func Newer(a, b string) bool {
	va, ok1 := parseVersion(a)
	vb, ok2 := parseVersion(b)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 3; i++ {
		if va.n[i] != vb.n[i] {
			return va.n[i] > vb.n[i]
		}
	}
	return !va.suffix && vb.suffix
}

// UpgradeCommand is the one command that upgrades a machine of host kind
// kind in the region (china: Gitee, else GitHub), and where to run it. The
// texts are the ones the README and the Skill give (install-windows.md,
// install-mac.md).
func UpgradeCommand(kind string, china bool) (command, where string) {
	sh, ps := "https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh", "https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.ps1"
	if china {
		sh, ps = "https://gitee.com/VIP13390/AcornFox/raw/main/get.sh", "https://gitee.com/VIP13390/AcornFox/raw/main/get.ps1"
	}
	switch kind {
	case HostWindows:
		return `powershell -NoProfile -ExecutionPolicy Bypass -Command "[Net.ServicePointManager]::SecurityProtocol=3072; & ([scriptblock]::Create([Text.Encoding]::UTF8.GetString((New-Object Net.WebClient).DownloadData('` + ps + `')).TrimStart([char]0xFEFF))) -Action upgrade"`,
			"在这台电脑的 PowerShell 里运行（普通窗口，不要“以管理员身份运行”）；先看会做什么：在命令里的 -Action upgrade 后面加 -Plan"
	case HostMac:
		return "curl -fsSL " + sh + " | bash -s -- upgrade",
			"在这台 Mac 的“终端”里运行（普通用户，不要加 sudo）；把 upgrade 换成 upgrade --plan 先看会做什么"
	}
	return "curl -fsSL " + sh + " | sudo bash -s -- upgrade",
		"在这台服务器上运行（会先打印要做的事，升级没成功会自动恢复原样）"
}

// DetectHostKind says where the server runs. wslMode is the server's --wsl
// flag (set for a WSL distribution on Windows and for the virtual machine on
// a Mac); procVersion is /proc/version's text. ACORNFOX_HOST_KIND overrides.
func DetectHostKind(wslMode bool, procVersion string, getenv func(string) string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch k := strings.ToLower(strings.TrimSpace(getenv(EnvHostKind))); k {
	case HostLinux, HostWindows, HostMac:
		return k
	}
	if !wslMode {
		return HostLinux
	}
	if strings.Contains(strings.ToLower(procVersion), "microsoft") || getenv("WSL_DISTRO_NAME") != "" {
		return HostWindows
	}
	return HostMac
}
