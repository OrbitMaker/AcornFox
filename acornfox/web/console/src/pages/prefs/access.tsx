// 设置 › 访问：一行一项、点开才展开（和应用的“设置”同一个样子，行在 components/expand-row.tsx），同一时间只展开一项。
// 样稿第 10 版（评估 N7、N8、N9、R7、X4、X8）：叫法是“这台机器的域名”“在家里打开控制台”“从外面打开控制台”（不说平台域名、
// 局域网控制台、公网控制台）；命令都收进默认收起的“用命令行”，故障编号不显示（要查时“复制给 AI 工作台”）。
//   这台机器的域名：公网应用自动得到 <应用名>.<域名>。三种样子：正常、还没有、还没生效。旧服务端没有这个接口（404）时整行不显示。
//   在家里打开控制台：点开的顺序是 地址（和用哪个密码）、记住的设备（能移除）、“忘了密码或要关掉”（默认收起，
//         里面才是“密码是什么时候从哪设的”的提醒和服务器上的命令）。
//   从外面打开控制台：开没开；服务端允许时（can_toggle）页面上能开、能关，都先弹确认框，
//         needs_password 时要再输一次现在的控制台密码；开启成功给的新密码只显示这一次。不想对外开放给 acornfox open（收起）。
//         开着时会问服务端证书好了没有（cert-wait.tsx）：第一次开启要等一两分钟，好了再说可以打开。
//   家里的机器（不在公网上，onPublic；旧服务端看域名和从外面打开两个接口都说用不了）：上面两行并成一行灰字
//         “这台机器不在公网上，域名和从外面打开都用不上。”，点开只讲人在外面想用怎么办。这是正常情况，不算问题。
//   两个入口各用哪个密码写在各自展开里（评估 R7）：在家里打开用你第一次打开时自己设的；从外面打开用开启时给的那一个。
// 接口：GET/PUT/DELETE /v1/platform/domain、POST /v1/platform/domain/check；
//       GET /v1/lan-console、GET/DELETE /v1/lan-console/devices[/{id}]；
//       GET /v1/public-console[?check=https]、POST /v1/public-console/enable {domain,password}、DELETE /v1/public-console {password}。
// 密码只放在组件状态里，关掉弹窗就清掉，不写进地址、存储和日志。
import * as React from 'react'
import { Check, Copy, Info } from 'lucide-react'
import { ApiError, api } from '@/lib/api'
import { copyText } from '@/lib/clipboard'
import { usePoll } from '@/lib/hooks'
import { timeText } from '@/lib/format'
import { normDomain, recordName } from '@/lib/platform-domain'
import { onPublic } from '@/lib/display'
import type { LANConsoleStatus, LANDevice, PlatformDomainView, PublicConsoleEnableResult, PublicConsoleStatus } from '@/lib/types'
import { useConsole } from '@/state'
import { ExpandRow, type RowTone } from '@/components/expand-row'
import { AskAI, CliFold, Fold } from '@/components/fold'
import { Button } from '@/components/ui/button'
import { Loading } from '@/components/ui/skeleton'
import { SkRows } from '@/components/skeletons'
import { CopyButton, CopyLine } from '@/components/ui/copy'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { Input, Label } from '@/components/ui/input'
import { useToast } from '@/components/ui/toast'
import { CertLine, useCertState } from './cert-wait'

type Key = 'domain' | 'lan' | 'pub' | 'away'
const EXAMPLE = 'apps.example.com'

// errText：出错时给人看的话，不带故障编号。
function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

function Muted({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <p className={'text-xs text-muted-foreground ' + className}>{children}</p>
}

// Tunnel：人在外面、不想把控制台开到公网上时怎么办（命令收起）。
function Tunnel({ title = '不想对外开放？在自己的电脑上用命令行走安全通道打开' }: { title?: string }) {
  return (
    <CliFold title={title} className="pt-0.5">
      <CopyLine text="acornfox open" />
      <p className="text-2xs text-muted-foreground">在你自己的电脑上运行（命令行的安装方法见 <a href="#prefs/dev" className="underline underline-offset-2">设置 › 开发者与 AI</a>），它会走安全通道连上这台机器并打开浏览器。</p>
    </CliFold>
  )
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
        <Input className="min-w-0 max-w-sm flex-1 basis-56" aria-label="这台机器的域名" placeholder={'例如 ' + EXAMPLE} value={value} onChange={(e) => onChange(e.target.value)} autoComplete="off" spellCheck={false} />
        <Button type="submit" disabled={!typed} loading={busy}>{submitText}</Button>
      </form>
    </div>
  )
}

function domainRow(v: PlatformDomainView | null, err: Error | null): { tone: RowTone; summary: string } | null {
  if (!v) return err ? { tone: '', summary: '没读到：' + err.message } : null
  if (v.supported === false) return { tone: '', summary: '这台机器不在公网上，用不上域名' }
  if (v.state === 'ok') return { tone: 'success', summary: (v.domain || '') + ' · 正常' }
  if (v.state === 'problem') return { tone: 'danger', summary: (v.domain || '') + ' · 还没生效：' + (v.problem?.text || '检查没过') }
  return { tone: '', summary: '还没有。配一个，应用就不用记数字地址了' }
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
      // 没做成：确认窗开着就写在窗里，否则写在这一块的按钮下面（样稿第 14 版，不弹提示）
      setErr((key === 'save' ? '没能保存：' : key === 'check' ? '没能检查：' : '没能完成：') + errText(e))
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
    // 还没生效：这一块上面已经原位写着原因（view.problem），不再弹提示
    if (v.state === 'ok') toast('域名正常了', v.domain)
  })
  const clear = () => run('clear', () => api.del<PlatformDomainView>('/v1/platform/domain?confirm=true'), () => {
    setDialog('')
    toast('不用域名了', '应用回到原来的地址')
  })
  const openDialog = (d: 'change' | 'clear') => { setErr(''); setNext(''); setDialog(d) }

  if (view.supported === false) {
    return <Muted>这台机器不在公网上，地址前面的小锁要从公网验证，配了也用不上。应用照常用原来的地址打开。</Muted>
  }
  if (view.state !== 'ok' && view.state !== 'problem') {
    return (
      <>
        <DomainSteps view={view} value={input} onChange={setInput} onSubmit={() => save(input)} busy={busy === 'save'} submitText="保存" />
        {err && !dialog && <p className="mt-2 text-xs text-danger" role="alert">{err}</p>}
      </>
    )
  }
  const changeBtn = <Button variant="outline" size="sm" disabled={!!busy} onClick={() => openDialog('change')}>换一个域名</Button>
  const clearBtn = <Button variant="outline" size="sm" className="text-danger hover:text-danger" disabled={!!busy} onClick={() => openDialog('clear')}>不用域名了</Button>
  return (
    <div className="flex flex-col gap-2.5" data-testid="domain-body">
      {view.state === 'ok' ? (
        <>
          <Muted>开放到公网的应用，地址是 <span className="mono text-foreground">应用名.{view.domain}</span>；地址前面有小锁，浏览器不会报不安全。</Muted>
          <div className="flex flex-wrap gap-x-4 gap-y-1">
            {(view.checks || []).map((c) => (
              <span key={c.key} className="inline-flex items-center gap-1 text-xs">
                <Check className={c.ok ? 'size-icon text-success' : 'size-icon text-muted-foreground'} aria-hidden="true" />{c.text}
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
        {view.state === 'problem' && <Button size="sm" disabled={!!busy} onClick={check} loading={busy === 'check'}>重新检查</Button>}
        {changeBtn}
        {clearBtn}
        {view.state === 'problem' && <AskAI variant="ghost" text={`这台机器的域名 ${view.domain} 还没生效：${view.problem?.code || ''} ${view.problem?.text || ''}${view.problem?.detail ? '（' + view.problem.detail + '）' : ''}。用 acornfox skill 帮我查（acornfox domain base --json）。`} />}
      </div>
      {err && !dialog && <p className="text-xs text-danger" role="alert">{err}</p>}
      <Dialog open={dialog === 'change'} onClose={() => setDialog('')} labelledBy="pd-change-title">
        <DialogHead id="pd-change-title" title="换一个域名" desc={'换了以后，用 ' + view.domain + ' 的应用地址都会跟着换，原来的地址打不开。'} />
        <DomainSteps view={view} value={next} onChange={setNext}
          onSubmit={() => save(next, () => setDialog(''))} busy={busy === 'save'} submitText="换成这个" />
        {err && dialog === 'change' && <p className="mt-3 text-xs text-danger" role="alert">{err}</p>}
      </Dialog>
      <Dialog open={dialog === 'clear'} onClose={() => setDialog('')} labelledBy="pd-clear-title">
        <DialogHead id="pd-clear-title" title="不用域名了？" />
        <ul className="flex list-disc flex-col gap-1 pl-5 text-sm">
          <li>开放到公网的应用不再有 <span className="mono">应用名.{view.domain}</span> 这个地址，回到原来的地址（数字地址加端口）。</li>
          <li>应用自己绑的域名不受影响。</li>
          <li>以后可以再配。</li>
        </ul>
        {err && dialog === 'clear' && <p className="mt-3 text-xs text-danger" role="alert">{err}</p>}
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" onClick={() => setDialog('')}>取消</Button>
          <Button variant="destructive" onClick={clear} loading={busy === 'clear'}>不用了</Button>
        </div>
      </Dialog>
    </div>
  )
}

// ---- 局域网里打开控制台 ----

function lanSummary(l: LANConsoleStatus): { tone: RowTone; summary: string } {
  if (!l.enabled) return { tone: '', summary: '没开启。同一个网络里的电脑、手机打不开控制台' }
  const where = l.url ? l.url.replace(/^https?:\/\//, '') : '端口 ' + l.port
  if (l.password_set === false) return { tone: 'warning', summary: '已开启 · ' + where + ' · 还没设密码' }
  return { tone: 'success', summary: '已开启 · ' + where + (l.devices ? ' · 记住了 ' + l.devices + ' 台设备' : '') }
}

// whichPassword：在家里打开用哪个密码（评估 R7，只写明，不改机制）。
function whichPassword(l: LANConsoleStatus): string {
  if (l.password_set === false) return '还没设密码：请现在就用家里的电脑或手机打开上面的地址，设一个'
  if (l.generated_password) return '用开启时给的那个密码登录；和从外面打开用的不是同一个'
  return '用你第一次打开时自己设的密码登录；和从外面打开用的不是同一个'
}

// setAtLine：密码是什么时候从哪设的（收在“忘了密码或要关掉”里）。
function setAtLine(l: LANConsoleStatus): string {
  return l.password_set_at ? `密码是 ${timeText(l.password_set_at)} 从 ${l.password_set_from || '一个不知道的地址'} 设的` : ''
}

// 记住的设备：列表和移除。移除只会让那台设备重新输密码，所以页面上可以点（要带会话的校验值）。
function LANDevices({ onChanged }: { onChanged: () => void }) {
  const toast = useToast()
  const list = usePoll((s) => api.get<{ devices: LANDevice[] }>('/v1/lan-console/devices', s).then((b) => b.devices || []).catch(() => null), 0)
  const [busy, setBusy] = React.useState('')
  // 没移除：出错的话留在那台设备那一行（全部移除的留在标题那一行下面），不弹提示（样稿第 14 版）
  const [fail, setFail] = React.useState<{ id: string; text: string } | null>(null)
  if (!list.data) return null
  const remove = async (id: string) => {
    setBusy(id || '*')
    setFail(null)
    try {
      await api.send('DELETE', '/v1/lan-console/devices' + (id ? '/' + encodeURIComponent(id) : ''))
      toast(id ? '已移除这台设备' : '已移除全部设备', '它们要重新输密码才能进')
      await list.refresh()
      onChanged()
    } catch (e) {
      setFail({ id: id || '*', text: '没能移除' + (e instanceof Error && e.message ? '：' + e.message : '') })
    } finally {
      setBusy('')
    }
  }
  return (
    <div className="rounded-md border" data-testid="lan-devices">
      <div className="flex items-center justify-between border-b px-3 py-2 text-xs font-medium">
        <span>记住的设备</span>
        {list.data.length > 1 && <Button size="sm" variant="ghost" disabled={!!busy} loading={busy === '*'} onClick={() => remove('')}>全部移除</Button>}
      </div>
      {fail?.id === '*' && <p className="border-b px-3 py-2 text-xs text-danger" role="alert">{fail.text}</p>}
      {list.data.length === 0 && <div className="px-3 py-2.5 text-xs text-muted-foreground">没有</div>}
      {list.data.map((d) => (
        <div key={d.id} className="flex flex-wrap items-center gap-x-3 border-b px-3 py-2 text-xs last:border-b-0">
          <div className="min-w-0 flex-1">
            <div className="truncate font-medium">{d.name}</div>
            <div className="truncate text-2xs text-muted-foreground">上次使用 {timeText(d.last_used_at)}{d.last_ip ? ' · ' + d.last_ip : ''} · {d.expires_at.slice(0, 10)} 到期</div>
          </div>
          <Button size="sm" variant="outline" disabled={!!busy} loading={busy === d.id} aria-label={'移除 ' + d.name} onClick={() => remove(d.id)}>移除</Button>
          {fail?.id === d.id && <p className="basis-full pt-1 text-danger" role="alert">{fail.text}</p>}
        </div>
      ))}
    </div>
  )
}

function LANBody({ l, onChanged }: { l: LANConsoleStatus; onChanged: () => void }) {
  if (!l.enabled) {
    return (
      <div className="flex flex-col gap-1.5">
        <Muted>现在只有走安全通道才能打开控制台。家里或公司里的机器可以开启，让同一个网络里的电脑、手机也能打开；云服务器上开不了。</Muted>
        <CliFold title="在这台机器上用命令行开启">
          <CopyLine text="acornfox lan-console enable" />
          <p className="text-2xs text-muted-foreground">开启以后，第一次打开时在浏览器里设密码。</p>
        </CliFold>
      </div>
    )
  }
  const setAt = setAtLine(l)
  return (
    <div className="flex flex-col gap-2.5">
      <Muted>同一个网络里的电脑、手机，用浏览器打开 <span className="mono text-foreground">{l.url || '这台机器的地址，端口 ' + l.port}</span>，输密码就能进。</Muted>
      <p className={l.password_set === false ? 'text-xs text-warning' : 'text-2xs text-muted-foreground'} data-testid="lan-which-password">{whichPassword(l)}</p>
      <LANDevices onChanged={onChanged} />
      <Fold title="忘了密码或要关掉" className="pt-0.5">
        {setAt && <p className="text-2xs text-muted-foreground">{setAt}。不是你设的？照下面第一条清掉密码重新设。</p>}
        <p className="text-2xs text-muted-foreground">在这台机器上运行：第一条清掉密码，再打开上面的地址重新设一个（所有登录和记住的设备会一起作废）；第二条关掉，家里的电脑、手机就打不开了。</p>
        <CopyLine text="acornfox lan-console reset-password" />
        <CopyLine text="acornfox lan-console disable" />
      </Fold>
    </div>
  )
}

// ---- 在外面打开控制台 ----

function pubSummary(p: PublicConsoleStatus): { tone: RowTone; summary: string } {
  if (p.enabled) return { tone: 'success', summary: '已开启 · ' + (p.domain || p.url?.replace(/^https?:\/\//, '') || '') + ' · 要密码登录' }
  if (p.unsupported) return { tone: '', summary: '这台机器不在公网上，开不了' }
  if (p.verifying) return { tone: '', summary: '正在核对域名 ' + p.verifying }
  return { tone: '', summary: '没开启。开启后人在外面也能用浏览器打开' }
}

const pubURL = (p: PublicConsoleStatus) => p.url || (p.domain ? 'https://' + p.domain : '')

// 服务端不让页面开关（旧服务端没有 can_toggle，或这次登录不行）：只给这台机器上的命令（收起）。
function ServerOnly({ p }: { p: PublicConsoleStatus }) {
  return (
    <div className="flex flex-col gap-1.5" data-testid="pub-server-only">
      {p.can_toggle === false && <div className="text-2xs text-muted-foreground">这次登录不能在页面上开关。</div>}
      <CliFold title={p.enabled ? '在这台机器上用命令行关掉' : '在这台机器上用命令行开启'}>
        <CopyLine text={p.enabled ? 'acornfox public-console disable' : 'acornfox public-console enable 你的域名'} />
      </CliFold>
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
  // 没能复制：出错的话留在地址那一行下面（样稿第 14 版）
  const [copyErr, setCopyErr] = React.useState(false)
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
      else toast('外面也能打开控制台了', url)
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
      toast('已关掉', '外面打不开控制台了')
      await onChanged()
    } catch (e) {
      setErr(errText(e))
    } finally {
      setBusy(false)
    }
  }
  const copyURL = async () => {
    if (!(await copyText(pubURL(p)))) { setCopyErr(true); return }
    setCopyErr(false)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1200)
  }

  let body: React.ReactNode
  if (p.unsupported && !p.enabled) {
    body = (
      <>
        <Muted>这台机器不在公网上，从外面打不开。</Muted>
        <Tunnel title="人在外面想用？在自己的电脑上用命令行走安全通道打开" />
      </>
    )
  } else if (p.enabled) {
    body = (
      <>
        <Muted>在任何地方用浏览器打开下面的地址，输密码就能进控制台。</Muted>
        <div className="flex flex-wrap items-center gap-2">
          <span className="mono mr-1.5 min-w-0 max-w-full select-all truncate text-sm" data-testid="pub-url">{pubURL(p)}</span>
          <Button size="sm" variant="outline" onClick={copyURL}>{copied ? <Check className="size-icon" /> : <Copy className="size-icon" />}复制地址</Button>
          {p.can_toggle && <Button size="sm" variant="outline" className="text-danger hover:text-danger" onClick={() => open('disable')}>不让从外面打开</Button>}
        </div>
        {copyErr && <p className="text-xs text-danger" role="alert">没能复制，请手动选中上面的地址</p>}
        <CertLine s={cert} url={pubURL(p)} where="row" />
        <p className="text-2xs text-muted-foreground" data-testid="pub-which-password">
          用开启时给的那个密码登录（只显示过一次），和在家里打开用的不是同一个。{p.can_toggle && '忘了：关掉再重新开启，会给一个新密码。'}
        </p>
        {p.enabled_at && <div className="text-2xs text-muted-foreground">{timeText(p.enabled_at)} 开的</div>}
        {!p.can_toggle && <ServerOnly p={p} />}
      </>
    )
  } else {
    body = (
      <>
        <Muted>开启后，在任何地方用浏览器打开下面的地址，输密码就能进控制台。</Muted>
        {p.can_toggle ? (
          <>
            <form className="flex flex-wrap items-center gap-2" onSubmit={(e) => { e.preventDefault(); if (target) open('enable') }}>
              <Input className="min-w-0 max-w-sm flex-1 basis-56" aria-label="从外面打开控制台用的域名" placeholder="填一个指到这台机器的域名" value={domain} onChange={(e) => setDomain(e.target.value)} autoComplete="off" spellCheck={false} />
              <Button type="submit" disabled={!target}>开启</Button>
            </form>
            <div className="text-2xs text-muted-foreground">
              {p.suggested_domain ? '已经按这台机器的域名填好了，也可以换成自己的域名。' : '在上面“这台机器的域名”那一行配好域名，这里会自动填上；也可以填一个已经指到这台机器的域名。'}
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
        <DialogHead id="pub-enable-title" title="让外面也能打开控制台？" desc={'地址：https://' + target} />
        <ul className="flex list-disc flex-col gap-1 pl-5 text-sm">
          <li>互联网上任何人都能打开登录页，只靠密码挡着。</li>
          <li>会生成一个新密码，只显示这一次，请马上记下。</li>
          <li>随时可以回到这里关闭。</li>
        </ul>
        {needPw && <PasswordField value={password} onChange={setPassword} />}
        {err && <p className="mt-3 text-xs text-danger" role="alert">{err}</p>}
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" onClick={close}>取消</Button>
          <Button disabled={(needPw && !password)} onClick={enable} loading={busy}>开启</Button>
        </div>
      </Dialog>
      <Dialog open={dialog === 'disable'} onClose={close} labelledBy="pub-disable-title">
        <DialogHead id="pub-disable-title" title="不让从外面打开控制台？" desc={pubURL(p) ? '地址：' + pubURL(p) : undefined} />
        <ul className="flex list-disc flex-col gap-1 pl-5 text-sm">
          <li>外面打不开控制台的登录页了。</li>
          <li>在家里打开、走安全通道不受影响。</li>
          <li>以后再开启，会给一个新密码。</li>
        </ul>
        {needPw && <PasswordField value={password} onChange={setPassword} />}
        {err && <p className="mt-3 text-xs text-danger" role="alert">{err}</p>}
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" onClick={close}>取消</Button>
          <Button variant="destructive" disabled={(needPw && !password)} onClick={disable} loading={busy}>关掉</Button>
        </div>
      </Dialog>
      <Dialog open={!!shown} onClose={() => setShown(null)} labelledBy="pub-pw-title">
        <DialogHead id="pub-pw-title" title="外面也能打开控制台了" desc={shown ? '地址：' + shown.url : undefined} />
        <div className="mb-3 empty:hidden"><CertLine s={cert} url={shown?.url || ''} where="dialog" /></div>
        <div className="text-xs font-medium text-warning">登录密码只显示这一次，请马上记下来。关掉这个窗口后就再也看不到了。</div>
        <div className="mt-2 flex items-center gap-2 rounded-md border bg-muted/40 px-3 py-2.5" data-testid="pub-new-password">
          <code className="mono min-w-0 flex-1 select-all break-all text-lg font-semibold tracking-wide">{shown?.password}</code>
          {shown && <CopyButton text={shown.password} label="复制密码" />}
        </div>
        <div className="mt-2 text-2xs text-muted-foreground">这个密码只用于从外面打开，和在家里打开用的不是同一个。忘了的话：关掉再重新开启，会给一个新密码。</div>
        <div className="mt-5 flex justify-end">
          <Button onClick={() => setShown(null)}>我记下了</Button>
        </div>
      </Dialog>
    </div>
  )
}

// ---- 整页 ----

// AwayBody：家里的机器上那一行灰字点开以后：只讲人在外面想用怎么办。
function AwayBody() {
  return (
    <div className="flex flex-col gap-2 text-xs" data-testid="access-away">
      <p>在家里或公司里用，不用管它。</p>
      <Muted>人在外面想用控制台：在你自己的电脑上走安全通道打开，这台机器不用放到公网上。想让应用从外面也能打开，要把它装到一台在公网上的机器（例如云服务器）。</Muted>
      <Tunnel title="在自己的电脑上用命令行打开" />
    </div>
  )
}

// LogoutRow：最下面一行，退出登录（样稿第 11 版：右上角的头像去掉了，它菜单里的退出登录挪到这里）。
function LogoutRow() {
  const { logout } = useConsole()
  const [busy, setBusy] = React.useState(false)
  return (
    <div className="-mx-4 flex items-center gap-3 px-4 py-row" data-testid="access-logout">
      <span className="w-28 shrink-0 text-sm font-medium sm:w-36">这个浏览器</span>
      <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">已登录</span>
      <Button size="sm" variant="outline" loading={busy} onClick={() => { setBusy(true); void logout() }}>退出登录</Button>
    </div>
  )
}

export function Access() {
  const { status } = useConsole()
  const [open, setOpen] = React.useState<Key | ''>('')
  const dom = usePoll((s) => api.get<PlatformDomainView>('/v1/platform/domain', s).catch((e) => {
    if (e instanceof ApiError && e.status === 404) return null
    throw e
  }), 0)
  const lan = usePoll((s) => api.get<LANConsoleStatus>('/v1/lan-console', s), 0)
  const pub = usePoll((s) => api.get<PublicConsoleStatus>('/v1/public-console', s), 0)
  const toggle = (k: Key) => setOpen((o) => (o === k ? '' : k))
  const expand = (k: Key) => setOpen(k)

  // 家里的机器：状态里说了不在公网上；旧服务端没给 on_public 时，域名和从外面打开两个接口都说用不了也算。
  const home = status?.on_public !== undefined
    ? !onPublic(status)
    : !!dom.data && dom.data.supported === false && !!pub.data && !!pub.data.unsupported && !pub.data.enabled
  const d = dom.loading ? null : domainRow(dom.data, dom.error)
  const l = lan.data ? lanSummary(lan.data) : null
  const p = pub.data ? pubSummary(pub.data) : null
  const loading = dom.loading && lan.loading && pub.loading
  const canEnable = !!pub.data && !pub.data.enabled && !pub.data.unsupported && !!pub.data.can_toggle
  const unset = !!dom.data && dom.data.supported !== false && dom.data.state !== 'ok' && dom.data.state !== 'problem'
  // 家里的机器上，公网控制台已经开着（老机器）时那一行照旧显示，免得关不掉
  const showPub = !!pub.data && !!p && (!home || pub.data.enabled)
  const lanRow = lan.data && l && (
    <ExpandRow className="-mx-4" id="acc-lan" name="在家里打开控制台" wide tone={l.tone} summary={l.summary} summaryTestId="summary-lan" open={open === 'lan'} onToggle={() => toggle('lan')}>
      <LANBody l={lan.data} onChanged={() => { void lan.refresh() }} />
    </ExpandRow>
  )
  return (
    <>
      <Loading when={loading} skeleton={<SkRows className="-mx-4" n={2} />}>{null}</Loading>
      {home && lanRow}
      {!home && d && (
        <ExpandRow className="-mx-4" id="acc-domain" name="这台机器的域名" wide tone={d.tone} summary={d.summary} summaryTestId="summary-domain" open={open === 'domain'} onToggle={() => toggle('domain')}
          right={unset && open !== 'domain' ? <Button size="sm" variant="outline" onClick={() => expand('domain')}>配置</Button> : undefined}>
          {dom.data && <DomainBody view={dom.data} setView={(v) => dom.setData(v)} onChanged={() => { void pub.refresh() }} />}
        </ExpandRow>
      )}
      {!home && lanRow}
      {showPub && pub.data && p && (
        <ExpandRow className="-mx-4" id="acc-pub" name="从外面打开控制台" wide tone={p.tone} summary={p.summary} summaryTestId="summary-pub" open={open === 'pub'} onToggle={() => toggle('pub')}
          right={canEnable && open !== 'pub' ? <Button size="sm" variant="outline" onClick={() => expand('pub')}>开启</Button> : undefined}>
          <PubBody p={pub.data} onChanged={() => pub.refresh()} />
        </ExpandRow>
      )}
      {home && !loading && (
        <div data-testid="access-not-public">
          <ExpandRow className="-mx-4" id="acc-away" fluid lead={<Info className="size-4 text-muted-foreground" aria-hidden="true" />}
            name={<span className="font-normal text-muted-foreground">这台机器不在公网上，域名和从外面打开都用不上。</span>}
            summary="" open={open === 'away'} onToggle={() => toggle('away')}>
            <AwayBody />
          </ExpandRow>
        </div>
      )}
      {!loading && !dom.loading && !lan.loading && !pub.loading && !d && !lan.data && !pub.data && !home && (
        <div className="py-2.5 text-xs text-muted-foreground">这台机器不能让别的设备打开控制台</div>
      )}
      {!loading && <LogoutRow />}
    </>
  )
}
