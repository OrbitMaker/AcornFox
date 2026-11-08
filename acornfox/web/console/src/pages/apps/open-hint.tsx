// 只本机的应用怎么打开：Linux 服务器上地址是 127.0.0.1，用户在自己电脑上点不开。
// 服务器在用户的局域网里、应用有自己的登录时，可以一键改成局域网（PATCH /v1/apps/{app} 的 exposure: lan，要确认）；
// 总是给出在自己电脑上运行的 acornfox open 应用名（像打开控制台一样开 SSH 隧道）。没有局域网地址（公网服务器）时它是首选。
// 改成局域网以后应用不再是只本机，接口不再给 open_hint；这一块仍留在原位置显示完成态（局域网地址和怎么改回），
// 所以父组件总是渲染它、把 hint 原样传进来（没有时传 undefined），由它自己决定显示什么。
import { useEffect, useRef, useState } from 'react'
import { ExternalLink, Laptop, Wifi } from 'lucide-react'
import { cn, safeHref } from '@/lib/utils'
import { api, appPath } from '@/lib/api'
import type { OpenHint } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/controls'
import { CopyLine } from '@/components/ui/copy'
import { useToast } from '@/components/ui/toast'

// portOf：地址里的端口，取不到时说“这个应用的端口”。
function portOf(u?: string): string {
  try {
    return (u && new URL(u).port) || '（这个应用的端口）'
  } catch {
    return '（这个应用的端口）'
  }
}

export function OpenHintBlock({ name, hint, openURL, onChanged, className }: {
  name: string
  // 只本机时服务端给的说明；不是只本机（或已经改成局域网）时是 undefined。
  hint?: OpenHint
  // 应用列表里现在能打开的地址（和“打开”按钮同一来源），改成局域网后完成态优先用它。
  openURL?: string
  onChanged?: () => void | Promise<void>
  className?: string
}) {
  const toast = useToast()
  const [confirm, setConfirm] = useState(false)
  const [busy, setBusy] = useState(false)
  // 改成局域网成功后是完成态，记下当时说明里的局域网地址（应用列表刷新前用它）。
  const [done, setDone] = useState<{ url: string } | null>(null)
  // 应用改回只本机（说明又出现）时，回到说明，不再显示旧的完成态。
  const sawGone = useRef(false)
  useEffect(() => {
    if (!hint) {
      sawGone.current = true
    } else if (sawGone.current) {
      sawGone.current = false
      setDone(null)
    }
  }, [hint])
  const toLAN = async () => {
    if (!hint) return
    setBusy(true)
    try {
      await api.patch(appPath(name), { exposure: 'lan' })
      setConfirm(false)
      setDone({ url: hint.lan_url || '' })
      toast('已改成局域网可访问', name)
      await onChanged?.()
    } catch (e) {
      toast('没能改', (e as Error).message, 'error')
    } finally {
      setBusy(false)
    }
  }
  if (!done && !hint) return null
  if (done) {
    const url = openURL || done.url
    return (
      <div className={cn('flex flex-col gap-1.5 rounded-md border p-3', className)} data-testid="open-hint">
        <div className="flex flex-wrap items-center gap-1.5 text-xs" data-testid="open-lan-done">
          <Wifi className="size-3.5" />已改成局域网，同一局域网的电脑用这个地址打开：
          {safeHref(url) ? (
            <a href={safeHref(url)} target="_blank" rel="noopener noreferrer" className="mono inline-flex items-center gap-1 underline-offset-4 hover:underline">
              {url}<ExternalLink className="size-3" />
            </a>
          ) : (
            <span className="text-muted-foreground">（地址见应用详情）</span>
          )}
        </div>
        <p className="text-2xs text-muted-foreground">要改回只本机：在 {name} 的“高级”里把访问方式改成“只本机”，或者在服务器上运行 <span className="mono">acornfox app set --app {name} --expose local</span>。</p>
      </div>
    )
  }
  const h = hint!
  const tunnel = (
    <div className="flex flex-col gap-1.5" data-testid="open-tunnel">
      <div className="flex items-center gap-1.5 text-xs font-medium"><Laptop className="size-3.5" />在你自己的电脑上运行这条命令打开</div>
      <CopyLine text={h.command} />
      <p className="text-2xs text-muted-foreground">它像 acornfox open 打开控制台一样开一条 SSH 隧道，自动打开浏览器，看完按 Ctrl+C 关闭。不用放行安全组。</p>
    </div>
  )
  return (
    <div className={cn('flex flex-col gap-3 rounded-md border p-3', className)} data-testid="open-hint">
      <p className="text-xs text-muted-foreground">{name} 只在服务器本机能打开，127.0.0.1 的地址在你的电脑上点不开。</p>
      {h.can_lan && (
        <div className="flex flex-col gap-1.5" data-testid="open-lan">
          {!confirm ? (
            <Button size="sm" className="self-start" onClick={() => setConfirm(true)}><Wifi className="size-3.5" />让局域网里的电脑能打开</Button>
          ) : (
            <div className="flex flex-col gap-2 rounded-md bg-muted/50 p-2.5 text-xs">
              <span>会在服务器的所有网卡上开放端口 {portOf(h.lan_url)}：同一局域网（{h.lan_address}）里的电脑都能访问 {name}，要用它自己的账号密码登录。服务器如果还有公网地址、防火墙或安全组又放行了这个端口，公网也能访问到。改完马上生效，以后可以在“高级”里改回只本机。</span>
              <div className="flex gap-2">
                <Button size="sm" onClick={toLAN} disabled={busy}>{busy && <Spinner />}确认改成局域网</Button>
                <Button size="sm" variant="ghost" onClick={() => setConfirm(false)}>取消</Button>
              </div>
            </div>
          )}
        </div>
      )}
      {tunnel}
    </div>
  )
}
