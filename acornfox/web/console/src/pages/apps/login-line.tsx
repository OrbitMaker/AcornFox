// 应用概览“谁能打开”卡片里地址下面固定的一行：“第一次登录用：用户名 admin　密码 ••••　显示　复制”（样稿第 10 版，评估 E6）。
// 数据：GET /v1/apps/{app}/first-login（从应用中心装的应用才有）；保密项默认遮住（服务端给的是遮住的值），
// 点“显示”或“复制”才带 reveal=true 向服务端要明文，要一次后记住（和安装完成弹窗里的首次登录卡片同一套做法）。
// 标签按 loginLabel 改写：“控制台密码”这类会和 AcornFox 控制台的密码撞名，改成“这个应用的登录密码”。
// 没有字段（只在内部网络的数据库这类、旧服务端给 null）时什么都不显示。
import { useEffect, useRef, useState } from 'react'
import { api } from '@/lib/api'
import { copyText } from '@/lib/clipboard'
import type { FirstLogin } from '@/lib/types'
import { Spinner } from '@/components/ui/controls'
import { useToast } from '@/components/ui/toast'

// 样稿第 14 版：读不到密码、没能复制，出错的话跟在这一行后面（不弹提示）；复制成功仍是一条“已复制”。

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

// loginLabel：页面上显示的标签。“控制台密码”“管理后台密码”这类改成“这个应用的登录密码”（不和 AcornFox 控制台的密码撞名），
// “管理员用户名”这类去掉“管理员”，其余照服务端给的。
export function loginLabel(label: string): string {
  const t = (label || '').trim()
  if (/(控制台|后台).*密码/.test(t)) return '这个应用的登录密码'
  return t.replace(/^管理员/, '') || t
}

const LINK = 'tap-h rounded px-0.5 text-muted-foreground underline underline-offset-2 hover:text-foreground disabled:opacity-50'

export function LoginLine({ fl }: { fl: FirstLogin }) {
  const toast = useToast()
  const [shown, setShown] = useState<FirstLogin | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
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
    setErr('')
    try { setShown(await getReveal()) } catch (e) { setErr('没能读取密码：' + (e as Error).message) } finally { setBusy(false) }
  }
  const copySecret = async (i: number) => {
    setErr('')
    try {
      const full = await getReveal()
      const v = full.fields[i]?.value || ''
      if (await copyText(v)) toast('已复制')
      else setErr('没能复制，点“显示”后手动选中')
    } catch (e) {
      setErr('没能读取密码：' + (e as Error).message)
    }
  }
  const view = shown || fl
  const firstSecret = fl.fields.findIndex((f) => f.secret)
  return (
    <span className="inline-flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-xs" data-testid="first-login">
      <span className="text-muted-foreground">第一次登录用：</span>
      {fl.fields.map((f, i) => (
        <span key={f.label + i} className="inline-flex items-center gap-1.5">
          <span className="text-muted-foreground">{loginLabel(f.label)}</span>
          <span className="mono select-all" data-secret={f.secret ? 'true' : undefined}>{view.fields?.[i]?.value ?? f.value}</span>
          {/* “显示”只放在第一个保密项后面，一次显示全部保密项 */}
          {i === firstSecret && (
            <button type="button" className={LINK + ' inline-flex items-center gap-1'} onClick={toggle} disabled={busy}>
              {busy && <Spinner className="size-3" />}{shown ? '遮住' : '显示'}
            </button>
          )}
          {f.secret && <button type="button" className={LINK} onClick={() => copySecret(i)} aria-label={'复制' + loginLabel(f.label)}>复制</button>}
        </span>
      ))}
      {err && <span className="basis-full text-danger" role="alert">{err}</span>}
    </span>
  )
}
