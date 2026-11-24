// “添加我已有的”（以前叫“登记一条”，评估 N5）：先问“你手里有哪一种？”，每种写服务类型自带的中文名和说明（目录里定义的，页面不为某一种写专门的说法），
// 再按那种类型的定义（GET /v1/services/kinds 的 fields）生成表单，必填、保密、枚举、默认值、提示都照定义来，不为某种类型写专门的表单。
// 提交是 POST /v1/services。同样的事用命令行怎么做收在默认收起的“用命令行”里。
// 设置里的“服务”分区和安装弹窗里的当场添加共用这里。错误框只写人话，故障编号放进“复制给 AI 工作台”。
import * as React from 'react'
import { ChevronRight } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { ApiError } from '@/lib/api'
import { usePoll } from '@/lib/hooks'
import {
  UNNAMED_ITEM, UNNAMED_KIND, addCommand, addService, buildServiceRequest, checkServiceForm, fieldControl, fieldOptions, listKinds, newServiceValues,
  type FieldProblem, type ServiceFormValues,
} from '@/lib/services'
import type { ServiceField, ServiceKind, ServiceView } from '@/lib/types'
import { AskAI, CliFold } from '@/components/fold'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/controls'
import { Loading } from '@/components/ui/skeleton'
import { SkForm } from '@/components/skeletons'
import { CopyLine } from '@/components/ui/copy'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { Input, Label } from '@/components/ui/input'

export const NOT_SUPPORTED = '这个版本还没有这项功能，升级 AcornFox 以后才有'

export function useKinds() {
  return usePoll((s) => listKinds(s), 0)
}

// Named：服务类型或项的中文名。没有中文名时说“未命名”，程序用的原名放在旁边的小字里，只供排查。
function Named({ label, raw, unnamed }: { label?: string | null; raw: string; unnamed: string }) {
  const t = typeof label === 'string' ? label.trim() : ''
  if (t) return <>{t}</>
  return (
    <span data-testid="unnamed">
      {unnamed}
      {raw && <span className="mono ml-1 text-2xs font-normal text-muted-foreground" title="程序里用的名字，排查时用">{raw}</span>}
    </span>
  )
}

export function KindName({ label, kind }: { label?: string | null; kind: string }) {
  return <Named label={label} raw={kind} unnamed={UNNAMED_KIND} />
}

export function ItemName({ label, item }: { label?: string | null; item: string }) {
  return <Named label={label} raw={item} unnamed={UNNAMED_ITEM} />
}

const idOf = (prefix: string, key: string) => prefix + key.replace(/[^A-Za-z0-9_-]/g, '-')

// FieldInput：服务类型里的一项。保密项用密码框，枚举和是否用下拉。
export function FieldInput({ field: f, value, problem, onChange, idPrefix, placeholder }: {
  field: ServiceField
  value: string
  problem?: string
  onChange: (v: string) => void
  idPrefix: string
  placeholder?: string
}) {
  const id = idOf(idPrefix, f.key)
  const control = fieldControl(f)
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id}><ItemName label={f.label} item={f.key} />{f.required ? ' *' : ''}</Label>
      {control === 'select' ? (
        <Select id={id} value={value} onChange={(e) => onChange(e.target.value)} options={fieldOptions(f)} aria-invalid={!!problem} />
      ) : (
        <Input
          id={id}
          className={f.type === 'url' || f.type === 'int' ? 'mono' : undefined}
          type={control === 'password' ? 'password' : 'text'}
          inputMode={control === 'number' ? 'numeric' : undefined}
          autoComplete="off"
          value={value}
          placeholder={placeholder}
          onChange={(e) => onChange(e.target.value)}
          aria-invalid={!!problem}
        />
      )}
      {problem ? <span className="text-2xs text-danger">{problem}</span> : f.hint && <span className="text-2xs text-muted-foreground">{f.hint}</span>}
    </div>
  )
}

// ApiErrorBox：出错时的一句话。故障编号不单独显示，带编号的错误旁边给“复制给 AI 工作台”，编号在复制的文字里。
export function ApiErrorBox({ err, children }: { err: ApiError | Error; children?: React.ReactNode }) {
  const ae = err as ApiError
  const code = ae.code && /^[A-Z]+-/.test(ae.code) ? ae.code : ''
  return (
    <div className="rounded-md border border-danger/40 bg-danger/5 px-3 py-2 text-xs" role="alert">
      <div className="flex flex-wrap items-start gap-2">
        <div className="min-w-0 flex-1">
          <div className="font-medium text-danger">{err.message}</div>
          {ae.hint && <div className="mt-0.5 text-muted-foreground">{ae.hint}</div>}
        </div>
        {code && <AskAI variant="ghost" text={`AcornFox 控制台在服务这一块报错：${code} ${err.message}${ae.hint ? '（' + ae.hint + '）' : ''}。用 acornfox skill 帮我查。`} />}
      </div>
      {children}
    </div>
  )
}

// ServiceForm：添加一种服务的表单（名字 + 那种类型的各项）。
export function ServiceForm({ kind: k, onAdded, onCancel, idPrefix = 'svc-' }: {
  kind: ServiceKind
  onAdded: (s: ServiceView) => void
  onCancel?: () => void
  idPrefix?: string
}) {
  const [name, setName] = React.useState('')
  const [values, setValues] = React.useState<ServiceFormValues>(() => newServiceValues(k))
  const [problems, setProblems] = React.useState<FieldProblem[]>([])
  const [err, setErr] = React.useState<ApiError | null>(null)
  const [busy, setBusy] = React.useState(false)
  const problemFor = (key: string) => problems.find((p) => p.key === key)?.message
  const submit = async () => {
    const ps = checkServiceForm(k, name, values)
    setProblems(ps)
    setErr(null)
    if (ps.length) return
    setBusy(true)
    try {
      const s = await addService(buildServiceRequest(k, name, values))
      onAdded(s && s.name ? s : { name: name.trim(), kind: k.kind, source: 'user', status: 'ok' })
    } catch (e) {
      setErr(e as ApiError)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="flex flex-col gap-3" data-testid="service-form">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={idPrefix + 'name'}>名字 *</Label>
        <Input id={idPrefix + 'name'} autoComplete="off" value={name} placeholder="自己认得出就行，不能有空格" onChange={(e) => setName(e.target.value)} aria-invalid={!!problemFor('name')} />
        {problemFor('name') ? <span className="text-2xs text-danger">{problemFor('name')}</span> : <span className="text-2xs text-muted-foreground">装应用时按这个名字选它</span>}
      </div>
      {(k.fields || []).map((f) => (
        <FieldInput key={f.key} field={f} idPrefix={idPrefix} value={values[f.key] || ''} problem={problemFor(f.key)} onChange={(v) => setValues((x) => ({ ...x, [f.key]: v }))} />
      ))}
      {(k.fields || []).some((f) => f.secret) && <p className="-mt-1 text-2xs text-muted-foreground">添加以后，密码这类只显示后 4 位。</p>}
      <CliFold>
        <CopyLine text={addCommand(k, name, values)} />
        {(k.fields || []).some((f) => f.secret) && <p className="text-2xs text-muted-foreground">命令行不写密码这类的值，运行后会让你输入。</p>}
      </CliFold>
      {err && <ApiErrorBox err={err} />}
      <div className="flex justify-end gap-2">
        {onCancel && <Button variant="outline" onClick={onCancel}>取消</Button>}
        <Button onClick={submit} loading={busy}>添加</Button>
      </div>
    </div>
  )
}

// KindPicker：“你手里有哪一种？”每种写它自带的中文名，下面带它自己的说明。
function KindPicker({ kinds, onPick }: { kinds: ServiceKind[]; onPick: (k: ServiceKind) => void }) {
  if (kinds.length === 0) return <p className="text-xs text-muted-foreground">现在还没有能添加的服务。</p>
  return (
    <ul className="flex flex-col gap-1.5" aria-label="你手里有哪一种">
      {kinds.map((k) => (
        <li key={k.kind}>
          <button type="button" onClick={() => onPick(k)}
            className="flex w-full items-center gap-3 rounded-md border px-3 py-2 text-left hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
            <span className="min-w-0 flex-1">
              <span className="block text-sm font-medium"><KindName label={k.label} kind={k.kind} /></span>
              {k.summary && <span className="block text-2xs text-muted-foreground">{k.summary}</span>}
            </span>
            <ChevronRight className="size-4 shrink-0 text-muted-foreground" />
          </button>
        </li>
      ))}
    </ul>
  )
}

const TITLE_ID = 'svc-add-title'

// RegisterServiceDialog：设置里的“添加我已有的”。先问手里有哪一种，再填。
export function RegisterServiceDialog({ open, onClose, onAdded }: { open: boolean; onClose: () => void; onAdded: (s: ServiceView) => void }) {
  return (
    <Dialog open={open} onClose={onClose} labelledBy={TITLE_ID}>
      {open && <RegisterBody onClose={onClose} onAdded={onAdded} />}
    </Dialog>
  )
}

function RegisterBody({ onClose, onAdded }: { onClose: () => void; onAdded: (s: ServiceView) => void }) {
  const kinds = useKinds()
  const [picked, setPicked] = React.useState<ServiceKind | null>(null)
  return (
    <>
      <DialogHead id={TITLE_ID} title={picked ? <>添加：<KindName label={picked.label} kind={picked.kind} /></> : '你手里有哪一种？'} />
      <Loading when={kinds.loading && !kinds.data} skeleton={<SkForm n={2} />}>{null}</Loading>
      {kinds.error && <ApiErrorBox err={kinds.error} />}
      {!kinds.loading && !kinds.error && kinds.data === null && <p className="text-sm text-muted-foreground" data-testid="services-unsupported">{NOT_SUPPORTED}</p>}
      {kinds.data && !picked && <KindPicker kinds={kinds.data} onPick={setPicked} />}
      {picked && (
        <>
          <Button variant="link" size="sm" className={cn('-mt-2 mb-2 h-auto p-0 text-2xs')} onClick={() => setPicked(null)}>换一种</Button>
          <ServiceForm key={picked.kind} kind={picked} onAdded={onAdded} onCancel={onClose} />
        </>
      )}
    </>
  )
}

// InlineRegister：安装弹窗里当场添加一条指定类型的服务。
export function InlineRegister({ kind, onAdded, onCancel, idPrefix }: { kind: string; onAdded: (s: ServiceView) => void; onCancel: () => void; idPrefix: string }) {
  const kinds = useKinds()
  const k = kinds.data?.find((x) => x.kind === kind)
  return (
    <div className="mt-2 rounded-md border bg-muted/40 p-2.5" data-testid="inline-register">
      <Loading when={kinds.loading && !kinds.data} skeleton={<SkForm n={2} />}>{null}</Loading>
      {kinds.error && <ApiErrorBox err={kinds.error} />}
      {!kinds.loading && !kinds.error && !k && (
        <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
          <span>{kinds.data === null ? NOT_SUPPORTED : '这种服务现在还不能在页面上添加'}</span>
          <Button size="sm" variant="ghost" onClick={onCancel}>收起</Button>
        </div>
      )}
      {k && <ServiceForm kind={k} onAdded={onAdded} onCancel={onCancel} idPrefix={idPrefix} />}
    </div>
  )
}
