// 概览，从上到下（样稿第 10 版）：
//   出问题时的提示条（琥珀边，一句人话 + “复制给 AI 工作台”；样稿第 11 版起“要处理”都是琥珀，不用红）→ “谁能打开”卡片（who-card.tsx，所有应用都有、位置固定）
//   → “它用到的服务”卡片（links-card.tsx，没有时不显示）→ 运行、资源、版本三张卡片（overview-cards.tsx）。
// 红条（评估 E4）：从应用中心装的应用只写一句人话（lib/display.ts 的 problemLine），诊断原话、故障编号都放进复制给 AI 的文字里；
//   开发者自己部署的项目保留诊断原话（/health、REDIS_URL 这类是给开发者看的）。新版本没换上、旧版本照常用时用黄色，不吓人。
// 用到的服务（GET /v1/apps/{app} 的 services）里有不能用的：最上面提示；没有这个字段（旧服务端、旧清单装的应用）时不提示。
// 用旧格式清单装的应用，详情里带 legacy_note 时原样显示成一条提示。
// 落在后面的（平台自动填的值或用到的服务变了，应用里用的还是以前的，GET /v1/platform/affected）：最上面多一条提示加“让它跟上”（W8-3，pages/apps/reapply.tsx）。
// 概览里不放内存、CPU 的曲线图（2026-11-15 去掉；历史仍可用 acornfox stats 应用 --history 1h 看）。
import { AlertTriangle, FileText, Info } from 'lucide-react'
import { deployDiagnosisForAI, deriveStatus, failedDeployment, oldVersionServing } from '@/lib/appstatus'
import { appHref, go } from '@/lib/router'
import { appServiceStatus, kindLabel } from '@/lib/services'
import { useAppLinks } from '@/lib/app-links'
import { catalogAppOf } from '@/lib/catalog'
import { displayName, fromStore, problemLine, useAppNames, WORDS } from '@/lib/display'
import type { AppServiceUse, AppView } from '@/lib/types'
import { useConsole } from '@/state'
import { Button } from '@/components/ui/button'
import { Alert } from '@/components/ui/controls'
import { Tile } from '@/components/bits'
import { Loading } from '@/components/ui/skeleton'
import { SkLinks } from '@/components/skeletons'
import { AskAI } from '@/components/fold'
import { ResourceCard, RunCard, VersionCard } from './overview-cards'
import { WhoCard } from './who-card'
import { LinksCard, type TileOf } from './links-card'
import { useFirstLogin } from './login-line'
import { BehindAlert, useReapply } from './reapply'
import type { TabProps } from './app-page'

// 用到的服务里不能用的，放在最上面提示。
function ServicesAlert({ list, name }: { list: AppServiceUse[]; name: (app: string) => string }) {
  const bad = list.map((u) => ({ u, st: appServiceStatus(u) })).filter((x) => x.st.tone !== 'success')
  if (bad.length === 0) return null
  return (
    <Alert
      tone="warning"
      icon={<AlertTriangle />}
      title={bad.length === 1 ? '它用到的' + WORDS.service + '现在不能用' : `它用到的 ${bad.length} 个${WORDS.service}现在不能用`}
      action={<Button size="sm" variant="outline" onClick={() => go('#prefs/services')}>去看{WORDS.service}</Button>}
    >
      <span data-testid="services-alert">{bad.map(({ u, st }) => `${kindLabel(u.kind_label)}${u.service ? '（' + name(u.service) + '）' : ''}：${st.text}`).join('；')}</span>
    </Alert>
  )
}

// forAI：复制给 AI 工作台的那段文字。有部署诊断时用它（格式和命令行一样），否则写现象和状态。
function forAI(a: AppView, label: string, line: string): string {
  const diag = deployDiagnosisForAI(a)
  if (diag) return diag
  return `AcornFox 上的应用 ${label}（${a.name}）${line}。\n状态：${a.observed_state}，期望：${a.desired}，重启次数：${a.observed?.restart_count ?? '—'}。\n请帮我查原因：可以先运行 acornfox status ${a.name} 和 acornfox logs ${a.name} --tail 200。`
}

// Problem：出问题时最上面那一条。
function Problem({ a, label }: { a: AppView; label: string }) {
  const { installing } = useConsole()
  const st = deriveStatus(a, installing)
  const failed = failedDeployment(a)
  if (!failed && st !== 'crashed') return null
  const store = fromStore(a)
  const old = !!failed && oldVersionServing(a)
  const line = problemLine(label, a, failed ? 'failed' : 'crashed')
  const title = failed ? (old ? '新版本没换上，旧版本照常用' : label + ' 没启动成功') : label + ' 停了，正在自动重新启动'
  // 开发者的项目：诊断原话照留（给开发者看的）；应用中心装的：只留一句人话
  const detail = failed && !store
    ? (failed.diagnosis ? failed.diagnosis.message + (failed.diagnosis.hint ? '。' + failed.diagnosis.hint : '') : failed.failure_text)
    : store ? (old ? '可以照常打开。要查原因，把情况复制给 AI 工作台。' : '把情况复制给 AI 工作台，它会帮你查。') : '一直起不来的话看看日志。'
  return (
    <Alert
      tone="warning"
      icon={<AlertTriangle />}
      title={title}
      action={
        <div className="flex flex-wrap gap-1.5">
          {!failed && <Button size="sm" variant="outline" onClick={() => go(appHref(a.name, 'logs'))}><FileText className="size-icon" />看日志</Button>}
          <AskAI text={() => forAI(a, label, line)} />
        </div>
      }
    >
      <span data-testid="ov-problem">{detail}</span>
    </Alert>
  )
}

export function OverviewTab({ app: a, metrics: m, metricsLoading, reload }: TabProps) {
  const { apps, catalog, behind } = useConsole()
  const mine = behind.find((x) => x.app === a.name)
  const links = useAppLinks(a.name)
  const name = useAppNames()
  const label = displayName(catalog, a)
  // 服务卡片里别的应用的小图块：是已装的、从应用中心装的，用目录里的图标。
  const tileOf: TileOf = (app) => {
    const other = apps?.find((x) => x.name === app)
    const c = other ? catalogAppOf(catalog, other) : undefined
    return <Tile id={other?.catalog_id || app} name={name(app)} icon={c?.icon} iconType={c?.icon_type} size="xs" />
  }
  const fl = useFirstLogin(a.name, fromStore(a))
  const flow = useReapply(a.name, label, reload)
  return (
    <div className="flex flex-col gap-3">
      <Problem a={a} label={label} />
      {mine && <BehindAlert behind={mine} label={label} onCatchUp={flow.catchUp} />}
      {a.services && a.services.length > 0 && <ServicesAlert list={a.services} name={name} />}
      {typeof a.legacy_note === 'string' && a.legacy_note.trim() !== '' && (
        <Alert icon={<Info />} title="说明"><span data-testid="legacy-note">{a.legacy_note}</span></Alert>
      )}
      <WhoCard app={a} name={label} reload={reload} login={fl} />
      {/* 它用到的服务还没读到：按应用自己声明用了几条先占住位置（样稿第 14 版），读到以后原位换成真的。 */}
      {!links && a.services && a.services.length > 0 ? <Loading when skeleton={<SkLinks n={a.services.length} />}>{null}</Loading> : <LinksCard links={links} tile={tileOf} name={name} />}
      <div className="grid gap-3 md:grid-cols-3">
        <RunCard app={a} />
        <ResourceCard app={a} metrics={m} loading={metricsLoading} />
        <VersionCard app={a} />
      </div>
    </div>
  )
}
