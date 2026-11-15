// 应用详情概览里的首次登录信息：一行“登录：账号 admin · 密码 •••••• 显示 复制”，并在访问卡片底下。
// 数据：GET /v1/apps/{app}/first-login（从应用中心装的应用才有）；保密项默认遮住（服务端给的是遮住的值），
// 点“显示”或“复制”才带 reveal=true 向服务端要明文，要一次后记住（和安装完成弹窗里的首次登录卡片同一套做法）。
// 没有字段（只在内部网络的数据库这类、旧服务端给 null）时什么都不显示。
import { useEffect, useRef, useState } from 'react'
import { api } from '@/lib/api'
import { copyText } from '@/lib/clipboard'
import type { FirstLogin } from '@/lib/types'
import { Spinner } from '@/components/ui/controls'
import { useToast } from '@/components/ui/toast'

export function useFirstLogin(app: string, fromCatalog: boolean) {
  const [fl, setFl] = useState<FirstLogin | null>(null)
  useEffect(() => {
    if (!fromCatalog) return
    let alive = true
    api.get<FirstLogin>('/v1/apps/' + encodeURIComponent(app) + '/first-login').then((v) => { if (alive) setFl(v) }).catch(() => {})
    return () => { alive = false }
  }, [app, fromCatalog])
  return fl
}

export function hasLoginFields(fl: FirstLogin | null | undefined): fl is FirstLogin {
  return !!fl && Array.isArray(fl.fields) && fl.fields.length > 0
}

const LINK = 'rounded px-0.5 underline underline-offset-2 hover:text-foreground disabled:opacity-50'

export function LoginLine({ fl }: { fl: FirstLogin }) {
  const toast = useToast()
  const [shown, setShown] = useState<FirstLogin | null>(null)
  const [busy, setBusy] = useState(false)
  const reveal = useRef<Promise<FirstLogin> | null>(null)
  if (!hasLoginFields(fl)) return null
  const getReveal = () => {
    if (!reveal.current) {
      reveal.current = api.get<FirstLogin>('/v1/apps/' + encodeURIComponent(fl.app) + '/first-login?reveal=true')
      reveal.current.catch(() => { reveal.current = null })
    }
    return reveal.current
  }
  const toggle = async () => {
    if (shown) { setShown(null); return }
    setBusy(true)
    try { setShown(await getReveal()) } catch (e) { toast('没能读取密码', (e as Error).message, 'error') } finally { setBusy(false) }
  }
  const copySecret = async (i: number) => {
    try {
      const full = await getReveal()
      const v = full.fields[i]?.value || ''
      ;(await copyText(v)) ? toast('已复制') : toast('没能复制', '浏览器不允许', 'error')
    } catch (e) {
      toast('没能读取密码', (e as Error).message, 'error')
    }
  }
  const view = shown || fl
  const firstSecret = fl.fields.findIndex((f) => f.secret)
  return (
    <span className="inline-flex min-w-0 flex-wrap items-center gap-x-1 gap-y-0.5" data-testid="first-login">
      <span>登录：</span>
      {fl.fields.map((f, i) => (
        <span key={f.label + i} className="inline-flex items-center gap-1">
          {i > 0 && <span aria-hidden="true">·</span>}
          <span>{f.label}</span>
          <span className="mono select-all text-foreground" data-secret={f.secret ? 'true' : undefined}>{view.fields?.[i]?.value ?? f.value}</span>
          {/* “显示”只放在第一个保密项后面，一次显示全部保密项 */}
          {i === firstSecret && (
            <button type="button" className={LINK + ' inline-flex items-center gap-1'} onClick={toggle} disabled={busy}>
              {busy && <Spinner className="size-3" />}{shown ? '遮住' : '显示'}
            </button>
          )}
          {f.secret && <button type="button" className={LINK} onClick={() => copySecret(i)} aria-label={'复制' + f.label}>复制</button>}
        </span>
      ))}
    </span>
  )
}
