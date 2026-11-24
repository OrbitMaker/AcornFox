// 体检（样稿第 11 版：“体检也放到设置里，这是一个纯粹的显示有什么的，就可以了。”）。
// 以前是左边菜单里单独的一页（#runtime），现在是“设置 › 这台机器”里的一行（pages/prefs/machine.tsx）：
//   收起：一个圆点加一句结论（“这台机器正常”；有事时“有 1 件事要处理”，琥珀点）——useCheckup() 算好给那一行用；
//   展开（CheckupBody）：四行，每行一个圆点、一个名字、一句现状：磁盘、下载应用、后台程序、在不在公网上
//     （家里的机器不在公网上是正常情况：灰点，不算要处理）。只显示，不放设置项，不放“重新检查”，行里不再套展开。
//     有要处理的行时：那一行琥珀点，下面写“怎么办”（故障说明里的 next），只留一个“复制给 AI 工作台”（编号只进复制的文字）。
//     最下面两个默认收起的：“技术信息”（系统、Docker、数据目录、检查时间、每个应用占了多少磁盘、下载顺序）、“用命令行”（sudo acornfox doctor）。
// 每行怎么合、颜色和计数怎么算在 checkup.ts（和原来体检页是同一套，没有另写）。
// 接口：GET /v1/runtime/checks（和 acornfox doctor 同一套检查和编号；key、app_disk、keep_versions 是样稿第 10 版的假字段，要服务端配合）、
//       GET /v1/setup/faults（编号的说明，单一来源在服务端）、GET /v1/metrics/host（磁盘用量）、GET /v1/status（版本、在不在公网上）、
//       GET /v1/catalog（应用目录的同步情况）、POST /v1/catalog/connect（连接私有目录，这一版藏起，见 PRIVATE_CATALOG_UI）。
import * as React from 'react'
import { Link2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { api, type ApiError } from '@/lib/api'
import { usePoll } from '@/lib/hooks'
import { timeText } from '@/lib/format'
import { onPublic, useAppNames } from '@/lib/display'
import { PRIVATE_CATALOG_UI } from '@/lib/features'
import type { FaultDef, HostView, RuntimeChecks } from '@/lib/types'
import { useConsole } from '@/state'
import { KV } from '@/components/bits'
import { AskAI, CliFold, Fold } from '@/components/fold'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { SkZone, useSkeleton } from '@/components/ui/skeleton'
import { SkRows } from '@/components/skeletons'
import { CopyLine } from '@/components/ui/copy'
import { Progress } from '@/components/ui/progress'
import { ConnectCatalogDialog } from '../store/connect-dialog'
import { checkupForAI, checkupRows, conclusion, diskFree, needsAction, type CheckupRow, type RowTone } from './checkup'

export function useFaults() {
  return usePoll((s) => api.get<{ faults: FaultDef[] }>('/v1/setup/faults', s).then((b) => b.faults || []).catch(() => [] as FaultDef[]), 0)
}

// checksErrorText：体检接口出错时页面上的那一句。旧服务端没有这个接口（404）、连不上后台程序（没有 HTTP 状态）、
// 服务端出错分开说；编号和命令不写在这句里（放进“复制给 AI 工作台”和“用命令行”）。
export function checksErrorText(err: Partial<ApiError> & { message?: string }): string {
  if (err.status === 404) return '这个版本还不能在页面上体检'
  if (!err.status) return '连不上后台程序，没能体检'
  return '体检没做完' + (err.message ? '：' + err.message : '')
}

function errorForAI(err: Partial<ApiError> & { message?: string }): string {
  return `控制台的体检没做完（${err.status || '连不上'}${err.code ? ' ' + err.code : ''} ${err.message || ''}）。用 acornfox skill 帮我检查这台机器：在这台机器上运行 sudo acornfox doctor --json。`
}

const GB = 1024 ** 3
const sizeText = (b: number) => (b >= GB ? (b / GB).toFixed(1) + ' GB' : Math.max(1, Math.round(b / 1024 ** 2)) + ' MB')

const DOT: Record<RowTone, string> = { ok: 'bg-success', warn: 'bg-warning', fail: 'bg-warning', info: 'bg-muted-foreground/50' }
const DOT_LABEL: Record<RowTone, string> = { ok: '正常', warn: '要处理', fail: '要处理', info: '说明' }

// Raw：服务端原来的说法（收在展开里）。
function Raw({ row }: { row: CheckupRow }) {
  return (
    <ul className="flex flex-col gap-1 text-xs text-muted-foreground" data-testid="checkup-raw">
      {row.items.map((it, i) => (
        <li key={i} className="flex items-start gap-1.5">
          <span className={cn('mt-1.5 size-1.5 shrink-0 rounded-full', it.level === 'ok' ? 'bg-success' : 'bg-warning')} aria-hidden="true" />
          <span className="min-w-0 break-words">{it.text}</span>
        </li>
      ))}
    </ul>
  )
}

// HowTo：要处理的那一行下面的“怎么办”：故障说明里的那一句和常见原因；编号只进复制给 AI 的文字。
// 服务端原话只在这一行合了不止一项检查（要看是哪一项），或者没有故障说明时才摆出来，免得和上面那句现状说两遍。
function HowTo({ row, faults }: { row: CheckupRow; faults: Map<string, FaultDef> }) {
  const bad = row.items.filter((i) => i.level !== 'ok')
  const defs = bad.map((i) => (i.code ? faults.get(i.code) : undefined)).filter((x): x is FaultDef => !!x)
  return (
    <div className="flex flex-col gap-2 rounded-md bg-muted/50 p-2.5 text-xs" data-testid="checkup-howto">
      {(row.items.length > 1 || defs.length === 0) && <Raw row={{ ...row, items: bad }} />}
      {defs.map((f) => (
        <div key={f.code}>
          <div className="font-medium">怎么办</div>
          <p className="text-muted-foreground">{f.next}</p>
          {f.causes.length > 0 && <p className="mt-1 text-2xs text-muted-foreground">常见原因：{f.causes.join('；')}</p>}
          {f.needs_user && <p className="mt-1 text-warning">这一项要你自己确认或操作。</p>}
        </div>
      ))}
    </div>
  )
}

function DiskBody({ c, host }: { c: RuntimeChecks; host?: HostView | null }) {
  const nameOf = useAppNames()
  const list = c.app_disk || []
  const max = Math.max(1, ...list.map((x) => x.bytes))
  return (
    <div className="flex flex-col gap-3">
      {host?.available && host.disk_total && host.disk_used !== undefined && (
        <div className="flex flex-col gap-1.5">
          <Progress value={(host.disk_used / host.disk_total) * 100} label="磁盘已用"
            tone={host.disk_total - host.disk_used < 3 * GB ? 'danger' : host.disk_total - host.disk_used < 10 * GB ? 'warning' : undefined} />
          <div className="text-2xs text-muted-foreground">一共 {sizeText(host.disk_total)}，用了 {sizeText(host.disk_used)}</div>
        </div>
      )}
      {list.length > 0 && (
        <div className="flex flex-col gap-1.5" data-testid="app-disk">
          <div className="text-xs font-medium">每个应用占了多少</div>
          <ul className="flex flex-col gap-1.5">
            {list.map((x) => (
              <li key={x.app} className="flex items-center gap-3 text-xs">
                <span className="w-28 shrink-0 truncate sm:w-36">{nameOf(x.app)}</span>
                <span className="min-w-0 flex-1"><Progress value={(x.bytes / max) * 100} label={nameOf(x.app) + ' 占的磁盘'} /></span>
                <span className="tnum w-16 shrink-0 text-right text-muted-foreground">{sizeText(x.bytes)}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      <p className="text-2xs text-muted-foreground">
        每个应用除了正在用的，还留着最多 {c.keep_versions ?? 5} 个旧版本，用来退回上一版；更早的自动删掉。
      </p>
    </div>
  )
}

function DownloadBody({ c, onConnect }: { c: RuntimeChecks; onConnect: () => void }) {
  const { catalog } = useConsole()
  const src = c.sources
  const cats = catalog?.sources || []
  const host = (u: string) => (u === 'registry-1.docker.io' ? 'Docker Hub 官方' : u.replace(/^https?:\/\//, ''))
  return (
    <div className="flex flex-col gap-3 text-xs">
      {src && (
        <>
          <div className="flex flex-col gap-1">
            <div className="font-medium">下载应用时按这个顺序试，一个不通换下一个</div>
            <ol className="flex flex-col gap-1" data-testid="mirror-order">
              {src.docker_hub.map((h, i) => (
                <li key={h} className="flex items-center gap-2"><span className="tnum flex size-4 shrink-0 items-center justify-center rounded-full border text-2xs">{i + 1}</span><span className="mono break-all">{host(h)}</span></li>
              ))}
            </ol>
          </div>
          {src.ghcr && src.ghcr.length > 0 && <div><span className="text-muted-foreground">GitHub 上的应用程序走</span> <span className="mono">{src.ghcr.join('、')}</span></div>}
          <div><span className="text-muted-foreground">AcornFox 自己存的安装文件走</span> <span className="mono">{src.gateway ? host(src.gateway) : '（已关掉）'}</span></div>
        </>
      )}
      {cats.length > 0 && (
        <div className="flex flex-col gap-1">
          <div className="font-medium">应用中心的目录</div>
          <ul className="flex flex-col gap-1.5" data-testid="catalog-sources">
            {cats.map((s) => (
              <li key={s.id} className="flex flex-wrap items-center gap-2">
                <span>{s.kind === 'public' ? '应用中心' : s.name}</span>
                {s.disconnected ? <Badge variant="danger">已断开</Badge>
                  : s.code === 'CAT-SYNC-01' ? <Badge variant="outline">只用程序自带的目录</Badge>
                  : s.code ? <Badge variant="warning" title={s.message}>没同步上</Badge>
                  : <Badge variant="success">正常</Badge>}
                <span className="ml-auto text-2xs text-muted-foreground">{s.serial ? `${s.apps} 个应用` : '还没同步下来'}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {PRIVATE_CATALOG_UI && <div><Button variant="outline" size="sm" onClick={onConnect}><Link2 className="size-icon" />连接私有目录</Button></div>}
    </div>
  )
}

// useCheckup：体检的数据和算好的结论。“设置 › 这台机器”里体检那一行收起时的一句话、展开后的内容都用它。
export function useCheckup() {
  const { status, catalog } = useConsole()
  const show = catalog?.sync?.show || null
  const checks = usePoll((s) => api.get<RuntimeChecks>('/v1/runtime/checks', s), 60000)
  const host = usePoll((s) => api.get<HostView>('/v1/metrics/host', s).catch(() => null), 60000)
  const faults = useFaults()
  const c = checks.data
  const err = c ? null : (checks.error as ApiError | null)
  // 家里的机器：状态里说了不在公网上；旧服务端没给 on_public 时交给 checkup.ts 看编号
  const home = status?.on_public === undefined ? undefined : !onPublic(status)
  const rows = c ? checkupRows(c, { home, free: diskFree(c, host.data), show }) : []
  const head = conclusion(rows)
  return { c, err, rows, head, host: host.data, faults: faults.data || [], version: status?.version, show }
}
export type Checkup = ReturnType<typeof useCheckup>

// checkupSummary：收起时的那一句和圆点的颜色。
export function checkupSummary(k: Checkup): { text: string; tone: 'success' | 'warning' | '' } {
  if (k.err) return { text: checksErrorText(k.err), tone: 'warning' }
  if (!k.c) return { text: '正在检查…', tone: '' }
  return { text: k.head.text, tone: k.head.count ? 'warning' : 'success' }
}

// CheckupBody：展开后的内容。只显示，不放设置项。
export function CheckupBody({ k }: { k: Checkup }) {
  const [connect, setConnect] = React.useState(false)
  const { c, err, rows } = k
  const sk = useSkeleton(!c && !err)
  if (err) {
    return (
      <div className="flex flex-col items-start gap-2.5" data-testid="checkup-error">
        <AskAI text={errorForAI(err)} label="复制给 AI 工作台帮我查" />
        <CliFold><CopyLine text="sudo acornfox doctor" /><p className="text-2xs text-muted-foreground">在这台机器上运行。</p></CliFold>
      </div>
    )
  }
  if (sk.wait || !c) return <SkZone show={sk.show}><SkRows className="-mx-4" n={4} /></SkZone>
  const byCode = new Map(k.faults.map((f) => [f.code, f]))
  const bad = rows.filter(needsAction)
  const aiText = () => checkupForAI(c, { version: k.version, home: rows.some((r) => r.tone === 'info'), show: k.show })
  return (
    <div className="flex min-w-0 flex-col gap-3" data-testid="checkup-body">
      <ul className="flex min-w-0 flex-col gap-2.5">
        {rows.map((r) => (
          <li key={r.id} className="flex min-w-0 flex-col gap-2" data-testid="runtime-check" data-tone={r.tone}>
            <div className="flex min-w-0 items-baseline gap-2">
              <span className={cn('size-1.5 shrink-0 -translate-y-px self-center rounded-full', DOT[r.tone])} role="img" aria-label={DOT_LABEL[r.tone]} />
              <span className={cn('text-sm', r.state ? 'w-24 shrink-0 font-medium' : 'min-w-0 break-words', r.tone === 'info' && 'font-normal text-muted-foreground')}>{r.name}</span>
              {r.state && <span className={cn('min-w-0 break-words text-xs', needsAction(r) ? 'font-medium text-warning' : 'text-muted-foreground')}>{r.state}</span>}
            </div>
            {needsAction(r) && <HowTo row={r} faults={byCode} />}
          </li>
        ))}
      </ul>
      {bad.length > 0 && <div><AskAI text={aiText} label="复制给 AI 工作台帮我查" /></div>}
      <div className="flex flex-col gap-2 border-t pt-3">
        <Fold title="技术信息">
          <div className="flex flex-col gap-3" data-testid="checkup-tech">
            <div className="divide-y rounded-md border px-3 py-0.5">
              <KV k="系统">{c.os === 'linux' ? 'Linux' : c.os === 'darwin' ? 'macOS' : c.os === 'windows' ? 'Windows' : c.os}</KV>
              {c.docker_version && <KV k="Docker"><span className="tnum">{c.docker_version}</span></KV>}
              {k.version && <KV k="AcornFox"><span className="tnum">{k.version}</span></KV>}
              {c.data_dir && <KV k="数据目录"><span className="mono text-xs">{c.data_dir}</span></KV>}
              <KV k="检查时间"><span className="tnum">{timeText(c.checked_at)}</span></KV>
            </div>
            <DiskBody c={c} host={k.host} />
            <DownloadBody c={c} onConnect={() => setConnect(true)} />
          </div>
        </Fold>
        <CliFold>
          <CopyLine text="sudo acornfox doctor" />
          <p className="text-2xs text-muted-foreground">在这台机器上运行，查得更全：能看到别的程序占了哪个端口。</p>
        </CliFold>
      </div>
      {PRIVATE_CATALOG_UI && <ConnectCatalogDialog open={connect} onClose={() => setConnect(false)} />}
    </div>
  )
}
