package publicconsole

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/state"
)

const sessionCookie = "af_session" // shared with the native console

// GatewayConfig wires the public console gateway.
type GatewayConfig struct {
	Store    *FileStore
	Sessions Sessions
	Console  http.Handler     // the native console handler (apiserver.NewConsole)
	Limiter  *Limiter         // nil => NewLimiter(nil)
	Now      func() time.Time // nil => time.Now
	Logger   *slog.Logger     // nil => slog.Default()

	// LAN serves the console on the local network over plain HTTP instead of
	// behind Caddy on a domain: any Host is accepted, the Origin must match
	// the Host, the client address is the TCP peer (no proxy in front) and
	// cookies are not marked Secure. The LAN entry is the relaxed one (see
	// lan_gateway.go): only clients with a local-network address are served,
	// the first visitor sets the password in the browser, and a browser can
	// be remembered for 90 days.
	LAN bool
}

// NewGateway returns the handler Caddy reverse-proxies the public domain to.
// It answers only for the configured domain, logs users in with the generated
// password (rate limited), accepts only sessions it issued itself, requires an
// exact HTTPS Origin on state-changing requests, and hands everything else to
// the native console with a loopback Host and Secure cookies.
func NewGateway(cfg GatewayConfig) http.Handler {
	if cfg.Limiter == nil {
		cfg.Limiter = NewLimiter(nil)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &gateway{cfg: cfg}
}

type gateway struct{ cfg GatewayConfig }

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	// same-origin, not no-referrer: under no-referrer browsers send
	// "Origin: null" with form posts, and the login form would be refused by
	// the Origin check below. Cross-site requests still get no referrer.
	h.Set("Referrer-Policy", "same-origin")

	cfg, err := g.cfg.Store.Load()
	if err != nil {
		g.cfg.Logger.Error("public console: load config", "err", err)
		http.Error(w, "内部错误", http.StatusInternalServerError)
		return
	}
	if cfg == nil {
		http.Error(w, "公网访问未开启", http.StatusNotFound)
		return
	}
	origin := "https://" + cfg.Domain
	if g.cfg.LAN {
		if ip := hostOnly(r.RemoteAddr); !LANSource(ip) {
			g.refuseSource(w, ip)
			return
		}
		origin = "http://" + r.Host
	} else {
		if !strings.EqualFold(hostOnly(r.Host), cfg.Domain) {
			http.Error(w, "域名不匹配", http.StatusMisdirectedRequest)
			return
		}
		h.Set("Strict-Transport-Security", "max-age=31536000")
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if origins := r.Header.Values("Origin"); len(origins) != 1 || origins[0] != origin {
			http.Error(w, "来源不被允许", http.StatusForbidden)
			return
		}
	}

	switch r.URL.Path {
	case "/login":
		if g.cfg.LAN && !cfg.HasPassword() {
			// Nothing to log in with yet: the first visitor sets the password.
			if r.Method == http.MethodPost {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			g.setupPage(w, http.StatusOK, "")
			return
		}
		if r.Method == http.MethodPost {
			g.login(w, r, cfg)
			return
		}
		g.loginPage(w, http.StatusOK, "", cfg)
		return
	case "/setup":
		if !g.cfg.LAN {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost || cfg.HasPassword() {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		g.setup(w, r)
		return
	case "/login.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write([]byte(loginCSS))
		return
	case "/login.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write([]byte(loginJS))
		return
	case "/console/login", "/v1/console/tokens":
		// Token login belongs to `acornfox open` over SSH only.
		http.NotFound(w, r)
		return
	}

	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" || !cfg.HasSession(Digest(c.Value)) {
		// A remembered device gets a fresh session on a page load. API calls
		// are not renewed here: the page holds the old session's CSRF token,
		// so it must reload (which lands on the branch above).
		if g.cfg.LAN && r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/v1/") && g.renewFromDevice(w, r, cfg) {
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			writeJSONError(w, http.StatusUnauthorized, "session_expired", "登录已过期，请刷新页面重新登录")
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	out := r.Clone(r.Context())
	out.Host = "127.0.0.1"
	for _, k := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded"} {
		out.Header.Del(k)
	}
	logout := r.Method == http.MethodPost && r.URL.Path == "/v1/console/logout"
	if g.cfg.LAN {
		if logout {
			// Logging out also forgets this device; the cookie has to go out
			// before the console writes its response.
			g.clearDeviceCookie(w)
		}
		g.cfg.Console.ServeHTTP(w, out)
	} else {
		g.cfg.Console.ServeHTTP(&secureCookieWriter{ResponseWriter: w}, out)
	}

	if logout {
		g.forget(Digest(c.Value))
	}
}

func (g *gateway) login(w http.ResponseWriter, r *http.Request, cfg *Config) {
	ip := clientIP(r)
	if g.cfg.LAN {
		// Nothing proxies the LAN listener: X-Forwarded-For is client input.
		ip = hostOnly(r.RemoteAddr)
	}
	if ok, wait := g.cfg.Limiter.Allow(ip); !ok {
		mins := int(wait.Minutes() + 0.999)
		g.loginPage(w, http.StatusTooManyRequests, fmt.Sprintf("尝试次数过多，请 %d 分钟后再试。", mins), cfg)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		g.loginPage(w, http.StatusBadRequest, "请求无效，请重试。", cfg)
		return
	}
	if !cfg.CheckPassword(r.PostFormValue("password")) {
		g.cfg.Limiter.Fail(ip)
		g.cfg.Logger.Warn("public console: login failed", "client", ip, "lan", g.cfg.LAN)
		g.loginPage(w, http.StatusUnauthorized, "密码不对。", cfg)
		return
	}
	g.cfg.Limiter.Success(ip)

	deviceID := ""
	if g.cfg.LAN && r.PostFormValue("remember") == "1" {
		id, err := g.rememberDevice(w, r, ip)
		if err != nil {
			g.cfg.Logger.Error("lan console: remember device", "err", err)
			g.loginPage(w, http.StatusInternalServerError, "登录失败，请重试。", cfg)
			return
		}
		deviceID = id
	}
	if err := g.issueSession(w, r, deviceID); err != nil {
		g.cfg.Logger.Error("public console: issue session", "err", err)
		g.loginPage(w, http.StatusInternalServerError, "登录失败，请重试。", cfg)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// issueSession creates a console session, records it as issued by this
// gateway (for deviceID when the browser is remembered) and sets its cookie.
func (g *gateway) issueSession(w http.ResponseWriter, r *http.Request, deviceID string) error {
	ctx := r.Context()
	token, err := g.cfg.Sessions.CreateConsoleToken(ctx)
	if err != nil {
		return err
	}
	_, secret, _, err := g.cfg.Sessions.RedeemConsoleToken(ctx, token)
	if err != nil {
		return err
	}
	if err := g.remember(Digest(secret), deviceID); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: secret, Path: "/",
		Secure: !g.cfg.LAN, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	return nil
}

// remember records an issued session and drops ones past their lifetime.
func (g *gateway) remember(digest, deviceID string) error {
	now := g.cfg.Now().UTC()
	return g.cfg.Store.Update(func(c *Config) {
		kept := c.Sessions[:0]
		for _, s := range c.Sessions {
			if now.Sub(s.IssuedAt) < state.ConsoleSessionAbsolute {
				kept = append(kept, s)
			}
		}
		c.Sessions = append(kept, IssuedSession{Digest: digest, IssuedAt: now, DeviceID: deviceID})
	})
}

// forget drops a session at logout and, when the browser was remembered, the
// remembered device with it (logging out means "ask me again").
func (g *gateway) forget(digest string) {
	_ = g.cfg.Store.Update(func(c *Config) {
		device := ""
		kept := c.Sessions[:0]
		for _, s := range c.Sessions {
			if s.Digest != digest {
				kept = append(kept, s)
			} else {
				device = s.DeviceID
			}
		}
		c.Sessions = kept
		if device != "" {
			c.RemoveDevice(device)
		}
	})
}

// clientIP is the visitor's address: Caddy, connecting over loopback, sets
// X-Forwarded-For to the real client and drops what clients sent themselves.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	return host
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

// secureCookieWriter marks the console session cookie Secure on the way out.
type secureCookieWriter struct {
	http.ResponseWriter
	done bool
}

func (s *secureCookieWriter) fix() {
	if s.done {
		return
	}
	s.done = true
	h := s.Header()
	values := h.Values("Set-Cookie")
	for i, raw := range values {
		if c, err := http.ParseSetCookie(raw); err == nil && c.Name == sessionCookie {
			c.Secure = true
			values[i] = c.String()
		}
	}
	if len(values) > 0 {
		h["Set-Cookie"] = values
	}
}

func (s *secureCookieWriter) WriteHeader(code int) { s.fix(); s.ResponseWriter.WriteHeader(code) }
func (s *secureCookieWriter) Write(b []byte) (int, error) {
	s.fix()
	return s.ResponseWriter.Write(b)
}
func (s *secureCookieWriter) Flush() {
	s.fix()
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func writeJSONError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	body.Error.Code, body.Error.Message = code, msg
	_ = json.NewEncoder(w).Encode(body)
}
