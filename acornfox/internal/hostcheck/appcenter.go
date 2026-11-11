package hostcheck

// The app center checks shared by `acornfox doctor` and the console's
// runtime page (GET /v1/runtime/checks):
//
//   - the app catalog sync (fault SETUP-SYNC-01): when the server last
//     synced, whether it worked, from which source, which catalog is in use.
//     It reads the status acornfox-server keeps; it never syncs itself.
//   - the download gateway (fault SETUP-GW-01): one HEAD request to
//     <gateway>/v1/health (no body, no signature, no install ID), at most 5
//     seconds: a first HTTPS connection from a test machine in China took 2.5
//     seconds, too close to 3. It says whether the gateway answers 200 and
//     how far the Date of its answer is from this machine's clock. The clock
//     itself is judged by SETUP-CLOCK-01 (clock.go); this line only reports
//     the difference. Whether the gateway is issuing is in the body of a GET,
//     which is not sent: the catalog entry says how to look.
//
// The clock check and the gateway check share that one request
// (DefaultGatewayProbes): a run of doctor or of the runtime page sends one
// HEAD to the gateway and waits at most 5 seconds for it, also when the
// gateway does not answer at all.
//
// Both only warn: apps of the public catalog install without the gateway
// (from public image sources) and with an older catalog. Nothing is changed.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CatalogSyncStale is how old the last sync may be before doctor warns: the
// server syncs once a day, so two days without one means it did not run.
const CatalogSyncStale = 50 * time.Hour

// CatalogSync is what acornfox-server knows about the app catalog.
type CatalogSync struct {
	At      *time.Time // when the last attempt ended; nil = never
	OK      bool       // the last attempt did not fail
	Skipped bool       // no trusted key: nothing is contacted
	Code    string     // CAT-SYNC-* of the failure
	Message string
	Source  string // the source that answered
	Serial  int64  // the public catalog in use
	Origin  string // cache | snapshot
}

// CheckCatalogSync adds one line about the app catalog sync.
func CheckCatalogSync(r Reporter, s CatalogSync, now time.Time) {
	const code = "SETUP-SYNC-01"
	inUse := catalogInUse(s)
	switch {
	case s.Skipped:
		r.Add(true, code, "应用目录：程序里没有受信公钥，不联网同步，只用%s", inUse)
	case s.At == nil:
		r.Warn(code, "应用目录：还没同步过，在用%s。后台服务启动后会马上同步一次，刚启动就过几分钟再看；一直这样就运行 acornfox catalog sync 看原因", inUse)
	case !s.OK:
		msg := strings.TrimSpace(s.Message)
		if s.Code != "" && !strings.HasPrefix(msg, s.Code) {
			msg = s.Code + " " + msg
		}
		r.Warn(code, "应用目录：%s同步失败（%s），继续用%s。运行 acornfox catalog sync --json 看每个来源的结果", agoText(now, *s.At), msg, inUse)
	case now.Sub(*s.At) > CatalogSyncStale:
		r.Warn(code, "应用目录：上次同步是 %s，超过两天没同步（后台服务每天同步一次，多半是它停过），在用%s。运行 acornfox catalog sync", agoText(now, *s.At), inUse)
	default:
		from := ""
		if s.Source != "" {
			from = "（来源 " + s.Source + "）"
		}
		r.Add(true, code, "应用目录：%s同步过%s，在用%s", agoText(now, *s.At), from, inUse)
	}
}

func catalogInUse(s CatalogSync) string {
	where := "目录"
	switch s.Origin {
	case "cache":
		where = "同步下来的目录"
	case "snapshot":
		where = "程序内置的目录"
	}
	if s.Serial > 0 {
		return fmt.Sprintf("%s（序号 %d）", where, s.Serial)
	}
	return where
}

// agoText says how long ago t was, roughly ("刚刚", "3 分钟前", "5 小时前", "2 天前").
func agoText(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d/time.Hour))
	}
	return fmt.Sprintf("%d 天前", int(d/(24*time.Hour)))
}

// GatewayHealth is the answer to HEAD <gateway>/v1/health.
type GatewayHealth struct {
	Status  int       // HTTP status
	Date    time.Time // the answer's Date header (zero when missing)
	Latency time.Duration
	// At is this machine's clock halfway through the request (zero when
	// unknown); the clock check uses it when it reads a shared answer later.
	At time.Time
}

// GatewayProbe holds how the gateway check reads its inputs (tests replace
// Health).
type GatewayProbe struct {
	Base   string // the gateway's address; "" = switched off on this machine
	Health func(ctx context.Context) (GatewayHealth, error)
	Now    func() time.Time
}

// GatewayProbeTimeout bounds the health request.
const GatewayProbeTimeout = 5 * time.Second

// DefaultGatewayProbe sends HEAD base/v1/health (proxy settings from the
// environment, like the app center's own requests).
func DefaultGatewayProbe(base string) GatewayProbe {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	p := GatewayProbe{Base: base, Now: time.Now}
	if base != "" {
		p.Health = func(ctx context.Context) (GatewayHealth, error) { return getGatewayHealth(ctx, base) }
	}
	return p
}

// DefaultGatewayProbes builds the readers of the clock check (CheckClock)
// and of the gateway check (CheckGateway) for the gateway at base so that
// both use one HEAD request to base/v1/health: whichever check asks first
// sends it, with at most GatewayProbeTimeout to answer; the other reuses the
// answer (or the error). The clock check takes the gateway's current time as
// the answer's Date plus what has passed on this machine since. With base
// empty (the gateway is switched off) nothing is sent: the clock check only
// looks at network time sync and the gateway check says it is off.
func DefaultGatewayProbes(base string) (ClockProbe, GatewayProbe) {
	gw := DefaultGatewayProbe(base)
	clock := ClockProbe{Now: time.Now, Sync: readSyncStatus}
	if gw.Health == nil {
		return clock, gw
	}
	shared := shareHealth(gw.Health)
	gw.Health = shared
	clock.ReferenceTimeout = GatewayProbeTimeout
	clock.Reference = func(ctx context.Context) (time.Time, error) {
		h, err := shared(ctx)
		if err != nil {
			return time.Time{}, err
		}
		if h.Date.IsZero() {
			return time.Time{}, errors.New("应答没有 Date")
		}
		if h.At.IsZero() {
			return h.Date, nil
		}
		return h.Date.Add(time.Since(h.At)), nil
	}
	return clock, gw
}

// shareHealth makes get run once: the first caller's request is the answer
// for every later caller.
func shareHealth(get func(context.Context) (GatewayHealth, error)) func(context.Context) (GatewayHealth, error) {
	var (
		mu   sync.Mutex
		done bool
		h    GatewayHealth
		err  error
	)
	return func(ctx context.Context) (GatewayHealth, error) {
		mu.Lock()
		defer mu.Unlock()
		if !done {
			h, err = get(ctx)
			done = true
		}
		return h, err
	}
}

// CheckGateway adds one line about the download gateway.
func CheckGateway(ctx context.Context, r Reporter, p GatewayProbe) {
	const code = "SETUP-GW-01"
	if p.Base == "" {
		r.Add(true, code, "下载网关：这台服务器关掉了（ACORNFOX_GATEWAY 为空或 network.json 写了 off），应用中心的镜像都走公共源，私有目录连不了")
		return
	}
	if p.Health == nil {
		return
	}
	if p.Now == nil {
		p.Now = time.Now
	}
	hctx, cancel := context.WithTimeout(ctx, GatewayProbeTimeout)
	defer cancel()
	h, err := p.Health(hctx)
	effect := "应用中心的大镜像会改从公共源拉（慢一些），私有目录连不上、装不了"
	switch {
	case err != nil:
		r.Warn(code, "下载网关：连不上 %s（%s）。%s；先看网络和代理，再看网关地址有没有写错", p.Base, shortErr(err), effect)
		return
	case h.Status != http.StatusOK:
		r.Warn(code, "下载网关：%s 的应答不对（HTTP %d），多半是地址指错了、网关在维护，或中间有代理改了应答。%s", p.Base, h.Status, effect)
		return
	}
	clock := ""
	if !h.Date.IsZero() {
		// The answer's Date is somewhere between sending and receiving
		// (At; a reader without it answered just now).
		mid := h.At
		if mid.IsZero() {
			mid = p.Now().Add(-h.Latency / 2)
		}
		skew := h.Date.Sub(mid)
		if skew < 0 {
			skew = -skew
		}
		secs := int64(skew / time.Second)
		clock = fmt.Sprintf("，和本机时钟差约 %d 秒", secs)
		if skew >= ClockFailSkew {
			clock += "（太多了，网关会拒绝这台机器，见时钟一项 SETUP-CLOCK-01）"
		}
	}
	r.Add(true, code, "下载网关：能连上 %s（%d 毫秒%s）", p.Base, h.Latency.Milliseconds(), clock)
}

// getGatewayHealth sends one HEAD to base/v1/health.
func getGatewayHealth(ctx context.Context, base string) (GatewayHealth, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, base+"/v1/health", nil)
	if err != nil {
		return GatewayHealth{}, err
	}
	hc := &http.Client{Timeout: GatewayProbeTimeout, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
	start := time.Now()
	resp, err := hc.Do(req)
	if err != nil {
		return GatewayHealth{}, err
	}
	resp.Body.Close()
	h := GatewayHealth{Status: resp.StatusCode, Latency: time.Since(start)}
	h.At = start.Add(h.Latency / 2)
	if d, err := http.ParseTime(resp.Header.Get("Date")); err == nil {
		h.Date = d
	}
	return h, nil
}

// shortErr keeps the useful end of a transport error ("dial tcp …:
// connection refused" → "connection refused").
func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && i+2 < len(s) {
		s = s[i+2:]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
