package platformdomain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/publicconsole"
)

// Machine is what tells whether this machine can use a platform domain.
type Machine struct {
	// PublicHost is the address the server is reached at (--public-host).
	PublicHost string
	// Desktop: the server runs on a Mac or Windows computer.
	Desktop bool
	// ServerLAN is the server's address on the user's local network, ""
	// on a cloud or other public server (platform.ServerLANAddress). Nil
	// is "".
	ServerLAN func() string
}

// Config configures a Service. Path is required; every network access has
// a function that tests replace.
type Config struct {
	// Path is the file the platform domain is kept in
	// (<data directory>/platform-domain.json).
	Path    string
	Machine Machine
	// DetectIPs finds this machine's public addresses when PublicHost is a
	// name, not an address (cloud metadata, then public services). Nil
	// finds none.
	DetectIPs func(ctx context.Context) []string
	// Resolver and DNSServers: the dns check asks these public DNS servers
	// directly (a cache on this machine does not decide). Nil Resolver is
	// publicconsole.NetResolver; nil DNSServers are the Chinese ones when
	// China, the global ones otherwise.
	Resolver   publicconsole.Resolver
	DNSServers []string
	China      bool
	// Dial connects once to addr (host:port), for the ports check. Nil is a
	// TCP connection with a 3 second timeout.
	Dial func(ctx context.Context, addr string) error
	// CertProbe makes one TLS handshake with the entry proxy on this
	// machine for host and checks the certificate is valid for it. Nil
	// reports every certificate as not issued.
	CertProbe func(ctx context.Context, host string) error
	// ActiveHosts are the names under the platform domain on the entry
	// proxy's routes now (public apps that are running). Nil is none.
	ActiveHosts func(ctx context.Context) []string
	// HTTPPort and HTTPSPort are the entry proxy's (0: 80 and 443).
	HTTPPort, HTTPSPort int
	// OnChange is called after the platform domain was set, replaced or
	// cleared (the server hands the entry proxy its routes again). Nil does
	// nothing.
	OnChange func(ctx context.Context)
	// CertBudget is how long a certificate may take before the cert check
	// fails (0: 10 minutes, the same as for a domain an app binds).
	CertBudget time.Duration
	// CacheFor is how long View reuses the last checks (0: 1 minute).
	CacheFor time.Duration
	Now      func() time.Time
	// Label is the name the dns check looks up under the domain (nil: a
	// random one each time, so no cache answers for it).
	Label  func() string
	Logger *slog.Logger
}

// Service keeps the platform domain and checks it. Safe for concurrent use.
type Service struct {
	cfg Config
	log *slog.Logger

	mu        sync.Mutex
	saved     saved
	loadErr   error
	last      *View
	lastAt    time.Time
	firstFail map[string]time.Time // host -> first time its certificate check failed
	ips       []string             // detected public addresses
	ipsAt     time.Time
}

// New loads the platform domain from cfg.Path. A file that cannot be read
// leaves the machine without one; the error is in every View until it is
// set again.
func New(cfg Config) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.CertBudget <= 0 {
		cfg.CertBudget = 10 * time.Minute
	}
	if cfg.CacheFor <= 0 {
		cfg.CacheFor = time.Minute
	}
	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 80
	}
	if cfg.HTTPSPort == 0 {
		cfg.HTTPSPort = 443
	}
	if cfg.Resolver == nil {
		cfg.Resolver = publicconsole.NetResolver{}
	}
	if cfg.Dial == nil {
		cfg.Dial = dialTCP
	}
	if cfg.Label == nil {
		cfg.Label = randomLabel
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	s := &Service{cfg: cfg, log: log, firstFail: map[string]time.Time{}}
	s.saved, s.loadErr = load(cfg.Path)
	if s.loadErr != nil {
		log.Error("platform domain", "err", s.loadErr)
	}
	return s
}

// Base is the platform domain, "" when none is set.
func (s *Service) Base() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saved.Domain
}

// Check is one of the three checks.
type Check struct {
	Key string `json:"key"`
	OK  bool   `json:"ok"`
	// Waiting: not checked yet and not a fault (an earlier check did not
	// pass, no app is open to the internet yet, the certificate is still
	// being issued). OK is false.
	Waiting bool   `json:"waiting,omitempty"`
	Text    string `json:"text"`
	Code    string `json:"code,omitempty"` // set when it failed
}

// Problem is a fault in words: what, the details, what to do next.
type Problem struct {
	Code   string `json:"code"`
	Text   string `json:"text"`
	Detail string `json:"detail,omitempty"`
	Next   string `json:"next,omitempty"`
}

// View is the platform domain and its checks (the API's answer).
type View struct {
	Domain      string     `json:"domain"`
	State       string     `json:"state"`
	Supported   bool       `json:"supported"`
	Unsupported *Problem   `json:"unsupported,omitempty"`
	PublicIP    string     `json:"public_ip,omitempty"`
	Record      Record     `json:"record"`
	Checks      []Check    `json:"checks"`
	Problem     *Problem   `json:"problem,omitempty"`
	ConsoleHost string     `json:"console_host,omitempty"`
	SetAt       *time.Time `json:"set_at,omitempty"`
	CheckedAt   *time.Time `json:"checked_at,omitempty"`
	// Changes is the operation record: the last changes, newest first.
	Changes []Change `json:"changes,omitempty"`
}

// publicIP is this machine's public address, "" when it has none.
func (s *Service) publicIP(ctx context.Context) string {
	m := s.cfg.Machine
	if m.Desktop {
		return ""
	}
	if m.ServerLAN != nil && m.ServerLAN() != "" {
		return ""
	}
	h := strings.Trim(strings.TrimSpace(m.PublicHost), "[]")
	if a, err := netip.ParseAddr(h); err == nil {
		if isPublic(a) {
			return a.Unmap().String()
		}
		return ""
	}
	if s.cfg.DetectIPs == nil {
		return ""
	}
	s.mu.Lock()
	if len(s.ips) > 0 && s.cfg.Now().Sub(s.ipsAt) < 10*time.Minute {
		ip := s.ips[0]
		s.mu.Unlock()
		return ip
	}
	s.mu.Unlock()
	var found []string
	for _, ip := range s.cfg.DetectIPs(ctx) {
		if a, err := netip.ParseAddr(ip); err == nil && isPublic(a) {
			found = append(found, a.Unmap().String())
		}
	}
	if len(found) == 0 {
		return ""
	}
	s.mu.Lock()
	s.ips, s.ipsAt = found, s.cfg.Now()
	s.mu.Unlock()
	return found[0]
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

func isPublic(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsGlobalUnicast() && !a.IsPrivate() && !cgnat.Contains(a)
}

func unsupported() *Problem {
	return &Problem{Code: CodeNoPublicIP, Text: UnsupportedText, Next: unsupportedHint}
}

// View is the platform domain with its checks: the last ones when they are
// recent enough (Config.CacheFor) and force is false, otherwise checked now.
func (s *Service) View(ctx context.Context, force bool) View {
	s.mu.Lock()
	if !force && s.last != nil && s.last.Domain == s.saved.Domain && s.cfg.Now().Sub(s.lastAt) < s.cfg.CacheFor {
		v := *s.last
		s.mu.Unlock()
		return v
	}
	s.mu.Unlock()
	return s.check(ctx)
}

// check runs the checks now and keeps the result.
func (s *Service) check(ctx context.Context) View {
	s.mu.Lock()
	sv := s.saved
	loadErr := s.loadErr
	s.mu.Unlock()

	ip := s.publicIP(ctx)
	v := View{Domain: sv.Domain, Supported: ip != "", PublicIP: ip, Record: RecordFor(sv.Domain, ip), ConsoleHost: ConsoleHost(sv.Domain)}
	if len(sv.Changes) > 0 {
		for i := len(sv.Changes) - 1; i >= 0; i-- {
			v.Changes = append(v.Changes, sv.Changes[i])
		}
	}
	if !sv.SetAt.IsZero() {
		t := sv.SetAt
		v.SetAt = &t
	}
	if !v.Supported {
		v.Unsupported = unsupported()
	}
	switch {
	case loadErr != nil:
		v.State = StateProblem
		v.Problem = &Problem{Code: CodeInvalid, Text: "平台域名的设置文件读不了", Detail: loadErr.Error(),
			Next: "重新设一次平台域名（acornfox domain base set 域名）会覆盖这个文件"}
		v.Checks = []Check{}
	case sv.Domain == "":
		v.State = StateUnset
		v.Checks = []Check{}
	case !v.Supported:
		v.State = StateProblem
		v.Problem = v.Unsupported
		v.Checks = []Check{}
	default:
		var probs map[string]*Problem
		v.Checks, probs = s.runChecks(ctx, sv.Domain, ip)
		v.State = StateOK
		v.Problem = problemOf(v.Checks, probs)
		if v.Problem != nil {
			v.State = StateProblem
		}
	}
	now := s.cfg.Now()
	v.CheckedAt = &now
	s.mu.Lock()
	if s.saved.Domain == sv.Domain {
		cp := v
		s.last, s.lastAt = &cp, now
	}
	s.mu.Unlock()
	return v
}

// Check runs the checks now (POST /v1/platform/domain/check).
func (s *Service) Check(ctx context.Context) View { return s.check(ctx) }

// runChecks runs dns, ports and cert in order; a check after one that did
// not pass waits. probs has the detail and what to do for each failed one.
func (s *Service) runChecks(ctx context.Context, base, ip string) (checks []Check, probs map[string]*Problem) {
	rec := RecordFor(base, ip)
	probs = map[string]*Problem{}
	var out []Check

	// dns: a random name under the domain must point here, and only here.
	probe := s.cfg.Label() + "." + base
	servers := s.cfg.DNSServers
	if len(servers) == 0 {
		servers = publicconsole.DNSServersGlobal
		if s.cfg.China {
			servers = publicconsole.DNSServersCN
		}
	}
	want := publicconsole.PublicIPs{V4: []string{ip}}
	if rec.Type == "AAAA" {
		want = publicconsole.PublicIPs{V6: []string{ip}}
	}
	res := publicconsole.CheckDNS(ctx, s.cfg.Resolver, servers, probe, want)
	dnsOK := res.OK()
	if dnsOK {
		out = append(out, Check{Key: CheckDNS, OK: true, Text: "域名指过来了"})
	} else {
		p := &Problem{Code: CodeDNS, Text: "域名没有指到这台机器"}
		switch res.Code {
		case publicconsole.CodeDNSMismatch:
			p.Detail = fmt.Sprintf("现在指向 %s，应该指向 %s", strings.Join(res.Found, "、"), ip)
			p.Next = fmt.Sprintf("到你买域名的地方，把 %s 这条记录改成 %s（指向别处的同名记录删掉；开了 CDN 或代理先关掉），等几分钟再检查", rec.Name, ip)
		case publicconsole.CodeDNSError:
			p.Text = "查不了域名指向哪里"
			p.Detail = "公共 DNS 服务器都没有应答"
			p.Next = "检查这台服务器能不能上网，等一会儿再检查"
		default:
			p.Detail = fmt.Sprintf("还查不到 %s 下的名字（例如 %s）", base, probe)
			p.Next = fmt.Sprintf("到你买域名的地方加一条记录：名字 %s、类型 %s、指向 %s，等几分钟再检查", rec.Name, rec.Type, ip)
		}
		probs[CheckDNS] = p
		out = append(out, Check{Key: CheckDNS, Text: p.Text, Code: CodeDNS})
	}

	hosts := s.activeHosts(ctx, base)
	// ports: 80 and 443 of this machine's public address. The entry proxy
	// listens on them only while an app has a name, so before that there
	// is nothing to connect to.
	portsOK := false
	switch {
	case !dnsOK:
		out = append(out, Check{Key: CheckPorts, Waiting: true, Text: "等上一项"})
	case len(hosts) == 0:
		out = append(out, Check{Key: CheckPorts, Waiting: true, Text: "还没有开放到公网的应用，开放一个以后再检查"})
	default:
		var closed []string
		var details []string
		for _, port := range []int{s.cfg.HTTPPort, s.cfg.HTTPSPort} {
			addr := net.JoinHostPort(ip, strconv.Itoa(port))
			if err := s.cfg.Dial(ctx, addr); err != nil {
				closed = append(closed, strconv.Itoa(port))
				details = append(details, fmt.Sprintf("连 %s 不通（%s）", addr, dialReason(err)))
			}
		}
		if len(closed) == 0 {
			portsOK = true
			out = append(out, Check{Key: CheckPorts, OK: true, Text: "外面能连上"})
		} else {
			p := &Problem{Code: CodePorts, Text: "外面连不上 " + strings.Join(closed, " 和 "),
				Detail: strings.Join(details, "；"),
				Next: fmt.Sprintf("在云厂商控制台的安全组里放行入方向 TCP %d 和 %d（来源 0.0.0.0/0）；服务器自己的防火墙开着的也要放行。改好再检查",
					s.cfg.HTTPPort, s.cfg.HTTPSPort)}
			probs[CheckPorts] = p
			out = append(out, Check{Key: CheckPorts, Text: p.Text, Code: CodePorts})
		}
	}

	// cert: each name on the routes has a valid certificate.
	switch {
	case !dnsOK || (!portsOK && len(hosts) > 0):
		out = append(out, Check{Key: CheckCert, Waiting: true, Text: "等上一项"})
	case len(hosts) == 0:
		out = append(out, Check{Key: CheckCert, Waiting: true, Text: "开放一个应用到公网以后自动申请"})
	default:
		var pending, failed []string
		now := s.cfg.Now()
		for _, h := range hosts {
			err := errors.New("no probe")
			if s.cfg.CertProbe != nil {
				err = s.cfg.CertProbe(ctx, h)
			}
			s.mu.Lock()
			if err == nil {
				delete(s.firstFail, h)
				s.mu.Unlock()
				continue
			}
			first, ok := s.firstFail[h]
			if !ok {
				first = now
				s.firstFail[h] = now
			}
			s.mu.Unlock()
			if now.Sub(first) < s.cfg.CertBudget {
				pending = append(pending, h)
			} else {
				failed = append(failed, h)
			}
		}
		switch {
		case len(failed) > 0:
			p := &Problem{Code: CodeCert, Text: "证书没签下来",
				Detail: fmt.Sprintf("%s 的证书过了 %d 分钟还没签下来", strings.Join(failed, "、"), int(s.cfg.CertBudget.Minutes())),
				Next:   "中国大陆的服务器，域名要先备案；再看入口代理的日志（sudo docker logs --tail 50 acornfox-edge）。同一个主域名一周最多签 50 张证书，换来换去会被限"}
			probs[CheckCert] = p
			out = append(out, Check{Key: CheckCert, Text: p.Text, Code: CodeCert})
		case len(pending) > 0:
			out = append(out, Check{Key: CheckCert, Waiting: true, Text: "证书正在自动申请，几分钟后再检查"})
		default:
			out = append(out, Check{Key: CheckCert, OK: true, Text: "证书已自动申请"})
		}
	}
	return out, probs
}

// problemOf is the first check that failed, in words; nil when none did.
func problemOf(checks []Check, probs map[string]*Problem) *Problem {
	for _, c := range checks {
		if !c.OK && !c.Waiting {
			if p := probs[c.Key]; p != nil {
				return p
			}
			return &Problem{Code: c.Code, Text: c.Text}
		}
	}
	return nil
}

// activeHosts are the names under base on the entry proxy's routes now.
func (s *Service) activeHosts(ctx context.Context, base string) []string {
	if s.cfg.ActiveHosts == nil {
		return nil
	}
	var out []string
	for _, h := range s.cfg.ActiveHosts(ctx) {
		if strings.HasSuffix(h, "."+base) {
			out = append(out, h)
		}
	}
	sort.Strings(out)
	return out
}

// Plan is what Set or Clear would do.
type Plan struct {
	// Change is "set" (none before), "replace", "clear" or "none".
	Change string `json:"change"`
	From   string `json:"from"`
	To     string `json:"to"`
	// NeedsConfirm: replacing or clearing a platform domain that is set;
	// the request must carry confirm (PDOMAIN-07 otherwise).
	NeedsConfirm bool `json:"needs_confirm"`
	// Say is what will happen, in words.
	Say string `json:"say"`
}

// PlanSet is what Set(raw) would do; nothing is written.
func (s *Service) PlanSet(ctx context.Context, raw string) (Plan, error) {
	d, err := Normalize(raw)
	if err != nil {
		return Plan{}, err
	}
	if s.publicIP(ctx) == "" {
		return Plan{}, &Error{Status: http.StatusConflict, Code: CodeNoPublicIP, Message: UnsupportedText, Hint: unsupportedHint}
	}
	cur := s.Base()
	p := Plan{From: cur, To: d}
	switch {
	case cur == d:
		p.Change, p.Say = "none", "平台域名已经是 "+d+"，不会有变化"
	case cur == "":
		p.Change, p.Say = "set", fmt.Sprintf("设成 %s：开放到公网的应用会多一个地址 https://应用名.%s，原来带端口的地址照常能打开", d, d)
	default:
		p.Change, p.NeedsConfirm = "replace", true
		p.Say = fmt.Sprintf("把平台域名从 %s 换成 %s：所有应用在 %s 下的地址都会打不开，改用 %s 下的；装应用时写进应用里的旧地址不会跟着变", cur, d, cur, d)
	}
	return p, nil
}

// PlanClear is what Clear would do; nothing is written.
func (s *Service) PlanClear() Plan {
	cur := s.Base()
	if cur == "" {
		return Plan{Change: "none", Say: "还没有设平台域名，不会有变化"}
	}
	return Plan{Change: "clear", From: cur, NeedsConfirm: true,
		Say: fmt.Sprintf("清掉平台域名 %s：所有应用在 %s 下的地址都会打不开，应用回到原来带端口的地址（自己绑的域名不受影响）", cur, cur)}
}

func confirmError(p Plan) error {
	how := "acornfox domain base set " + p.To + " --yes"
	if p.Change == "clear" {
		how = "acornfox domain base clear --yes"
	}
	return &Error{Status: http.StatusConflict, Code: CodeConfirm,
		Message: p.Say + "。这一步要先确认",
		Hint:    "先问用户同意；同意后命令行加 --yes（" + how + "），接口请求体带 \"confirm\": true"}
}

// Set sets or replaces the platform domain. Replacing one that is set needs
// confirm (PDOMAIN-07); setting the same one again changes nothing. It
// returns the View checked right after.
func (s *Service) Set(ctx context.Context, raw string, confirm bool) (View, error) {
	p, err := s.PlanSet(ctx, raw)
	if err != nil {
		return View{}, err
	}
	if p.Change == "none" {
		return s.check(ctx), nil
	}
	if p.NeedsConfirm && !confirm {
		return View{}, confirmError(p)
	}
	if err := s.write(p.From, p.To); err != nil {
		return View{}, err
	}
	s.log.Info("platform domain set", "from", p.From, "to", p.To)
	if s.cfg.OnChange != nil {
		s.cfg.OnChange(ctx)
	}
	return s.check(ctx), nil
}

// Clear removes the platform domain (confirm required when one is set).
func (s *Service) Clear(ctx context.Context, confirm bool) (View, error) {
	p := s.PlanClear()
	if p.Change == "none" {
		return s.check(ctx), nil
	}
	if !confirm {
		return View{}, confirmError(p)
	}
	if err := s.write(p.From, ""); err != nil {
		return View{}, err
	}
	s.log.Info("platform domain cleared", "from", p.From)
	if s.cfg.OnChange != nil {
		s.cfg.OnChange(ctx)
	}
	return s.check(ctx), nil
}

func (s *Service) write(from, to string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.cfg.Now().UTC()
	next := saved{Domain: to, Changes: append(append([]Change(nil), s.saved.Changes...), Change{At: now, From: from, To: to})}
	if to != "" {
		next.SetAt = now
	}
	if len(next.Changes) > maxChanges {
		next.Changes = next.Changes[len(next.Changes)-maxChanges:]
	}
	if err := store(s.cfg.Path, next); err != nil {
		return &Error{Status: http.StatusInternalServerError, Code: "internal", Message: err.Error()}
	}
	s.saved, s.loadErr = next, nil
	s.last = nil
	s.firstFail = map[string]time.Time{}
	return nil
}

func dialTCP(ctx context.Context, addr string) error {
	d := net.Dialer{Timeout: 3 * time.Second}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return c.Close()
}

// dialReason says in words why a connection failed.
func dialReason(err error) string {
	var ne net.Error
	switch {
	case errors.As(err, &ne) && ne.Timeout():
		return "超时，多半是安全组或防火墙没放行"
	case strings.Contains(strings.ToLower(err.Error()), "refused"):
		return "被拒绝，这个端口上没有程序在听"
	default:
		return err.Error()
	}
}

func randomLabel() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "afx-check-" + hex.EncodeToString(b)
}
