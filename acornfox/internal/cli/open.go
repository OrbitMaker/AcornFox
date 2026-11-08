package cli

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/acornfox/acornfox/internal/client"
)

// openUsage is `acornfox open -h`.
const openUsage = `用法：acornfox open [应用名] [--no-browser] [--port N]

用途：
  不带应用名：在浏览器里打开网页控制台。经 SSH 登记的服务器会开一条 SSH 隧道，按 Ctrl+C 关闭。
  带应用名：在你自己的电脑上打开这个应用。只本机能打开的应用（对外方式 local）在你电脑上点
    127.0.0.1 的地址是打不开的，这个命令像打开控制台一样开一条 SSH 隧道，把应用转到你电脑的
    一个本地端口，再打开浏览器；看完按 Ctrl+C 关闭隧道。不用放行安全组，也不改应用的对外方式。
    - 服务器是经 SSH 登记的（target add --ssh）：总是走隧道，任何对外方式都能用。
    - 这台电脑自己的 AcornFox：直接打开 http://127.0.0.1:端口。
    - 服务器是经局域网控制台或公网控制台的网址登记的：应用已是局域网或公网时打开它的地址；
      只本机的应用打不开（OPEN-APP-02），改用 SSH 登记这台服务器，或经用户同意改成局域网。
参数：
  应用名         也可以用全局参数 --app 应用名 给出
  --no-browser   不自动打开浏览器，只打印地址
  --port N       本地转发端口（默认自动挑一个空闲端口）
副作用：只在你的电脑上开一个本地端口和一条 SSH 连接，不改服务器上的任何东西。
出错编号：
  OPEN-APP-01  应用还没有访问端口（没部署成功，或只在容器网络里）：acornfox status 应用名 看状态
  OPEN-APP-02  经网址登记的服务器上，只本机的应用打不开：用 acornfox target add 名称 --ssh 用户@主机
               登记后再试；或经用户同意改成局域网：acornfox app set --app 应用名 --expose lan
  连不上服务器、SSH 隧道建不起来时，输出里是连接诊断（和 acornfox open 打开控制台时一样）。
例子：
  acornfox open                 打开控制台
  acornfox open new-api         在自己电脑上打开 New API（看完按 Ctrl+C）
  acornfox open hermes-agent --port 9119
  acornfox --json open new-api --no-browser`

// Fault codes of acornfox open APP (listed in openUsage and the Skill).
const (
	codeOpenNoPort    = "OPEN-APP-01" // the app has no host port yet
	codeOpenLocalOnly = "OPEN-APP-02" // URL target, local-only app
)

// cmdOpen implements `open [APP] [--no-browser] [--port N]`. Without an app
// it fetches a single-use console token over the normal (proxy) connection,
// closes it, opens an SSH tunnel to the server's console listener (or uses
// the target URL directly in url mode), launches the browser at the login
// URL and blocks until Ctrl+C (context cancel) or the tunnel dies. See N3
// contract section 5 / 1.1. With an app it opens that app (cmdOpenApp).
func (a *app) cmdOpen(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(a.out.stderr)
	fs.Usage = func() { fmt.Fprintln(a.out.stdout, openUsage) }
	var (
		noBrowser = fs.Bool("no-browser", false, "不自动打开浏览器，只打印链接")
		port      = fs.Int("port", 0, "本地转发端口（0 为自动挑选）")
	)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) > 1 {
		return a.out.usageError("open 最多接一个应用名：acornfox open [应用名]")
	}
	if *port < 0 || *port > 65535 {
		return a.out.usageError("--port 要在 1..65535 之间")
	}
	appName := a.flagApp
	if len(pos) == 1 {
		appName = pos[0]
	}
	if appName != "" {
		return a.cmdOpenApp(ctx, appName, *noBrowser, *port)
	}

	t, _, err := a.resolveTarget()
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}

	// 1. Fetch the token over the normal connection, then close it.
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return a.out.fail(derr)
	}
	token, terr := api.ConsoleToken(ctx)
	_ = api.Close()
	if terr != nil {
		return a.out.fail(terr)
	}

	// 2. Establish the console base URL: url mode connects to the target's own
	// host:port; ssh mode forwards a local port to the console listener.
	var (
		baseURL string
		tunnel  *client.Tunnel
	)
	local := strings.HasPrefix(t.URL, "unix:")
	if local {
		// This computer's acornfox: the console listens on loopback.
		baseURL = strings.TrimRight(t.Console, "/")
		if baseURL == "" {
			baseURL = client.DefaultLocalConsole
		}
	} else if t.URL != "" {
		baseURL = strings.TrimRight(t.URL, "/")
	} else {
		localPort := *port
		if localPort == 0 {
			p, perr := a.pickFreePort()
			if perr != nil {
				return a.out.fail(&client.Error{Diag: client.Diagnosis{
					Stage: "connect", Code: "connect_failed", Message: "无法分配本地端口：" + perr.Error(),
				}})
			}
			localPort = p
		}
		tun, tunErr := a.openTunnelFn()(ctx, t, localPort)
		if tunErr != nil {
			return a.out.fail(tunErr)
		}
		tunnel = tun
		baseURL = tun.LocalURL
	}
	if tunnel != nil {
		defer tunnel.Close()
	}

	// 3. Build the login URL (the only place the token appears) and browser URL.
	loginURL := baseURL + "/console/login?t=" + url.QueryEscape(token)
	// The token must never be redacted-away in output; it only appears inside
	// loginURL which we never print unless the browser cannot be opened.

	// 4. Open the browser (unless suppressed); fall back to printing the link.
	browserOpened := false
	if !*noBrowser {
		if oerr := a.openURLFn()(loginURL); oerr == nil {
			browserOpened = true
		}
	}

	// 5. Report. Human output never prints the token when the browser opened.
	if a.out.json {
		a.out.emitJSON(map[string]any{"url": baseURL})
	} else if browserOpened {
		a.out.human("已在浏览器打开控制台：%s", baseURL)
		if !local {
			a.out.human("按 Ctrl+C 关闭。")
		}
	} else {
		if *noBrowser {
			a.out.human("未打开浏览器。请在浏览器打开下面的登录链接（一次性，60 秒内有效）：")
		} else {
			a.out.human("无法自动打开浏览器。请手动打开下面的登录链接（一次性，60 秒内有效）：")
		}
		a.out.human("%s", loginURL)
		if !local {
			a.out.human("按 Ctrl+C 关闭。")
		}
	}

	// 6. Block until Ctrl+C (ctx cancel) or the tunnel dies. A local target
	// needs no tunnel: the console stays up without this command.
	if local {
		return exitOK
	}
	if tunnel == nil {
		// url mode: no tunnel to watch; wait for ctx.
		<-ctx.Done()
		return exitOK
	}
	select {
	case <-ctx.Done():
		return exitOK
	case <-tunnel.Done():
		if e := tunnel.Err(); e != nil {
			if ce, ok := e.(*client.Error); ok && ce.Diag.Stage == "connect" {
				if !a.out.json {
					a.out.printDiagnosis(ce.Diag)
				} else {
					a.out.emitDiagnosisJSON(ce.Diag)
				}
				return exitConnect
			}
		}
		return exitOK
	}
}

// cmdOpenApp implements `open APP`: it finds the app's port and exposure,
// then opens the app on the user's own computer. An ssh target gets a tunnel
// from a free local port to the app's port on the server's loopback (apps
// listen there whatever their exposure), like the console; this computer's
// acornfox opens http://127.0.0.1:PORT directly; a URL target (the LAN or
// public console) uses the app's own address when it is reachable from the
// network and fails with OPEN-APP-02 for a local-only app.
func (a *app) cmdOpenApp(ctx context.Context, name string, noBrowser bool, port int) int {
	// The server checks the name (invalid_app) like every app command.
	t, _, err := a.resolveTarget()
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return a.out.fail(derr)
	}
	ap, aerr := api.App(ctx, name)
	_ = api.Close()
	if aerr != nil {
		return a.out.fail(aerr)
	}
	if ap.PublicPort <= 0 {
		return a.openFail(codeOpenNoPort, name+" 还没有访问端口（没部署成功，或只在容器网络里）",
			"运行 acornfox status "+name+" 看状态；部署成功后再打开")
	}

	var (
		appURL string
		tunnel *client.Tunnel
	)
	switch {
	case strings.HasPrefix(t.URL, "unix:"):
		appURL = "http://127.0.0.1:" + strconv.Itoa(ap.PublicPort)
	case t.URL != "":
		if ap.LocalOnly || ap.Exposure == "local" {
			return a.openFail(codeOpenLocalOnly, name+" 只在服务器本机能打开，经网址连接的服务器开不了隧道",
				"用 acornfox target add 名称 --ssh 用户@主机 登记这台服务器后再运行 acornfox open "+name+
					"；或经用户同意改成局域网：acornfox app set --app "+name+" --expose lan")
		}
		appURL = urlTargetAppURL(t.URL, ap)
	default:
		localPort := port
		if localPort == 0 {
			p, perr := a.pickFreePort()
			if perr != nil {
				return a.out.fail(&client.Error{Diag: client.Diagnosis{
					Stage: "connect", Code: "connect_failed", Message: "无法分配本地端口：" + perr.Error(),
				}})
			}
			localPort = p
		}
		tun, tunErr := a.openAppTunnelFn()(ctx, t, localPort, ap.PublicPort)
		if tunErr != nil {
			return a.out.fail(tunErr)
		}
		tunnel = tun
		defer tunnel.Close()
		appURL = tun.LocalURL
	}

	browserOpened := false
	if !noBrowser {
		browserOpened = a.openURLFn()(appURL) == nil
	}
	if a.out.json {
		a.out.emitJSON(map[string]any{"app": name, "url": appURL, "tunnel": tunnel != nil})
	} else {
		switch {
		case browserOpened:
			a.out.human("已在浏览器打开 %s：%s", name, appURL)
		case noBrowser:
			a.out.human("%s 的地址（在这台电脑的浏览器里打开）：%s", name, appURL)
		default:
			a.out.human("无法自动打开浏览器。请在这台电脑的浏览器里打开：%s", appURL)
		}
		if tunnel != nil {
			a.out.human("地址经 SSH 隧道转到服务器上的 %s，只在这台电脑上能用；看完按 Ctrl+C 关闭。", name)
		}
	}
	if tunnel == nil {
		return exitOK
	}
	select {
	case <-ctx.Done():
		return exitOK
	case <-tunnel.Done():
		if e := tunnel.Err(); e != nil {
			if ce, ok := e.(*client.Error); ok && ce.Diag.Stage == "connect" {
				if a.out.json {
					a.out.emitDiagnosisJSON(ce.Diag)
				} else {
					a.out.printDiagnosis(ce.Diag)
				}
				return exitConnect
			}
		}
		return exitOK
	}
}

// urlTargetAppURL is where an app reachable from the network opens for a
// target added by URL (the LAN or public console): the host the user
// already reaches the server by, at the app's port; a public app with a
// public address keeps it.
func urlTargetAppURL(targetURL string, ap client.App) string {
	if ap.Exposure == "public" && ap.URL != "" && !strings.Contains(ap.URL, "127.0.0.1") {
		return ap.URL
	}
	host := ""
	if u, err := url.Parse(targetURL); err == nil {
		host = u.Hostname()
	}
	if host == "" {
		if ap.LANURL != "" {
			return ap.LANURL
		}
		return ap.URL
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(ap.PublicPort))
}

// openFail reports a fault of open APP: in --json a diagnosis (stage open),
// otherwise the reason, the fault code and what to do. Exit code 1.
func (a *app) openFail(code, msg, hint string) int {
	if a.out.json {
		a.out.emitDiagnosisJSON(client.Diagnosis{Stage: "open", Code: code, Message: msg, Hint: hint})
	} else {
		a.out.humanErr("失败：%s", msg)
		a.out.humanErr("故障编号 %s", code)
		a.out.humanErr("怎么办 %s", hint)
	}
	return exitOpFailed
}

// openAppTunnelFn returns the injected app tunnel function or the real one.
func (a *app) openAppTunnelFn() func(ctx context.Context, t client.Target, localPort, remotePort int) (*client.Tunnel, error) {
	if a.openAppTunnel != nil {
		return a.openAppTunnel
	}
	return client.OpenTunnelTo
}

// openTunnelFn returns the injected tunnel function or the real one.
func (a *app) openTunnelFn() func(ctx context.Context, t client.Target, localPort int) (*client.Tunnel, error) {
	if a.openTunnel != nil {
		return a.openTunnel
	}
	return client.OpenTunnel
}

// openURLFn returns the injected browser opener or the OS-specific default.
func (a *app) openURLFn() func(url string) error {
	if a.openURL != nil {
		return a.openURL
	}
	return openBrowser
}

// pickFreePort returns a free local TCP port using the injected picker or the
// OS.
func (a *app) pickFreePort() (int, error) {
	if a.freePort != nil {
		return a.freePort()
	}
	return freeLocalPort()
}

// freeLocalPort asks the OS for an ephemeral port and returns it. There is an
// inherent race between closing the listener and ssh binding the forward, but
// it is negligible for an interactive command.
func freeLocalPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// openBrowser launches the OS default handler for url. Commands per N3 contract
// section 5.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default: // linux and others
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
