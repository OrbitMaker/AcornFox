package edgeadapter

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/acornfox/acornfox/internal/edge"
)

// maxRoutes bounds one configuration (the app port range is 90 ports).
const maxRoutes = 1000

// Validate refuses a configuration the proxy could not serve as written. It
// checks shape only; whether a port is free is the proxy's answer to Apply.
func Validate(cfg edge.Config) error {
	if len(cfg.Routes) > maxRoutes {
		return fmt.Errorf("路由太多（%d 条，最多 %d）", len(cfg.Routes), maxRoutes)
	}
	h := cfg.HTTPS
	if err := checkPort("HTTP 端口", h.HTTPPort, true); err != nil {
		return err
	}
	if err := checkPort("HTTPS 端口", h.HTTPSPort, true); err != nil {
		return err
	}
	switch h.Issuer {
	case "", edge.IssuerACME, edge.IssuerInternal:
	default:
		return fmt.Errorf("不认识的证书签发方式 %q", h.Issuer)
	}
	if len(h.Email) > 254 || strings.ContainsAny(h.Email, " \t\r\n") {
		return errors.New("证书账号邮箱不合法")
	}
	hd := h.WithDefaults()
	apps := map[string]bool{}
	ports := map[int]string{hd.HTTPPort: "HTTP 端口", hd.HTTPSPort: "HTTPS 端口"}
	usePort := func(p int, who string) error {
		if other, ok := ports[p]; ok {
			return fmt.Errorf("端口 %d 同时给了%s和%s", p, other, who)
		}
		ports[p] = who
		return nil
	}
	for _, r := range cfg.Routes {
		if !validName(r.App) {
			return fmt.Errorf("路由名 %q 不合法", r.App)
		}
		if apps[r.App] {
			return fmt.Errorf("路由名 %q 重复", r.App)
		}
		apps[r.App] = true
		if err := checkPort("应用 "+r.App+" 的端口", r.PublicPort, true); err != nil {
			return err
		}
		if err := checkPort("应用 "+r.App+" 的局域网端口", r.LANPort, true); err != nil {
			return err
		}
		if r.PublicPort == 0 {
			if len(r.Domains) == 0 {
				return fmt.Errorf("应用 %s 既没有端口也没有域名", r.App)
			}
			if r.LANPort != 0 || r.Listen != "" {
				return fmt.Errorf("应用 %s 只走域名，不能再写局域网端口或绑定地址", r.App)
			}
		} else {
			if err := usePort(r.PublicPort, "应用 "+r.App); err != nil {
				return err
			}
			if r.LANPort != 0 {
				if err := usePort(r.LANPort, "应用 "+r.App+" 的局域网端口"); err != nil {
					return err
				}
			}
		}
		if r.Listen != "" {
			if _, err := netip.ParseAddr(r.Listen); err != nil {
				return fmt.Errorf("应用 %s 的绑定地址 %q 不合法", r.App, r.Listen)
			}
		}
		host, port, err := net.SplitHostPort(r.Upstream)
		if err != nil || host == "" || strings.ContainsAny(host, " /") {
			return fmt.Errorf("应用 %s 的转发目标 %q 不合法", r.App, r.Upstream)
		}
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("应用 %s 的转发目标 %q 端口不合法", r.App, r.Upstream)
		}
		for _, d := range r.Domains {
			if !validDomain(d) {
				return fmt.Errorf("应用 %s 的域名 %q 不合法", r.App, d)
			}
		}
	}
	return nil
}

func checkPort(what string, p int, zeroOK bool) error {
	if p == 0 && zeroOK {
		return nil
	}
	if p < 1 || p > 65535 {
		return fmt.Errorf("%s %d 不合法", what, p)
	}
	return nil
}

// validName accepts app names and the program's own routes ("_public-console").
func validName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

func validDomain(d string) bool {
	if d == "" || len(d) > 253 {
		return false
	}
	for _, c := range d {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '.', c == '*':
		default:
			return false
		}
	}
	return true
}
