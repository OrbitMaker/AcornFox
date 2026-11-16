// 公网控制台开启以后，地址的证书好了没有。
// 为什么要有：第一次开启时入口要现申请证书（通常一两分钟），这段时间浏览器打开地址会报“连接不安全”。
// 页面在这段时间写明“正在申请证书”，好了再说“可以打开了”，不让人白跑一趟。
// 接口：GET /v1/public-console?check=https，应答里的 https（服务器自己查的）：
//   code ok：可以打开；cert_pending：正在申请；cert_failed：等了几分钟还没签下来；
//   别的 code：这台服务器确认不了（有的云不让服务器连自己的公网地址）。
//   旧服务端的应答里没有 https：什么都不显示，和以前一样。
// 结论（哪种状态）由服务端给，页面只管显示和隔一会儿再问；证书好了或公网控制台关了就不再问。
import * as React from 'react'
import { AlertTriangle, Check, ExternalLink } from 'lucide-react'
import { api } from '@/lib/api'
import type { HTTPSCheck, PublicConsoleStatus } from '@/lib/types'
import { Spinner } from '@/components/ui/controls'

export type CertPhase = 'none' | 'checking' | 'pending' | 'ready' | 'failed' | 'unknown'
// waited：这次打开页面以来见过“还不能打开”的状态（之后好了，那一行也要说一声）
export interface CertState { phase: CertPhase; diag?: HTTPSCheck; waited: boolean }

// 隔多久再问一次（毫秒）：申请中问得勤，别的情况问得慢。测试里调小。
export const certPoll = { pending: 3000, other: 10000 }

export function certPhase(d?: HTTPSCheck): CertPhase {
  if (!d || !d.code) return 'none'
  if (d.code === 'ok') return 'ready'
  if (d.code === 'cert_pending') return 'pending'
  if (d.code === 'cert_failed') return 'failed'
  return 'unknown'
}

// active：公网控制台开着。key 变了（换了地址、重新开启）就从头再问。
export function useCertState(active: boolean, key: string): CertState {
  const [state, setState] = React.useState<CertState>({ phase: active ? 'checking' : 'none', waited: false })
  React.useEffect(() => {
    if (!active) { setState({ phase: 'none', waited: false }); return }
    setState({ phase: 'checking', waited: false })
    const ctl = new AbortController()
    let timer = 0
    let stopped = false
    const ask = async () => {
      let phase: CertPhase | '' = ''
      let diag: HTTPSCheck | undefined
      try {
        const v = await api.get<PublicConsoleStatus>('/v1/public-console?check=https', ctl.signal)
        phase = v.enabled ? certPhase(v.https) : 'none'
        diag = v.https
      } catch {
        // 这一次没问到（网络抖动、服务端在重启）：保持原样，过一会儿再问
      }
      if (stopped) return
      if (phase) {
        const p = phase
        setState((s) => ({ phase: p, diag, waited: s.waited || p === 'pending' || p === 'failed' || p === 'unknown' }))
        if (p === 'ready' || p === 'none') return
      }
      timer = window.setTimeout(ask, phase === 'pending' ? certPoll.pending : certPoll.other)
    }
    void ask()
    return () => { stopped = true; ctl.abort(); window.clearTimeout(timer) }
  }, [active, key])
  return state
}

// where：dialog 是刚开启时的弹窗（每种状态都写）；row 是“在外面打开控制台”那一行（证书一直是好的就不多说）。
export function CertLine({ s, url, where }: { s: CertState; url: string; where: 'dialog' | 'row' }) {
  if (s.phase === 'none') return null
  if (where === 'row' && (s.phase === 'checking' || (s.phase === 'ready' && !s.waited))) return null
  let body: React.ReactNode
  if (s.phase === 'checking') {
    body = <><Spinner className="mt-0.5" /><span>正在看证书好了没有…</span></>
  } else if (s.phase === 'pending') {
    body = <><Spinner className="mt-0.5" /><span>正在申请证书，通常一两分钟，好了这里会提示。现在打开，浏览器会报“连接不安全”。</span></>
  } else if (s.phase === 'ready') {
    body = (
      <>
        <Check className="mt-0.5 size-3.5 shrink-0 text-success" aria-hidden="true" />
        <span>证书好了，可以打开了。{url && <a className="ml-1.5 inline-flex items-center gap-1 underline underline-offset-2" href={url} target="_blank" rel="noreferrer">打开<ExternalLink className="size-3" aria-hidden="true" /></a>}</span>
      </>
    )
  } else if (s.phase === 'failed') {
    body = (
      <>
        <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-warning" aria-hidden="true" />
        <span>
          证书还没签下来，地址现在打不开。服务器还在自己重试，好了这里会提示。
          {s.diag?.hint && <span className="mt-1 block text-2xs text-muted-foreground">{s.diag.hint}</span>}
        </span>
      </>
    )
  } else {
    body = (
      <>
        <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
        <span>这台服务器自己确认不了证书好了没有{s.diag?.message ? '（' + s.diag.message + '）' : ''}。第一次开启要等一两分钟；到时候直接打开地址试试，浏览器报“连接不安全”就再等一会儿。</span>
      </>
    )
  }
  return (
    <div className={'flex items-start gap-1.5 text-xs ' + (s.phase === 'unknown' || s.phase === 'checking' ? 'text-muted-foreground' : '')} role="status" data-testid="pub-cert" data-phase={s.phase}>
      {body}
    </div>
  )
}
