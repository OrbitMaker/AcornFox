// 安装弹窗里的“需要的服务”：数据来自安装前的计划（POST /v1/catalog/plan）。
// 每个需要一行：已接上（接的是哪条）、可选的没有（没有时会怎样）、必须的没有（登记一条你已有的：命令，也可以当场登记）、
// 有多条要选（选了写进安装请求的 use）。不认识的状态写“状态未知”，交给服务端在安装时检查。
// 配方里后装的应用要接的服务由前面的应用装好后登记（pending_from）：写“装好 X 后接上它提供的 …”，算已接上。
// 平台只在这台机器已有的服务里找，不替用户装别的应用，也不提示先装哪个应用。
// 服务端没有计划接口（旧版本）或计划里没有 needs 时，这一块不显示，安装弹窗和以前一样。
import { useRef, useState } from 'react'
import { AlertTriangle } from 'lucide-react'
import { cn } from '@/lib/utils'
import { TONE_BADGE, candidateNames, hasNeeds, kindLabel, multiApp, needText, noCandidates, pendingFrom, pendingText, planRows, type PlanRow } from '@/lib/services'
import type { InstallPlan, PlanNeed } from '@/lib/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/controls'
import { CopyLine } from '@/components/ui/copy'
import { InlineRegister, KindName } from '../prefs/service-form'

export function NeedsBlock({ plan, uses, onUse, onRegistered }: {
  plan: InstallPlan | null
  uses: Record<string, string>
  onUse: (key: string, service: string) => void
  onRegistered: (name: string) => void
}) {
  // 选过一次以后计划会变成“已接上”、不再给候选；记住第一次看到的候选，让用户还能换。
  const seen = useRef<Record<string, string[]>>({})
  if (!plan || !hasNeeds(plan)) return null
  const rows = planRows(plan)
  for (const r of rows) {
    const names = candidateNames(r.need)
    if (names.length) seen.current[r.key] = [...new Set([...(seen.current[r.key] || []), ...names])]
  }
  const multi = multiApp(plan)
  return (
    <div className="flex flex-col gap-2 rounded-md border px-2.5 py-2" data-testid="plan-needs">
      <div className="text-xs font-medium">需要的服务</div>
      {plan.say && <p className="text-2xs text-muted-foreground">{plan.say}</p>}
      <ul className="flex flex-col divide-y">
        {rows.map((r) => (
          <NeedRow key={r.key} row={r} appLabel={multi ? r.app.name : ''} picked={uses[r.key] || ''} candidates={seen.current[r.key] || []}
            onUse={(v) => onUse(r.key, v)} onRegistered={onRegistered} />
        ))}
      </ul>
    </div>
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
  const n: PlanNeed = row.need
  const [registering, setRegistering] = useState(false)
  const t = needText(n)
  // 句子里用中文名（没有时是“未命名的服务类型”）；标题那一处另用小字带上原名供排查。
  const label = kindLabel(n.kind_label)
  // 说要选一条却一条都没有（之前也没见过）：让用户登记一条再试。
  const empty = noCandidates(n) && candidates.length === 0
  const canPick = !empty && (n.status === 'choose' || (!!picked && candidates.length > 1))
  const canRegister = n.status === 'required_missing' || n.status === 'optional_missing' || empty
  return (
    <li className="flex flex-col gap-1.5 py-2 first:pt-1 last:pb-0.5" data-testid="need" data-status={n.status}>
      <div className="flex flex-wrap items-center gap-1.5 text-xs">
        {appLabel && <span className="text-muted-foreground">{appLabel} ·</span>}
        <span className="font-medium"><KindName label={n.kind_label} kind={n.kind} /></span>
        {!n.required && <span className="text-2xs text-muted-foreground">可选</span>}
        <Badge variant={TONE_BADGE[t.tone]} className="ml-auto">{t.text}</Badge>
      </div>
      {pendingFrom(n) && !canPick && <p className="text-2xs text-muted-foreground" data-testid="need-pending">{pendingText(n)}</p>}
      {n.status === 'matched' && !pendingFrom(n) && !canPick && <p className="text-2xs text-muted-foreground">接的是“{n.service}”</p>}
      {n.status === 'optional_missing' && <p className="text-2xs text-muted-foreground">{n.without || '没有也能装，以后可以再接上'}</p>}
      {n.status === 'required_missing' && (
        <>
          <p className="flex items-center gap-1 text-2xs text-danger"><AlertTriangle className="size-3" />这台机器上没有{label}。登记一条你已有的，再装：</p>
          {(n.how || []).map((c) => <CopyLine key={c} text={c} />)}
        </>
      )}
      {canPick && (
        <Select aria-label={(appLabel ? appLabel + ' 的' : '') + label + '用哪一条'} value={picked || (n.status === 'matched' ? n.service || '' : '')} onChange={(e) => onUse(e.target.value)}
          options={[...(n.status === 'choose' && !picked ? [['', '请选一条'] as [string, string]] : []), ...candidates.map((c): [string, string] => [c, c])]} />
      )}
      {n.status === 'choose' && !picked && !empty && <p className="text-2xs text-warning">这台机器上有 {candidates.length} 条{label}，选一条给它用</p>}
      {empty && <p className="text-2xs text-warning" data-testid="need-no-candidates">没有可选的服务，登记一条后再试</p>}
      {canRegister && !registering && (
        <div><Button variant="link" size="sm" className={cn('h-auto p-0 text-2xs')} onClick={() => setRegistering(true)}>登记一条你已有的{label}</Button></div>
      )}
      {registering && (
        <InlineRegister kind={n.kind} idPrefix={'need-' + row.key.replace(/[^A-Za-z0-9_-]/g, '-') + '-'} onCancel={() => setRegistering(false)}
          onAdded={(s) => { setRegistering(false); onRegistered(s.name) }} />
      )}
    </li>
  )
}

// PlanBlocked：现在不能装的原因（安装按钮不可点）。
export function PlanBlocked({ reasons }: { reasons: string[] }) {
  if (reasons.length === 0) return null
  return (
    <div className="rounded-md border border-danger/40 bg-danger/5 px-3 py-2 text-xs" role="alert" data-testid="plan-blocked">
      <div className="font-medium text-danger">现在还不能装</div>
      <ul className="mt-0.5 flex flex-col gap-0.5 text-muted-foreground">{reasons.map((r) => <li key={r}>{r}</li>)}</ul>
    </div>
  )
}
