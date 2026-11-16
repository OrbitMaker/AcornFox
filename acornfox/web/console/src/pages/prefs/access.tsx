// 设置 › 访问：一行一项、点开才展开（和应用的“设置”同一个样子，行在 components/expand-row.tsx），同一时间只展开一项。
//   域名：这台机器的平台域名。公网应用自动得到 <应用名>.<域名>。四种样子：正常、还没有、还没生效、这台机器用不了。
//         旧服务端没有这个接口（404）时整行不显示。
//   局域网里打开控制台：开没开、地址、密码是谁设的、记住的设备（能移除）；开关和清密码只给服务器上的命令。
//   在外面打开控制台：开没开；服务端允许时（can_toggle）页面上能开、能关，都先弹确认框，
//         needs_password 时要再输一次现在的控制台密码；开启成功给的新密码只显示这一次。不想对外开放给 acornfox open。
//         开着时会问服务端证书好了没有（cert-wait.tsx）：第一次开启要等一两分钟，这段时间写“正在申请证书”，好了再说可以打开。
// 接口：GET/PUT/DELETE /v1/platform/domain、POST /v1/platform/domain/check；
//       GET /v1/lan-console、GET/DELETE /v1/lan-console/devices[/{id}]；
//       GET /v1/public-console[?check=https]、POST /v1/public-console/enable {domain,password}、DELETE /v1/public-console {password}。
// 密码只放在组件状态里，关掉弹窗就清掉，不写进地址、存储和日志。
import * as React from 'react'
import { Check, Copy } from 'lucide-react'
import { ApiError, api } from '@/lib/api'
import { copyText } from '@/lib/clipboard'
import { usePoll } from '@/lib/hooks'
import { timeText } from '@/lib/format'
import { normDomain, recordName } from '@/lib/platform-domain'
import type { LANConsoleStatus, LANDevice, PlatformDomainView, PublicConsoleEnableResult, PublicConsoleStatus } from '@/lib/types'
import { ExpandRow, type RowTone } from '@/components/expand-row'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/controls'
import { CopyButton, CopyLine } from '@/components/ui/copy'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { Input, Label } from '@/components/ui/input'
import { useToast } from '@/components/ui/toast'
import { CertLine, useCertState } from './cert-wait'

type Key = 'domain' | 'lan' | 'pub'
const EXAMPLE = 'apps.example.com'

function errText(e: unknown): string {
  if (e instanceof ApiError) return e.message + (e.code && /^[A-Z]/.test(e.code) ? '（' + e.code + '）' : '')
  return e instanceof Error ? e.message : String(e)
}

function Muted({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <p className={'text-xs text-muted-foreground ' + className}>{children}</p>
}

function Code({ code }: { code?: string }) {
  return code ? <span className="mono ml-1.5 text-2xs text-muted-foreground">{code}</span> : null
}

// ---- 域名 ----

function Chip({ k, v }: { k: string; v: string }) {
  return (
    <span className="mr-4 inline-flex items-center gap-1.5">
      <span className="text-2xs text-muted-foreground">{k}</span>
      <span className="mono rounded bg-muted px-1.5 py-px text-xs">{v}</span>
    </span>
  )
}

// 两步：到买域名的地方加一条记录、回来把域名填上。用在“还没有”展开后和“换一个域名”的弹窗里。
function DomainSteps({ view, value, onChange, onSubmit, busy, submitText }: {
  view: PlatformDomainView; value: string; onChange: (v: string) => void; onSubmit: () => void; busy: boolean; submitText: string
}) {
  const typed = normDomain(value)
  const name = recordName(typed || EXAMPLE) || '*.apps'
  const to = view.record?.value || view.public_ip || ''
  return (
    <div className="flex flex-col gap-1.5" data-testid="domain-steps">
      <div className="text-xs"><span className="font-medium">第一步</span>　到你买域名的地方，加一条记录：</div>
      <div className="flex flex-wrap gap-y-1" data-testid="domain-record">
        <Chip k="名字" v={name} /><Chip k="类型" v={view.record?.type || 'A'} /><Chip k="指向" v={to || '这台机器的公网地址'} />
      </div>
      {!typed && <div className="text-2xs text-muted-foreground">上面以 {EXAMPLE} 为例，在下面填了域名会跟着变。</div>}
      <div className="mt-2.5 text-xs"><span className="font-medium">第二步</span>　回来把域名填在这里：</div>
      <form className="flex flex-wrap items-center gap-2" onSubmit={(e) => { e.preventDefault(); if (typed && !busy) onSubmit() }}>
        <Input className="min-w-0 max-w-sm flex-1 basis-56" aria-label="平台域名" placeholder={'例如 ' + EXAMPLE} value={value} onChange={(e) => onChange(e.target.value)} autoComplete="off" spellCheck={false} />
        <Button type="submit" disabled={!typed || busy}>{busy && <Spinner />}{submitText}</Button>
      </form>
    </div>
  )
}

function domainRow(v: PlatformDomainView | null, err: Error | null): { tone: RowTone; summary: string } | null {
  if (!v) return err ? { tone: '', summary: '没读到：' + err.message } : null
  if (v.supported === false) return { tone: '', summary: v.unsupported?.text || '这台机器用不了域名' }
  if (v.state === 'ok') return { tone: 'success', summary: (v.domain || '') + ' · 正常' }
  if (v.state === 'problem') return { tone: 'danger', summary: (v.domain || '') + ' · 还没生效：' + (v.problem?.text || '检查没过') }
  return { tone: '', summary: '还没有。配一个域名，应用就不用记数字地址了' }
}

function DomainBody({ view, setView, onChanged }: { view: PlatformDomainView; setView: (v: PlatformDomainView) => void; onChanged: () => void }) {
  const toast = useToast()
  const [input, setInput] = React.useState('')
  const [busy, setBusy] = React.useState('')
  const [dialog, setDialog] = React.useState<'' | 'change' | 'clear'>('')
  const [next, setNext] = React.useState('')
  const [err, setErr] = React.useState('')

  const run = async (key: string, fn: () => Promise<PlatformDomainView>, done: (v: PlatformDomainView) => void) => {
    setBusy(key)
    setErr('')
    try {
      const v = await fn()
      setView(v)
      onChanged()
      done(v)
    } catch (e) {
      if (dialog) setErr(errText(e))
      else toast('没能完成', errText(e), 'error')
    } finally {
      setBusy('')
    }
  }
  // 换掉已有的域名、不用域名了，服务端要求带上“已确认”（确认框就是页面上的确认）；第一次设不用带。
  const save = (d: string, after?: () => void) => run('save', () => api.put<PlatformDomainView>('/v1/platform/domain', view.domain ? { domain: normDomain(d), confirm: true } : { domain: normDomain(d) }), (v) => {
    setInput('')
    after?.()
    if (v.state === 'ok') toast('域名配好了', '开放到公网的应用地址是 应用名.' + v.domain)
    else toast('已保存，还没生效', v.problem?.text || '展开看怎么办')
  })
  const check = () => run('check', () => api.post<PlatformDomainView>('/v1/platform/domain/check'), (v) => {
    if (v.state === 'ok') toast('域名正常了', v.domain)
    else toast('还没生效', v.problem?.text || '', 'error')
  })
  const clear = () => run('clear', () => api.del<PlatformDomainView>('/v1/platform/domain?confirm=true'), () => {
    setDialog('')
    toast('不用域名了', '应用回到原来的地址')
  })
  const openDialog = (d: 'change' | 'clear') => { setErr(''); setNext(''); setDialog(d) }

  if (view.supported === false) {
    return (
      <Muted>
        域名的证书要从公网验证，没有公网地址的机器验证不了。应用照常用原来的地址打开。<Code code={view.unsupported?.code} />
      </Muted>
    )
  }
  if (view.state !== 'ok' && view.state !== 'problem') {
    return <DomainSteps view={view} value={input} onChange={setInput} onSubmit={() => save(input)} busy={busy === 'save'} submitText="保存" />
  }
  const changeBtn = <Button variant="outline" size="sm" disabled={!!busy} onClick={() => openDialog('change')}>换一个域名</Button>
  const clearBtn = <Button variant="outline" size="sm" className="text-danger hover:text-danger" disabled={!!busy} onClick={() => openDialog('clear')}>不用域名了</Button>
  return (
    <div className="flex flex-col gap-2.5" data-testid="domain-body">
      {view.state === 'ok' ? (
        <>
          <Muted>开放到公网的应用，地址是 <span className="mono text-foreground">应用名.{view.domain}</span>，自动带 HTTPS。</Muted>
          <div className="flex flex-wrap gap-x-4 gap-y-1">
            {(view.checks || []).map((c) => (
              <span key={c.key} className="inline-flex items-center gap-1 text-xs">
                <Check className={c.ok ? 'size-3.5 text-success' : 'size-3.5 text-muted-foreground'} aria-hidden="true" />{c.text}
              </span>
            ))}
          </div>
        </>
      ) : (
        <>
          <p className="text-xs">{view.problem?.detail || view.problem?.text}</p>
          {view.problem?.next && <Muted>{view.problem.next}</Muted>}
          <Muted>这期间应用照常用原来的地址打开。</Muted>
        </>
      )}
      <div className="flex flex-wrap items-center gap-2">
        {view.state === 'problem' && <Button size="sm" disabled={!!busy} onClick={check}>{busy === 'check' && <Spinner />}重新检查</Button>}
        {changeBtn}
        {clearBtn}
        {view.state === 'problem' && <Code code={view.problem?.code} />}
      </div>
      <Dialog open={dialog === 'change'} onClose={() => setDialog('')} labelledBy="pd-change-title">
        <DialogHead id="pd-change-title" title="换一个域名" desc={'换了以后，用 ' + view.domain + ' 的应用地址都会跟着换，原来的地址打不开。'} />
        <DomainSteps view={view} value={next} onChange={setNext}
          onSubmit={() => save(next, () => setDialog(''))} busy={busy === 'save'} submitText="换成这个" />
        {err && <p className="mt-3 text-xs text-danger" role="alert">{err}</p>}
      </Dialog>
      <Dialog open={dialog === 'clear'} onClose={() => setDialog('')} labelledBy="pd-clear-title">
        <DialogHead id="pd-clear-title" title="不用域名了？" />
        <ul className="flex list-disc flex-col gap-1 pl-5 text-sm">
          <li>开放到公网的应用不再有 <span className="mono">应用名.{view.domain}</span> 这个地址，回到原来的地址（数字地址加端口）。</li>
          <li>应用自己绑的域名不受影响。</li>
          <li>以后可以再配。</li>
        </ul>
        {err && <p className="mt-3 text-xs text-danger" role="alert">{err}</p>}
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" onClick={() => setDialog('')}>取消</Button>
          <Button variant="destructive" disabled={busy === 'clear'} onClick={clear}>{busy === 'clear' && <Spinner />}不用了</Button>
        </div>
      </Dialog>
    </div>
  )
}

// ---- 局域网里打开控制台 ----

function lanSummary(l: LANConsoleStatus): { tone: RowTone; summary: string } {
  if (!l.enabled) return { tone: '', summary: '没开启。局域网里的电脑、手机打不开控制台' }
  const where = l.url ? l.url.replace(/^https?:\/\//, '') : '端口 ' + l.port
  if (l.password_set === false) return { tone: 'warning', summary: '已开启 · ' + where + ' · 还没设密码' }
  return { tone: 'success', summary: '已开启 · ' + where + (l.devices ? ' · 记住了 ' + l.devices + ' 台设备' : '') }
}

function passwordLine(l: LANConsoleStatus): string {
  if (l.password_set === false) return '还没设密码：请现在就用局域网里的电脑打开它设一个'
  if (l.generated_password) return '用开启时生成的密码登录'
  if (l.password_set_at) return `密码是 ${timeText(l.password_set_at)} 从 ${l.password_set_from || '未知地址'} 设的`
  return ''
}

// 记住的设备：列表和移除。移除只会让那台设备重新输密码，所以页面上可以点（要带会话的校验值）。
function LANDevices({ onChanged }: { onChanged: () => void }) {
  const toast = useToast()
  const list = usePoll((s) => api.get<{ devices: LANDevice[] }>('/v1/lan-console/devices', s).then((b) => b.devices || []).catch(() => null), 0)
  const [busy, setBusy] = React.useState('')
  if (!list.data) return null
  const remove = async (id: string) => {
    setBusy(id || '*')
    try {
      await api.send('DELETE', '/v1/lan-console/devices' + (id ? '/' + encodeURIComponent(id) : ''))
      toast(id ? '已移除这台设备' : '已移除全部设备', '它们要重新输密码才能进')
      await list.refresh()
      onChanged()
    } catch (e) {
      toast('没能移除', e instanceof Error ? e.message : '', 'error')
    } finally {
      setBusy('')
    }
  }
  return (
    <div className="rounded-md border" data-testid="lan-devices">
      <div className="flex items-center justify-between border-b px-3 py-2 text-xs font-medium">
        <span>记住的设备 <span className="font-normal text-muted-foreground">登录时勾了“记住这台设备 90 天”的</span></span>
        {list.data.length > 1 && <Button size="sm" variant="ghost" disabled={!!busy} onClick={() => remove('')}>全部移除</Button>}
      </div>
      {list.data.length === 0 && <div className="px-3 py-2.5 text-xs text-muted-foreground">没有</div>}
      {list.data.map((d) => (
        <div key={d.id} className="flex items-center gap-3 border-b px-3 py-2 text-xs last:border-b-0">
          <div className="min-w-0 flex-1">
            <div className="truncate font-medium">{d.name}</div>
            <div className="truncate text-2xs text-muted-foreground">上次使用 {timeText(d.last_used_at)}{d.last_ip ? ' · ' + d.last_ip : ''} · {d.expires_at.slice(0, 10)} 到期</div>
          </div>
          <Button size="sm" variant="outline" disabled={!!busy} aria-label={'移除 ' + d.name} onClick={() => remove(d.id)}>移除</Button>
        </div>
      ))}
    </div>
  )
}

function LANBody({ l, onChanged }: { l: LANConsoleStatus; onChanged: () => void }) {
  if (!l.enabled) {
    return (
      <div className="flex flex-col gap-1.5">
        <Muted>没开：控制台只在服务器本机（经 SSH 隧道）能打开。要让同一个局域网里的电脑、手机也能打开，在服务器上运行：</Muted>
        <CopyLine text="acornfox lan-console enable" />
        <div className="text-2xs text-muted-foreground">只给局域网里的地址用，云主机开不了。第一次打开时在浏览器里设密码。</div>
      </div>
    )
  }
  const pw = passwordLine(l)
  return (
    <div className="flex flex-col gap-2.5">
      <Muted>同一个局域网里的电脑、手机，用浏览器打开 <span className="mono text-foreground">{l.url || '这台机器的局域网地址，端口 ' + l.port}</span>，输密码就能进。</Muted>
      {pw && <p className={l.password_set === false ? 'text-xs text-warning' : 'text-2xs text-muted-foreground'}>{pw}</p>}
      {l.password_set_at && <p className="text-2xs text-muted-foreground">设密码的时间和地址不是你？在服务器上运行下面第一条命令清掉密码，再打开局域网地址重新设；所有登录和记住的设备会一起作废。</p>}
      <LANDevices onChanged={onChanged} />
      <div className="flex flex-col gap-1.5">
        <div className="text-2xs text-muted-foreground">忘了密码或要关掉，在服务器上运行：</div>
        <CopyLine text="acornfox lan-console reset-password" />
        <CopyLine text="acornfox lan-console disable" />
      </div>
    </div>
  )
}

// ---- 在外面打开控制台 ----

function pubSummary(p: PublicConsoleStatus): { tone: RowTone; summary: string } {
  if (p.enabled) return { tone: 'success', summary: '已开启 · ' + (p.domain || p.url?.replace(/^https?:\/\//, '') || '') + ' · 要密码登录' }
  if (p.unsupported) return { tone: '', summary: p.unsupported.text }
  if (p.verifying) return { tone: '', summary: '正在验证域名 ' + p.verifying }
  return { tone: '', summary: '没开启。开启后人在外面也能用浏览器打开' }
}

const pubURL = (p: PublicConsoleStatus) => p.url || (p.domain ? 'https://' + p.domain : '')

function Tunnel() {
  return (
    <div className="flex flex-col gap-1.5" data-testid="pub-tunnel">
      <div className="text-2xs text-muted-foreground">不想对外开放？人在外面时，在自己的电脑上运行这条命令，它会走安全通道连上并打开浏览器：</div>
      <CopyLine text="acornfox open" />
    </div>
  )
}

// 服务端不让页面开关（旧服务端没有 can_toggle，或这次登录不行）：只给服务器上的命令。
function ServerOnly({ p }: { p: PublicConsoleStatus }) {
  return (
    <div className="flex flex-col gap-1.5" data-testid="pub-server-only">
      <div className="text-2xs text-muted-foreground">{p.can_toggle === false ? '这次登录不能在页面上开关公网控制台，要在服务器上运行：' : '要开关，在服务器上运行：'}</div>
      <CopyLine text={p.enabled ? 'acornfox public-console disable' : 'acornfox public-console enable 你的域名'} />
    </div>
  )
}

function PasswordField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div className="mt-4 flex flex-col gap-1.5">
      <Label htmlFor="pub-current-password">再输一次现在的控制台密码</Label>
      <Input id="pub-current-password" type="password" autoComplete="current-password" value={value} onChange={(e) => onChange(e.target.value)} />
    </div>
  )
}

function PubBody({ p, onChanged }: { p: PublicConsoleStatus; onChanged: () => Promise<unknown> }) {
  const toast = useToast()
  const [domain, setDomain] = React.useState(p.suggested_domain || '')
  const [dialog, setDialog] = React.useState<'' | 'enable' | 'disable'>('')
  const [password, setPassword] = React.useState('')
  const [busy, setBusy] = React.useState(false)
  const [err, setErr] = React.useState('')
  const [shown, setShown] = React.useState<{ url: string; password: string } | null>(null)
  const [copied, setCopied] = React.useState(false)
  // 平台域名后来才配好：输入框还空着就填上
  React.useEffect(() => { if (p.suggested_domain) setDomain((d) => d || p.suggested_domain || '') }, [p.suggested_domain])

  const needPw = !!p.needs_password
  const target = normDomain(domain)
  // 开着时问证书好了没有；换了地址或重新开启就从头问
  const cert = useCertState(p.enabled, (p.domain || '') + '|' + (p.enabled_at || ''))
  const open = (d: 'enable' | 'disable') => { setErr(''); setPassword(''); setDialog(d) }
  const close = () => { setDialog(''); setPassword(''); setErr('') }
  const enable = async () => {
    setBusy(true)
    setErr('')
    try {
      const r = await api.post<PublicConsoleEnableResult>('/v1/public-console/enable', needPw ? { domain: target, password } : { domain: target })
      close()
      const pw = r.password || r.password_once || ''
      const url = r.url || 'https://' + (r.domain || target)
      if (pw) setShown({ url, password: pw })
      else toast('公网控制台已开启', url)
      await onChanged()
    } catch (e) {
      setErr(errText(e))
    } finally {
      setBusy(false)
    }
  }
  const disable = async () => {
    setBusy(true)
    setErr('')
    try {
      await api.send('DELETE', '/v1/public-console', needPw ? { password } : undefined)
      close()
      toast('公网控制台已关闭', '外面打不开控制台了')
      await onChanged()
    } catch (e) {
      setErr(errText(e))
    } finally {
      setBusy(false)
    }
  }
  const copyURL = async () => {
    if (!(await copyText(pubURL(p)))) { toast('没能复制', '浏览器不允许，请手动选中复制', 'error'); return }
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1200)
  }

  let body: React.ReactNode
  if (p.unsupported && !p.enabled) {
    body = (
      <>
        <Muted>{p.unsupported.text}<Code code={p.unsupported.code} /></Muted>
        <Tunnel />
      </>
    )
  } else if (p.enabled) {
    body = (
      <>
        <Muted>在任何地方用浏览器打开下面的地址，输密码就能进控制台。</Muted>
        <div className="flex flex-wrap items-center gap-2">
          <span className="mono mr-1.5 min-w-0 max-w-full truncate text-sm" data-testid="pub-url">{pubURL(p)}</span>
          <Button size="sm" variant="outline" onClick={copyURL}>{copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}复制地址</Button>
          {p.can_toggle && <Button size="sm" variant="outline" className="text-danger hover:text-danger" onClick={() => open('disable')}>关闭公网控制台</Button>}
        </div>
        <CertLine s={cert} url={pubURL(p)} where="row" />
        {p.enabled_at && <div className="text-2xs text-muted-foreground">{timeText(p.enabled_at)} 开的</div>}
        {p.can_toggle ? <div className="text-2xs text-muted-foreground">忘了密码：关闭后重新开启，会给一个新密码。</div> : <ServerOnly p={p} />}
      </>
    )
  } else {
    body = (
      <>
        <Muted>开启后，在任何地方用浏览器打开下面的地址，输密码就能进控制台。</Muted>
        {p.can_toggle ? (
          <>
            <form className="flex flex-wrap items-center gap-2" onSubmit={(e) => { e.preventDefault(); if (target) open('enable') }}>
              <Input className="min-w-0 max-w-sm flex-1 basis-56" aria-label="公网控制台的域名" placeholder="填一个指到这台机器的域名" value={domain} onChange={(e) => setDomain(e.target.value)} autoComplete="off" spellCheck={false} />
              <Button type="submit" disabled={!target}>开启公网控制台</Button>
            </form>
            <div className="text-2xs text-muted-foreground">
              {p.suggested_domain ? '已经按上面的域名填好了，也可以换成自己的域名。' : '在上面“域名”那一行配好域名，这里会自动填上；也可以填一个自己的、已经指到这台机器的域名。'}
            </div>
          </>
        ) : <ServerOnly p={p} />}
        <Tunnel />
      </>
    )
  }

  return (
    <div className="flex flex-col gap-2.5" data-testid="pub-body">
      {body}
      <Dialog open={dialog === 'enable'} onClose={close} labelledBy="pub-enable-title">
        <DialogHead id="pub-enable-title" title="开启公网控制台？" desc={'地址：https://' + target} />
        <ul className="flex list-disc flex-col gap-1 pl-5 text-sm">
          <li>互联网上任何人都能打开登录页，只靠密码挡着。</li>
          <li>会生成一个新密码，只显示这一次，请马上记下。</li>
          <li>随时可以回到这里关闭。</li>
        </ul>
        {needPw && <PasswordField value={password} onChange={setPassword} />}
        {err && <p className="mt-3 text-xs text-danger" role="alert">{err}</p>}
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" onClick={close}>取消</Button>
          <Button disabled={busy || (needPw && !password)} onClick={enable}>{busy && <Spinner />}开启</Button>
        </div>
      </Dialog>
      <Dialog open={dialog === 'disable'} onClose={close} labelledBy="pub-disable-title">
        <DialogHead id="pub-disable-title" title="关闭公网控制台？" desc={pubURL(p) ? '地址：' + pubURL(p) : undefined} />
        <ul className="flex list-disc flex-col gap-1 pl-5 text-sm">
          <li>外面打不开控制台的登录页了。</li>
          <li>局域网里打开、走安全通道不受影响。</li>
          <li>以后再开启，会给一个新密码。</li>
        </ul>
        {needPw && <PasswordField value={password} onChange={setPassword} />}
        {err && <p className="mt-3 text-xs text-danger" role="alert">{err}</p>}
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" onClick={close}>取消</Button>
          <Button variant="destructive" disabled={busy || (needPw && !password)} onClick={disable}>{busy && <Spinner />}关闭公网控制台</Button>
        </div>
      </Dialog>
      <Dialog open={!!shown} onClose={() => setShown(null)} labelledBy="pub-pw-title">
        <DialogHead id="pub-pw-title" title="公网控制台已开启" desc={shown ? '地址：' + shown.url : undefined} />
        <div className="mb-3 empty:hidden"><CertLine s={cert} url={shown?.url || ''} where="dialog" /></div>
        <div className="text-xs font-medium text-warning">登录密码只显示这一次，请马上记下来。关掉这个窗口后就再也看不到了。</div>
        <div className="mt-2 flex items-center gap-2 rounded-md border bg-muted/40 px-3 py-2.5" data-testid="pub-new-password">
          <code className="mono min-w-0 flex-1 select-all break-all text-lg font-semibold tracking-wide">{shown?.password}</code>
          {shown && <CopyButton text={shown.password} label="复制密码" />}
        </div>
        <div className="mt-2 text-2xs text-muted-foreground">忘了的话：关闭公网控制台再重新开启，会给一个新密码。</div>
        <div className="mt-5 flex justify-end">
          <Button onClick={() => setShown(null)}>我记下了</Button>
        </div>
      </Dialog>
    </div>
  )
}

// ---- 整页 ----

export function Access() {
  const [open, setOpen] = React.useState<Key | ''>('')
  const dom = usePoll((s) => api.get<PlatformDomainView>('/v1/platform/domain', s).catch((e) => {
    if (e instanceof ApiError && e.status === 404) return null
    throw e
  }), 0)
  const lan = usePoll((s) => api.get<LANConsoleStatus>('/v1/lan-console', s), 0)
  const pub = usePoll((s) => api.get<PublicConsoleStatus>('/v1/public-console', s), 0)
  const toggle = (k: Key) => setOpen((o) => (o === k ? '' : k))
  const expand = (k: Key) => setOpen(k)

  const d = dom.loading ? null : domainRow(dom.data, dom.error)
  const l = lan.data ? lanSummary(lan.data) : null
  const p = pub.data ? pubSummary(pub.data) : null
  const loading = dom.loading && lan.loading && pub.loading
  const canEnable = !!pub.data && !pub.data.enabled && !pub.data.unsupported && !!pub.data.can_toggle
  const unset = !!dom.data && dom.data.supported !== false && dom.data.state !== 'ok' && dom.data.state !== 'problem'
  return (
    <>
      {loading && <div className="flex items-center gap-2 py-2.5 text-xs text-muted-foreground"><Spinner />正在读取…</div>}
      {d && (
        <ExpandRow className="-mx-4" id="acc-domain" name="域名" wide tone={d.tone} summary={d.summary} summaryTestId="summary-domain" open={open === 'domain'} onToggle={() => toggle('domain')}
          right={unset && open !== 'domain' ? <Button size="sm" variant="outline" onClick={() => expand('domain')}>配置</Button> : undefined}>
          {dom.data && <DomainBody view={dom.data} setView={(v) => dom.setData(v)} onChanged={() => { void pub.refresh() }} />}
        </ExpandRow>
      )}
      {lan.data && l && (
        <ExpandRow className="-mx-4" id="acc-lan" name="局域网里打开控制台" wide tone={l.tone} summary={l.summary} summaryTestId="summary-lan" open={open === 'lan'} onToggle={() => toggle('lan')}>
          <LANBody l={lan.data} onChanged={() => { void lan.refresh() }} />
        </ExpandRow>
      )}
      {pub.data && p && (
        <ExpandRow className="-mx-4" id="acc-pub" name="在外面打开控制台" wide tone={p.tone} summary={p.summary} summaryTestId="summary-pub" open={open === 'pub'} onToggle={() => toggle('pub')}
          right={canEnable && open !== 'pub' ? <Button size="sm" variant="outline" onClick={() => expand('pub')}>开启</Button> : undefined}>
          <PubBody p={pub.data} onChanged={() => pub.refresh()} />
        </ExpandRow>
      )}
      {!loading && !dom.loading && !lan.loading && !pub.loading && !d && !lan.data && !pub.data && (
        <div className="py-2.5 text-xs text-muted-foreground">这台服务器不支持把控制台开到局域网或公网</div>
      )}
    </>
  )
}
