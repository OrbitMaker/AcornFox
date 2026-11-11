// 安装弹窗：填写 → 提交安装任务 → 按任务步骤实时显示进度 → 成功给首次登录信息，失败给故障编号和建议。
// 接口：GET /v1/catalog/apps/{id}（表单）、POST /v1/catalog/install（202 返回任务）、
//       GET /v1/catalog/install/{task}（每秒轮询）、GET /v1/apps/{app}/first-login?reveal=true（点了才显示密码）。
// 要更新版本的 AcornFox 才能装的（needs_platform 有值）：说明要升到哪个版本、给升级命令，不能提交；
// 提交后才被拒绝或失败的：服务端的说明原样显示；错误里带 needs_platform 的，下面说“要先升级到 x.y.z”并给升级命令。
// 表单要读的应用详情读不了、原因是版本不够时（409，带 needs_platform）也一样。
import { useEffect, useMemo, useRef, useState } from 'react'
import { AlertTriangle, CircleCheck, Copy, ExternalLink, Eye, EyeOff, KeyRound } from 'lucide-react'
import { cn, safeHref } from '@/lib/utils'
import { api, ApiError } from '@/lib/api'
import {
  PUBLIC, appsIn, findApp, sourceId,
  appMemory, buildRequest, checkForm, cliCommand, defaultModel, faultForAI, freeMB, groupSteps, memoryCheck, newInstallForm, recipeMemory,
  type FormProblem, type InstallForm, type LLMField, type LLMSource, type PlainField, type PlatformField,
} from '@/lib/catalog'
import { copyText } from '@/lib/clipboard'
import { faultNeedsPlatform, installNeedsPlatform } from '@/lib/upgrade'
import { appHref, go } from '@/lib/router'
import { gatewayTokenText, isLoopbackURL, loginAddress, openableAddress } from '@/lib/appstatus'
import type { AppDetail, AppView, FirstLogin, OpenHint, Recipe, StepProgress, TaskView } from '@/lib/types'
import { useConsole, type InstallTarget } from '@/state'
import { StepDot } from '@/components/bits'
import { Button } from '@/components/ui/button'
import { Select, Spinner, Tabs } from '@/components/ui/controls'
import { CopyButton, CopyLine } from '@/components/ui/copy'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { Input, Label } from '@/components/ui/input'
import { useToast } from '@/components/ui/toast'
import { OpenHintBlock } from '../apps/open-hint'
import { UpgradeFirst, UpgradeNeeded } from './upgrade-note'

const TITLE_ID = 'install-title'

export function InstallDialog({ target, onClose }: { target: InstallTarget | null; onClose: () => void }) {
  return (
    <Dialog open={!!target} onClose={onClose} labelledBy={TITLE_ID} className="sm:max-w-lg">
      {target && <InstallBody key={target.kind + (target.catalog || '') + target.id + (target.taskId || '')} target={target} onClose={onClose} />}
    </Dialog>
  )
}

function InstallBody({ target, onClose }: { target: InstallTarget; onClose: () => void }) {
  const { catalog, refreshApps, refreshCatalog, refreshTasks, refreshResources } = useConsole()
  const [details, setDetails] = useState<AppDetail[] | null>(null)
  const [loadErr, setLoadErr] = useState<Error | null>(null)
  const [taskId, setTaskId] = useState(target.taskId || '')
  const [task, setTask] = useState<TaskView | null>(null)
  const [lost, setLost] = useState(false)
  const src = target.catalog || PUBLIC
  const recipe: Recipe | null = target.kind === 'recipe' ? catalog?.recipes.find((r) => r.id === target.id && sourceId(r) === src) || null : null
  const title = target.kind === 'recipe' ? recipe?.title || target.id : details?.[0]?.name || findApp(catalog, target.id, src)?.name || target.id

  // 读表单需要的应用详情（配方就读其中每个应用）。
  useEffect(() => {
    if (taskId) return
    let alive = true
    const ids = target.kind === 'app' ? [target.id] : (recipe?.apps || []).map((a) => a.app)
    if (ids.length === 0) {
      setLoadErr(new Error('目录里没有这个配方'))
      return
    }
    const q = src !== PUBLIC ? '?catalog=' + encodeURIComponent(src) : ''
    Promise.all(ids.map((id) => api.get<AppDetail>('/v1/catalog/apps/' + encodeURIComponent(id) + q)))
      .then((ds) => { if (alive) setDetails(ds) })
      .catch((e) => { if (alive) setLoadErr(e) })
    return () => { alive = false }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target.kind, target.id, src, taskId])

  // 安装任务每秒查一次，结束后刷新应用列表和目录。
  // 任务不存在（404，例如服务重启后任务记录没了）就停下，告诉用户去哪里看；其他错误隔 2 秒再试。
  useEffect(() => {
    if (!taskId) return
    setLost(false)
    let alive = true
    let timer = 0
    const tick = async () => {
      try {
        const t = await api.get<TaskView>('/v1/catalog/install/' + encodeURIComponent(taskId))
        if (!alive) return
        setTask(t)
        if (t.status === 'running') timer = window.setTimeout(tick, 1000)
        else {
          refreshApps(); refreshCatalog(); refreshTasks(); refreshResources()
        }
      } catch (e) {
        if (!alive) return
        if ((e as ApiError).status === 401) return
        if ((e as ApiError).status === 404) {
          setLost(true)
          refreshApps(); refreshTasks()
          return
        }
        timer = window.setTimeout(tick, 2000)
      }
    }
    tick()
    return () => { alive = false; window.clearTimeout(timer) }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskId])

  if (taskId && lost) {
    return (
      <>
        <DialogHead id={TITLE_ID} title={'安装 ' + title} />
        <div className="flex flex-col gap-2 text-sm" role="alert" data-testid="task-lost">
          <p>找不到这个安装任务。服务重启后，进行中的任务记录会丢失。</p>
          <p className="text-xs text-muted-foreground">
            去“我的应用”看应用有没有装好，或者在命令行运行 <span className="mono">acornfox apps</span> 查看。
          </p>
        </div>
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="outline" onClick={onClose}>关闭</Button>
          <Button onClick={() => { onClose(); go('#apps') }}>去我的应用</Button>
        </div>
      </>
    )
  }
  if (taskId) {
    return task ? <TaskProgress task={task} title={title} onClose={onClose} /> : (
      <>
        <DialogHead id={TITLE_ID} title={'正在安装 ' + title} />
        <div className="flex items-center gap-2 text-xs text-muted-foreground"><Spinner />正在读取进度…</div>
      </>
    )
  }
  if (loadErr) {
    return (
      <>
        <DialogHead id={TITLE_ID} title={(target.kind === 'app' ? '安装 ' : '') + title} />
        <p className="text-sm text-danger" role="alert">{loadErr.message}</p>
        {faultNeedsPlatform(loadErr) && <UpgradeFirst version={faultNeedsPlatform(loadErr)} className="mt-3" />}
      </>
    )
  }
  if (!details) {
    return (
      <>
        <DialogHead id={TITLE_ID} title={(target.kind === 'app' ? '安装 ' : '') + title} />
        <div className="flex items-center gap-2 text-xs text-muted-foreground"><Spinner />正在读取…</div>
      </>
    )
  }
  return <InstallFormView target={target} details={details} recipe={recipe} title={title} onClose={onClose} onStarted={setTaskId} />
}

// ---- 填写 ----

function InstallFormView({ target, details, recipe, title, onClose, onStarted }: {
  target: InstallTarget
  details: AppDetail[]
  recipe: Recipe | null
  title: string
  onClose: () => void
  onStarted: (taskId: string) => void
}) {
  const { catalog, resources, install, refreshTasks } = useConsole()
  const toast = useToast()
  const src = target.catalog || PUBLIC
  const [form, setForm] = useState<InstallForm>(() => newInstallForm(target.kind, target.id, details, recipe, src))
  const [problems, setProblems] = useState<FormProblem[]>([])
  const [err, setErr] = useState<ApiError | null>(null)
  const [busy, setBusy] = useState(false)
  const [confirmReuse, setConfirmReuse] = useState(false)
  const d0 = details[0]
  const mem = target.kind === 'app' ? appMemory(d0) : recipeMemory(recipe!, appsIn(catalog, src))
  const check = memoryCheck(mem.need, mem.min, freeMB(resources))
  const command = useMemo(() => cliCommand(form), [form])
  // 服务端给的是目录里当前版本的要求；选了别的版本时交给服务端判断。
  const needs = target.kind === 'recipe' || !form.version || form.version === form.defaultVersion ? installNeedsPlatform(target.kind, details, recipe) : ''
  const required = form.plain.filter((p) => p.param.required && !p.param.generated && (p.param.default === undefined || p.param.default === ''))
  const optional = form.plain.filter((p) => !required.includes(p))
  const isGateway = (a: { provides: string[] | null }) => (a.provides || []).includes('llm_gateway')
  const gatewayApp = appsIn(catalog, PUBLIC).find(isGateway) || catalog?.apps.find(isGateway)

  const setPlain = (key: string, value: string) => setForm((f) => ({ ...f, plain: f.plain.map((p) => (p.key === key ? { ...p, value } : p)) }))
  const setPlatform = (name: string, value: string) => setForm((f) => ({ ...f, platform: f.platform.map((p) => (p.view.name === name ? { ...p, value } : p)) }))
  const setLLM = (key: string, patch: Partial<LLMField>) => setForm((f) => ({ ...f, llm: f.llm.map((l) => (l.key === key ? { ...l, ...patch } : l)) }))
  const problemFor = (key: string) => problems.find((p) => p.key === key)?.message

  const submit = async (reuseData = false) => {
    const ps = checkForm(form)
    setProblems(ps)
    setErr(null)
    if (ps.length > 0) return
    setBusy(true)
    try {
      const req = buildRequest(form)
      if (reuseData) req.reuse_data = true
      const t = await api.post<TaskView>('/v1/catalog/install', req)
      refreshTasks()
      onStarted(t.id)
    } catch (e) {
      const ae = e as ApiError
      // 同一个应用已经在装：直接看那个任务的进度。
      if (ae.code === 'CAT-INST-04' && ae.taskId) {
        onStarted(ae.taskId)
        return
      }
      setErr(ae)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <DialogHead id={TITLE_ID} title={(target.kind === 'app' ? '安装 ' : '') + title} desc={target.kind === 'recipe' ? (recipe?.apps || []).map((a) => findApp(catalog, a.app, src)?.name || a.app).join(' · ') : d0.summary} />
      <div className="flex flex-col gap-3">
        {target.kind === 'app' && (
          <div className="grid grid-cols-2 gap-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="ins-ver">版本</Label>
              <Select id="ins-ver" value={form.version || form.defaultVersion} onChange={(e) => setForm((f) => ({ ...f, version: e.target.value }))} options={(d0.versions?.length ? d0.versions : [d0.version]).map((v) => [v, v])} />
            </div>
            <div className="flex flex-col gap-1.5">
              <span className="text-xs font-medium leading-none">端口</span>
              <div className="flex h-8 items-center text-xs text-muted-foreground">自动分配，被占用时往后挪</div>
            </div>
          </div>
        )}
        {form.llm.map((l) => (
          <LLMPicker
            key={l.key}
            field={l}
            multi={form.llm.length > 1}
            appName={details.find((x) => x.id === l.appId)?.name || l.appId}
            problemFor={problemFor}
            onChange={(patch) => setLLM(l.key, patch)}
            onPlatform={() => toast('平台 Key 即将开放', '现在请用自己的 Key 或本机 New API')}
            onInstallGateway={gatewayApp && gatewayApp.installed.length === 0 ? () => install({ kind: 'app', id: gatewayApp.id, catalog: sourceId(gatewayApp) }) : undefined}
            gatewayName={gatewayApp?.name}
            defaultModel={defaultModel(form)}
          />
        ))}
        {required.map((p) => <ParamInput key={p.key} p={p} problem={problemFor(p.key)} onChange={(v) => setPlain(p.key, v)} />)}
        {optional.length > 0 && (
          <details className="group rounded-md border px-2.5 py-1.5" open={optional.some((p) => !!problemFor(p.key))}>
            <summary className="cursor-pointer select-none text-xs font-medium text-muted-foreground">更多设置（可不填）</summary>
            <div className="mt-2 flex flex-col gap-2.5 pb-1">
              {optional.map((p) => <ParamInput key={p.key} p={p} problem={problemFor(p.key)} onChange={(v) => setPlain(p.key, v)} />)}
            </div>
          </details>
        )}
        {form.platform.length > 0 && (
          <details className="group rounded-md border px-2.5 py-1.5" open={form.platform.some((p) => !!problemFor('platform.' + p.view.name))} data-testid="platform-vars">
            <summary className="cursor-pointer select-none text-xs font-medium text-muted-foreground">平台自动填的 {form.platform.length} 项（一般不用改）</summary>
            <div className="mt-2 flex flex-col gap-2.5 pb-1">
              <p className="text-2xs text-muted-foreground">时区、下载源、默认模型这类底层设置，平台按这台机器自动填进应用，装好就能用。能改的只对这次安装生效。</p>
              {form.platform.map((p) => <PlatformInput key={p.view.name} p={p} problem={problemFor('platform.' + p.view.name)} onChange={(v) => setPlatform(p.view.name, v)} />)}
            </div>
          </details>
        )}
        <div className={cn('flex items-center gap-1.5 text-xs', check.tone === 'ok' ? 'text-muted-foreground' : 'text-warning')} data-testid="mem-check">
          {check.tone === 'ok' ? <CircleCheck className="size-3.5 text-success" /> : <AlertTriangle className="size-3.5" />}
          {check.text}
        </div>
        {needs && <UpgradeNeeded version={needs} />}
        <CopyLine text={command} />
        {err && (
          <div className="rounded-md border border-danger/40 bg-danger/5 px-3 py-2 text-xs" role="alert">
            <div className="font-medium text-danger">{err.code.startsWith('CAT-') ? err.code + ' · ' : ''}{err.message}</div>
            {err.hint && <div className="mt-0.5 text-muted-foreground">{err.hint}</div>}
            {faultNeedsPlatform(err) && <UpgradeFirst version={faultNeedsPlatform(err)} className="mt-2" />}
            {err.code === 'CAT-INST-24' && (
              <div className="mt-2 flex flex-wrap items-center gap-2" data-testid="reuse-data">
                {!confirmReuse ? (
                  <Button variant="outline" size="sm" onClick={() => setConfirmReuse(true)}>接上旧数据重装</Button>
                ) : (
                  <>
                    <span className="text-foreground">会接着用以前留下的数据，账号密码和以前一样；装失败也不会删这些数据。</span>
                    <Button size="sm" onClick={() => { setConfirmReuse(false); submit(true) }} disabled={busy}>确认接上</Button>
                    <Button variant="ghost" size="sm" onClick={() => setConfirmReuse(false)}>取消</Button>
                  </>
                )}
              </div>
            )}
          </div>
        )}
      </div>
      <div className="mt-5 flex justify-end gap-2">
        <Button variant="outline" onClick={onClose}>取消</Button>
        <Button onClick={() => submit()} disabled={busy || !!needs}>{busy && <Spinner />}{target.kind === 'recipe' ? '开始' : '安装'}</Button>
      </div>
    </>
  )
}

function ParamInput({ p, problem, onChange }: { p: PlainField; problem?: string; onChange: (v: string) => void }) {
  const id = 'prm-' + p.key.replace(/[^A-Za-z0-9_-]/g, '-')
  const def = p.param.default !== undefined && p.param.default !== null ? String(p.param.default) : ''
  const placeholder = p.param.generated ? '留空自动生成' : def ? '默认 ' + def : ''
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id}>{p.param.label || p.param.key}{p.param.required && !def && !p.param.generated ? ' *' : ''}</Label>
      {p.param.options && p.param.options.length > 0 ? (
        <Select id={id} value={p.value || def} onChange={(e) => onChange(e.target.value)} options={p.param.options.map((o) => [String(o), String(o)])} />
      ) : (
        <Input id={id} type={p.param.type === 'secret' ? 'password' : 'text'} autoComplete="off" value={p.value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} aria-invalid={!!problem} />
      )}
      {problem ? <span className="text-2xs text-danger">{problem}</span> : p.param.hint && <span className="text-2xs text-muted-foreground">{p.param.hint}</span>}
    </div>
  )
}

function PlatformInput({ p, problem, onChange }: { p: PlatformField; problem?: string; onChange: (v: string) => void }) {
  const id = 'plat-' + p.view.name.replace(/[^A-Za-z0-9_-]/g, '-')
  if (!p.view.user_can_set) {
    return (
      <div className="flex items-start gap-2 text-xs">
        <span className="w-24 shrink-0 text-muted-foreground">{p.view.label}</span>
        <span className="mono min-w-0 flex-1 break-all">{p.view.value || p.view.note || '—'}</span>
      </div>
    )
  }
  const changed = p.value.trim() !== p.base
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between">
        <Label htmlFor={id}>{p.view.label}</Label>
        {changed && <Button variant="link" size="sm" className="h-auto p-0 text-2xs" onClick={() => onChange(p.base)}>恢复</Button>}
      </div>
      <Input id={id} className="mono" autoComplete="off" value={p.value} onChange={(e) => onChange(e.target.value)} aria-invalid={!!problem} />
      {problem ? <span className="text-2xs text-danger">{problem}</span> : <span className="text-2xs text-muted-foreground">{changed ? '平台填的是 ' + (p.base || '空') : p.view.standalone}</span>}
    </div>
  )
}

function LLMPicker({ field: l, multi, appName, problemFor, onChange, onPlatform, onInstallGateway, gatewayName, defaultModel }: {
  field: LLMField
  multi: boolean
  appName: string
  problemFor: (k: string) => string | undefined
  onChange: (p: Partial<LLMField>) => void
  onPlatform: () => void
  onInstallGateway?: () => void
  gatewayName?: string
  defaultModel: string
}) {
  const label = (multi ? appName + ' · ' : '') + '模型'
  const modelInput = (placeholder: string) => (
    <Input id={'llm-model-' + l.appId} className="mono" autoComplete="off" placeholder={placeholder} value={l.model} onChange={(e) => onChange({ model: e.target.value })} aria-label="模型名" aria-invalid={!!problemFor(l.key + '.model')} />
  )
  if (l.fixed) {
    return (
      <div className="flex flex-col gap-1.5">
        <span className="text-xs font-medium">{label}</span>
        <div className="rounded-md border px-2.5 py-1.5 text-xs text-muted-foreground">用本机的 {l.fixed}，配方会自动连好，装完在 {l.fixed} 里加一次上游 Key</div>
        {modelInput('模型名，留空用 ' + defaultModel)}
      </div>
    )
  }
  const local = l.sources?.local_gateway
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-xs font-medium">{label}</span>
      <Tabs<LLMSource>
        value={l.source}
        label={label}
        onChange={(s) => {
          if (s === 'platform') onPlatform()
          onChange({ source: s })
        }}
        items={[['own', '自己的 Key'], ['local_gateway', '本机 New API'], ['platform', '平台 Key']]}
      />
      {l.source === 'own' && (
        <div className="flex flex-col gap-1.5">
          <Input className="mono" autoComplete="off" placeholder="接口地址，例如 https://api.deepseek.com/v1" aria-label="接口地址" value={l.baseURL} onChange={(e) => onChange({ baseURL: e.target.value })} aria-invalid={!!problemFor(l.key + '.base_url')} />
          <Input className="mono" type="password" autoComplete="off" placeholder="Key（DeepSeek / 通义 / OpenAI 兼容）" aria-label="Key" value={l.apiKey} onChange={(e) => onChange({ apiKey: e.target.value })} aria-invalid={!!problemFor(l.key + '.api_key')} />
          {modelInput('模型名，例如 deepseek-chat')}
        </div>
      )}
      {l.source === 'local_gateway' && (local?.available ? (
        <div className="flex flex-col gap-1.5">
          {local.apps.length > 1 && <Select aria-label="用哪个 New API" value={l.gatewayApp} onChange={(e) => onChange({ gatewayApp: e.target.value })} options={local.apps.map((a) => [a, a])} />}
          {modelInput('模型名，留空用 ' + defaultModel)}
          <span className="text-2xs text-muted-foreground">会在本机 {l.gatewayApp || 'New API'} 里建一把令牌给 {appName} 用</span>
        </div>
      ) : (
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border px-2.5 py-1.5 text-xs text-muted-foreground">
          <span>{local?.message || '本机还没有 New API'}</span>
          {onInstallGateway && <Button variant="link" size="sm" className="h-auto p-0" onClick={onInstallGateway}>先装 {gatewayName}</Button>}
        </div>
      ))}
      {l.source === 'platform' && (
        <div className="flex items-center justify-between rounded-md border px-2.5 py-1.5 text-xs text-muted-foreground">
          登录后可用
          <Button variant="link" size="sm" className="h-auto p-0" onClick={onPlatform}>登录</Button>
        </div>
      )}
      {['', '.base_url', '.api_key', '.model'].map((s) => problemFor(l.key + s)).filter(Boolean).map((m) => <span key={m} className="text-2xs text-danger">{m}</span>)}
      {l.hint && <span className="text-2xs text-muted-foreground">{l.hint}</span>}
    </div>
  )
}

// ---- 进度和结果 ----

function TaskProgress({ task: t, title, onClose }: { task: TaskView; title: string; onClose: () => void }) {
  const toast = useToast()
  const { apps, refreshApps } = useConsole()
  const groups = groupSteps(t)
  const main = [...(t.apps || [])].reverse().find((a) => !a.reused) || t.apps?.[t.apps.length - 1]
  // 只本机的应用（Linux 上地址是 127.0.0.1）不给“打开”链接，改成怎么打开的说明。
  // 应用列表里有这个应用时以它为准（任务结果里的地址不会跟着对外方式变）：有 open_hint 才是只本机，
  // 打开的地址也取它的；列表还没刷新到时才按任务结果的地址判断，先只给 acornfox open 的写法。
  const mainView = main ? apps?.find((a) => a.name === main.app) : undefined
  const openHint: OpenHint | undefined = mainView
    ? mainView.open_hint
    : main && isLoopbackURL(main.url)
      ? { has_login: false, can_lan: false, recommend: 'tunnel', command: 'acornfox open ' + main.app }
      : undefined
  const openURL = mainView ? openableAddress(mainView) : main && !isLoopbackURL(main.url) ? main.url || '' : ''
  const tokens = (t.apps || []).filter((a) => a.gateway_token)
  const heading = t.status === 'succeeded' ? title + ' 已装好' : t.status === 'failed' ? title + ' 没装好' : '正在安装 ' + title
  return (
    <>
      <DialogHead id={TITLE_ID} title={heading} desc={'任务 ' + t.id} />
      <div className="flex flex-col gap-3" data-testid="install-steps">
        {groups.map((g) => (
          <div key={g.app}>
            {groups.length > 1 && <div className="mb-1.5 text-2xs font-medium text-muted-foreground">{g.app}</div>}
            <ol className="flex flex-col gap-2">
              {g.steps.map((s) => (
                <li key={s.app + s.name} className="flex items-start gap-2.5 text-sm">
                  <span className="mt-0.5"><StepDot status={s.status} /></span>
                  <div className="min-w-0">
                    <span className={cn(s.status === 'pending' && 'text-muted-foreground', s.status === 'failed' && 'text-danger')}>{s.label}</span>
                    {s.message && <div className="break-words text-2xs text-muted-foreground">{s.message}</div>}
                    {s.status === 'running' && s.progress && <StepBar p={s.progress} />}
                  </div>
                </li>
              ))}
            </ol>
          </div>
        ))}
      </div>
      {t.status === 'running' && <p className="mt-3 text-2xs text-muted-foreground">可以关掉这个窗口，安装会在后台继续；我的应用里会显示“安装中”。</p>}
      {t.status === 'failed' && t.fault && (
        <div className="mt-4 rounded-md border border-danger/40 bg-danger/5 px-3 py-2.5 text-xs" role="alert">
          <div className="font-medium text-danger">{t.fault.code} · {t.fault.message}</div>
          {t.fault.hint && <div className="mt-1 text-muted-foreground">建议：{t.fault.hint}</div>}
          {t.keep_volumes_on_failure ? (
            <div className="mt-1 text-muted-foreground">数据卷按要求保留了：{t.apps.flatMap((a) => a.volumes || []).join('、') || '无'}</div>
          ) : (
            <div className="mt-1 text-muted-foreground">这次新建的应用和数据卷已经清理，{faultNeedsPlatform(t.fault) ? '升级后再装。' : '可以直接重装。'}</div>
          )}
          {faultNeedsPlatform(t.fault) && <UpgradeFirst version={faultNeedsPlatform(t.fault)} className="mt-2" />}
        </div>
      )}
      {/* 总是渲染：改成局域网后 openHint 没了，这一块在原位置显示完成态。 */}
      {t.status === 'succeeded' && main && <OpenHintBlock className="mt-4" name={main.app} hint={openHint} openURL={openURL} onChanged={refreshApps} />}
      {t.status === 'succeeded' && tokens.map((a) => (
        <p key={a.app} className="mt-3 text-xs text-muted-foreground" data-testid="token-note">{gatewayTokenText(a.app, a.gateway_token!.provider)}</p>
      ))}
      {t.status === 'succeeded' && <FirstLogins logins={t.first_login || []} apps={apps || undefined} />}
      {t.status === 'succeeded' && (t.user_steps?.length || 0) > 0 && (
        <div className="mt-3">
          <div className="mb-1 text-xs font-medium">还要你做的 {t.user_steps!.length} 步</div>
          <ol className="list-decimal space-y-1 rounded-md border p-2.5 pl-6 text-xs text-muted-foreground">
            {t.user_steps!.map((s, i) => <li key={i}>{s}</li>)}
          </ol>
        </div>
      )}
      <div className="mt-5 flex flex-wrap justify-end gap-2">
        {t.status === 'failed' && (
          <Button variant="outline" onClick={async () => { (await copyText(faultForAI(t))) ? toast('已复制', '粘贴给 AI 工作台') : toast('没能复制', '浏览器不允许', 'error') }}>
            <Copy className="size-3.5" />复制诊断给 AI
          </Button>
        )}
        {t.status !== 'succeeded' && <Button variant={t.status === 'failed' ? 'default' : 'outline'} onClick={onClose}>关闭</Button>}
        {t.status === 'succeeded' && main && (
          <>
            <Button variant="outline" onClick={() => { onClose(); go(appHref(main.app)) }}>管理</Button>
            {!openHint && safeHref(openURL) && (
              <a href={safeHref(openURL)} target="_blank" rel="noopener noreferrer" className="inline-flex h-8 items-center gap-1.5 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground hover:bg-primary/90">
                打开<ExternalLink className="size-3.5" />
              </a>
            )}
          </>
        )}
      </div>
    </>
  )
}

// StepBar：下载镜像的进度条。下载时按字节画；下完导入时（docker load 量不出进度）画一条来回走的条。
function StepBar({ p }: { p: StepProgress }) {
  const pct = p.phase === 'download' && p.total_bytes ? Math.min(100, Math.floor((p.done_bytes * 100) / p.total_bytes)) : null
  return (
    <div className="mt-1 h-1.5 w-56 max-w-full overflow-hidden rounded-full bg-muted" role="progressbar" aria-label="下载镜像进度"
      aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct ?? undefined} data-testid="step-progress">
      {pct !== null
        ? <div className="h-full rounded-full bg-primary transition-[width] duration-700" style={{ width: pct + '%' }} />
        : <div className="h-full w-1/3 animate-pulse rounded-full bg-primary/70" />}
    </div>
  )
}

// FirstLogins：首次登录信息。密码默认遮住，点“显示”才向服务端要明文；复制密码时也先要明文。
// 地址优先用应用列表里现在的地址（改了对外方式会跟着变），列表里还没有这个应用时用任务结果里的。
export function FirstLogins({ logins, apps }: { logins: FirstLogin[]; apps?: AppView[] }) {
  if (logins.length === 0) return null
  return (
    <div className="mt-4 flex flex-col gap-2">
      {logins.map((fl) => <FirstLoginCard key={fl.app} fl={fl} url={loginAddress(fl.url, apps?.find((a) => a.name === fl.app))} />)}
    </div>
  )
}

// url：要显示的地址（调用方按应用现在的样子算好）；不给时用 fl.url。
export function FirstLoginCard({ fl, title, url: urlProp }: { fl: FirstLogin; title?: string; url?: string }) {
  const toast = useToast()
  const [shown, setShown] = useState<FirstLogin | null>(null)
  const [busy, setBusy] = useState(false)
  const reveal = useRef<Promise<FirstLogin> | null>(null)
  const getReveal = () => {
    if (!reveal.current) {
      reveal.current = api.get<FirstLogin>('/v1/apps/' + encodeURIComponent(fl.app) + '/first-login?reveal=true')
      reveal.current.catch(() => { reveal.current = null })
    }
    return reveal.current
  }
  const toggle = async () => {
    if (shown) { setShown(null); return }
    setBusy(true)
    try { setShown(await getReveal()) } catch (e) { toast('没能读取密码', (e as Error).message, 'error') } finally { setBusy(false) }
  }
  const copySecret = async (i: number) => {
    try {
      const full = await getReveal()
      const v = full.fields[i]?.value || ''
      ;(await copyText(v)) ? toast('已复制') : toast('没能复制', '浏览器不允许', 'error')
    } catch (e) {
      toast('没能读取密码', (e as Error).message, 'error')
    }
  }
  const view = shown || fl
  const url = urlProp || fl.url
  const hasSecret = fl.fields.some((f) => f.secret)
  return (
    <div className="rounded-md border p-2.5" data-testid="first-login">
      <div className="mb-1.5 flex items-center gap-1.5 text-xs font-medium">
        <KeyRound className="size-3.5" />{title || '首次登录 · ' + fl.app}
        {hasSecret && (
          <button type="button" onClick={toggle} className="ml-auto inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-2xs font-normal text-muted-foreground hover:bg-accent hover:text-foreground">
            {busy ? <Spinner className="size-3" /> : shown ? <EyeOff className="size-3" /> : <Eye className="size-3" />}{shown ? '遮住' : '显示密码'}
          </button>
        )}
      </div>
      <div className="flex flex-col gap-1">
        {url && (
          <div className="flex items-center gap-2 text-xs" data-testid="first-login-url">
            <span className="w-16 shrink-0 text-muted-foreground">地址</span>
            {/* 127.0.0.1 只在服务器本机能用，不做成链接（怎么打开见上面的说明）。 */}
            {isLoopbackURL(url)
              ? <span className="mono min-w-0 flex-1 truncate text-muted-foreground" title="只在服务器本机能打开">{url}（只本机）</span>
              : <a href={safeHref(url)} target="_blank" rel="noopener noreferrer" className="mono min-w-0 flex-1 truncate underline-offset-4 hover:underline">{url}</a>}
            <CopyButton text={url} label="复制地址" />
          </div>
        )}
        {view.fields.map((f, i) => (
          <div key={f.label + i} className="flex items-center gap-2 text-xs">
            <span className="w-16 shrink-0 truncate text-muted-foreground" title={f.label}>{f.label}</span>
            <span className="mono min-w-0 flex-1 truncate" data-secret={f.secret ? 'true' : undefined}>{f.value}</span>
            {f.secret ? (
              <button type="button" onClick={() => copySecret(i)} className="inline-flex size-6 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground" aria-label={'复制' + f.label}>
                <Copy className="size-3.5" />
              </button>
            ) : (
              <CopyButton text={f.value} label={'复制' + f.label} />
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
