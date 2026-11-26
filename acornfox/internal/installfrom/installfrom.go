// Package installfrom reports the install source tag to the download gateway, once (JW15-3, LW10).
//
// 做什么：官网的安装命令末尾可以带一个来源标记（`--from bili-1201`，Windows 是 `-From bili-1201`）。安装脚本把它写进数据目录里的
// install-from 文件；acornfox 服务启动后看到这个文件、而且还没报过，就用下载网关客户端调 W1 登记（POST /v1/register），
// 在请求的 client 里带上 from，由网关记下“这台机器是哪个链接带来的”。报成功后记下，以后不再报。
//
// 什么时候会联网：只有“数据目录里有 install-from 且还没报过”这一种情况。没带标记的安装没有这个文件，什么都不做，也不因为这件事去登记网关。
// 报的内容：和下载网关第一次登记同一份（公钥、安装编号、版本、系统、CPU 架构、客户端类型）外加这个标记；不报别的。
//
// 文件（都在数据目录下，属主 acornfox，0600）：
//
//	install-from              标记本身，一行，安装脚本写；写法 ^[a-z0-9-]{1,32}$，不合规的当没有（不报、记一行日志）。已有就不覆盖
//	install-from-state.json   本服务记的状态：tag、first_try_at、last_try_at、failures、last_error、reported_at（报成功的时间）、gave_up_at
//
// 失败怎么办：不阻塞、不影响别的功能（在自己的 goroutine 里跑，出错只记日志）。第一次在服务启动约 1 分钟后试；失败后按
// 5 分钟、15 分钟、1 小时、3 小时、之后每 6 小时重试；从第一次试起满 7 天仍没成功就放弃（记 gave_up_at 和一行日志），以后不再试。
// 状态写在文件里，服务重启后接着按原来的节奏，不会因为重启反复报；也不会在报成功后重复报。
//
// 怎么停：删掉 install-from 文件，或设环境变量 ACORNFOX_INSTALL_FROM=off（也认 0、false、no、disabled）后重启 acornfox-server；
// 下载网关关了（ACORNFOX_GATEWAY 为空、network.json 写 off）时每次尝试都会失败、7 天后放弃，不联网。
// 怎么看：journalctl -u acornfox-server 里搜“来源标记”；或看 install-from-state.json。`acornfox doctor` 不检查它，也不会因它报错。
//
// 网关一侧：W1 只认 client.from 这个可选字段，网关老版本会忽略它（W1 用普通的 json.Unmarshal，没有禁止未知字段），所以新程序配老网关也能登记，
// 只是网关没记标记；网关新版本只存第一次非空的值、不覆盖，并在应答里多一个 "from_recorded": true。**以这个字段为准才算“已报过”**：
// 200 但没有它（老网关）按没报成，继续按退避重试，仍是最多 7 天后放弃——这样网关上了新版本以后，还没放弃的机器会补报上。
package installfrom

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	// MarkerFile is what the install script writes (the tag, one line).
	MarkerFile = "install-from"
	// StateFile is this package's own record next to it.
	StateFile = "install-from-state.json"
	// EnvSwitch turns the report off ("off", "0", "false", "no", "disabled").
	EnvSwitch = "ACORNFOX_INSTALL_FROM"

	// DefaultFirstDelay is how long after the server starts the first try waits.
	DefaultFirstDelay = time.Minute
	// DefaultGiveUpAfter: this long after the first try without a success, stop for good.
	DefaultGiveUpAfter = 7 * 24 * time.Hour

	maxMarkerBytes = 256
)

// tagRE is the tag's shape, the same everywhere (website, install scripts,
// gateway): lower-case letters, digits and "-", 1 to 32 characters. Go's $
// without the m flag only matches at the very end of the text.
var tagRE = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// Valid says whether s is a usable tag.
func Valid(s string) bool { return tagRE.MatchString(s) }

// Backoff is the wait after the n-th failure (n >= 1): 5 minutes, 15 minutes,
// 1 hour, 3 hours, then every 6 hours.
func Backoff(failures int) time.Duration {
	switch {
	case failures <= 1:
		return 5 * time.Minute
	case failures == 2:
		return 15 * time.Minute
	case failures == 3:
		return time.Hour
	case failures == 4:
		return 3 * time.Hour
	default:
		return 6 * time.Hour
	}
}

// State is install-from-state.json.
type State struct {
	// Tag is the tag these numbers are about; if the marker file changes to
	// another tag the record starts over.
	Tag string `json:"tag"`
	// FirstTryAt, LastTryAt, ReportedAt, GaveUpAt are RFC 3339 UTC times ("" = not yet).
	FirstTryAt string `json:"first_try_at,omitempty"`
	LastTryAt  string `json:"last_try_at,omitempty"`
	ReportedAt string `json:"reported_at,omitempty"`
	GaveUpAt   string `json:"gave_up_at,omitempty"`
	// Failures counts the tries that failed since the last success; LastError is the short reason of the last one.
	Failures  int    `json:"failures,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

// Done says whether nothing more will be tried: reported, or given up.
func (s State) Done() bool { return s.ReportedAt != "" || s.GaveUpAt != "" }

// Config builds a Reporter.
type Config struct {
	// DataDir holds the marker and the state files.
	DataDir string
	// Register tells the download gateway about this machine with the tag
	// (W1 with client.from) and returns nil only when the gateway answered
	// "from_recorded": true; ErrNotRecorded when it answered without that
	// (an old gateway). nil means there is no gateway: nothing is reported.
	Register func(ctx context.Context, tag string) error
	// Disabled switches the whole thing off.
	Disabled bool
	// FirstDelay, GiveUpAfter, Backoff default to the values above.
	FirstDelay  time.Duration
	GiveUpAfter time.Duration
	Backoff     func(failures int) time.Duration
	// Now is the clock (nil: time.Now); Sleep waits d or until ctx ends (nil: a timer). Tests replace both.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
	// Logf logs one line (nil: silent).
	Logf func(format string, args ...any)
}

// FromEnv fills what the environment says into c (see the package comment).
func (c *Config) FromEnv(getenv func(string) string) {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch strings.ToLower(strings.TrimSpace(getenv(EnvSwitch))) {
	case "off", "0", "false", "no", "disabled":
		c.Disabled = true
	}
}

// Reporter reports the marker's tag to the gateway once.
type Reporter struct {
	cfg Config
}

// New builds a Reporter.
func New(cfg Config) *Reporter {
	if cfg.FirstDelay <= 0 {
		cfg.FirstDelay = DefaultFirstDelay
	}
	if cfg.GiveUpAfter <= 0 {
		cfg.GiveUpAfter = DefaultGiveUpAfter
	}
	if cfg.Backoff == nil {
		cfg.Backoff = Backoff
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleep
	}
	return &Reporter{cfg: cfg}
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (r *Reporter) logf(format string, args ...any) {
	if r.cfg.Logf != nil {
		r.cfg.Logf(format, args...)
	}
}

func (r *Reporter) markerPath() string { return filepath.Join(r.cfg.DataDir, MarkerFile) }
func (r *Reporter) statePath() string  { return filepath.Join(r.cfg.DataDir, StateFile) }

// ReadMarker reads the tag the install script left in dataDir. found is false
// when there is no marker file (the normal case: an install without a tag);
// when the file is there but holds no usable tag, found is true and tag is "".
func ReadMarker(dataDir string) (tag string, found bool) {
	f, err := os.Open(filepath.Join(dataDir, MarkerFile))
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf := make([]byte, maxMarkerBytes)
	n, _ := f.Read(buf)
	line := strings.SplitN(string(buf[:n]), "\n", 2)[0]
	line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	if !Valid(line) {
		return "", true
	}
	return line, true
}

// ReadState reads install-from-state.json (a missing or unreadable file is the zero State).
func ReadState(dataDir string) State {
	var s State
	if data, err := os.ReadFile(filepath.Join(dataDir, StateFile)); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

func (r *Reporter) save(s State) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	tmp := r.statePath() + ".tmp"
	if os.WriteFile(tmp, append(data, '\n'), 0o600) != nil {
		return
	}
	if os.Rename(tmp, r.statePath()) != nil {
		_ = os.Remove(tmp)
	}
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func parse(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// short keeps an error text on one line of reasonable length for the state file and the log.
func short(err error) string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return s
}

// Run reports now and then until it is done or ctx ends. It returns at once
// when there is nothing to do: switched off, no gateway function, no marker
// file, an unusable tag, or already reported / given up.
func (r *Reporter) Run(ctx context.Context) {
	if r.cfg.Disabled || r.cfg.Register == nil || r.cfg.DataDir == "" {
		return
	}
	tag, found := ReadMarker(r.cfg.DataDir)
	if !found {
		return
	}
	if tag == "" {
		r.logf("来源标记：%s 里的内容不合规（只认小写字母、数字、-，最长 32 个字符），忽略，不向下载网关报", r.markerPath())
		return
	}
	st := ReadState(r.cfg.DataDir)
	if st.Tag != tag {
		st = State{Tag: tag} // first time, or the marker was changed by hand: start over
	}
	if st.Done() {
		return
	}
	for {
		if err := r.cfg.Sleep(ctx, r.waitFor(st)); err != nil {
			return
		}
		err := r.cfg.Register(ctx, tag)
		if ctx.Err() != nil {
			return // shutting down: not a failure of the gateway, do not count it
		}
		now := r.cfg.Now()
		if st.FirstTryAt == "" {
			st.FirstTryAt = stamp(now)
		}
		st.LastTryAt = stamp(now)
		if err == nil {
			st.ReportedAt, st.Failures, st.LastError = stamp(now), 0, ""
			r.save(st)
			r.logf("来源标记：已向下载网关报告（%s）", tag)
			return
		}
		st.Failures++
		st.LastError = short(err)
		if now.Sub(parse(st.FirstTryAt)) >= r.cfg.GiveUpAfter {
			st.GaveUpAt = stamp(now)
			r.save(st)
			r.logf("来源标记：从第一次试起满 %d 天仍没报成功，放弃，以后不再试（%s）", int(r.cfg.GiveUpAfter/(24*time.Hour)), st.LastError)
			return
		}
		r.save(st)
		r.logf("来源标记：向下载网关报告没成功（第 %d 次：%s），%s 后再试", st.Failures, st.LastError, r.cfg.Backoff(st.Failures))
	}
}

// waitFor is how long to wait before the next try: the start delay before the
// first one, afterwards the backoff counted from the last try (so a restart
// keeps the old rhythm instead of starting over).
func (r *Reporter) waitFor(st State) time.Duration {
	if st.LastTryAt == "" {
		return r.cfg.FirstDelay
	}
	wait := parse(st.LastTryAt).Add(r.cfg.Backoff(max(st.Failures, 1))).Sub(r.cfg.Now())
	if wait < 0 {
		wait = 0
	}
	return wait
}

// ErrNotRecorded is what a Register function returns when the gateway answered
// the registration (200) but did not say it recorded the tag (no
// "from_recorded": true): an old gateway that does not know the field yet.
// Only a recorded tag counts as reported, so this is retried like any failure.
var ErrNotRecorded = errors.New("下载网关收下了登记，但没有记下来源标记（网关还没上新版本）")

// ErrGatewayOff is what a Register function returns when the gateway is switched off.
var ErrGatewayOff = errors.New("下载网关已关闭（ACORNFOX_GATEWAY 为空，或 network.json 里 gateway 是 off）")
