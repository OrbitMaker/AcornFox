// 设置里的“服务”分区（样稿第 10 版，评估 N3、N5、N6、X3、X9）：这台机器上能给应用用的服务（装好的应用提供的，或你自己添加的），
// 一行一项，点开才展开，同一时间只展开一项。应用都用应用中心里的名字（useAppNames），代号只在技术信息里。
// 一行：左边是服务类型的中文名，中间一句“PostgreSQL 16 提供 · n8n 在用”或“你添加的“名字” · 谁在用”；能用是绿点，
// 别的状态按 serviceStatus 的结论给颜色，并把状态那几个字放在这句话最前面。
// 点开：
//   应用提供的：只有三样——谁提供（点名字去那个应用）、谁在用、[试一下通不通]；要在应用里做一步的项照旧显示那句提示和“填这一项”；
//     地址、端口、加密方式（disable→不加密 这类换成人话）、每个用它的应用各发一份的项，收进默认收起的“技术信息”；
//     没有中文名的项不列（以前会漏出“（未命名的项）url”）。删不了时说“它跟着 PostgreSQL 16：删掉这个应用，它才会消失”。
//   你自己添加的：各项的值能看、能改（保密项只显示后 4 位，页面上没有明文）、谁在用、试一下、删除。
// “试一下”：通了、没通、这次试不了三种，给人看的话里不带编号和地址；没通时旁边一个“复制给 AI 工作台”，编号和地址在复制的文字里。
// 存的值解不开的服务（列表接口的 unreadable）不在列表里，列表上方提示是哪几条，带“复制给 AI 工作台”。
// 谁在用：已删、数据还留着的应用（used_by 里 gone 为 true）写成“名字（已删除，数据还留着）”，不算在用，不拦删除。
// 删除成功时服务端可能带 notes：在列表上方原样列出，关掉或再删一条没有说明的服务时消失。
// 空的时候：“还没有。装应用时会自动出现在这里。”加一个“添加我已有的”按钮。“平台自动填的”挪到了 高级 › 平台自动填的（platform.tsx）。
// 接口：GET /v1/services、GET /v1/services/kinds、POST /v1/services、PATCH /v1/services/{name}、
//       POST /v1/services/{name}/test、DELETE /v1/services/{name}（有应用在用时被拒绝，列出是哪些应用）。
// 旧版本服务端没有服务接口：显示“这个版本还没有这项功能”。
import * as React from 'react'
import { AlertTriangle, CircleCheck, CircleMinus, CircleX, Plus, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { ApiError } from '@/lib/api'
import { usePoll } from '@/lib/hooks'
import { appHref } from '@/lib/router'
import { useAppNames, WORDS } from '@/lib/display'
import {
  canRemove, checkFieldValue, deleteService, editPlaceholder, fieldControl, fieldOptions, issuedItems, itemLabel, listServices, patchService, plainValue,
  providerText, refusedBy, removeConfirmText, serviceStatus, shownValues, slotsText, testForAI, testPlain, testService, usedByText, usersText, type NameOf,
} from '@/lib/services'
import type { ServiceField, ServiceKind, ServiceTestResult, ServiceValue, ServiceView } from '@/lib/types'
import { ExpandRow, type RowTone } from '@/components/expand-row'
import { AskAI, EmptyState, Fold } from '@/components/fold'
import { Button } from '@/components/ui/button'
import { Loading } from '@/components/ui/skeleton'
import { SkRows } from '@/components/skeletons'
import { useToast } from '@/components/ui/toast'
import { ApiErrorBox, FieldInput, ItemName, KindName, NOT_SUPPORTED, RegisterServiceDialog, useKinds } from './service-form'

const rowKey = (name: string) => 'svc:' + name
const domId = (name: string) => name.replace(/[^A-Za-z0-9_-]/g, '-')

export function Services() {
  const toast = useToast()
  const nameOf = useAppNames()
  const list = usePoll((s) => listServices(s), 0)
  const kinds = useKinds()
  const [adding, setAdding] = React.useState(false)
  const [removed, setRemoved] = React.useState<{ name: string; notes: string[] } | null>(null)
  const [open, setOpen] = React.useState('')
  const toggle = (k: string) => setOpen((o) => (o === k ? '' : k))
  const unsupported = !list.loading && !list.error && list.data === null
  const items = list.data?.services
  const unreadable = list.data?.unreadable || []
  const empty = !!items && items.length === 0 && unreadable.length === 0
  const addBtn = (variant: 'outline' | 'default') => <Button size="sm" variant={variant} onClick={() => setAdding(true)} disabled={list.loading && !list.data}><Plus className="size-icon" />{WORDS.addMine}</Button>
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-3 py-2.5">
        <div className="text-sm">这台机器上的服务</div>
        {!unsupported && !empty && addBtn('outline')}
      </div>
      <Loading when={list.loading && !list.data} skeleton={<SkRows className="-mx-4" />}>{null}</Loading>
      {list.error && <div className="py-2.5"><ApiErrorBox err={list.error} /></div>}
      {unsupported && (
        <div className="py-2.5 text-xs text-muted-foreground" data-testid="services-unsupported">
          {NOT_SUPPORTED}。<a href="#prefs/update" className="underline">怎么升级</a>
        </div>
      )}
      {unreadable.length > 0 && (
        <div className="flex flex-wrap items-start gap-2 py-2.5 text-xs text-warning" role="alert" data-testid="services-unreadable">
          <AlertTriangle className="mt-px size-icon shrink-0" />
          <span className="min-w-0 flex-1 break-words">这几条服务读不出来：{unreadable.join('、')}。用到它们的应用可能接不上。</span>
          <AskAI text={`这台机器上有几条服务读不出来：${unreadable.join('、')}。用 acornfox skill 帮我查（在这台机器上运行 acornfox services list --json 看详情）。`} />
        </div>
      )}
      {removed && (
        <div className="flex items-start gap-2 py-2.5 text-xs" role="status" data-testid="service-removed-notes">
          <div className="min-w-0 flex-1">
            <div className="font-medium">已删除“{removed.name}”</div>
            <ul className="mt-0.5 flex flex-col gap-0.5 text-muted-foreground">{removed.notes.map((n, i) => <li key={i} className="break-words">{n}</li>)}</ul>
          </div>
          <Button size="icon-sm" variant="ghost" className="shrink-0" onClick={() => setRemoved(null)} aria-label="知道了" title="知道了"><X className="size-icon" /></Button>
        </div>
      )}
      {empty && (
        <div className="py-3" data-testid="services-empty">
          <EmptyState title="还没有。装应用时会自动出现在这里。" action={addBtn('outline')} />
        </div>
      )}
      {(items || []).map((s) => (
        <ServiceRow key={s.name} s={s} nameOf={nameOf} kind={kinds.data?.find((k) => k.kind === s.kind)} kindsKnown={!!kinds.data}
          open={open === rowKey(s.name)} onToggle={() => toggle(rowKey(s.name))} onChanged={list.refresh}
          onRemoved={(notes) => setRemoved(notes.length ? { name: s.name, notes } : null)} />
      ))}
      <RegisterServiceDialog open={adding} onClose={() => setAdding(false)} onAdded={(s) => { setAdding(false); toast('已添加', s.name); list.refresh() }} />
    </>
  )
}

// KV：展开后的一项：左边名字，右边值。
function KV({ k, children, mono, className, testId }: { k: React.ReactNode; children: React.ReactNode; mono?: boolean; className?: string; testId?: string }) {
  return (
    <div className={cn('flex items-baseline gap-3 py-1', className)} data-testid={testId}>
      <dt className="w-20 shrink-0 text-muted-foreground">{k}</dt>
      <dd className={cn('min-w-0 flex-1 break-all', mono && 'mono')}>{children}</dd>
    </div>
  )
}

function ServiceRow({ s, nameOf, kind, kindsKnown, open, onToggle, onChanged, onRemoved }: {
  s: ServiceView; nameOf: NameOf; kind?: ServiceKind; kindsKnown: boolean; open: boolean; onToggle: () => void; onChanged: () => Promise<void>; onRemoved: (notes: string[]) => void
}) {
  const st = serviceStatus(s, nameOf)
  const ok = s.status === 'ok'
  const tone: RowTone = ok ? 'success' : st.tone === 'muted' ? '' : st.tone
  return (
    <div className="-mx-4" data-testid="service" data-name={s.name}>
      <ExpandRow id={'svc-body-' + domId(s.name)} wide stack tone={tone} open={open} onToggle={onToggle}
        name={<KindName label={s.kind_label || kind?.label} kind={s.kind} />}
        summaryTestId="service-summary"
        summary={<>{!ok && <><span data-testid="service-status" title={st.detail}>{st.text}</span>{' · '}</>}{providerText(s, nameOf)} · {usedByText(s, nameOf)}</>}>
        <ServiceBody s={s} nameOf={nameOf} kind={kind} kindsKnown={kindsKnown} onChanged={onChanged} onRemoved={onRemoved} />
      </ExpandRow>
    </div>
  )
}

// fieldFor：改一项时用的定义。必填照服务类型的定义（必填项不能留空保存）；不带默认值，免得没改的项被默认值盖掉。
function fieldFor(v: ServiceValue, kind?: ServiceKind): ServiceField {
  const f = kind?.fields.find((x) => x.key === v.key)
  return f ? { ...f, default: null } : { key: v.key, label: v.label || '', type: 'string', secret: v.secret }
}

// ValueLine：一项的值（能改的带“改”或“填这一项”）。
function ValueLine({ s, v, kind, editing, setEditing, onSaved }: {
  s: ServiceView; v: ServiceValue; kind?: ServiceKind; editing: string; setEditing: (k: string) => void; onSaved: (notes: string[]) => Promise<void>
}) {
  const f = kind?.fields.find((x) => x.key === v.key)
  return (
    <div className="py-1">
      <div className="flex items-center gap-3">
        <dt className="w-20 shrink-0 text-muted-foreground"><ItemName label={v.label} item={v.key} /></dt>
        <dd className={cn('min-w-0 flex-1 break-all', v.set ? (v.secret || f?.type === 'url' ? 'mono' : '') : 'text-muted-foreground')}>{v.set ? plainValue(v, f) : '没填'}</dd>
        {editing !== v.key && (s.source === 'user' || !!v.ask) && (
          <Button size="sm" variant={v.ask && !v.set ? 'outline' : 'ghost'} className="tap h-ctl-xs shrink-0 px-2 text-2xs" onClick={() => setEditing(v.key)} aria-label={(v.ask && !v.set ? '填' : '改') + itemLabel(v.label)}>
            {v.ask && !v.set ? '填这一项' : '改'}
          </Button>
        )}
      </div>
      {v.ask && !v.set && <p className="mt-1 text-2xs text-warning" data-testid="service-ask">{v.ask}</p>}
      {editing === v.key && (
        <EditValue s={s} current={v} field={fieldFor(v, kind)} onDone={async (n) => { setEditing(''); await onSaved(n) }} onCancel={() => setEditing('')} />
      )}
    </div>
  )
}

function ServiceBody({ s, nameOf, kind, kindsKnown, onChanged, onRemoved }: {
  s: ServiceView; nameOf: NameOf; kind?: ServiceKind; kindsKnown: boolean; onChanged: () => Promise<void>; onRemoved: (notes: string[]) => void
}) {
  const toast = useToast()
  const [editing, setEditing] = React.useState('')
  const [notes, setNotes] = React.useState<string[]>([])
  const [testing, setTesting] = React.useState(false)
  const [tested, setTested] = React.useState<ServiceTestResult | null>(null)
  const [testErr, setTestErr] = React.useState<ApiError | null>(null)
  const [confirm, setConfirm] = React.useState(false)
  const [deleting, setDeleting] = React.useState(false)
  const [delErr, setDelErr] = React.useState<{ err: ApiError; apps: string[] } | null>(null)
  const fromApp = s.source === 'app'
  const canTest = kindsKnown ? !!kind?.has_test : true
  const values = shownValues(s)
  // 应用提供的：要用户在应用里做一步的项放外面，其余收进技术信息；没有中文名的项不列
  const asks = fromApp ? values.filter((v) => !!v.ask && !v.set) : []
  const techValues = fromApp ? values.filter((v) => !(v.ask && !v.set) && !!(v.label || '').trim()) : []
  const ownValues = fromApp ? asks : values
  const issued = issuedItems(s)
    .map((k) => ({ key: k, label: (kind?.fields.find((f) => f.key === k)?.label || '').trim() }))
    .filter((x) => !!x.label)
  const result = tested ? testPlain(tested) : null
  const slots = slotsText(s, nameOf)
  const saved = async (n: string[]) => { setNotes(n); toast('已保存', s.name); await onChanged() }

  const runTest = async () => {
    setTesting(true)
    setTested(null)
    setTestErr(null)
    try {
      setTested(await testService(s.name))
    } catch (e) {
      setTestErr(e as ApiError)
    } finally {
      setTesting(false)
    }
  }
  const remove = async () => {
    setDeleting(true)
    setDelErr(null)
    try {
      const r = await deleteService(s.name)
      toast('已删除', s.name)
      // 先交给上面记下，再刷新列表（这一条随刷新消失，话不能跟着丢）。
      onRemoved(r.notes)
      await onChanged()
    } catch (e) {
      const ae = e as ApiError
      const refused = ae.code === 'CAT-SVC-04' || Array.isArray(ae.raw?.used_by)
      setDelErr({ err: ae, apps: refused ? refusedBy(ae, s.used_by).map(nameOf) : [] })
      setConfirm(false)
    } finally {
      setDeleting(false)
    }
  }
  return (
    <div className="flex flex-col gap-2">
      {s.status === 'not_ready' && (
        <div className="text-xs text-warning" data-testid="service-not-ready">
          <p>还不能用：{s.ready_say || '提供它的应用还没准备好'}</p>
          <p className="mt-0.5 text-2xs text-muted-foreground">别的应用照样能接上它，用到它的功能要等它好了才能用。</p>
        </div>
      )}
      <dl className="flex flex-col text-xs">
        {fromApp && (
          <KV k="谁提供" testId="service-provider">
            {s.app && s.status !== 'provider_gone' ? <a href={appHref(s.app)} className="underline-offset-2 hover:underline">{nameOf(s.app)}</a> : s.app ? nameOf(s.app) + '（已删除）' : '一个应用'}
          </KV>
        )}
        <KV k="谁在用" testId="service-users">{usersText(s, nameOf)}</KV>
        {slots.text && <KV k="还能接吗" testId="service-slots" className={slots.full ? 'text-warning' : ''}>{slots.text}</KV>}
      </dl>
      {ownValues.length > 0 && (
        <dl className="flex flex-col text-xs" data-testid="service-values">
          {ownValues.map((v) => <ValueLine key={v.key} s={s} v={v} kind={kind} editing={editing} setEditing={setEditing} onSaved={saved} />)}
        </dl>
      )}
      {!fromApp && values.length === 0 && <div className="py-1 text-xs text-muted-foreground">没有可显示的项</div>}
      {notes.length > 0 && <ul className="flex flex-col gap-0.5 text-2xs text-muted-foreground" data-testid="service-notes">{notes.map((n, i) => <li key={i}>{n}</li>)}</ul>}
      {result && (
        <div className={cn('flex flex-wrap items-start gap-1.5 text-xs', result.kind === 'ok' ? 'text-success' : result.kind === 'failed' ? 'text-danger' : 'text-muted-foreground')}
          role="status" data-testid="service-test" data-result={result.kind}>
          {result.kind === 'ok' ? <CircleCheck className="mt-px size-icon shrink-0" /> : result.kind === 'failed' ? <CircleX className="mt-px size-icon shrink-0" /> : <CircleMinus className="mt-px size-icon shrink-0" />}
          <span className="min-w-0 flex-1 break-words">{result.text}</span>
          {result.kind === 'failed' && tested && <AskAI text={testForAI(s.name, tested)} variant="ghost" />}
        </div>
      )}
      {testErr && <ApiErrorBox err={testErr} />}
      {delErr && (
        <ApiErrorBox err={delErr.err}>
          {delErr.apps.length > 0 && (
            <div className="mt-1 text-foreground" data-testid="service-refused">
              这些应用在用它：{delErr.apps.join('、')}。先把它们删掉或换成别的服务，再删这一条。
            </div>
          )}
        </ApiErrorBox>
      )}
      <div className="flex flex-wrap items-center gap-2 pt-0.5">
        {canTest && <Button size="sm" variant="outline" onClick={runTest} loading={testing}>试一下通不通</Button>}
        {!canRemove(s) ? (
          <span className="text-2xs text-muted-foreground">它跟着 {s.app ? nameOf(s.app) : '提供它的应用'}：删掉这个应用，它才会消失</span>
        ) : !confirm ? (
          <Button size="sm" variant="ghost" className="text-danger" onClick={() => setConfirm(true)}>删除</Button>
        ) : (
          <span className="flex flex-wrap items-center gap-2 text-xs">
            <span data-testid="service-remove-confirm">{removeConfirmText(s, nameOf)}</span>
            <Button size="sm" variant="destructive" onClick={remove} loading={deleting}>确认删除</Button>
            <Button size="sm" variant="ghost" onClick={() => setConfirm(false)}>取消</Button>
          </span>
        )}
      </div>
      {fromApp && (techValues.length > 0 || issued.length > 0 || s.name !== s.app) && (
        <Fold title="技术信息" className="pt-1">
          <dl className="flex flex-col" data-testid="service-tech">
            {s.name !== s.app && <KV k="服务名" mono testId="service-name">{s.name}</KV>}
            {s.app && <KV k="应用代号" mono>{s.app}</KV>}
            {techValues.map((v) => <KV key={v.key} k={v.label}><span className={v.secret ? 'mono' : undefined}>{v.set ? plainValue(v, kind?.fields.find((x) => x.key === v.key)) : '没填'}</span></KV>)}
            {issued.length > 0 && (
              <KV k={issued.map((x) => x.label).join('、')} testId="service-issued">每个用它的应用各发一份，不用你填</KV>
            )}
          </dl>
        </Fold>
      )}
    </div>
  )
}

// editStart：开始改时输入框里的值。下拉框从现在的值开始（必填的下拉没有“不填”，从第一项开始）；输入框从空开始。
function editStart(field: ServiceField, current: ServiceValue): string {
  if (fieldControl(field) !== 'select') return ''
  const opts = fieldOptions(field).map(([v]) => v)
  if (current.set && !field.secret && opts.includes(current.value)) return current.value
  return opts[0] || ''
}

// EditValue：改一项（或填“要在应用里做一步”的那项）。只发这一项；空值表示清掉，清掉前再确认一次；必填项不能留空。
function EditValue({ s, current, field, onDone, onCancel }: { s: ServiceView; current: ServiceValue; field: ServiceField; onDone: (notes: string[]) => void; onCancel: () => void }) {
  const [value, setValue] = React.useState(() => editStart(field, current))
  const [problem, setProblem] = React.useState('')
  const [err, setErr] = React.useState<ApiError | null>(null)
  const [busy, setBusy] = React.useState(false)
  const [confirmClear, setConfirmClear] = React.useState(false)
  const label = itemLabel(field.label)
  const save = async (cleared = false) => {
    const m = checkFieldValue(field, value)
    setProblem(m)
    setErr(null)
    if (m) return
    const v = value.trim()
    if (!v && !current.set) {
      setProblem('还没填新的值')
      return
    }
    if (!v && !cleared) {
      setConfirmClear(true)
      return
    }
    setConfirmClear(false)
    setBusy(true)
    try {
      const r = await patchService(s.name, { [field.key]: v })
      onDone(r?.notes || [])
    } catch (e) {
      setErr(e as ApiError)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="mt-1.5 flex flex-col gap-2 rounded-md bg-muted/40 p-2" data-testid="service-edit">
      <FieldInput field={field} idPrefix={'svc-edit-' + s.name.replace(/[^A-Za-z0-9_-]/g, '-') + '-'} value={value} problem={problem}
        onChange={(x) => { setValue(x); setConfirmClear(false) }} placeholder={editPlaceholder(field)} />
      {err && <ApiErrorBox err={err} />}
      {confirmClear ? (
        <div className="flex flex-wrap items-center justify-end gap-2 text-xs" data-testid="service-clear-confirm">
          <span>保存后会清掉“{label}”，确定吗？</span>
          <Button size="sm" variant="destructive" onClick={() => save(true)} loading={busy}>确认清掉</Button>
          <Button size="sm" variant="ghost" onClick={() => setConfirmClear(false)}>不清</Button>
        </div>
      ) : (
        <div className="flex justify-end gap-2">
          <Button size="sm" variant="ghost" onClick={onCancel}>取消</Button>
          <Button size="sm" onClick={() => save()} loading={busy}>保存</Button>
        </div>
      )}
    </div>
  )
}
