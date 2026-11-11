package publicconsole

// The LAN side of the gateway: what makes the local-network entry relaxed
// compared with the public one.
//
//   - Only clients whose address is on a local network are served
//     (LANSource); a port forwarded to the internet by mistake answers 403
//     with LAN-CONSOLE-02.
//   - While no password exists the login page is a "set a password" form;
//     the first visitor sets it (POST /setup). When and from which address is
//     recorded so the owner can tell it was them.
//   - "Remember this device" stores a second, long-lived cookie (af_device,
//     90 days). A page load with an expired session and a valid device cookie
//     gets a fresh session without a password.
//
// Forgotten password or a password someone else set: on the server run
// `acornfox lan-console reset-password`; it clears the password, every
// session and every remembered device, and the page shows the form again.

import (
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"
)

const deviceCookie = "af_device"

// lanCGNAT is 100.64.0.0/10 (carrier-grade NAT; also what Tailscale assigns).
var lanCGNAT = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// LANSource reports whether a client address belongs to a local network:
// loopback, RFC 1918 and IPv6 unique-local, link-local, or 100.64.0.0/10.
// Anything else (a public address, or text that is not an address) is not.
func LANSource(addr string) bool {
	if i := strings.IndexByte(addr, '%'); i >= 0 { // fe80::1%eth0
		addr = addr[:i]
	}
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || lanCGNAT.Contains(ip)
}

func (g *gateway) refuseSource(w http.ResponseWriter, ip string) {
	g.cfg.Logger.Warn("lan console: refused a client that is not on the local network", "client", ip, "code", "LAN-CONSOLE-02")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	fmt.Fprintf(w, "这个入口只给局域网里的电脑用，你的地址 %s 不在局域网里（故障编号 LAN-CONSOLE-02）。\n从别的网络进控制台：在自己电脑上运行 acornfox open（经 SSH 隧道）。\n", ip)
}

func (g *gateway) setupPage(w http.ResponseWriter, status int, msg string) {
	g.renderLogin(w, status, loginView{
		Msg: msg, Setup: true, LAN: true,
		Sub:  "这个控制台还没有密码。现在设一个，以后在局域网里用它登录。",
		Note: "只在可信的网络里用：局域网里不加密，同一网络里的人能看到传输的内容。",
	})
}

// setup handles POST /setup: the first visitor sets the password. The caller
// has checked that none exists; the write checks again under the store lock,
// so of two visitors submitting at once exactly one wins.
func (g *gateway) setup(w http.ResponseWriter, r *http.Request) {
	ip := hostOnly(r.RemoteAddr)
	if ok, wait := g.cfg.Limiter.Allow(ip); !ok {
		g.setupPage(w, http.StatusTooManyRequests, fmt.Sprintf("尝试次数过多，请 %d 分钟后再试。", int(wait.Minutes()+0.999)))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		g.setupPage(w, http.StatusBadRequest, "请求无效，请重试。")
		return
	}
	password := r.PostFormValue("password")
	switch {
	case utf8.RuneCountInString(password) < MinPasswordLen:
		g.setupPage(w, http.StatusBadRequest, fmt.Sprintf("密码至少 %d 位。", MinPasswordLen))
		return
	case len(password) > MaxPasswordLen:
		g.setupPage(w, http.StatusBadRequest, fmt.Sprintf("密码太长，最多 %d 个字符。", MaxPasswordLen))
		return
	case password != r.PostFormValue("confirm"):
		g.setupPage(w, http.StatusBadRequest, "两次输入的密码不一样。")
		return
	}
	hash, err := HashPassword(password)
	if err != nil {
		g.setupPage(w, http.StatusInternalServerError, "没能保存密码，请重试。")
		return
	}
	done := false
	now := g.cfg.Now().UTC()
	err = g.cfg.Store.Update(func(c *Config) {
		if c.HasPassword() {
			return
		}
		c.PasswordHash, c.PasswordSetAt, c.PasswordSetFrom = hash, &now, ip
		done = true
	})
	if err != nil {
		g.cfg.Logger.Error("lan console: save password", "err", err)
		g.setupPage(w, http.StatusInternalServerError, "没能保存密码，请重试。")
		return
	}
	if !done {
		// Someone else set it first (or the LAN console was closed meanwhile).
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	g.cfg.Logger.Info("lan console: password set", "client", ip)

	deviceID := ""
	if r.PostFormValue("remember") == "1" {
		if deviceID, err = g.rememberDevice(w, r, ip); err != nil {
			g.cfg.Logger.Error("lan console: remember device", "err", err)
			deviceID = ""
		}
	}
	if err := g.issueSession(w, r, deviceID); err != nil {
		g.cfg.Logger.Error("lan console: issue session", "err", err)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// rememberDevice records this browser for DeviceRemember and sets its cookie.
// Expired devices are dropped; past MaxDevices the least recently used go.
func (g *gateway) rememberDevice(w http.ResponseWriter, r *http.Request, ip string) (string, error) {
	secret, err := randomHex(32)
	if err != nil {
		return "", err
	}
	id, err := randomHex(6)
	if err != nil {
		return "", err
	}
	now := g.cfg.Now().UTC()
	d := Device{ID: id, Digest: Digest(secret), Name: DeviceName(r.UserAgent()), CreatedAt: now, LastUsedAt: now, LastIP: ip, ExpiresAt: now.Add(DeviceRemember)}
	saved := false
	err = g.cfg.Store.Update(func(c *Config) {
		var expired []string
		for _, x := range c.Devices {
			if !now.Before(x.ExpiresAt) {
				expired = append(expired, x.ID)
			}
		}
		for _, x := range expired {
			c.RemoveDevice(x)
		}
		for len(c.Devices) >= MaxDevices {
			sort.SliceStable(c.Devices, func(i, j int) bool { return c.Devices[i].LastUsedAt.Before(c.Devices[j].LastUsedAt) })
			c.RemoveDevice(c.Devices[0].ID)
		}
		c.Devices = append(c.Devices, d)
		saved = true
	})
	if err != nil {
		return "", err
	}
	if !saved {
		return "", fmt.Errorf("局域网控制台已关闭")
	}
	http.SetCookie(w, &http.Cookie{
		Name: deviceCookie, Value: secret, Path: "/", Expires: d.ExpiresAt, MaxAge: int(DeviceRemember.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	return id, nil
}

func (g *gateway) clearDeviceCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: deviceCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

// renewFromDevice gives a remembered browser a fresh session and sends it
// back to the page it asked for. It reports whether it answered the request.
func (g *gateway) renewFromDevice(w http.ResponseWriter, r *http.Request, cfg *Config) bool {
	dc, err := r.Cookie(deviceCookie)
	if err != nil || dc.Value == "" {
		return false
	}
	now := g.cfg.Now().UTC()
	d := cfg.FindDevice(Digest(dc.Value), now)
	if d == nil {
		g.clearDeviceCookie(w)
		return false
	}
	id, ip := d.ID, hostOnly(r.RemoteAddr)
	if err := g.issueSession(w, r, id); err != nil {
		g.cfg.Logger.Error("lan console: renew session for a remembered device", "err", err)
		return false
	}
	_ = g.cfg.Store.Update(func(c *Config) {
		for i := range c.Devices {
			if c.Devices[i].ID == id {
				c.Devices[i].LastUsedAt, c.Devices[i].LastIP = now, ip
			}
		}
	})
	http.Redirect(w, r, r.URL.RequestURI(), http.StatusSeeOther)
	return true
}

// DeviceName names a browser for the remembered-devices list from its
// User-Agent: "<system> · <browser>", e.g. "Mac · Chrome". Unknown parts are
// left out; nothing recognisable is "未知设备".
func DeviceName(ua string) string {
	pick := func(pairs [][2]string) string {
		for _, p := range pairs {
			if strings.Contains(ua, p[0]) {
				return p[1]
			}
		}
		return ""
	}
	// Order matters: iPhone and Android UAs also contain "Mac OS X" / "Linux".
	sys := pick([][2]string{{"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Android", "Android"}, {"Windows", "Windows"}, {"Mac OS X", "Mac"}, {"Macintosh", "Mac"}, {"CrOS", "ChromeOS"}, {"Linux", "Linux"}})
	// Edge and others include "Chrome" and "Safari"; Chrome includes "Safari".
	br := pick([][2]string{{"Edg/", "Edge"}, {"EdgA/", "Edge"}, {"OPR/", "Opera"}, {"Firefox/", "Firefox"}, {"FxiOS/", "Firefox"}, {"MicroMessenger", "微信"}, {"CriOS/", "Chrome"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"}})
	switch {
	case sys != "" && br != "":
		return sys + " · " + br
	case sys != "":
		return sys
	case br != "":
		return br
	}
	return "未知设备"
}
