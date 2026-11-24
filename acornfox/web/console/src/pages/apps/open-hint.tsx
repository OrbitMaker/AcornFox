// 只本机的应用怎么打开（Linux 服务器上地址是 127.0.0.1，用户在自己电脑上点不开）。用在两处：
//   应用概览最上面的“谁能打开”卡片里（who-card.tsx，bare：不带外框）；安装弹窗装好以后（pages/store/install-dialog.tsx）。
// 样子（样稿第 10 版，评估 E5）：一句“现在只有这台机器自己能打开。”；
//   这台机器在用户的局域网里、应用有自己的登录时（hint.can_lan）给主按钮“让家里的电脑、手机也能打开”，
//   点了先在原地确认（两句人话），确认后 PATCH /v1/apps/{app} 的 exposure: lan；对用到应用地址的应用（改了要重启），服务端再要用户同意一次，
//   弹确认窗（pages/apps/reapply.tsx）：同意后改好，这一块等应用列表刷新后显示完成态；没改成（取消）就还是原样；
//   extra：调用方另放的按钮（云服务器上是“改成公网”）；
//   两条命令（acornfox open 应用名；没有命令行时的 ssh 隧道）收进默认收起的“我在外面，用命令行打开”。
// 改成局域网以后接口不再给 open_hint；安装弹窗里这一块仍留在原位置显示完成态（新地址），所以那里总是渲染它、
// 把 hint 原样传进来（没有时传 undefined），由它自己决定显示什么。概览里卡片会换成正常的样子，不用完成态。
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { ExternalLink, Wifi } from 'lucide-react'
import { cn, safeHref } from '@/lib/utils'
import { api, appPath } from '@/lib/api'
import { useAppNames } from '@/lib/display'
import type { OpenHint } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { CopyLine } from '@/components/ui/copy'
import { CliFold } from '@/components/fold'
import { useToast } from '@/components/ui/toast'
import { useReapply } from './reapply'

// lanAfter：改成局域网以后的地址（改之前打不开）。旧服务端把它放在 lan_url 里，新的放在 lan_url_after_expose。
function lanAfter(h: OpenHint): string {
  return h.lan_url_after_expose || h.lan_url || ''
}

export function OpenHintBlock({ name, hint, openURL, onChanged, className, bare, extra }: {
  // 应用的代号（接口、命令用）；给人看的名字由这里按代号取
  name: string
  // 只本机时服务端给的说明；不是只本机（或已经改成局域网）时是 undefined。
  hint?: OpenHint
  // 应用列表里现在能打开的地址（和“打开”按钮同一来源），改成局域网后完成态优先用它。
  openURL?: string
  onChanged?: () => void | Promise<void>
  className?: string
  // 放在卡片里时不画外框
  bare?: boolean
  // 主按钮旁边另放的东西（例如云服务器上的“改成公网”）
  extra?: ReactNode
}) {
  const toast = useToast()
  const label = useAppNames()(name)
  const flow = useReapply(name, label, async () => { await onChanged?.() })
  const [confirm, setConfirm] = useState(false)
  const [busy, setBusy] = useState(false)
  // 出错留在这一块上，不用会消失的提示（样稿第 12 版）
  const [err, setErr] = useState('')
  // 改成局域网成功后是完成态，记下当时说明里的局域网地址（应用列表刷新前用它）。
  const [done, setDone] = useState<{ url: string } | null>(null)
  // 应用改回只本机（说明又出现）时，回到说明，不再显示旧的完成态。
  const sawGone = useRef(false)
  // 点了“好，改”但改的结果要等用户在确认窗里同意：应用真的改成局域网（说明没了）以后再显示完成态
  const want = useRef('')
  useEffect(() => {
    if (!hint) {
      sawGone.current = true
      if (want.current) { setDone({ url: want.current }); want.current = '' }
    } else if (sawGone.current) {
      sawGone.current = false
      setDone(null)
    }
  }, [hint])
  const toLAN = async () => {
    if (!hint) return
    setErr('')
    setBusy(true)
    try {
      const r = await flow.address('让家里的电脑、手机也能打开 ' + label, (mode) =>
        api.patch(appPath(name), { exposure: 'lan', ...(mode === 'confirm' ? { confirm_restart: true } : {}), ...(mode === 'skip' ? { reapply: 'skip' } : {}) }))
      setConfirm(false)
      if (r === 'plain') {
        setDone({ url: lanAfter(hint) })
        toast('已改好', '家里的电脑、手机也能打开 ' + label + ' 了', {
          undo: async () => { await api.patch(appPath(name), { exposure: 'local' }); setDone(null); await onChanged?.() },
          undone: '改回只有这台机器能打开',
        })
      } else want.current = lanAfter(hint)
    } catch (e) {
      setErr('没能改：' + (e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const box = cn('flex flex-col gap-2.5', !bare && 'rounded-md border p-3', className)
  if (!done && !hint) return null
  if (done) {
    const url = openURL || done.url
    return (
      <div className={box} data-testid="open-hint">
        <div className="flex flex-wrap items-center gap-1.5 text-sm" data-testid="open-lan-done">
          <Wifi className="size-icon" />已改好，家里的电脑、手机用这个地址打开：
          {safeHref(url) ? (
            <a href={safeHref(url)} target="_blank" rel="noopener noreferrer" className="mono inline-flex items-center gap-1 underline-offset-4 hover:underline">
              {url.replace(/^https?:\/\//, '')}<ExternalLink className="size-3" />
            </a>
          ) : (
            <span className="text-muted-foreground">（地址在应用的概览里）</span>
          )}
        </div>
      </div>
    )
  }
  const h = hint!
  return (
    <div className={box} data-testid="open-hint">
      <p className="text-sm">现在只有这台机器自己能打开。</p>
      {(h.can_lan || extra) && (
        <div className="flex flex-wrap items-center gap-2" data-testid="open-lan">
          {h.can_lan && !confirm && (
            <Button size="sm" onClick={() => setConfirm(true)}><Wifi className="size-icon" />让家里的电脑、手机也能打开</Button>
          )}
          {h.can_lan && confirm && (
            <div className="flex flex-col gap-2 rounded-md bg-muted/50 p-2.5 text-xs">
              <span>同一个网络里的电脑、手机都能打开 {label}，要用它自己的密码登录。以后想改回来，在它的“谁能打开”里点“只本机”。</span>
              <div className="flex gap-2">
                <Button size="sm" onClick={toLAN} loading={busy}>好，改</Button>
                <Button size="sm" variant="ghost" onClick={() => setConfirm(false)}>取消</Button>
                {err && <span className="self-center text-danger" role="alert">{err}</span>}
              </div>
            </div>
          )}
          {extra}
        </div>
      )}
      <CliFold title="我在外面，用命令行打开">
        <div className="flex flex-col gap-1.5" data-testid="open-tunnel">
          <p className="text-2xs text-muted-foreground">在你自己的电脑上运行这一行，会自动打开浏览器，看完按 Ctrl+C 关掉：</p>
          <CopyLine text={h.command} />
          {h.tunnel_command && (
            <div className="flex flex-col gap-1.5" data-testid="open-ssh">
              <p className="text-2xs text-muted-foreground">电脑上没装 acornfox 命令行：运行下面这一行并保持不关（“用户@服务器地址”换成你登录这台机器用的），再用浏览器打开 <span className="mono">{h.tunnel_url}</span>。</p>
              <CopyLine text={h.tunnel_command} />
            </div>
          )}
        </div>
      </CliFold>
    </div>
  )
}
