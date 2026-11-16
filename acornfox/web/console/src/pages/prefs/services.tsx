// 设置里的“服务”分区：这台机器上能给应用用的服务（装好的应用提供的，或自己登记的），一行一项，点开才展开，同一时间只展开一项。
// 一行：左边是服务类型的中文名，中间一句“应用名 提供 · 谁在用”或“你登记的“名字” · 谁在用”；能用是绿点，
// 别的状态按 serviceStatus 的结论给颜色，并把状态那几个字放在这句话最前面。
// 点开看各项的值（保密项只显示后 4 位，页面上没有显示明文）；要在应用里做一步的项显示那句提示和“填这一项”；
// 按每个使用它的应用单独发的项（issued_per_consumer）单独列出，不算“没填”；有名额限制时写还能接几个。
// “试一下”：通了、没通（带故障编号）、试不了（skipped，不是没通）三种，连的是哪（target）也写出来。
// 存的值解不开的服务（列表接口的 unreadable）不在列表里，列表上方提示是哪几条。
// 谁在用：已删、数据还留着的应用（used_by 里 gone 为 true）写成“名字（已删除，数据还留着）”，不算在用，不拦删除。
// 删除成功时服务端可能带 notes（例如哪些已删的应用以后装回来要重新接服务）：在列表上方原样列出，关掉或再删一条没有说明的服务时消失。
// 应用提供的服务只在提供它的应用已删时能在这里删。服务类型或项没有中文名时说“未命名”，原名用小字带上。
// 页面上的说明不举具体的服务类型：例子取自目录里定义的服务类型。
// 列表最后固定一行“平台自动填的”：平台按这台机器替应用填好的几项（时区、下载源等）和现在的值；每个应用各算各的不列。
// 接口：GET /v1/services、GET /v1/services/kinds、POST /v1/services、PATCH /v1/services/{name}、
//       POST /v1/services/{name}/test、DELETE /v1/services/{name}（有应用在用时被拒绝，列出是哪些应用）、
//       GET /v1/catalog/platform-vars（“平台自动填的”）。
// 旧版本服务端没有服务接口：显示“这个版本的服务端还没有这项功能”；没有 platform-vars 接口（404）时不显示“平台自动填的”。
import * as React from 'react'
import { AlertTriangle, CircleCheck, CircleMinus, CircleX, Plus, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { ApiError } from '@/lib/api'
import { usePoll } from '@/lib/hooks'
import {
  canRemove, checkFieldValue, deleteService, editPlaceholder, fieldControl, fieldOptions, issuedItems, itemLabel, listPlatformVars, listServices, patchService,
  platformLabel, platformShown, platformSummary, providerText, refusedBy, removeConfirmText, serviceStatus, shownValues, slotsText, testService, testText, usedByText, usersText,
} from '@/lib/services'
import type { PlatformVarView, ServiceField, ServiceKind, ServiceTestResult, ServiceValue, ServiceView } from '@/lib/types'
import { ExpandRow, type RowTone } from '@/components/expand-row'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/controls'
import { useToast } from '@/components/ui/toast'
import { ApiErrorBox, FieldInput, ItemName, KindName, NOT_SUPPORTED, RegisterServiceDialog, useKinds } from './service-form'

// kindExamples：说明里举的例子，取目录里定义的服务类型的中文名（最多两个）；没有时不举例。
function kindExamples(kinds: ServiceKind[] | null | undefined): string {
  const names = (kinds || []).map((k) => (k.label || '').trim()).filter(Boolean).slice(0, 2)
  return names.length ? '（例如' + names.join('、') + '）' : ''
}

const PLATFORM_ROW = 'platform'
const rowKey = (name: string) => 'svc:' + name
const domId = (name: string) => name.replace(/[^A-Za-z0-9_-]/g, '-')

export function Services() {
  const toast = useToast()
  const list = usePoll((s) => listServices(s), 0)
  const pv = usePoll((s) => listPlatformVars(s), 0)
  const kinds = useKinds()
  const [adding, setAdding] = React.useState(false)
  const [removed, setRemoved] = React.useState<{ name: string; notes: string[] } | null>(null)
  const [open, setOpen] = React.useState('')
  const toggle = (k: string) => setOpen((o) => (o === k ? '' : k))
  const unsupported = !list.loading && !list.error && list.data === null
  const items = list.data?.services
  const unreadable = list.data?.unreadable || []
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-3 py-2.5">
        <div className="min-w-0">
          <div className="text-sm">这台机器上的服务</div>
          <div className="text-2xs text-muted-foreground" data-testid="services-intro">应用之间互相借用的东西。装应用时需要哪种，自动接上。</div>
        </div>
        {!unsupported && <Button size="sm" variant="outline" onClick={() => setAdding(true)} disabled={list.loading && !list.data}><Plus className="size-3.5" />登记一条</Button>}
      </div>
      {list.loading && !list.data && <div className="flex items-center gap-2 py-2.5 text-xs text-muted-foreground"><Spinner />正在读取…</div>}
      {list.error && <div className="py-2.5"><ApiErrorBox err={list.error} /></div>}
      {unsupported && (
        <div className="py-2.5 text-xs text-muted-foreground" data-testid="services-unsupported">
          {NOT_SUPPORTED}。<a href="#prefs/update" className="underline">怎么升级</a>
        </div>
      )}
      {unreadable.length > 0 && (
        <div className="flex items-start gap-1.5 py-2.5 text-xs text-warning" role="alert" data-testid="services-unreadable">
          <AlertTriangle className="mt-px size-3.5 shrink-0" />
          <span className="break-words">这几条服务的内容读不出来：{unreadable.join('、')}。用到它们的应用可能接不上；在服务器上运行 acornfox services list 看详情。</span>
        </div>
      )}
      {removed && (
        <div className="flex items-start gap-2 py-2.5 text-xs" role="status" data-testid="service-removed-notes">
          <div className="min-w-0 flex-1">
            <div className="font-medium">已删除“{removed.name}”</div>
            <ul className="mt-0.5 flex flex-col gap-0.5 text-muted-foreground">{removed.notes.map((n, i) => <li key={i} className="break-words">{n}</li>)}</ul>
          </div>
          <Button size="icon-sm" variant="ghost" className="shrink-0" onClick={() => setRemoved(null)} aria-label="知道了" title="知道了"><X className="size-3.5" /></Button>
        </div>
      )}
      {items && items.length === 0 && unreadable.length === 0 && (
        <div className="py-3 text-xs text-muted-foreground" data-testid="services-empty">
          还没有。装一个能提供服务的应用，它的服务会自己出现在这里；你手里已有的{kindExamples(kinds.data)}也可以登记进来。
        </div>
      )}
      {(items || []).map((s) => (
        <ServiceRow key={s.name} s={s} kind={kinds.data?.find((k) => k.kind === s.kind)} kindsKnown={!!kinds.data}
          open={open === rowKey(s.name)} onToggle={() => toggle(rowKey(s.name))} onChanged={list.refresh}
          onRemoved={(notes) => setRemoved(notes.length ? { name: s.name, notes } : null)} />
      ))}
      {pv.data && <PlatformRow vars={pv.data} open={open === PLATFORM_ROW} onToggle={() => toggle(PLATFORM_ROW)} />}
      {pv.error && !pv.data && (
        <div className="-mx-4" data-testid="platform-auto">
          <ExpandRow id="svc-platform" wide name="平台自动填的" tone="danger" summary="读不出来" summaryTestId="platform-auto-summary" open={open === PLATFORM_ROW} onToggle={() => toggle(PLATFORM_ROW)}>
            <ApiErrorBox err={pv.error} />
          </ExpandRow>
        </div>
      )}
      <RegisterServiceDialog open={adding} onClose={() => setAdding(false)} onAdded={(s) => { setAdding(false); toast('已登记', s.name); list.refresh() }} />
    </>
  )
}

// KV：展开后的一项：左边名字，右边值。
function KV({ k, children, mono, className, testId }: { k: React.ReactNode; children: React.ReactNode; mono?: boolean; className?: string; testId?: string }) {
  return (
    <div className={cn('flex items-baseline gap-3 py-1', className)} data-testid={testId}>
      <dt className="w-24 shrink-0 text-muted-foreground">{k}</dt>
      <dd className={cn('min-w-0 flex-1 break-all', mono && 'mono')}>{children}</dd>
    </div>
  )
}

// PlatformRow：最后固定的一行“平台自动填的”。
function PlatformRow({ vars, open, onToggle }: { vars: PlatformVarView[]; open: boolean; onToggle: () => void }) {
  const shown = platformShown(vars)
  return (
    <div className="-mx-4" data-testid="platform-auto">
      <ExpandRow id="svc-platform" wide name="平台自动填的" summary={platformSummary(shown.length)} summaryTestId="platform-auto-summary" open={open} onToggle={onToggle}>
        {shown.length > 0 && (
          <dl className="flex flex-col text-xs" data-testid="platform-auto-values">
            {shown.map((v) => <KV key={v.name} k={platformLabel(v)} mono>{v.value}</KV>)}
          </dl>
        )}
        <p className="mt-1.5 text-2xs text-muted-foreground">想给某个应用用别的值，装它的时候可以改。</p>
      </ExpandRow>
    </div>
  )
}

function ServiceRow({ s, kind, kindsKnown, open, onToggle, onChanged, onRemoved }: {
  s: ServiceView; kind?: ServiceKind; kindsKnown: boolean; open: boolean; onToggle: () => void; onChanged: () => Promise<void>; onRemoved: (notes: string[]) => void
}) {
  const st = serviceStatus(s)
  const ok = s.status === 'ok'
  const tone: RowTone = ok ? 'success' : st.tone === 'muted' ? '' : st.tone
  return (
    <div className="-mx-4" data-testid="service" data-name={s.name}>
      <ExpandRow id={'svc-body-' + domId(s.name)} wide tone={tone} open={open} onToggle={onToggle}
        name={<KindName label={s.kind_label || kind?.label} kind={s.kind} />}
        summaryTestId="service-summary"
        summary={<>{!ok && <><span data-testid="service-status" title={st.detail}>{st.text}</span>{' · '}</>}{providerText(s)} · {usedByText(s)}</>}>
        <ServiceBody s={s} kind={kind} kindsKnown={kindsKnown} onChanged={onChanged} onRemoved={onRemoved} />
      </ExpandRow>
    </div>
  )
}

// fieldFor：改一项时用的定义。必填照服务类型的定义（必填项不能留空保存）；不带默认值，免得没改的项被默认值盖掉。
function fieldFor(v: ServiceValue, kind?: ServiceKind): ServiceField {
  const f = kind?.fields.find((x) => x.key === v.key)
  return f ? { ...f, default: null } : { key: v.key, label: v.label || '', type: 'string', secret: v.secret }
}

function ServiceBody({ s, kind, kindsKnown, onChanged, onRemoved }: { s: ServiceView; kind?: ServiceKind; kindsKnown: boolean; onChanged: () => Promise<void>; onRemoved: (notes: string[]) => void }) {
  const toast = useToast()
  const [editing, setEditing] = React.useState('')
  const [notes, setNotes] = React.useState<string[]>([])
  const [testing, setTesting] = React.useState(false)
  const [tested, setTested] = React.useState<ServiceTestResult | null>(null)
  const [testErr, setTestErr] = React.useState<ApiError | null>(null)
  const [confirm, setConfirm] = React.useState(false)
  const [deleting, setDeleting] = React.useState(false)
  const [delErr, setDelErr] = React.useState<{ err: ApiError; apps: string[] } | null>(null)
  const canTest = kindsKnown ? !!kind?.has_test : true
  const values = shownValues(s)
  const issued = issuedItems(s).map((k) => ({ key: k, label: kind?.fields.find((f) => f.key === k)?.label }))
  const result = tested ? testText(tested) : null
  const slots = slotsText(s)

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
      setDelErr({ err: ae, apps: refused ? refusedBy(ae, s.used_by) : [] })
      setConfirm(false)
    } finally {
      setDeleting(false)
    }
  }
  return (
    <div className="flex flex-col gap-2">
      {s.status === 'not_ready' && (
        <div className="text-xs text-warning" data-testid="service-not-ready">
          <p>还不能用：{s.ready_say || '提供它的应用的就绪检查没通过'}</p>
          {s.ready_detail && <p className="mt-0.5 text-2xs text-muted-foreground">没过的那一步：{s.ready_detail}</p>}
          <p className="mt-0.5 text-2xs text-muted-foreground">不挡安装：别的应用照样能接上它，用到它的功能要等它好了才能用。</p>
        </div>
      )}
      <dl className="flex flex-col text-xs" data-testid="service-values">
        {values.map((v) => (
          <div key={v.key} className="py-1">
            <div className="flex items-center gap-3">
              <dt className="w-24 shrink-0 text-muted-foreground"><ItemName label={v.label} item={v.key} /></dt>
              <dd className={cn('min-w-0 flex-1 break-all', v.set ? 'mono' : 'text-muted-foreground')}>{v.set ? v.value : '没填'}</dd>
              {editing !== v.key && (s.source === 'user' || !!v.ask) && (
                <Button size="sm" variant={v.ask && !v.set ? 'outline' : 'ghost'} className="h-6 shrink-0 px-2 text-2xs" onClick={() => setEditing(v.key)} aria-label={(v.ask && !v.set ? '填' : '改') + itemLabel(v.label)}>
                  {v.ask && !v.set ? '填这一项' : '改'}
                </Button>
              )}
            </div>
            {v.ask && !v.set && <p className="mt-1 text-2xs text-warning" data-testid="service-ask">{v.ask}</p>}
            {editing === v.key && (
              <EditValue s={s} current={v} field={fieldFor(v, kind)} onDone={async (n) => { setEditing(''); setNotes(n); toast('已保存', s.name + ' · ' + itemLabel(v.label)); await onChanged() }} onCancel={() => setEditing('')} />
            )}
          </div>
        ))}
        {issued.length > 0 && (
          <div className="flex items-baseline gap-3 py-1" data-testid="service-issued">
            <dt className="w-24 shrink-0 text-muted-foreground">{issued.map((x, i) => <React.Fragment key={x.key}>{i > 0 && '、'}<ItemName label={x.label} item={x.key} /></React.Fragment>)}</dt>
            <dd className="min-w-0 flex-1 text-muted-foreground">这几项按每个使用它的应用单独发，不在这里填</dd>
          </div>
        )}
        {values.length === 0 && issued.length === 0 && <div className="py-1 text-muted-foreground">没有可显示的项</div>}
      </dl>
      <dl className="flex flex-col text-xs">
        {s.source === 'app' && s.name !== s.app && <KV k="名字" testId="service-name">{s.name}</KV>}
        <KV k="谁在用" testId="service-users">{usersText(s)}</KV>
        {slots.text && <KV k="还能不能接" testId="service-slots" className={slots.full ? 'text-warning' : ''}>{slots.text}</KV>}
      </dl>
      {notes.length > 0 && <ul className="flex flex-col gap-0.5 text-2xs text-muted-foreground" data-testid="service-notes">{notes.map((n, i) => <li key={i}>{n}</li>)}</ul>}
      {result && (
        <div className={cn('flex items-start gap-1.5 text-xs', result.kind === 'ok' ? 'text-success' : result.kind === 'failed' ? 'text-danger' : 'text-muted-foreground')}
          role="status" data-testid="service-test" data-result={result.kind}>
          {result.kind === 'ok' ? <CircleCheck className="mt-px size-3.5 shrink-0" /> : result.kind === 'failed' ? <CircleX className="mt-px size-3.5 shrink-0" /> : <CircleMinus className="mt-px size-3.5 shrink-0" />}
          <span className="break-words">{result.text}</span>
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
        {canTest && <Button size="sm" variant="outline" onClick={runTest} disabled={testing}>{testing && <Spinner />}试一下</Button>}
        {!canRemove(s) ? (
          <span className="text-2xs text-muted-foreground">这条由应用 {s.app} 提供，删掉那个应用它才会消失</span>
        ) : !confirm ? (
          <Button size="sm" variant="ghost" className="text-danger" onClick={() => setConfirm(true)}>删除</Button>
        ) : (
          <span className="flex flex-wrap items-center gap-2 text-xs">
            <span data-testid="service-remove-confirm">{removeConfirmText(s)}</span>
            <Button size="sm" variant="destructive" onClick={remove} disabled={deleting}>{deleting && <Spinner />}确认删除</Button>
            <Button size="sm" variant="ghost" onClick={() => setConfirm(false)}>取消</Button>
          </span>
        )}
      </div>
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
          <Button size="sm" variant="destructive" onClick={() => save(true)} disabled={busy}>{busy && <Spinner />}确认清掉</Button>
          <Button size="sm" variant="ghost" onClick={() => setConfirmClear(false)}>不清</Button>
        </div>
      ) : (
        <div className="flex justify-end gap-2">
          <Button size="sm" variant="ghost" onClick={onCancel}>取消</Button>
          <Button size="sm" onClick={() => save()} disabled={busy}>{busy && <Spinner />}保存</Button>
        </div>
      )}
    </div>
  )
}
