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
//
// 忘了密码：登录页（不是设密码页）最下面一行“忘了密码”，点开是一句话、一个默认收起的“用命令行”（命令加复制）和一个
// “复制给 AI 工作台”（一段交给 AI 工作台的话，login.js 复制时再接上这个页面的地址）。页面上不能直接重设：不登录就能重设
// 等于谁都能抢走控制台，重设只能在服务器上做。命令和说明在 forgotFor（局域网入口 LAN-CONSOLE-04，公网入口 PUBCON-05）。

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
{{end}}{{if .Forgot}}<details class="forgot"><summary><svg class="chev" viewBox="0 0 24 24" aria-hidden="true"><path d="m9 18 6-6-6-6"/></svg>忘了密码</summary>
<p class="forgot-text">{{.Forgot.Text}}</p>
<details class="cli"><summary><svg class="chev" viewBox="0 0 24 24" aria-hidden="true"><path d="m9 18 6-6-6-6"/></svg><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="m4 17 6-6-6-6"/><path d="M12 19h8"/></svg>用命令行</summary>
<div class="cmd"><code>{{.Forgot.Command}}</code><button type="button" class="copy" data-copy="{{.Forgot.Command}}"><span class="label">复制</span></button></div></details>
<button type="button" class="ask" data-ask="{{.Forgot.Ask}}"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M9.9 4.4 11 8.2a2 2 0 0 0 1.3 1.3l3.8 1.1a.5.5 0 0 1 0 1l-3.8 1.1a2 2 0 0 0-1.3 1.3L9.9 17.8a.5.5 0 0 1-1 0L7.8 14a2 2 0 0 0-1.3-1.3l-3.8-1.1a.5.5 0 0 1 0-1l3.8-1.1A2 2 0 0 0 7.8 8.2L8.9 4.4a.5.5 0 0 1 1 0"/><path d="M18 3v4"/><path d="M20 5h-4"/></svg><span class="label">复制给 AI 工作台</span></button>
</details>{{else}}<p class="note">{{.Note}}</p>{{end}}
</main></body></html>`))

type loginView struct {
	Msg, Sub, Note string
	Setup, LAN     bool
	Forgot         *forgotView // nil: no “忘了密码” (the set-password page has no password to forget)
}

// forgotView is the content of the “忘了密码” fold on the login page.
type forgotView struct {
	Text    string // the one sentence: what to do on the server
	Command string // shown under “用命令行”, with a copy button
	Ask     string // what “复制给 AI 工作台” copies; login.js adds this page's address
}

// forgotFor builds the “忘了密码” content: the LAN entry resets with
// `acornfox lan-console reset-password` (the user sets a new password in the
// browser), the public entry generates a new one with `acornfox public-console
// enable 域名 --reset-password` (shown once). domain is the public entry's
// domain, "" when the config does not say.
func forgotFor(lan bool, domain string) *forgotView {
	if lan {
		return &forgotView{
			Text:    "在装着 AcornFox 的服务器上重设，再回到这里设一个新密码。所有登录和记住的设备会一起退出。",
			Command: "acornfox lan-console reset-password",
			Ask: "我忘了 AFX 控制台（局域网入口）的登录密码，同意重设。故障编号 LAN-CONSOLE-04（运行 acornfox faults LAN-CONSOLE-04 看处理步骤）。\n" +
				"步骤：1）经 SSH 登录装着 AcornFox 的服务器；2）运行 acornfox lan-console status --json 看现状；" +
				"3）告诉我一句：重设会让所有登录和记住的设备都退出；然后运行 acornfox lan-console reset-password；" +
				"如果你发现我其实是别的原因（比如只想让丢了的手机退出），先问我；" +
				"4）把下面的控制台地址告诉我，我自己在浏览器里设新密码。\n" +
				"不要替我设密码，也不要让我把密码发到对话里。",
		}
	}
	d := domain
	if d == "" {
		d = "公网控制台的域名"
	}
	return &forgotView{
		Text:    "在装着 AcornFox 的服务器上重新生成密码。新密码只显示一次，旧密码和现在的登录会马上失效。",
		Command: "acornfox public-console enable " + d + " --reset-password",
		Ask: "我忘了 AFX 控制台（公网入口）的登录密码，同意重新生成。故障编号 PUBCON-05（运行 acornfox faults PUBCON-05 看处理步骤）。\n" +
			"步骤：1）经 SSH 登录装着 AcornFox 的服务器；2）运行 acornfox public-console status --json 看域名；" +
			"3）告诉我一句：重新生成会让旧密码和现在的登录马上失效；然后运行 acornfox public-console enable " + d + " --reset-password；" +
			"如果你发现我其实是别的原因，先问我；" +
			"4）新密码只显示一次，请让我自己在终端或密码文件里看、存进密码管理器，不要读出来写进对话。",
	}
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
	domain := ""
	if cfg != nil {
		domain = cfg.Domain
	}
	v := loginView{Msg: msg, Sub: "输入开通公网访问时生成的密码。", Forgot: forgotFor(false, domain)}
	if g.cfg.LAN {
		v.LAN = true
		v.Sub = "输入你给这个控制台设的密码。"
		if cfg != nil && cfg.PasswordHash == "" {
			v.Sub = "输入开启局域网访问时生成的密码。"
		}
		v.Forgot = forgotFor(true, "")
	}
	g.renderLogin(w, status, v)
}

// loginJS applies the theme and accent color remembered for the console in
// this browser before the page paints. It only reads two localStorage keys
// and sets two attributes; when storage is unavailable the page follows the
// system theme with the neutral accent. It also makes the two copy buttons of
// “忘了密码” work (data-copy: the text itself; data-ask: the text plus this
// page's address): navigator.clipboard where the page is a secure context,
// otherwise a hidden textarea and execCommand (the LAN console is plain http).
const loginJS = `(function () {
  try {
    var r = document.documentElement
    var t = localStorage.getItem('afx-theme')
    if (t === 'light' || t === 'dark') r.setAttribute('data-theme', t)
    var a = localStorage.getItem('afx-accent')
    if (a === 'blue' || a === 'green' || a === 'orange') r.setAttribute('data-accent', a)
  } catch (e) {}
  function legacyCopy(text) {
    var ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.className = 'offscreen'
    document.body.appendChild(ta)
    ta.select()
    var ok = false
    try { ok = document.execCommand('copy') } catch (e) {}
    document.body.removeChild(ta)
    return ok
  }
  function copyText(text, done) {
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(function () { done(true) }, function () { done(legacyCopy(text)) })
      return
    }
    done(legacyCopy(text))
  }
  document.addEventListener('click', function (e) {
    var el = e.target
    while (el && el !== document && !(el.getAttribute && (el.hasAttribute('data-copy') || el.hasAttribute('data-ask')))) el = el.parentNode
    if (!el || el === document) return
    var text = el.getAttribute('data-copy')
    if (text === null) text = el.getAttribute('data-ask') + '\n控制台地址：' + location.origin + '/'
    var label = el.querySelector('.label')
    var before = label ? label.textContent : ''
    copyText(text, function (ok) {
      if (!label) return
      label.textContent = ok ? '已复制' : '没能复制，请手动选中'
      el.classList.toggle('failed', !ok)
      setTimeout(function () { label.textContent = before; el.classList.remove('failed') }, 2000)
    })
  })
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
.forgot{margin:14px 0 0;padding-top:12px;border-top:1px solid hsl(var(--border));font-size:11.5px;line-height:16px;color:hsl(var(--muted-foreground))}
summary{display:inline-flex;align-items:center;gap:4px;min-height:28px;border-radius:4px;color:hsl(var(--muted-foreground));cursor:pointer;list-style:none;user-select:none}
summary::-webkit-details-marker{display:none}
summary:hover{color:hsl(var(--foreground))}
summary:focus-visible{outline:none;box-shadow:0 0 0 2px hsl(var(--background)),0 0 0 4px hsl(var(--ring))}
.chev,.ico{width:12px;height:12px;flex:none;fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
.chev{transition:transform .15s ease}
details[open]>summary .chev{transform:rotate(90deg)}
.forgot-text{margin:4px 0 8px}
.cli{margin:0 0 8px}
.cmd{display:flex;align-items:center;gap:6px;margin-top:4px}
.cmd code{flex:1;min-width:0}
button.copy,button.ask{display:inline-flex;align-items:center;justify-content:center;gap:6px;height:28px;padding:0 10px;border:1px solid hsl(var(--input));background:transparent;color:hsl(var(--foreground));font-size:11.5px;white-space:nowrap}
button.copy:hover,button.ask:hover{background:hsl(var(--muted))}
button.failed{border-color:hsl(var(--danger) / .5);color:hsl(var(--danger))}
.offscreen{position:fixed;top:0;left:0;width:1px;height:1px;opacity:0}
@media (prefers-reduced-motion:reduce){.chev{transition:none}}
@media (max-width:640px){summary{min-height:36px}button.copy,button.ask{height:36px}}
code{padding:1px 5px;border:1px solid hsl(var(--border));border-radius:4px;background:hsl(var(--muted) / .4);font:10.5px/14px "Geist Mono",ui-monospace,"SF Mono",Menlo,Consolas,monospace;color:hsl(var(--foreground));word-break:break-all}
`
