// Package gatewayclient talks to the AcornFox download gateway: it registers
// this machine's random key (W1, POST /v1/register), asks for a short-lived
// download address for one file (W2, POST /v1/issue, also for files of a
// private catalog), connects this machine to a private catalog with a code
// and fetches the private catalogs it may see (W10, POST /v1/catalog/connect
// and /v1/catalog/index; see catalog.go). The
// gateway never serves file contents; the caller downloads from the returned
// URL and checks the file against its own pinned SHA-256.
//
// Every request is signed with the machine key (package identity); the
// headers and the string to sign are fixed by the gateway's interface
// contract and checked by the shared test vectors.
//
// Errors are *Error with a GW-* code. Codes from the gateway are listed in
// Codes; two more are made here: GW-NET-01 (gateway unreachable or timed out)
// and GW-RESP-01 (an answer this client cannot read). Callers treat every
// error as "this source failed, try the next one": the gateway must never be
// the reason an install fails.
//
// Operating notes:
//
//   - Used by: internal/setup's downloader (the "AcornFox 下载网关" source),
//     only when a gateway address is configured (platform.Network
//     EffectiveGateway: ACORNFOX_GATEWAY, else network.json "gateway").
//   - Needs: a machine key (identity.LoadOrCreate) and a reachable gateway.
//   - Side effects: W1 registers the public key on the gateway (repeatable,
//     same answer each time); W2 counts against the machine's daily quota.
//     Nothing is written locally.
//   - Verify: go test ./internal/gatewayclient/ (a fake gateway checks the
//     signatures); against a real gateway, `acornfox identity show` gives
//     the install ID to look up in the gateway's records.
//   - Stop: ACORNFOX_GATEWAY= (empty) or "gateway": "off" in network.json;
//     the downloader then behaves as before the gateway existed.
//   - First look: the GW-* code in the setup's "换下一个下载源" line; GW-AUTH-01
//     is usually a wrong clock (see below); GW-NET-01 a proxy or a wrong
//     address.
//
// Clock: the gateway refuses a request whose timestamp is more than 300
// seconds away from its own time (GW-AUTH-01). When such an answer carries a
// Date header that is at least ClockSkewMin away from this machine's clock,
// the client takes the gateway's time, signs the same request again with the
// corrected timestamp once, and keeps that correction for later requests of
// the same Client. If the gateway still refuses, the error's message says how
// far the clocks are apart and how to fix it (timedatectl set-ntp true);
// Error.ClockSkew carries the difference. `acornfox doctor` reports a wrong
// clock on its own as SETUP-CLOCK-01.
package gatewayclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/acornfox/acornfox/internal/identity"
)

// DefaultTimeout bounds each request to the gateway: sending the small
// signed request and reading its JSON answer (W1, W2, W10). It never bounds
// a download: the caller fetches the file from the issued URL with its own
// client (appinstall and setup bound a download by inactivity, not by total
// time). From test machines in China the gateway took 1.2 to 8.3 seconds to
// send its first byte; at 10 seconds installs gave up on it now and then and
// fell back to the much slower public image sources, so it waits 30. When
// the gateway is down for real, the fallback starts 30 seconds later.
// doctor's HEAD to the gateway has its own 5 seconds (hostcheck).
const DefaultTimeout = 30 * time.Second

// ClockSkewMin is how far the gateway's Date must be from this machine's
// clock before a GW-AUTH-01 is taken as a clock problem and retried with the
// gateway's time. Date has a resolution of one second and the gateway allows
// 300 seconds, so anything smaller is not what made the gateway refuse.
const ClockSkewMin = 60 * time.Second

// DefaultMaxRetryAfter is the longest GW-LIM-02 wait Issue sits out before
// its single retry; a longer retry_after is returned to the caller at once.
const DefaultMaxRetryAfter = 30 * time.Second

// ClientInfo describes this client in W1.
type ClientInfo struct {
	Version  string `json:"version"`
	Platform string `json:"platform"` // windows | darwin | linux
	Arch     string `json:"arch"`     // amd64 | arm64
	Via      string `json:"via"`      // acornfox | install-ps1 | install-sh
}

// DefaultInfo is ClientInfo for this acornfox binary.
func DefaultInfo(version string) ClientInfo {
	return ClientInfo{Version: version, Platform: runtime.GOOS, Arch: runtime.GOARCH, Via: "acornfox"}
}

// Client calls one gateway.
type Client struct {
	BaseURL  string             // e.g. https://dl.acornfox.com (no trailing path needed)
	Identity *identity.Identity // the machine key
	HTTP     *http.Client       // nil => proxy from environment, DefaultTimeout
	Info     ClientInfo         // sent on automatic registration

	// MaxRetryAfter caps the GW-LIM-02 wait (0 => DefaultMaxRetryAfter).
	MaxRetryAfter time.Duration

	now   func() time.Time
	sleep func(context.Context, time.Duration) error

	// offset is the gateway's time minus this machine's, in seconds, as
	// learned from a GW-AUTH-01 answer's Date header (0 until then).
	offset atomic.Int64
}

// ClockOffset is the correction this client applies to its timestamps (the
// gateway's time minus this machine's), 0 when none was needed.
func (c *Client) ClockOffset() time.Duration {
	return time.Duration(c.offset.Load()) * time.Second
}

// Registered is W1's answer.
type Registered struct {
	InstallID string `json:"install_id"`
	PerDay    int    `json:"per_day"`
}

// Issued is W2's answer: where to download one file until ExpiresAt.
type Issued struct {
	FileID    string    `json:"file_id"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	SHA256    string    `json:"sha256"`
	Size      int64     `json:"size"`
	Backend   string    `json:"backend"`
	DayUsed   int       `json:"day_used"`
	DayLimit  int       `json:"day_limit"`
}

// Error is a gateway failure with its GW-* code.
type Error struct {
	Code       string
	Message    string
	HTTPStatus int           // 0 when no answer arrived
	RetryAfter time.Duration // GW-LIM-02
	Err        error         // the transport error behind GW-NET-01
	// ClockSkew is the gateway's time minus this machine's clock when a
	// GW-AUTH-01 was caused by a wrong clock (0 otherwise).
	ClockSkew time.Duration

	serverTime time.Time // the answer's Date header, zero when absent
	sentAt     time.Time // this machine's clock (with the correction) when the answer arrived
}

func (e *Error) Error() string {
	s := e.Code + " " + e.Message
	if e.Err != nil {
		s += "（" + e.Err.Error() + "）"
	}
	return s
}

func (e *Error) Unwrap() error { return e.Err }

// CodeOf returns err's GW-* code, or "".
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// CodeDef explains one code for people and for the setup fault summary.
type CodeDef struct {
	Code    string `json:"code"`
	Meaning string `json:"meaning"`
	Client  string `json:"client"` // what this client does
}

// Codes is the client's view of the gateway's error codes (the gateway's own
// fault catalog has causes and operator steps).
var Codes = []CodeDef{
	{"GW-REQ-01", "请求格式不对", "跳过网关，换下一个下载源"},
	{"GW-REG-01", "这个 IP 今天新登记的机器太多", "跳过网关"},
	{"GW-AUTH-01", "签名不对、时间戳超出范围或 nonce 重复", "应答带 Date 头且和本机时钟差 60 秒以上时，按网关时间重签一次；还不行就跳过网关，提示里写明时钟差了多少，请校时（timedatectl set-ntp true）"},
	{"GW-AUTH-02", "这台机器还没登记", "自动登记一次再重试；还不行就跳过网关"},
	{"GW-AUTH-03", "这台机器被拉黑", "跳过网关"},
	{"GW-LIM-01", "这台机器今天的次数用完了（明天恢复）", "跳过网关"},
	{"GW-LIM-02", "这个 IP 请求太频繁", "等 retry_after 秒（不超过 30 秒）重试一次，还不行就跳过网关"},
	{"GW-OFF-01", "网关停发（全局或这个文件）", "跳过网关"},
	{"GW-ART-01", "网关上没有这个文件", "跳过网关"},
	{"GW-BE-01", "网关后端（网盘）出错", "跳过网关"},
	{"GW-NIMP-01", "接口预留，还没实现", "跳过网关"},
	{"GW-CAT-01", "连接码不存在、格式不对或已被别的机器用过", "提示用户检查连接码或要新码；不自动重试"},
	{"GW-CAT-02", "连接码已过期", "提示用户要新码"},
	{"GW-CAT-03", "私有目录已停用", "提示用户；已装的应用照常运行"},
	{"GW-CAT-04", "连接码试得太频繁", "等 retry_after 秒后再让用户试；不自动循环"},
	{"GW-CAT-05", "不能通过这个私有目录下载这个文件（没授权、已撤销、目录停用或文件不在目录里）", "不去公共源找；提示重新连接或联系管理员"},
	{"GW-NET-01", "连不上网关或超时（本机判断）", "跳过网关；看网络、代理和 ACORNFOX_GATEWAY 的地址"},
	{"GW-RESP-01", "网关的应答看不懂（本机判断）", "跳过网关；多半是地址指错了，或中间有代理改了应答"},
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: DefaultTimeout, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
}

// localClock is this machine's clock (tests replace it).
func (c *Client) localClock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// clock is the time requests are signed with: this machine's clock plus the
// correction learned from the gateway.
func (c *Client) clock() time.Time {
	return c.localClock().Add(c.ClockOffset())
}

func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, d)
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

// Register runs W1. Registering the same key again is harmless.
func (c *Client) Register(ctx context.Context, info ClientInfo) (Registered, error) {
	if c.Identity == nil {
		return Registered{}, &Error{Code: "GW-REQ-01", Message: "没有机器钥匙"}
	}
	body, _ := json.Marshal(struct {
		PubKey string     `json:"pubkey"`
		Client ClientInfo `json:"client"`
	}{c.Identity.PublicKey, info})
	var out struct {
		InstallID string `json:"install_id"`
		Limits    struct {
			PerDay int `json:"per_day"`
		} `json:"limits"`
	}
	if err := c.call(ctx, "/v1/register", body, &out, maxAnswer); err != nil {
		return Registered{}, err
	}
	if out.InstallID != c.Identity.InstallID {
		return Registered{}, &Error{Code: "GW-RESP-01", Message: "网关返回的安装编号和本机算的不一样：" + out.InstallID}
	}
	return Registered{InstallID: out.InstallID, PerDay: out.Limits.PerDay}, nil
}

var hexSHA256 = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// Issue runs W2 for one public file. On GW-AUTH-02 it registers and tries
// once more; on GW-LIM-02 with a short retry_after it waits and tries once
// more.
func (c *Client) Issue(ctx context.Context, fileID, purpose string) (Issued, error) {
	var iss Issued
	err := c.retry(ctx, func() error {
		var err error
		iss, err = c.issueOnce(ctx, fileID, purpose, nil)
		return err
	})
	return iss, err
}

// AppRef names a private catalog app in W2 ("app": {"catalog", "app"}).
type AppRef struct {
	Catalog string `json:"catalog"`
	App     string `json:"app"`
}

// IssueApp runs W2 for a file of a private catalog (purpose app, with the
// catalog and app named). The gateway answers GW-CAT-05 when this machine may
// not fetch it through that catalog; callers must then not look for the file
// on a public source.
func (c *Client) IssueApp(ctx context.Context, fileID string, app AppRef) (Issued, error) {
	var iss Issued
	err := c.retry(ctx, func() error {
		var err error
		iss, err = c.issueOnce(ctx, fileID, "app", &app)
		return err
	})
	return iss, err
}

// retry runs fn; on GW-AUTH-02 it registers and runs fn once more; on
// GW-LIM-02 with a retry_after up to MaxRetryAfter it waits and runs it once
// more. Nothing else is retried.
func (c *Client) retry(ctx context.Context, fn func() error) error {
	if c.Identity == nil {
		return &Error{Code: "GW-REQ-01", Message: "没有机器钥匙"}
	}
	registered, waited := false, false
	for {
		err := fn()
		if err == nil {
			return nil
		}
		var ge *Error
		if !errors.As(err, &ge) {
			return err
		}
		switch {
		case ge.Code == "GW-AUTH-02" && !registered:
			registered = true
			info := c.Info
			if info.Platform == "" {
				info = DefaultInfo(info.Version)
			}
			if _, rerr := c.Register(ctx, info); rerr != nil {
				return rerr
			}
			continue
		case ge.Code == "GW-LIM-02" && !waited:
			max := c.MaxRetryAfter
			if max <= 0 {
				max = DefaultMaxRetryAfter
			}
			if ge.RetryAfter <= 0 || ge.RetryAfter > max {
				return err
			}
			waited = true
			if werr := c.wait(ctx, ge.RetryAfter); werr != nil {
				return werr
			}
			continue
		}
		return err
	}
}

func (c *Client) issueOnce(ctx context.Context, fileID, purpose string, app *AppRef) (Issued, error) {
	// Field order is fixed so the signed bytes are predictable; app is null
	// for public files and account_token is reserved and always null.
	body, _ := json.Marshal(struct {
		FileID       string  `json:"file_id"`
		Purpose      string  `json:"purpose"`
		App          *AppRef `json:"app"`
		AccountToken *string `json:"account_token"`
	}{FileID: fileID, Purpose: purpose, App: app})
	var out struct {
		FileID    string `json:"file_id"`
		URL       string `json:"url"`
		ExpiresAt string `json:"expires_at"`
		SHA256    string `json:"sha256"`
		Size      int64  `json:"size"`
		Backend   string `json:"backend"`
		Quota     struct {
			DayUsed  int `json:"day_used"`
			DayLimit int `json:"day_limit"`
		} `json:"quota"`
	}
	if err := c.call(ctx, "/v1/issue", body, &out, maxAnswer); err != nil {
		return Issued{}, err
	}
	u, err := url.Parse(out.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return Issued{}, &Error{Code: "GW-RESP-01", Message: "网关返回的下载地址不对"}
	}
	if out.SHA256 != "" && !hexSHA256.MatchString(out.SHA256) {
		return Issued{}, &Error{Code: "GW-RESP-01", Message: "网关返回的 sha256 格式不对"}
	}
	iss := Issued{FileID: out.FileID, URL: out.URL, SHA256: strings.ToLower(out.SHA256), Size: out.Size, Backend: out.Backend,
		DayUsed: out.Quota.DayUsed, DayLimit: out.Quota.DayLimit}
	if out.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, out.ExpiresAt); err == nil {
			iss.ExpiresAt = t
		}
	}
	return iss, nil
}

// Answer size limits: W1, W2 and connect answers are small; the private
// catalog list carries whole signed indexes.
const (
	maxAnswer      = 64 << 10
	maxIndexAnswer = 64 << 20
)

// call signs and POSTs body to path and decodes a successful answer (at most
// limit bytes) into out. A GW-AUTH-01 caused by a wrong clock is retried
// once with the gateway's time (see the package comment).
func (c *Client) call(ctx context.Context, path string, body []byte, out any, limit int64) error {
	err := c.callOnce(ctx, path, body, out, limit)
	ge, skew := clockRefusal(err)
	if ge == nil {
		return err
	}
	// The gateway's time minus this machine's raw clock.
	raw := c.ClockOffset() + skew
	c.offset.Store(int64(raw.Round(time.Second) / time.Second))
	err = c.callOnce(ctx, path, body, out, limit)
	var again *Error
	if errors.As(err, &again) && again.Code == "GW-AUTH-01" {
		again.ClockSkew = raw
		again.Message += fmt.Sprintf("；这台机器的时钟和网关差了约 %d 秒，请校时（timedatectl set-ntp true）", int64(absDuration(raw)/time.Second))
	}
	return err
}

// clockRefusal reports a GW-AUTH-01 whose Date header is at least
// ClockSkewMin away from the time the request was signed with, and how far.
func clockRefusal(err error) (*Error, time.Duration) {
	var ge *Error
	if !errors.As(err, &ge) || ge.Code != "GW-AUTH-01" || ge.serverTime.IsZero() || ge.sentAt.IsZero() {
		return nil, 0
	}
	skew := ge.serverTime.Sub(ge.sentAt)
	if absDuration(skew) < ClockSkewMin {
		return nil, 0
	}
	return ge, skew
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func (c *Client) callOnce(ctx context.Context, path string, body []byte, out any, limit int64) error {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		return &Error{Code: "GW-NET-01", Message: "没有配置下载网关地址"}
	}
	ts := c.clock().Unix()
	nonce, err := identity.NewNonce()
	if err != nil {
		return &Error{Code: "GW-NET-01", Message: "生成随机数失败", Err: err}
	}
	sig, err := c.Identity.Sign(http.MethodPost, path, ts, nonce, body)
	if err != nil {
		return &Error{Code: "GW-NET-01", Message: "签名失败", Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil {
		return &Error{Code: "GW-NET-01", Message: "下载网关地址不对：" + base, Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AFX-Install", c.Identity.InstallID)
	req.Header.Set("X-AFX-Timestamp", fmt.Sprint(ts))
	req.Header.Set("X-AFX-Nonce", nonce)
	req.Header.Set("X-AFX-Signature", sig)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return &Error{Code: "GW-NET-01", Message: "连不上下载网关", Err: err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return &Error{Code: "GW-NET-01", Message: "读取网关应答失败", HTTPStatus: resp.StatusCode, Err: err}
	}
	if int64(len(raw)) > limit {
		return &Error{Code: "GW-RESP-01", Message: fmt.Sprintf("网关应答超过 %d 字节", limit), HTTPStatus: resp.StatusCode}
	}
	var head struct {
		OK         bool   `json:"ok"`
		Code       string `json:"code"`
		Message    string `json:"message"`
		RetryAfter int    `json:"retry_after"`
	}
	if json.Unmarshal(raw, &head) != nil {
		return &Error{Code: "GW-RESP-01", Message: fmt.Sprintf("网关应答不是 JSON（HTTP %d）", resp.StatusCode), HTTPStatus: resp.StatusCode}
	}
	if !head.OK || resp.StatusCode/100 != 2 {
		code := head.Code
		if !strings.HasPrefix(code, "GW-") {
			code = "GW-RESP-01"
		}
		msg := head.Message
		if msg == "" {
			msg = fmt.Sprintf("网关拒绝（HTTP %d）", resp.StatusCode)
		}
		ra := time.Duration(head.RetryAfter) * time.Second
		if ra == 0 {
			if v := resp.Header.Get("Retry-After"); v != "" {
				var n int
				if _, err := fmt.Sscan(v, &n); err == nil && n > 0 {
					ra = time.Duration(n) * time.Second
				}
			}
		}
		e := &Error{Code: code, Message: msg, HTTPStatus: resp.StatusCode, RetryAfter: ra}
		if d := resp.Header.Get("Date"); d != "" {
			if t, err := http.ParseTime(d); err == nil {
				e.serverTime, e.sentAt = t, c.clock()
			}
		}
		return e
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &Error{Code: "GW-RESP-01", Message: "网关应答字段不对", HTTPStatus: resp.StatusCode, Err: err}
	}
	return nil
}
