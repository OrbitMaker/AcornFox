package publicconsole

// The login and set-password page of the LAN and public console entries. It
// is rendered here, before any session exists, so it cannot use the console's
// own bundle; it copies the console's look instead: the same color variables
// (shadcn zinc, neutral primary), 13px text, 32px controls, 8px card radius,
// the block-fox logo. login.js applies the theme and accent color the browser
// remembered for the console (localStorage afx-theme / afx-accent, the same
// origin), so the page matches what the user sees after logging in. Latin
// text falls back to the system font: the console's Geist files sit behind
// the session and are not loaded here.
//
// Everything is same-origin and static: no inline script or style (the CSP
// below forbids them), no external request.

import (
	"html/template"
	"net/http"
)

var loginTmpl = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>{{if .Setup}}设置密码{{else}}登录{{end}} · AFX Console</title>
<script src="/login.js"></script>
<link rel="stylesheet" href="/login.css"></head>
<body><main>
<div class="brand"><svg viewBox="0 0 120 120" aria-hidden="true"><g class="fox"><rect x="12" y="34" width="16" height="16" rx="3"/><rect x="32" y="34" width="16" height="16" rx="3"/><rect x="52" y="34" width="16" height="16" rx="3"/><rect x="72" y="34" width="16" height="16" rx="3"/><rect x="92" y="34" width="16" height="16" rx="3"/><rect x="12" y="54" width="16" height="16" rx="3"/><rect x="32" y="54" width="16" height="16" rx="3"/><rect x="52" y="54" width="16" height="16" rx="3"/><rect x="72" y="54" width="16" height="16" rx="3"/><rect x="92" y="54" width="16" height="16" rx="3"/><rect x="32" y="74" width="16" height="16" rx="3"/><rect x="52" y="74" width="16" height="16" rx="3"/><rect x="72" y="74" width="16" height="16" rx="3"/></g><g fill="#E08A2E"><rect x="12" y="14" width="16" height="16" rx="3"/><rect x="92" y="14" width="16" height="16" rx="3"/><rect x="52" y="94" width="16" height="16" rx="3"/></g></svg><span>AFX Console</span></div>
<h1>{{if .Setup}}设置密码{{else}}登录{{end}}</h1>
<p class="sub">{{.Sub}}</p>
{{if .Msg}}<p class="err" role="alert"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/></svg><span>{{.Msg}}</span></p>{{end}}
{{if .Setup}}<form method="post" action="/setup">
<label for="password">密码（至少 8 位）</label>
<input id="password" name="password" type="password" autocomplete="new-password" minlength="8" maxlength="128" required autofocus>
<label for="confirm">再输一遍</label>
<input id="confirm" name="confirm" type="password" autocomplete="new-password" minlength="8" maxlength="128" required>
<label class="check"><input name="remember" type="checkbox" value="1" checked> 记住这台设备 90 天</label>
<button type="submit">设好并进入</button>
</form>
{{else}}<form method="post" action="/login">
<label for="password">密码</label>
<input id="password" name="password" type="password" autocomplete="current-password" required autofocus>
{{if .LAN}}<label class="check"><input name="remember" type="checkbox" value="1" checked> 记住这台设备 90 天</label>
{{end}}<button type="submit">登录</button>
</form>
{{end}}<p class="note">{{.Note}}{{if .Reset}} <code>{{.Reset}}</code>{{end}}{{.NoteTail}}</p>
</main></body></html>`))

type loginView struct {
	Msg, Sub, Note, Reset, NoteTail string
	Setup, LAN                      bool
}

// loginCSP: only this origin's stylesheet and script; nothing inline.
const loginCSP = "default-src 'none'; style-src 'self'; script-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"

func (g *gateway) renderLogin(w http.ResponseWriter, status int, v loginView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", loginCSP)
	w.WriteHeader(status)
	_ = loginTmpl.Execute(w, v)
}

func (g *gateway) loginPage(w http.ResponseWriter, status int, msg string, cfg *Config) {
	v := loginView{Msg: msg, Sub: "输入开通公网访问时生成的密码。", Note: "忘记密码：运行", Reset: "acornfox public-console enable 域名 --reset-password", NoteTail: " 重新生成。"}
	if g.cfg.LAN {
		v.LAN = true
		v.Sub = "输入你给这个控制台设的密码。"
		if cfg != nil && cfg.PasswordHash == "" {
			v.Sub = "输入开启局域网访问时生成的密码。"
		}
		v.Note, v.Reset, v.NoteTail = "忘了密码：在这台服务器上运行", "acornfox lan-console reset-password", "，再回到这个页面重新设。"
	}
	g.renderLogin(w, status, v)
}

// loginJS applies the theme and accent color remembered for the console in
// this browser before the page paints. It only reads two localStorage keys
// and sets two attributes; when storage is unavailable the page follows the
// system theme with the neutral accent.
const loginJS = `(function () {
  try {
    var r = document.documentElement
    var t = localStorage.getItem('afx-theme')
    if (t === 'light' || t === 'dark') r.setAttribute('data-theme', t)
    var a = localStorage.getItem('afx-accent')
    if (a === 'blue' || a === 'green' || a === 'orange') r.setAttribute('data-accent', a)
  } catch (e) {}
})()
`

// loginCSS: the console's color variables and control sizes (kept in step
// with web/console/src/index.css by hand; only the handful this page uses).
const loginCSS = `:root{--background:0 0% 100%;--foreground:240 10% 3.9%;--card:0 0% 100%;--primary:240 5.9% 10%;--primary-foreground:0 0% 98%;--muted:240 4.8% 95.9%;--muted-foreground:240 3.8% 46.1%;--border:240 5.9% 91%;--input:240 5.9% 89%;--ring:240 5.9% 10%;--sidebar:0 0% 98.5%;--danger:0 72% 48%;--logo:204 61% 15%;color-scheme:light}
@media (prefers-color-scheme:dark){:root:not([data-theme=light]){--background:240 10% 3.9%;--foreground:0 0% 98%;--card:240 8% 6%;--primary:0 0% 98%;--primary-foreground:240 5.9% 10%;--muted:240 3.7% 14%;--muted-foreground:240 5% 64.9%;--border:240 3.7% 15.5%;--input:240 3.7% 18%;--ring:240 4.9% 83.9%;--sidebar:240 6% 6%;--danger:0 85% 66%;--logo:0 0% 100%;color-scheme:dark}}
:root[data-theme=dark]{--background:240 10% 3.9%;--foreground:0 0% 98%;--card:240 8% 6%;--primary:0 0% 98%;--primary-foreground:240 5.9% 10%;--muted:240 3.7% 14%;--muted-foreground:240 5% 64.9%;--border:240 3.7% 15.5%;--input:240 3.7% 18%;--ring:240 4.9% 83.9%;--sidebar:240 6% 6%;--danger:0 85% 66%;--logo:0 0% 100%;color-scheme:dark}
:root[data-accent=blue]{--primary:221.2 83.2% 53.3%;--primary-foreground:210 40% 98%;--ring:221.2 83.2% 53.3%}
:root[data-accent=green]{--primary:142.1 76.2% 36.3%;--primary-foreground:355.7 100% 97.3%;--ring:142.1 76.2% 36.3%}
:root[data-accent=orange]{--primary:24.6 95% 50%;--primary-foreground:60 9.1% 97.8%;--ring:24.6 95% 50%}
@media (prefers-color-scheme:dark){:root:not([data-theme=light])[data-accent=blue]{--primary:217.2 91.2% 59.8%;--primary-foreground:222.2 47.4% 11.2%;--ring:217.2 91.2% 59.8%}:root:not([data-theme=light])[data-accent=green]{--primary:142.1 70.6% 45.3%;--primary-foreground:144.9 80.4% 10%;--ring:142.1 70.6% 45.3%}:root:not([data-theme=light])[data-accent=orange]{--primary:20.5 90.2% 52%;--primary-foreground:60 9.1% 97.8%;--ring:20.5 90.2% 52%}}
:root[data-theme=dark][data-accent=blue]{--primary:217.2 91.2% 59.8%;--primary-foreground:222.2 47.4% 11.2%;--ring:217.2 91.2% 59.8%}
:root[data-theme=dark][data-accent=green]{--primary:142.1 70.6% 45.3%;--primary-foreground:144.9 80.4% 10%;--ring:142.1 70.6% 45.3%}
:root[data-theme=dark][data-accent=orange]{--primary:20.5 90.2% 52%;--primary-foreground:60 9.1% 97.8%;--ring:20.5 90.2% 52%}
*{box-sizing:border-box}
html,body{height:100%}
body{margin:0;display:flex;align-items:center;justify-content:center;padding:16px;background:hsl(var(--sidebar));color:hsl(var(--foreground));font:13px/1.5 "Geist Sans","Geist","PingFang SC","Hiragino Sans GB","Microsoft YaHei","Noto Sans SC",system-ui,sans-serif;-webkit-font-smoothing:antialiased}
main{width:100%;max-width:340px;padding:20px;border:1px solid hsl(var(--border));border-radius:8px;background:hsl(var(--card))}
.brand{display:flex;align-items:center;gap:8px;margin-bottom:16px;font-size:13px;font-weight:600}
.brand svg{width:20px;height:20px;flex:none}
.fox rect{fill:hsl(var(--logo))}
h1{margin:0 0 4px;font-size:16px;line-height:22px;font-weight:600}
.sub{margin:0 0 14px;font-size:11.5px;line-height:16px;color:hsl(var(--muted-foreground))}
.err{display:flex;align-items:center;gap:8px;margin:0 0 12px;padding:6px 10px;border:1px solid hsl(var(--danger) / .4);border-radius:8px;font-size:11.5px;line-height:16px;color:hsl(var(--danger))}
.err svg{width:14px;height:14px;flex:none;fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
form{display:flex;flex-direction:column;margin:0}
label{margin:0 0 4px;font-size:11.5px;line-height:16px;font-weight:500}
input[type=password]{height:32px;width:100%;margin:0 0 10px;padding:0 10px;border:1px solid hsl(var(--input));border-radius:6px;background:transparent;color:inherit;font:inherit}
input[type=password]:focus-visible{outline:none;box-shadow:0 0 0 1px hsl(var(--ring));border-color:hsl(var(--ring))}
label.check{display:flex;align-items:center;gap:8px;margin:2px 0 12px;font-weight:400;color:hsl(var(--muted-foreground))}
label.check input{width:14px;height:14px;margin:0;accent-color:hsl(var(--primary))}
button{height:32px;padding:0 12px;border:0;border-radius:6px;background:hsl(var(--primary));color:hsl(var(--primary-foreground));font:inherit;font-weight:500;cursor:pointer}
button:hover{background:hsl(var(--primary) / .9)}
button:focus-visible{outline:none;box-shadow:0 0 0 2px hsl(var(--background)),0 0 0 4px hsl(var(--ring))}
.note{margin:14px 0 0;padding-top:12px;border-top:1px solid hsl(var(--border));font-size:11.5px;line-height:16px;color:hsl(var(--muted-foreground))}
code{padding:1px 5px;border:1px solid hsl(var(--border));border-radius:4px;background:hsl(var(--muted) / .4);font:10.5px/14px "Geist Mono",ui-monospace,"SF Mono",Menlo,Consolas,monospace;color:hsl(var(--foreground));word-break:break-all}
`
