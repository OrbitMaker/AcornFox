// 安装弹窗里“它要用到的服务”：数据来自安装前的计划（POST /v1/catalog/plan）。页面上统一叫“服务”。
// 样稿第 10 版（评估 L2、L5、R8、X3、X4、X9）分三种样子：
// - 都接上了：一行“✓ 会自动接上这台机器上的 PostgreSQL 16”（MatchedLine；服务名按应用中心里的名字显示）。
//   解决方案里后装的应用要接前面应用的服务（pending_from）不单独说：确认那一句“装完自动接好”已经说了。
// - 要用户动手的（有多条要选、可选的没有、解决方案里某个必须的没有）：NeedsBlock 一项一行，
//   “添加我已有的”当场填（InlineRegister），不摆命令。
// - 单个应用缺必须的服务：整屏换成 MissingScreen——一句话、一个主按钮“用‘某某’一起装好”（有能补上的解决方案时）、
//   一行小字“我自己有 MySQL 数据库，添加进来 ›”；没有解决方案能补时，一句话加“添加我已有的”一个按钮。
//   平台只在这台机器已有的服务里找，不提示“先装某某应用”，一起装靠解决方案（AGENTS.md“单机版很纯粹”）。
// 服务端没有计划接口（旧版本）或计划里没有 needs 时，这些都不显示，安装弹窗和以前一样。
// 不认识的状态不在这里说，交给服务端在安装时检查。
import { useRef, useState } from 'react'
import { AlertTriangle, CircleCheck } from 'lucide-react'
import { candidateNames, hasNeeds, kindLabel, multiApp, noCandidates, pendingFrom, planRows, type PlanRow } from '@/lib/services'
import { WORDS, gap, useAppNames } from '@/lib/display'
import { findApp } from '@/lib/catalog'
import type { InstallPlan, PlanNeed } from '@/lib/types'
import { useConsole } from '@/state'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/controls'
import { InlineRegister } from '../prefs/service-form'

// missingRows：必须的、这台机器上又没有的（解决方案里等前面应用装好再接的不算）。
export function missingRows(plan: InstallPlan | null | undefined): PlanRow[] {
  return planRows(plan).filter((r) => r.need.status === 'required_missing' && !pendingFrom(r.need))
}

// actionRows：要用户动手的那几项。
function actionRows(plan: InstallPlan | null | undefined): PlanRow[] {
  return planRows(plan).filter((r) => !pendingFrom(r.need) && (r.need.status === 'choose' || r.need.status === 'optional_missing' || r.need.status === 'required_missing'))
}

// MatchedLine：都接上了时的一行。
export function MatchedLine({ plan }: { plan: InstallPlan | null }) {
  const names = useAppNames()
  if (!plan || !hasNeeds(plan) || actionRows(plan).length > 0) return null
  const got = planRows(plan).filter((r) => r.need.status === 'matched' && !pendingFrom(r.need) && r.need.service)
  if (got.length === 0) return null
  return (
    <div className="flex items-start gap-1.5 text-xs text-muted-foreground" data-testid="plan-matched">
      <CircleCheck className="mt-px size-icon shrink-0 text-success" />
      <span>会自动接上这台机器上的 {got.map((r) => names(r.need.service || '')).join('、')}</span>
    </div>
  )
}

export function NeedsBlock({ plan, uses, onUse, onRegistered }: {
  plan: InstallPlan | null
  uses: Record<string, string>
  onUse: (key: string, service: string) => void
  onRegistered: (name: string) => void
}) {
  // 选过一次以后计划会变成“已接上”、不再给候选；记住第一次看到的候选，让用户还能换。
  const seen = useRef<Record<string, string[]>>({})
  const { catalog } = useConsole()
  if (!plan || !hasNeeds(plan)) return null
  const rows = planRows(plan)
  for (const r of rows) {
    const names = candidateNames(r.need)
    if (names.length) seen.current[r.key] = [...new Set([...(seen.current[r.key] || []), ...names])]
  }
  // 选过的（现在是“已接上”）也留着下拉框，让用户还能换。
  const act = new Set(actionRows(plan).map((r) => r.key))
  const shown = rows.filter((r) => act.has(r.key) || (!!uses[r.key] && (seen.current[r.key] || []).length > 1))
  if (shown.length === 0) return null
  const multi = multiApp(plan)
  return (
    <ul className="flex flex-col gap-2.5" data-testid="plan-needs">
      {shown.map((r) => (
        <NeedRow key={r.key} row={r} appLabel={multi ? findApp(catalog, r.app.id)?.name || r.app.name : ''} picked={uses[r.key] || ''} candidates={seen.current[r.key] || []}
          onUse={(v) => onUse(r.key, v)} onRegistered={onRegistered} />
      ))}
    </ul>
  )
}

function NeedRow({ row, appLabel, picked, candidates, onUse, onRegistered }: {
  row: PlanRow
  appLabel: string
  picked: string
  candidates: string[]
  onUse: (service: string) => void
  onRegistered: (name: string) => void
}) {
  const names = useAppNames()
  const n: PlanNeed = row.need
  const [registering, setRegistering] = useState(false)
  const label = kindLabel(n.kind_label)
  const who = appLabel ? appLabel + ' ' : ''
  // 说要选一条却一条都没有（之前也没见过）：让用户添加一条再试。
  const empty = noCandidates(n) && candidates.length === 0
  const canPick = !empty && (n.status === 'choose' || (!!picked && candidates.length > 1))
  const canRegister = n.status === 'required_missing' || n.status === 'optional_missing' || empty
  const id = 'need-' + row.key.replace(/[^A-Za-z0-9_-]/g, '-')
  return (
    <li className="flex flex-col gap-1.5 text-xs" data-testid="need" data-status={n.status}>
      {canPick && (
        <>
          <label htmlFor={id} className="font-medium">{who}用哪一个 {label}</label>
          <Select id={id} value={picked || (n.status === 'matched' ? n.service || '' : '')} onChange={(e) => onUse(e.target.value)}
            options={[...(n.status === 'choose' && !picked ? [['', '选一个'] as [string, string]] : []), ...candidates.map((c): [string, string] => [c, names(c)])]} />
        </>
      )}
      {n.status === 'optional_missing' && <p className="text-muted-foreground">{who}还可以接一个 {label}；{n.without || '没有也能装，以后可以再接上'}</p>}
      {n.status === 'required_missing' && <p className="flex items-center gap-1 text-warning"><AlertTriangle className="size-3 shrink-0" />{who}还要用到{gap(label)}，这台机器上还没有</p>}
      {empty && <p className="text-warning" data-testid="need-no-candidates">这台机器上还没有 {label}</p>}
      {canRegister && !registering && (
        <div><Button variant="link" size="sm" className="h-auto p-0 text-2xs" onClick={() => setRegistering(true)}>{WORDS.addMine} {label}</Button></div>
      )}
      {registering && (
        <InlineRegister kind={n.kind} idPrefix={id + '-'} onCancel={() => setRegistering(false)}
          onAdded={(s) => { setRegistering(false); onRegistered(s.name) }} />
      )}
    </li>
  )
}

// MissingScreen：单个应用缺必须的服务时，安装弹窗里只有这一屏（评估 L2）。
// appName：应用中心里的名字；rows：缺的那几项；purpose：某项“用来做什么”（清单里 uses 的 use，没有时不说）；
// fix：能一起补上的解决方案的名字（没有时为空）；onFix：点了改装那个解决方案。
export function MissingScreen({ appName, rows, purpose, fix, onFix, onRegistered, onCancel }: {
  appName: string
  rows: PlanRow[]
  purpose: (row: PlanRow) => string
  fix: string
  onFix: () => void
  onRegistered: (name: string) => void
  onCancel: () => void
}) {
  const [registering, setRegistering] = useState('')
  const labels = rows.map((r) => kindLabel(r.need.kind_label))
  const one = rows.length === 1 ? purpose(rows[0]) : ''
  const tail = /[A-Za-z0-9]$/.test(appName) ? ' ' : ''
  const sentence = rows.length === 1
    ? `${appName}${tail}还要用到一个${WORDS.service}：${labels[0]}${one ? `（用来${one}）` : ''}。`
    : `${appName}${tail}还要用到 ${rows.length} 个${WORDS.service}：${labels.join('、')}。`
  const reg = rows.find((r) => r.key === registering)
  return (
    <div className="flex flex-col gap-4" data-testid="plan-missing">
      <p className="text-sm">{sentence}</p>
      {reg ? (
        <InlineRegister kind={reg.need.kind} idPrefix={'miss-' + reg.key.replace(/[^A-Za-z0-9_-]/g, '-') + '-'} onCancel={() => setRegistering('')}
          onAdded={(s) => { setRegistering(''); onRegistered(s.name) }} />
      ) : fix ? (
        <div className="flex flex-col items-start gap-2.5">
          <Button onClick={onFix} data-testid="missing-fix">用“{fix}”一起装好</Button>
          {rows.map((r) => (
            <button key={r.key} type="button" onClick={() => setRegistering(r.key)} className="text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">
              我自己有{gap(kindLabel(r.need.kind_label))}，添加进来 ›
            </button>
          ))}
        </div>
      ) : (
        <div className="flex flex-col items-start gap-2">
          {rows.map((r) => (
            <Button key={r.key} onClick={() => setRegistering(r.key)}>{WORDS.addMine}{rows.length > 1 ? ' ' + kindLabel(r.need.kind_label) : ''}</Button>
          ))}
        </div>
      )}
      <div className="flex justify-end">
        <Button variant="outline" onClick={onCancel}>取消</Button>
      </div>
    </div>
  )
}

// PlanBlocked：现在还不能装的原因（安装按钮不可点）。故障编号不单独显示（评估 X4），同一句只说一次。
export function PlanBlocked({ reasons }: { reasons: string[] }) {
  const lines = [...new Set(reasons.map((r) => r.replace(/^[A-Z][A-Z0-9-]*-\d+\s*·\s*/, '')))]
  if (lines.length === 0) return null
  return (
    <div className="flex flex-col gap-0.5 rounded-md border border-warning/40 px-3 py-2 text-xs" role="alert" data-testid="plan-blocked">
      {lines.map((r) => <p key={r} className="flex items-start gap-1.5"><AlertTriangle className="mt-px size-icon shrink-0 text-warning" />{r}</p>)}
    </div>
  )
}
