// 概览：出问题时的提示（带“复制诊断给 AI”）、运行情况、资源、来源、访问地址（只本机的给怎么打开的说明）、
// 经本机 New API 连模型时令牌不限额的提示、首次登录信息。
import { useEffect, useState } from 'react'
import { AlertTriangle, Copy, FileText } from 'lucide-react'
import { api } from '@/lib/api'
import { deployDiagnosisForAI, deriveStatus, failedDeployment, gatewayTokenText, liveSeq, loginAddress, oldVersionServing, openableAddress } from '@/lib/appstatus'
import { copyText } from '@/lib/clipboard'
import { mbText, pctText, sinceText } from '@/lib/format'
import { appHref, go } from '@/lib/router'
import type { FirstLogin } from '@/lib/types'
import { useConsole } from '@/state'
import { KV } from '@/components/bits'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Alert } from '@/components/ui/controls'
import { CopyLine } from '@/components/ui/copy'
import { useToast } from '@/components/ui/toast'
import { FirstLoginCard } from '../store/install-dialog'
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
