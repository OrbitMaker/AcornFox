// 概览，从上到下：出问题时的提示（带“复制诊断给 AI”）→ 访问卡片（地址、三档，从应用中心装的应用把首次登录信息并在里面）
// → 运行、资源、版本三张卡片（见 overview-cards.tsx）→ 用到的服务 → 只本机应用的“怎么打开”说明。
// 没有访问卡片的应用（只本机要按说明打开的）有首次登录信息时，登录那一行单独放在说明下面。
// 用旧格式清单装的应用，详情里带 legacy_note 时原样显示成一条提示。
// 概览里不再放内存、CPU 的曲线图（2026-11-15 去掉，圆环看现在的用量；历史仍可用 acornfox stats 应用 --history 1h 看）。
// 用到的服务（GET /v1/apps/{app} 的 services）：需要的名字、类型、接的是哪条、状态；不是可用时最上面醒目提示。
// 没有这个字段（旧服务端、旧清单装的应用）时不显示。
import { AlertTriangle, Copy, FileText, Info } from 'lucide-react'
import { deployDiagnosisForAI, deriveStatus, failedDeployment, oldVersionServing, openableAddress } from '@/lib/appstatus'
import { copyText } from '@/lib/clipboard'
import { appHref, go } from '@/lib/router'
import { TONE_BADGE, appServiceStatus, kindLabel } from '@/lib/services'
import { accessInfo } from '@/lib/overview'
import type { AppServiceUse } from '@/lib/types'
import { useConsole } from '@/state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Alert } from '@/components/ui/controls'
import { useToast } from '@/components/ui/toast'
import { KindName } from '../prefs/service-form'
import { OpenHintBlock } from './open-hint'
import { AccessCard, ResourceCard, RunCard, VersionCard } from './overview-cards'
import { LoginLine, hasLoginFields, useFirstLogin } from './login-line'
import type { TabProps } from './app-page'

// 用到的服务里不能用的，放在最上面提示。
function ServicesAlert({ list }: { list: AppServiceUse[] }) {
  const bad = list.map((u) => ({ u, st: appServiceStatus(u) })).filter((x) => x.st.tone !== 'success')
  if (bad.length === 0) return null
  return (
    <Alert
      tone={bad.some((x) => x.st.tone === 'danger') ? 'danger' : 'warning'}
      icon={<AlertTriangle />}
      title={bad.length === 1 ? '用到的服务不能用' : `有 ${bad.length} 项用到的服务不能用`}
      action={<Button size="sm" variant="outline" onClick={() => go('#prefs/services')}>去看服务</Button>}
    >
      <span data-testid="services-alert">{bad.map(({ u, st }) => `${kindLabel(u.kind_label)}${u.service ? '（' + u.service + '）' : ''}：${st.text}`).join('；')}</span>
    </Alert>
  )
}

function ServicesCard({ list }: { list: AppServiceUse[] }) {
  return (
    <Card className="px-3.5 py-2" data-testid="app-services">
      <div className="pt-1 text-xs font-medium">用到的服务</div>
      <div className="divide-y">
        {list.map((u) => {
          const st = appServiceStatus(u)
          return (
            <div key={u.use} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-1.5 text-sm">
              <span className="min-w-0 flex-1">
                <KindName label={u.kind_label} kind={u.kind} /> <span className="mono text-2xs text-muted-foreground">{u.use}</span>
              </span>
              <span className="min-w-0 truncate text-xs">{u.service ? '接的是“' + u.service + '”' : '没接上'}</span>
              <Badge variant={TONE_BADGE[st.tone]}>{st.text}</Badge>
            </div>
          )
        })}
      </div>
    </Card>
  )
}

export function OverviewTab({ app: a, metrics: m, reload }: TabProps) {
  const { installing } = useConsole()
  const toast = useToast()
  const st = deriveStatus(a, installing)
  const failed = failedDeployment(a)
  const fl = useFirstLogin(a.name, !!a.catalog_id)
  const withAccess = !a.open_hint && !!accessInfo(a)
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
      {a.services && a.services.length > 0 && <ServicesAlert list={a.services} />}
      {typeof a.legacy_note === 'string' && a.legacy_note.trim() !== '' && (
        <Alert icon={<Info />} title="说明"><span data-testid="legacy-note">{a.legacy_note}</span></Alert>
      )}
      {withAccess && <AccessCard app={a} reload={reload} login={fl} />}
      <div className="grid gap-3 md:grid-cols-3">
        <RunCard app={a} />
        <ResourceCard app={a} metrics={m} />
        <VersionCard app={a} />
      </div>
      {a.services && a.services.length > 0 && <ServicesCard list={a.services} />}
      {/* 总是渲染：改成局域网后 open_hint 没了，这一块在原位置显示完成态。 */}
      <OpenHintBlock name={a.name} hint={a.open_hint} openURL={openableAddress(a)} onChanged={reload} />
      {!withAccess && hasLoginFields(fl) && (
        <Card data-testid="ov-login" className="px-3.5 py-2.5 text-2xs text-muted-foreground"><LoginLine fl={fl} /></Card>
      )}
    </div>
  )
}
