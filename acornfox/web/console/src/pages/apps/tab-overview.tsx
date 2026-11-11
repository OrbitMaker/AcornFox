// 概览：出问题时的提示（带“复制诊断给 AI”）、运行情况、资源、来源、访问地址（只本机的给怎么打开的说明）、
// 经本机 New API 连模型时令牌不限额的提示、首次登录信息。
// 最上面两张小图：内存、CPU 的近 1 小时或 24 小时（GET /v1/metrics/apps/{app}/history，30 秒刷新一次）。
// 历史只存在后台服务的内存里，重启后从头记；旧服务端没有这个接口时两张图不显示。
import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, Copy, FileText } from 'lucide-react'
import { api } from '@/lib/api'
import { appCharts } from '@/lib/apphistory'
import { RANGE_TAB, type RangeKey } from '@/lib/dashboard'
import { usePoll } from '@/lib/hooks'
import { deployDiagnosisForAI, deriveStatus, failedDeployment, gatewayTokenText, liveSeq, loginAddress, oldVersionServing, openableAddress } from '@/lib/appstatus'
import { copyText } from '@/lib/clipboard'
import { mbText, pctText, sinceText } from '@/lib/format'
import { appHref, go } from '@/lib/router'
import type { AppHistory, FirstLogin } from '@/lib/types'
import { useConsole } from '@/state'
import { KV } from '@/components/bits'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Alert } from '@/components/ui/controls'
import { CopyLine } from '@/components/ui/copy'
import { useToast } from '@/components/ui/toast'
import { FirstLoginCard } from '../store/install-dialog'
import { TimeChart } from '../home/viz'
import { OpenHintBlock } from './open-hint'
import type { TabProps } from './app-page'

export const EXPOSURE_TEXT: Record<string, string> = { local: '只本机', lan: '局域网', public: '公网', afx_cloud: 'AFX 云' }

function useFirstLogin(app: string, fromCatalog: boolean) {
  const [fl, setFl] = useState<FirstLogin | null>(null)
  useEffect(() => {
    if (!fromCatalog) return
    let alive = true
    api.get<FirstLogin>('/v1/apps/' + encodeURIComponent(app) + '/first-login').then((v) => { if (alive) setFl(v) }).catch(() => {})
    return () => { alive = false }
  }, [app, fromCatalog])
  return fl
}

// 内存、CPU 两张小图和时间范围切换。没有历史接口（旧服务端）时什么都不显示。
function HistoryCharts({ app, running }: { app: string; running: boolean }) {
  const [range, setRange] = useState<RangeKey>('1h')
  const hist = usePoll((s) => api.get<AppHistory>('/v1/metrics/apps/' + encodeURIComponent(app) + '/history?range=' + range, s).catch(() => null), 30000, [app, range])
  const h = hist.data
  const c = useMemo(() => (h ? appCharts(h) : null), [h])
  if (!h || !c || !h.ranges.length) return null
  const empty = running ? '还没有记录，每 30 秒记一次，稍等一会儿' : '这段时间没有记录（应用停着的时候不记）'
  const head = (name: string) => (
    <div className="mb-2 flex items-center justify-between text-xs font-medium">
      <span>{name} <span className="font-normal text-muted-foreground">近 {RANGE_TAB[h.range] || h.range}</span></span>
    </div>
  )
  return (
    <div data-testid="app-history" className="flex flex-col gap-2">
      <div className="flex items-center justify-end gap-1" role="group" aria-label="时间范围">
        {h.ranges.filter((r): r is RangeKey => r === '1h' || r === '24h').map((r) => (
          <Button key={r} size="sm" variant={r === range ? 'secondary' : 'ghost'} aria-pressed={r === range} onClick={() => setRange(r)}>{RANGE_TAB[r]}</Button>
        ))}
      </div>
      <div className="grid gap-3 md:grid-cols-2">
        <Card className="p-3.5">
          {head('内存')}
          <div className="h-28">{c.count ? <TimeChart d={c.mem} type="area" gaps /> : <div className="flex h-full items-center justify-center text-xs text-muted-foreground">{empty}</div>}</div>
        </Card>
        <Card className="p-3.5">
          {head('CPU')}
          <div className="h-28">{c.count ? <TimeChart d={c.cpu} type="area" gaps /> : <div className="flex h-full items-center justify-center text-xs text-muted-foreground">{empty}</div>}</div>
        </Card>
      </div>
    </div>
  )
}

export function OverviewTab({ app: a, metrics: m, reload }: TabProps) {
  const { installing } = useConsole()
  const toast = useToast()
  const st = deriveStatus(a, installing)
  const failed = failedDeployment(a)
  const fl = useFirstLogin(a.name, !!a.catalog_id)
  const copyDiag = async () => {
    (await copyText(deployDiagnosisForAI(a))) ? toast('已复制', '粘贴给 AI 工作台') : toast('没能复制', '浏览器不允许', 'error')
  }
  return (
    <div className="flex flex-col gap-3">
      {failed && (
        <Alert
          tone="danger"
          icon={<AlertTriangle />}
          title={oldVersionServing(a) ? '第 ' + failed.seq + ' 版没上线，之前的版本还在服务' : '第 ' + failed.seq + ' 版部署失败'}
          action={failed.diagnosis ? <Button size="sm" variant="outline" onClick={copyDiag}><Copy className="size-3.5" />复制诊断给 AI</Button> : undefined}
        >
          {failed.diagnosis ? failed.diagnosis.message + (failed.diagnosis.hint ? '。' + failed.diagnosis.hint : '') : failed.failure_text}
        </Alert>
      )}
      {!failed && st === 'crashed' && (
        <Alert tone="danger" icon={<AlertTriangle />} title="没在运行" action={<Button size="sm" variant="outline" onClick={() => go(appHref(a.name, 'logs'))}><FileText className="size-3.5" />看日志</Button>}>
          容器停了，系统会自动拉起；一直起不来就看日志
        </Alert>
      )}
      {!!a.current_deployment && <HistoryCharts app={a.name} running={!!a.observed?.running} />}
      <div className="grid gap-3 md:grid-cols-3">
        <Card className="px-3.5 py-2">
          <div className="pt-1 text-xs font-medium">运行</div>
          <div className="divide-y">
            <KV k="已运行">{a.observed?.running ? sinceText(a.observed.started_at) : '—'}</KV>
            <KV k="重启次数"><span className="tnum">{a.observed ? a.observed.restart_count : '—'}</span></KV>
            <KV k="健康检查"><span className="mono text-xs">{a.health_path || '/'}</span></KV>
            <KV k="容器端口"><span className="tnum">{a.port || '—'}</span></KV>
          </div>
        </Card>
        <Card className="px-3.5 py-2">
          <div className="pt-1 text-xs font-medium">资源 <span className="font-normal text-muted-foreground">现在</span></div>
          <div className="divide-y">
            <KV k="CPU"><span className="tnum">{m?.available && m.cpu_available !== false ? pctText(m.cpu_percent) : '—'}</span></KV>
            <KV k="内存"><span className="tnum">{m?.available ? mbText(m.memory_usage_mb) : '—'} / {mbText(a.memory_mb)}</span></KV>
            <KV k="进程数"><span className="tnum">{m?.available ? m.pids ?? '—' : '—'}</span></KV>
            <KV k="网络收发"><span className="tnum">{m?.available ? mbText(m.network_rx_mb) + ' / ' + mbText(m.network_tx_mb) : '—'}</span></KV>
          </div>
        </Card>
        <Card className="px-3.5 py-2">
          <div className="pt-1 text-xs font-medium">来源</div>
          <div className="divide-y">
            <KV k="当前版本">{liveSeq(a) ? '第 ' + liveSeq(a) + ' 版' : '—'}</KV>
            <KV k="来自">{a.catalog_id ? (a.catalog_origin ? (a.catalog_origin_name || a.catalog_origin) : '应用中心') + ' · ' + a.catalog_id : a.live?.source_text || '—'}</KV>
            {a.catalog_disconnected && <KV k="目录"><span className="text-warning" title="照常运行，但不能再从这个私有目录安装或升级">已断开</span></KV>}
            {a.catalog_version && <KV k="目录版本"><span className="tnum">{a.catalog_version}</span></KV>}
            <KV k="访问方式">{EXPOSURE_TEXT[a.exposure] || a.exposure}</KV>
          </div>
        </Card>
      </div>
      {/* 总是渲染：改成局域网后 open_hint 没了，这一块在原位置显示完成态。 */}
      <OpenHintBlock name={a.name} hint={a.open_hint} openURL={openableAddress(a)} onChanged={reload} />
      {a.gateway_token_from && <p className="text-xs text-muted-foreground" data-testid="token-note">{gatewayTokenText(a.name, a.gateway_token_from)}</p>}
      {!a.open_hint && (a.url || a.lan_url) && (
        <Card className="flex flex-col gap-2 p-3">
          {a.url && <CopyLine prompt={false} label="复制地址" text={a.url} />}
          {a.lan_url && a.lan_url !== a.url && (
            <div className="flex items-center gap-2"><span className="shrink-0 text-2xs text-muted-foreground">局域网</span><CopyLine className="flex-1" prompt={false} label="复制地址" text={a.lan_url} /></div>
          )}
        </Card>
      )}
      {fl && <FirstLoginCard fl={fl} title="首次登录" url={loginAddress(fl.url, a)} />}
    </div>
  )
}
