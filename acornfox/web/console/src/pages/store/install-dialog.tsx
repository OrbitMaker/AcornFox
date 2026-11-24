// 安装弹窗：填写 → 提交安装任务 → 按任务步骤实时显示进度 → 成功给“打开”、第一次登录用的信息和接下来要做的几步，
// 失败给一句原因、建议和“复制给 AI 工作台”（故障编号、任务号放在复制的那段文字里，页面上不单独显示）。
// 接口：GET /v1/catalog/apps/{id}（表单）、POST /v1/catalog/install（202 返回任务）、
//       GET /v1/catalog/install/{task}（每秒轮询）、GET /v1/apps/{app}/first-login?reveal=true（点了才显示密码）。
// 要更新版本的 AcornFox 才能装的（needs_platform 有值）：说明要升到哪个版本，不能提交；升级命令收在“用命令行”里。
// 提交后才被拒绝或失败的：服务端的说明原样显示；错误里带 needs_platform 的，下面说“要先升级到 x.y.z”。
// 表单要读的应用详情读不了、原因是版本不够时（409，带 needs_platform）也一样。
// 谁能打开（W3-5）：表单里一行三档，默认是服务端说这次装会得到的（应用详情的 exposure）；用户改了才随安装请求发 expose。
// 它要用到的服务：打开时、换版本或选了用哪一个时读一次安装前的计划（POST /v1/catalog/plan，只读），样子见 install-needs.tsx；
// 服务端没有这个接口时和以前一样。点“安装”时用当时表单的内容再读一次计划，按这次的结果决定能不能装；
// 这次读失败不拦安装，交给服务端检查。表单里没有为某种服务或某个应用写的专门选项。
//
// 样稿第 10 版（评估 L2～L7、X3、X4）：
// - 单个应用缺必须的服务：整屏换成一句话、“用‘某某’一起装好”（换成装那个解决方案）、“我自己有…，添加进来 ›”。
// - 能自动生成、用户又不用记的项（参数带 hidden，会话密钥这类）不问；“技术设置”只折一层，平台自动填的直接列在里面；
//   弹窗里不放命令行（命令在应用页的“技术信息”里）。
// - 装解决方案：一句“会装 2 个应用：MySQL 8.4、WordPress，装完自动接好。”按钮“开始安装”。
// - 安装中：顶上一条总进度（按步数；按字节的进度只在下载那一步里画），步骤名说人话（“下载应用”），
//   几个应用时按应用中心里的名字分组，任务号不显示。
// - 装好：主按钮“打开 WordPress”（解决方案打开的是能直接打开的那个应用），旁边“去管理”；下面“接下来：1 … 2 …”
//   取自介绍文档里“装好以后”那一节的列表（取不到时用清单里的 user_steps，再没有就不显示）；
//   第一次登录的信息标题写“某某的登录信息”。
import { useEffect, useRef, useState } from 'react'
import { AlertTriangle, CircleCheck, Copy, ExternalLink, Eye, EyeOff, KeyRound } from 'lucide-react'
import { cn, safeHref } from '@/lib/utils'
import { api, ApiError } from '@/lib/api'
import {
  PUBLIC, appsIn, findApp, sourceId,
  appMemory, buildRequest, checkForm, faultForAI, freeMB, groupSteps, memoryCheck, newInstallForm, plainStep, recipeMemory, taskProgress,
  type FormProblem, type InstallForm, type PlainField, type PlatformField,
} from '@/lib/catalog'
import { copyText } from '@/lib/clipboard'
import { DOC_HEADINGS, docSection } from '@/lib/doc'
import { faultNeedsPlatform, installNeedsPlatform } from '@/lib/upgrade'
import { appHref, go } from '@/lib/router'
import { isLoopbackURL, loginAddress, openableAddress } from '@/lib/appstatus'
import { WORDS } from '@/lib/display'
import { applyPlan, fetchPlan, planBlock } from '@/lib/services'
import type { AppDetail, AppView, FirstLogin, InstallPlan, OpenHint, Recipe, TaskView } from '@/lib/types'
import { useConsole, type InstallTarget } from '@/state'
import { StepDot } from '@/components/bits'
import { StepBar } from '@/components/step-bar'
import { ExposureChoice } from '@/components/exposure-choice'
import { AskAI } from '@/components/fold'
import { Button, buttonVariants } from '@/components/ui/button'
import { Select, Spinner } from '@/components/ui/controls'
import { Loading } from '@/components/ui/skeleton'
import { SkForm, SkLines } from '@/components/skeletons'
import { CopyButton } from '@/components/ui/copy'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { Input, Label } from '@/components/ui/input'
import { useToast } from '@/components/ui/toast'
import { OpenHintBlock } from '../apps/open-hint'
import { UpgradeFirst, UpgradeNeeded } from './upgrade-note'
import { fitText, solutionFor } from './shelf'
import { MatchedLine, MissingScreen, NeedsBlock, PlanBlocked, missingRows } from './install-needs'

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
      setLoadErr(new Error('没有这个解决方案'))
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
          <p>找不到这次安装的进度了（这台机器上的 AcornFox 重启过）。去“我的应用”看看装好没有。</p>
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
        <Loading when skeleton={<SkLines n={4} />}>{null}</Loading>
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
        <Loading when skeleton={<SkForm n={3} />}>{null}</Loading>
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
  const { catalog, resources, refreshTasks, install } = useConsole()
  const toast = useToast()
  const src = target.catalog || PUBLIC
  const [form, setForm] = useState<InstallForm>(() => newInstallForm(target.kind, target.id, details, recipe, src))
  const [problems, setProblems] = useState<FormProblem[]>([])
  const [err, setErr] = useState<ApiError | null>(null)
  const [busy, setBusy] = useState(false)
  const [confirmReuse, setConfirmReuse] = useState(false)
  // 安装前的计划：uses 是用户选的“某个需要接哪一个服务”；planSeq 变了重新读（例如当场添加了一个）。
  const [uses, setUses] = useState<Record<string, string>>({})
  const [planSeq, setPlanSeq] = useState(0)
  const [plan, setPlan] = useState<InstallPlan | null>(null)
  const [planErr, setPlanErr] = useState<ApiError | null>(null)
  // 每读一次计划加一；晚到的旧结果不覆盖新的（例如点安装时读的那次先回来）。
  const planGen = useRef(0)
  const usesKey = JSON.stringify(uses)
  useEffect(() => {
    let alive = true
    const gen = ++planGen.current
    fetchPlan(applyPlan(buildRequest(form), uses))
      .then((v) => { if (alive && gen === planGen.current) { setPlan(v); setPlanErr(null) } })
      .catch((e) => { if (alive && gen === planGen.current) setPlanErr(e as ApiError) })
    return () => { alive = false }
    // 填参数时不重读：计划只看版本和用哪一个服务。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [form.version, usesKey, planSeq])
  const d0 = details[0]
  const mem = target.kind === 'app' ? appMemory(d0) : recipeMemory(recipe!, appsIn(catalog, src))
  const check = memoryCheck(mem.need, mem.min, freeMB(resources))
  const blocked = planBlock(plan, uses)
  // 服务端给的是目录里当前版本的要求；选了别的版本时交给服务端判断。
  const needs = target.kind === 'recipe' || !form.version || form.version === form.defaultVersion ? installNeedsPlatform(target.kind, details, recipe) : ''
  // 能自动生成、用户又不用记的（hidden）不问（评估 L3）。
  const shown = form.plain.filter((p) => !p.param.hidden)
  const required = shown.filter((p) => p.param.required && !p.param.generated && (p.param.default === undefined || p.param.default === ''))
  const optional = shown.filter((p) => !required.includes(p))
  const registered = (name: string) => { toast('已添加', name); setPlanSeq((x) => x + 1) }

  const setPlain = (key: string, value: string) => setForm((f) => ({ ...f, plain: f.plain.map((p) => (p.key === key ? { ...p, value } : p)) }))
  const setPlatform = (name: string, value: string) => setForm((f) => ({ ...f, platform: f.platform.map((p) => (p.view.name === name ? { ...p, value } : p)) }))
  const problemFor = (key: string) => problems.find((p) => p.key === key)?.message

  const submit = async (reuseData = false) => {
    const ps = checkForm(form)
    setProblems(ps)
    setErr(null)
    if (ps.length > 0) return
    setBusy(true)
    try {
      const req = applyPlan(buildRequest(form), uses)
      if (reuseData) req.reuse_data = true
      // 用现在的表单再读一次计划，按这次的结果决定能不能装。读失败（不是旧服务端）不拦，交给服务端检查。
      const gen = ++planGen.current
      try {
        const pl = await fetchPlan(req)
        if (gen === planGen.current) { setPlan(pl); setPlanErr(null) }
        if (planBlock(pl, uses).length > 0) return
      } catch (pe) {
        if (gen === planGen.current) setPlanErr(pe as ApiError)
      }
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

  // 单个应用缺必须的服务：整屏换成“用解决方案一起装好 / 添加我已有的”（评估 L2）。
  const missing = target.kind === 'app' ? missingRows(plan) : []
  if (missing.length > 0) {
    const fix = solutionFor(catalog, target.id, missing.map((r) => r.need.kind), src)
    const purpose = (r: { need: { use: string } }) => (d0.uses || []).find((u) => u.name === r.need.use)?.use || ''
    return (
      <>
        <DialogHead id={TITLE_ID} title={'安装 ' + title} />
        <MissingScreen appName={title} rows={missing} purpose={purpose} fix={fix?.r.title || ''}
          onFix={() => fix && install({ kind: 'recipe', id: fix.r.id, catalog: src })} onRegistered={registered} onCancel={onClose} />
      </>
    )
  }

  // 装解决方案时的一句话（评估 L5）：这次新装哪几个、哪几个已经装好直接接上。
  const fresh: string[] = []
  const kept: string[] = []
  for (const ra of recipe?.apps || []) {
    const a = findApp(catalog, ra.app, src)
    const name = a?.name || ra.app
    if (a && a.installed.length > 0 && ra.reuse_installed !== false) kept.push(name)
    else fresh.push(name)
  }
  return (
    <>
      <DialogHead id={TITLE_ID} title={(target.kind === 'app' ? '安装 ' : '') + title} desc={target.kind === 'recipe' ? recipe?.show?.tagline || recipe?.summary : d0.show?.tagline || d0.summary} />
      <div className="flex flex-col gap-3">
        {target.kind === 'recipe' && (
          <p className="text-sm" data-testid="recipe-sentence">
            会装 {fresh.length} 个应用：{fresh.join('、')}，装完自动接好。{kept.length > 0 ? `${kept.join('、')} 已经装好了，直接接上。` : ''}
          </p>
        )}
        <NeedsBlock plan={plan} uses={uses} onUse={(k, v) => setUses((u) => ({ ...u, [k]: v }))} onRegistered={registered} />
        {target.kind === 'app' && <MatchedLine plan={plan} />}
        {planErr && <p className="text-2xs text-muted-foreground" data-testid="plan-error">没能先查它要用到的{WORDS.service}，安装时再查。</p>}
        {required.map((p) => <ParamInput key={p.key} p={p} problem={problemFor(p.key)} onChange={(v) => setPlain(p.key, v)} />)}
        {/* 谁能打开：默认是这台机器给新装应用的；只在内部网络的（数据库这类）没有这一项 */}
        {form.exposeDefault && <ExposureChoice name={title} value={form.expose} onChange={(v) => setForm((f) => ({ ...f, expose: v }))} />}
        {optional.length > 0 && (
          <details className="group rounded-md border px-2.5 py-1.5" open={optional.some((p) => !!problemFor(p.key))}>
            <summary className="tap-h cursor-pointer select-none content-center text-xs font-medium text-muted-foreground">更多设置（可不填）</summary>
            <div className="mt-2 flex flex-col gap-2.5 pb-1">
              {optional.map((p) => <ParamInput key={p.key} p={p} problem={problemFor(p.key)} onChange={(v) => setPlain(p.key, v)} />)}
            </div>
          </details>
        )}
        <div className={cn('flex items-center gap-1.5 text-xs', check.tone === 'ok' ? 'text-muted-foreground' : 'text-warning')} data-testid="mem-check">
          {check.tone === 'ok' ? <CircleCheck className="size-icon text-success" /> : <AlertTriangle className="size-icon" />}
          <span title={check.text}>{fitText(check.tone)}{check.tone === 'low' ? '，先删掉一个不用的应用再装' : ''}</span>
        </div>
        {needs && <UpgradeNeeded version={needs} />}
        {/* 版本、端口、平台自动填的：给懂技术的人看，收起来，只折一层（评估 L4）。 */}
        {(target.kind === 'app' || form.platform.length > 0) && (
          <details className="group rounded-md border px-2.5 py-1.5" open={form.platform.some((p) => !!problemFor('platform.' + p.view.name))} data-testid="install-tech">
            <summary className="tap-h cursor-pointer select-none content-center text-xs font-medium text-muted-foreground">技术设置（一般不用管）</summary>
            <div className="mt-2 flex flex-col gap-3 pb-1">
              {target.kind === 'app' && (
                <div className="grid grid-cols-2 gap-2">
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="ins-ver">版本</Label>
                    <Select id="ins-ver" value={form.version || form.defaultVersion} onChange={(e) => setForm((f) => ({ ...f, version: e.target.value }))} options={(d0.versions?.length ? d0.versions : [d0.version]).map((v) => [v, v])} />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <span className="text-xs font-medium leading-none">端口</span>
                    <div className="flex h-ctl items-center text-xs text-muted-foreground">自动分配，被占用时往后挪</div>
                  </div>
                </div>
              )}
              {form.platform.length > 0 && (
                <div className="flex flex-col gap-2.5" data-testid="platform-vars">
                  <div className="text-2xs font-medium text-muted-foreground">{WORDS.platformFilled}</div>
                  {form.platform.map((p) => <PlatformInput key={p.view.name} p={p} problem={problemFor('platform.' + p.view.name)} onChange={(v) => setPlatform(p.view.name, v)} />)}
                </div>
              )}
            </div>
          </details>
        )}
        <PlanBlocked reasons={blocked} />
        {err && (
          <div className="rounded-md border border-danger/40 bg-danger/5 px-3 py-2 text-xs" role="alert">
            <div className="font-medium text-danger">{err.message}</div>
            {err.hint && <div className="mt-0.5 text-muted-foreground">{err.hint}</div>}
            {faultNeedsPlatform(err) && <UpgradeFirst version={faultNeedsPlatform(err)} className="mt-2" />}
            {err.code === 'CAT-INST-24' ? (
              <div className="mt-2 flex flex-wrap items-center gap-2" data-testid="reuse-data">
                {!confirmReuse ? (
                  <Button variant="outline" size="sm" onClick={() => setConfirmReuse(true)}>接上以前留下的数据重装</Button>
                ) : (
                  <>
                    <span className="text-foreground">会接着用以前留下的数据，账号密码和以前一样；装失败也不会删这些数据。</span>
                    <Button size="sm" onClick={() => { setConfirmReuse(false); submit(true) }} disabled={busy}>确认接上</Button>
                    <Button variant="ghost" size="sm" onClick={() => setConfirmReuse(false)}>取消</Button>
                  </>
                )}
              </div>
            ) : !faultNeedsPlatform(err) && (
              <AskAI className="mt-2" variant="ghost" text={`我在 AcornFox 应用中心安装“${title}”时没装上：${err.code ? err.code + ' ' : ''}${err.message}${err.hint ? '（' + err.hint + '）' : ''}`} />
            )}
          </div>
        )}
      </div>
      <div className="mt-5 flex justify-end gap-2">
        <Button variant="outline" onClick={onClose}>取消</Button>
        <Button onClick={() => submit()} disabled={!!needs || blocked.length > 0} loading={busy}>{target.kind === 'recipe' ? '开始安装' : '安装'}</Button>
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

// ---- 进度和结果 ----

function TaskProgress({ task: t, title, onClose }: { task: TaskView; title: string; onClose: () => void }) {
  const { apps, catalog, refreshApps } = useConsole()
  const src = t.catalog || PUBLIC
  const groups = groupSteps(t)
  // 每一组的名字：应用中心里的名字（任务里只有代号和目录 id）。
  const nameOf = (app: string) => {
    const r = t.apps.find((a) => a.app === app)
    return findApp(catalog, r?.catalog_id || app, src)?.name || app
  }
  const prog = taskProgress(t)
  const pct = prog.total ? Math.round((prog.done * 100) / prog.total) : 0
  // 装好后“打开”哪一个：这次新装的里，能直接打开的那个；都打不开时是最后一个新装的。
  const fresh = (t.apps || []).filter((a) => !a.reused)
  const viewOf = (a: { app: string }) => apps?.find((x) => x.name === a.app)
  const main = [...fresh].reverse().find((a) => { const v = viewOf(a); return v ? !!openableAddress(v) : !!a.url && !isLoopbackURL(a.url) }) || fresh[fresh.length - 1] || t.apps?.[t.apps.length - 1]
  const mainName = main ? nameOf(main.app) : ''
  // 只本机的应用（Linux 上地址是 127.0.0.1）不给“打开”链接，改成怎么打开的说明。
  // 应用列表里有这个应用时以它为准（任务结果里的地址不会跟着对外方式变）：有 open_hint 才是只本机，
  // 打开的地址也取它的；列表还没刷新到时才按任务结果的地址判断。
  const mainView = main ? viewOf(main) : undefined
  const openHint: OpenHint | undefined = mainView
    ? mainView.open_hint
    : main && isLoopbackURL(main.url)
      ? { has_login: false, can_lan: false, recommend: 'tunnel', command: 'acornfox open ' + main.app }
      : undefined
  const openURL = mainView ? openableAddress(mainView) : main && !isLoopbackURL(main.url) ? main.url || '' : ''
  // 接下来要做的几步：介绍文档里“装好以后”那一节（解决方案用解决方案的文档）；取不到时用清单里的 user_steps。
  const recipe = t.kind === 'recipe' ? catalog?.recipes.find((r) => r.id === t.target && sourceId(r) === src) : undefined
  const doc = recipe ? recipe.show?.doc : main ? findApp(catalog, main.catalog_id || main.app, src)?.show?.doc : undefined
  const next = docSection(doc, DOC_HEADINGS.after)
  const steps = next.length > 0 ? next : t.user_steps || []
  const heading = t.status === 'succeeded' ? title + ' 已装好' : t.status === 'failed' ? title + ' 没装好' : '正在安装 ' + title
  return (
    <>
      <DialogHead id={TITLE_ID} title={heading} />
      {t.status === 'running' && prog.total > 0 && (
        <div className="mb-4 flex items-center gap-2" data-testid="install-total">
          <div className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-muted" role="progressbar" aria-label="安装进度" aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct}>
            <div className="h-full rounded-full bg-primary transition-[width] duration-700 ease-linear" style={{ width: pct + '%' }} />
          </div>
          <span className="tnum shrink-0 text-2xs text-muted-foreground">{prog.done}/{prog.total}</span>
        </div>
      )}
      <div className="flex flex-col gap-3" data-testid="install-steps">
        {groups.map((g) => (
          <div key={g.app}>
            {groups.length > 1 && <div className="mb-1.5 text-2xs font-medium text-muted-foreground">{nameOf(g.app)}</div>}
            <ol className="flex flex-col gap-2">
              {g.steps.map((s) => (
                <li key={s.app + s.name} className="flex items-start gap-2.5 text-sm">
                  <span className="mt-0.5"><StepDot status={s.status} /></span>
                  <div className="min-w-0">
                    <span className={cn(s.status === 'pending' && 'text-muted-foreground', s.status === 'failed' && 'text-danger')}>{plainStep(s.label)}</span>
                    {s.message && <div className="break-words text-2xs text-muted-foreground">{plainStep(s.message)}</div>}
                    {s.status === 'running' && s.progress && <StepBar p={s.progress} />}
                  </div>
                </li>
              ))}
            </ol>
          </div>
        ))}
      </div>
      {t.status === 'running' && <p className="mt-3 text-2xs text-muted-foreground">可以关掉这个窗口，安装会接着进行，装好了右上角的铃铛里会告诉你。</p>}
      {t.status === 'failed' && t.fault && (
        <div className="mt-4 rounded-md border border-danger/40 bg-danger/5 px-3 py-2.5 text-xs" role="alert">
          <div className="font-medium text-danger">{t.fault.message}</div>
          {t.fault.hint && <div className="mt-1 text-muted-foreground">建议：{t.fault.hint}</div>}
          {t.keep_volumes_on_failure ? (
            <div className="mt-1 text-muted-foreground">它的数据按你的要求留着了。</div>
          ) : (
            <div className="mt-1 text-muted-foreground">这次装了一半的已经清理掉，{faultNeedsPlatform(t.fault) ? '升级后再装。' : '可以直接重装。'}</div>
          )}
          {faultNeedsPlatform(t.fault) && <UpgradeFirst version={faultNeedsPlatform(t.fault)} className="mt-2" />}
        </div>
      )}
      {/* 总是渲染：改成局域网后 openHint 没了，这一块在原位置显示完成态。 */}
      {t.status === 'succeeded' && main && <OpenHintBlock className="mt-4" name={main.app} hint={openHint} openURL={openURL} onChanged={refreshApps} />}
      {t.status === 'succeeded' && <FirstLogins logins={t.first_login || []} apps={apps || undefined} nameOf={nameOf} />}
      {t.status === 'succeeded' && steps.length > 0 && (
        <div className="mt-4" data-testid="install-next">
          <div className="mb-1 text-xs font-medium">接下来</div>
          <ol className="list-decimal space-y-1 pl-5 text-xs text-muted-foreground">
            {steps.map((s, i) => <li key={i}>{s}</li>)}
          </ol>
        </div>
      )}
      <div className="mt-5 flex flex-wrap items-center justify-end gap-2">
        {t.status === 'failed' && <AskAI text={() => faultForAI(t)} />}
        {t.status !== 'succeeded' && <Button variant={t.status === 'failed' ? 'default' : 'outline'} onClick={onClose}>关闭</Button>}
        {t.status === 'succeeded' && main && (
          !openHint && safeHref(openURL) ? (
            <>
              <button type="button" onClick={() => { onClose(); go(appHref(main.app)) }} className="px-1 text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">去管理</button>
              <a href={safeHref(openURL)} target="_blank" rel="noopener noreferrer" className={buttonVariants()} data-testid="install-open">
                打开 {mainName}<ExternalLink className="size-icon" />
              </a>
            </>
          ) : (
            <Button onClick={() => { onClose(); go(appHref(main.app)) }}>去管理</Button>
          )
        )}
      </div>
    </>
  )
}

// FirstLogins：第一次登录用的信息。密码默认遮住，点“显示”才向服务端要明文；复制密码时也先要明文。
// 地址优先用应用列表里现在的地址（改了对外方式会跟着变），列表里还没有这个应用时用任务结果里的。
// nameOf：把应用的代号换成应用中心里的名字（标题写“某某的登录信息”）。
export function FirstLogins({ logins, apps, nameOf }: { logins: FirstLogin[]; apps?: AppView[]; nameOf?: (app: string) => string }) {
  if (logins.length === 0) return null
  return (
    <div className="mt-4 flex flex-col gap-2">
      {logins.map((fl) => <FirstLoginCard key={fl.app} fl={fl} title={(nameOf ? nameOf(fl.app) : fl.app) + ' 的登录信息'} url={loginAddress(fl.url, apps?.find((a) => a.name === fl.app))} />)}
    </div>
  )
}

// url：要显示的地址（调用方按应用现在的样子算好）；不给时用 fl.url。
export function FirstLoginCard({ fl, title, url: urlProp }: { fl: FirstLogin; title?: string; url?: string }) {
  const toast = useToast()
  const [shown, setShown] = useState<FirstLogin | null>(null)
  const [busy, setBusy] = useState(false)
  // 读不到密码、没能复制：出错的话留在这张卡片最下面（样稿第 14 版，不弹提示）
  const [err, setErr] = useState('')
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
    setErr('')
    try { setShown(await getReveal()) } catch (e) { setErr('没能读取密码：' + (e as Error).message) } finally { setBusy(false) }
  }
  const copySecret = async (i: number) => {
    setErr('')
    try {
      const full = await getReveal()
      const v = full.fields[i]?.value || ''
      if (await copyText(v)) toast('已复制')
      else setErr('没能复制，点“显示密码”后手动选中')
    } catch (e) {
      setErr('没能读取密码：' + (e as Error).message)
    }
  }
  const view = shown || fl
  const url = urlProp || fl.url
  // 没有登录信息的应用（例如给别的应用用、没有网页的数据库）：服务端给的字段是空的，不显示这一块。
  const fields = fl.fields || []
  if (fields.length === 0) return null
  const hasSecret = fields.some((f) => f.secret)
  return (
    <div className="rounded-md border p-2.5" data-testid="first-login">
      <div className="mb-1.5 flex items-center gap-1.5 text-xs font-medium">
        <KeyRound className="size-icon" />{title || fl.app + ' 的登录信息'}
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
            {/* 127.0.0.1 只在这台机器自己身上能用，不做成链接（怎么打开见上面的说明）。 */}
            {isLoopbackURL(url)
              ? <span className="mono min-w-0 flex-1 truncate text-muted-foreground" title="现在只有这台机器自己能打开">{url}（只这台机器）</span>
              : <a href={safeHref(url)} target="_blank" rel="noopener noreferrer" className="mono min-w-0 flex-1 truncate underline-offset-4 hover:underline">{url}</a>}
            <CopyButton text={url} label="复制地址" onFail={() => setErr('没能复制，请手动选中')} onOk={() => setErr('')} />
          </div>
        )}
        {(view.fields || []).map((f, i) => (
          <div key={f.label + i} className="flex items-center gap-2 text-xs">
            <span className="w-16 shrink-0 truncate text-muted-foreground" title={f.label}>{f.label}</span>
            <span className="mono min-w-0 flex-1 select-all truncate" data-secret={f.secret ? 'true' : undefined}>{f.value}</span>
            {f.secret ? (
              <button type="button" onClick={() => copySecret(i)} className="tap inline-flex size-ctl-xs shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground" aria-label={'复制' + f.label}>
                <Copy className="size-icon" />
              </button>
            ) : (
              <CopyButton text={f.value} label={'复制' + f.label} onFail={() => setErr('没能复制，请手动选中')} onOk={() => setErr('')} />
            )}
          </div>
        ))}
        {err && <p className="text-xs text-danger" role="alert">{err}</p>}
      </div>
    </div>
  )
}
