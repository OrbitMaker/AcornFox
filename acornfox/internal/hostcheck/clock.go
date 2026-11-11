package hostcheck

// The clock check (fault SETUP-CLOCK-01), shared by `acornfox doctor` and the
// console's runtime page.
//
// Why it matters: the download gateway refuses a request whose timestamp is
// more than 300 seconds away from its own time (GW-AUTH-01). On a machine
// with a wrong clock, public apps then quietly fall back to public image
// sources and private catalogs cannot connect at all.
//
// What it looks at, each part skipped when it cannot be found out:
//
//   - whether network time sync is on and the clock is synchronized:
//     `timedatectl show` (systemd 239 and later), else `timedatectl status`,
//     else the file /run/systemd/timesync/synchronized (systemd-timesyncd);
//   - when the download gateway is on (the clock that actually matters), the
//     Date header of one HEAD request to its /v1/health compared with this
//     machine's clock. `acornfox doctor` and the runtime page build the
//     readers with DefaultGatewayProbes (appcenter.go): the clock check and
//     the gateway check (SETUP-GW-01) share that one request, at most 5
//     seconds in all. No network, no gateway or no Date header: the
//     comparison is skipped, never an error. The Date header counts whole
//     seconds, so the difference is known to about a second.
//
// Thresholds: 300 seconds or more off fails (the gateway refuses); 60 seconds
// or more warns; sync switched off or not yet synchronized warns. It never
// changes the system clock or its settings; fixing it is left to the user
// (sudo timedatectl set-ntp true).

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Clock thresholds.
const (
	ClockFailSkew = 300 * time.Second // the gateway's allowed window
	ClockWarnSkew = 60 * time.Second
	// ClockProbeTimeout bounds timedatectl, and the reference when
	// ClockProbe.ReferenceTimeout is not set.
	ClockProbeTimeout = 3 * time.Second
)

// SyncStatus is what is known about network time sync.
type SyncStatus struct {
	NTPKnown     bool // NTP was found out
	NTP          bool // network time sync is switched on
	Synchronized bool // the clock is synchronized (kernel or timesyncd says so)
	SyncKnown    bool
}

// ClockProbe holds how the clock check reads its inputs (tests replace them).
type ClockProbe struct {
	Now  func() time.Time
	Sync func(ctx context.Context) (SyncStatus, error)
	// Reference returns a trusted current time (nil: no comparison).
	Reference func(ctx context.Context) (time.Time, error)
	// ReferenceTimeout bounds Reference (0: ClockProbeTimeout).
	ReferenceTimeout time.Duration
}

// DefaultClockProbe reads timedatectl and, when refURL is not empty, the
// Date header of a HEAD request of its own to refURL/v1/health. doctor and
// the runtime page use DefaultGatewayProbes instead, which shares the request
// with the gateway check.
func DefaultClockProbe(refURL string) ClockProbe {
	p := ClockProbe{Now: time.Now, Sync: readSyncStatus}
	if refURL = strings.TrimSpace(refURL); refURL != "" {
		p.Reference = func(ctx context.Context) (time.Time, error) { return headDate(ctx, refURL) }
	}
	return p
}

// CheckClock adds one line about the clock.
func CheckClock(ctx context.Context, r Reporter, p ClockProbe) {
	if p.Now == nil {
		p.Now = time.Now
	}
	var st SyncStatus
	if p.Sync != nil {
		sctx, cancel := context.WithTimeout(ctx, ClockProbeTimeout)
		st, _ = p.Sync(sctx)
		cancel()
	}
	var skew time.Duration
	compared := false
	if p.Reference != nil {
		limit := p.ReferenceTimeout
		if limit <= 0 {
			limit = ClockProbeTimeout
		}
		rctx, cancel := context.WithTimeout(ctx, limit)
		start := p.Now()
		ref, err := p.Reference(rctx)
		end := p.Now()
		cancel()
		if err == nil && !ref.IsZero() {
			// The answer's Date is somewhere between start and end.
			mid := start.Add(end.Sub(start) / 2)
			skew, compared = ref.Sub(mid), true
		}
	}
	abs := skew
	if abs < 0 {
		abs = -abs
	}
	secs := int64(abs / time.Second)
	dir := "快"
	if skew > 0 {
		dir = "慢"
	}
	syncText := ""
	switch {
	case st.SyncKnown && st.Synchronized:
		syncText = "已同步"
	case st.NTPKnown && !st.NTP:
		syncText = "没开网络对时"
	case st.NTPKnown && st.NTP:
		syncText = "开了网络对时，但还没同步上"
	case st.SyncKnown:
		syncText = "还没同步上"
	}
	const fix = "运行 sudo timedatectl set-ntp true 打开网络对时，过一两分钟用 timedatectl 看是否已同步"
	switch {
	case compared && abs >= ClockFailSkew:
		r.Add(false, "SETUP-CLOCK-01", "时钟：比下载网关%s了约 %d 秒（超过 300 秒），下载网关会拒绝这台机器，应用中心改走公共源、私有目录连不上。%s", dir, secs, fix)
	case compared && abs >= ClockWarnSkew:
		r.Warn("SETUP-CLOCK-01", "时钟：比下载网关%s了约 %d 秒，再差到 300 秒下载网关就会拒绝。%s", dir, secs, fix)
	case syncText == "没开网络对时":
		r.Warn("SETUP-CLOCK-01", "时钟：%s%s，时间会慢慢走偏。%s", syncText, comparedText(compared, secs), fix)
	case syncText == "开了网络对时，但还没同步上" || syncText == "还没同步上":
		r.Warn("SETUP-CLOCK-01", "时钟：%s%s（刚开机时正常，过几分钟再看；一直同步不上多半是连不上时间服务器）", syncText, comparedText(compared, secs))
	case syncText == "已同步" || compared:
		s := "时钟：" + syncText
		if syncText == "" {
			s += "查不到对时状态"
		}
		r.Add(true, "SETUP-CLOCK-01", "%s%s", s, comparedText(compared, secs))
	default:
		r.Add(true, "SETUP-CLOCK-01", "时钟：查不到对时状态，也没能和下载网关比对时间（没联网或关了下载网关），跳过")
	}
}

func comparedText(compared bool, secs int64) string {
	if !compared {
		return ""
	}
	return fmt.Sprintf("，和下载网关相差 %d 秒", secs)
}

// timedatectl runs timedatectl with args (replaced by tests).
var timedatectl = func(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "timedatectl", args...).Output()
}

// timesyncFile exists while systemd-timesyncd has synchronized the clock.
var timesyncFile = "/run/systemd/timesync/synchronized"

// readSyncStatus asks timedatectl, falling back to timesyncd's file.
func readSyncStatus(ctx context.Context) (SyncStatus, error) {
	if out, err := timedatectl(ctx, "show", "--property=NTP", "--property=NTPSynchronized"); err == nil {
		if st, ok := parseTimedatectl(out); ok {
			return st, nil
		}
	}
	if out, err := timedatectl(ctx, "status"); err == nil {
		if st, ok := parseTimedatectl(out); ok {
			return st, nil
		}
	}
	if _, err := os.Stat(timesyncFile); err == nil {
		return SyncStatus{SyncKnown: true, Synchronized: true}, nil
	}
	return SyncStatus{}, errors.New("查不到对时状态")
}

// parseTimedatectl reads `timedatectl show` (NTP=yes, NTPSynchronized=yes)
// or `timedatectl status` ("NTP service: active", "System clock
// synchronized: yes"; older systemd: "NTP enabled: yes", "NTP synchronized:
// yes").
func parseTimedatectl(out []byte) (SyncStatus, bool) {
	var st SyncStatus
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			k, v, ok = strings.Cut(line, ":")
		}
		if !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.ToLower(strings.TrimSpace(v))
		yes := v == "yes" || v == "active"
		known := yes || v == "no" || v == "inactive" || v == "n/a"
		if !known {
			continue
		}
		switch k {
		case "ntp", "ntp service", "ntp enabled", "network time on":
			st.NTPKnown, st.NTP = true, yes
		case "ntpsynchronized", "system clock synchronized", "ntp synchronized":
			st.SyncKnown, st.Synchronized = true, yes
		}
	}
	return st, st.NTPKnown || st.SyncKnown
}

// headDate sends one HEAD request to base (the gateway's /v1/health) and
// returns the answer's Date header.
func headDate(ctx context.Context, base string) (time.Time, error) {
	u := strings.TrimRight(base, "/") + "/v1/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return time.Time{}, err
	}
	hc := &http.Client{Timeout: ClockProbeTimeout, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
	resp, err := hc.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	resp.Body.Close()
	d := resp.Header.Get("Date")
	if d == "" {
		return time.Time{}, errors.New("应答没有 Date")
	}
	return http.ParseTime(d)
}
