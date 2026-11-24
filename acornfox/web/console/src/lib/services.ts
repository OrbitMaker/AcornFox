// 服务：这台机器上能给应用用的东西（装好的应用提供的，或用户自己添加的），
// 以及安装前的计划（这个应用需要哪些服务、接上了哪条、还缺什么）。
// 请求、状态文字、按服务类型定义生成的表单、计划的整理都在这里；页面只负责显示。单元测试见 services.test.ts。
// 样稿第 10 版：页面上“登记”一律改叫“添加”（添加我已有的）；说到应用的地方都可以传 nameOf（页面用 useAppNames()），
// 把代号换成应用中心里的名字，不传时照旧用代号；故障编号不放进给人看的那句话，放进“复制给 AI 工作台”的文字。
//
// 接口：GET /v1/services/kinds、GET /v1/services、POST /v1/services、PATCH /v1/services/{name}、
//       POST /v1/services/{name}/test、DELETE /v1/services/{name}、POST /v1/catalog/plan。
// 旧版本服务端没有这些接口（404 或 405）：读的函数返回 null，页面显示“这个版本的服务端还没有这项功能”。
import { api, ApiError } from './api'
import { quote, type InstallRequest } from './catalog'
import type { AppServiceUse, InstallPlan, PlanApp, PlanNeed, PlatformVarList, PlatformVarView, RemovedService, ServiceField, ServiceKind, ServiceList, ServiceTestResult, ServiceUse, ServiceView } from './types'

export const servicePath = (name: string, rest = '') => '/v1/services/' + encodeURIComponent(name) + rest

// isUnsupported：服务端没有这个接口（旧版本）。
export function isUnsupported(e: unknown): boolean {
  return e instanceof ApiError && (e.status === 404 || e.status === 405)
}

async function orNull<T>(p: Promise<T>): Promise<T | null> {
  try {
    return await p
  } catch (e) {
    if (isUnsupported(e)) return null
    throw e
  }
}

// ---- 请求 ----

// listServices：服务列表，加上存的值解不开的那几条的名字（unreadable，没有时是空数组）。
export function listServices(signal?: AbortSignal): Promise<ServiceList | null> {
  return orNull(
    api.get<{ services?: ServiceView[] | null; unreadable?: unknown }>('/v1/services', signal).then((b) => ({
      services: b.services || [],
      unreadable: Array.isArray(b.unreadable) ? b.unreadable.filter((x): x is string => typeof x === 'string' && x !== '') : [],
    })),
  )
}

// listPlatformVars：平台按这台机器自动填给应用的那几项（GET /v1/catalog/platform-vars）。
// 旧服务端或没启用应用中心（404、405）返回 null，页面不显示“平台自动填的”那一行。
export function listPlatformVars(signal?: AbortSignal): Promise<PlatformVarView[] | null> {
  return orNull(api.get<PlatformVarList>('/v1/catalog/platform-vars', signal).then((b) => (Array.isArray(b?.vars) ? b.vars : [])))
}

export function listKinds(signal?: AbortSignal): Promise<ServiceKind[] | null> {
  return orNull(api.get<{ kinds: ServiceKind[] | null }>('/v1/services/kinds', signal).then((b) => b.kinds || []))
}

export interface AddServiceRequest { name: string; kind: string; values: Record<string, string> }

export function addService(req: AddServiceRequest): Promise<ServiceView> {
  return api.post<ServiceView>('/v1/services', req)
}

// patchService：只发要改的项；值是空字符串表示清掉。返回的 notes 是“有几个应用在用它、重新应用设置后才生效”这类说明。
export function patchService(name: string, values: Record<string, string>): Promise<ServiceView> {
  return api.patch<ServiceView>(servicePath(name), { values })
}

export function testService(name: string): Promise<ServiceTestResult> {
  return api.post<ServiceTestResult>(servicePath(name, '/test'))
}

// deleteService：删一条服务。服务端回 204（没有内容）或 200 带 notes（删掉的同时要告诉用户的话）；
// notes 整理成字符串数组，没有时是空数组，调用方要原样显示出来。
export async function deleteService(name: string): Promise<RemovedService> {
  const r = await api.del<{ name?: unknown; notes?: unknown } | null>(servicePath(name))
  const notes = Array.isArray(r?.notes) ? r!.notes.filter((x): x is string => typeof x === 'string' && x.trim() !== '') : []
  return { name: typeof r?.name === 'string' && r.name ? r.name : name, notes }
}

// fetchPlan：安装前的计划。服务端没有这个接口时返回 null（页面照旧，不显示“需要的服务”）。
export function fetchPlan(req: InstallRequest): Promise<InstallPlan | null> {
  return orNull(api.post<InstallPlan>('/v1/catalog/plan', req))
}

// refusedBy：删一条服务被拒绝（有应用在用）时，是哪些应用在用。服务端错误里带 used_by；
// 没带时用页面上已知的那条服务的 used_by。已删、数据还留着的应用（gone）不算在用。
export function refusedBy(e: unknown, known?: ServiceView['used_by']): string[] {
  const raw = e instanceof ApiError ? e.raw : undefined
  const list = Array.isArray(raw?.used_by) ? (raw!.used_by as unknown[]) : known || []
  const out: string[] = []
  for (const x of list) {
    if (x && typeof x === 'object' && (x as { gone?: unknown }).gone === true) continue
    const app = typeof x === 'string' ? x : x && typeof x === 'object' && typeof (x as { app?: unknown }).app === 'string' ? (x as { app: string }).app : ''
    if (app && !out.includes(app)) out.push(app)
  }
  return out
}

// ---- 显示 ----

// 服务类型或项没有中文名时（目录里的定义漏写了），不把程序用的名字直接给用户看：说“未命名”，
// 原名只在旁边用小字带上供排查（页面用 service-form.tsx 的 KindName、ItemName）。
export const UNNAMED_KIND = '（未命名的服务类型）'
export const UNNAMED_ITEM = '（未命名的项）'

export function kindLabel(label: string | null | undefined): string {
  return (typeof label === 'string' && label.trim()) || UNNAMED_KIND
}

export function itemLabel(label: string | null | undefined): string {
  return (typeof label === 'string' && label.trim()) || UNNAMED_ITEM
}

export type Tone = 'success' | 'warning' | 'danger' | 'muted'

// detail：给排查看的原名（有未命名的项时），页面放在 title 里，不当正文显示。
export interface StatusText { text: string; tone: Tone; detail?: string }

// 状态颜色对应的标签样式。
export const TONE_BADGE: Record<Tone, 'success' | 'warning' | 'danger' | 'outline'> = { success: 'success', warning: 'warning', danger: 'danger', muted: 'outline' }

// NameOf：应用代号 → 给人看的名字（页面传 useAppNames()；不传就是代号本身）。
export type NameOf = (app: string) => string
const same: NameOf = (x) => x

// serviceStatus：一条服务现在能不能用。还差的项用中文名列出来。
export function serviceStatus(s: ServiceView, nameOf: NameOf = same): StatusText {
  switch (s.status) {
    case 'ok':
      return { text: '可用', tone: 'success' }
    case 'incomplete': {
      const missing = s.missing || []
      const labels = missing.map((k) => itemLabel(s.values?.find((v) => v.key === k)?.label))
      const raw = missing.filter((k) => !s.values?.find((v) => v.key === k)?.label?.trim())
      const out: StatusText = { text: labels.length ? '还差：' + [...new Set(labels)].join('、') : s.status_label || '还差几项没填', tone: 'warning' }
      if (raw.length) out.detail = raw.join('、')
      return out
    }
    case 'provider_stopped':
      return { text: s.app ? `${nameOf(s.app)} 停了，暂时用不了` : '提供它的应用停了，暂时用不了', tone: 'warning' }
    case 'provider_gone':
      return { text: s.app ? `${nameOf(s.app)} 已经删了` : '提供它的应用已经删了', tone: 'danger' }
    case 'not_ready': {
      const out: StatusText = { text: '还不能用', tone: 'warning' }
      if (s.ready_detail) out.detail = s.ready_detail
      return out
    }
    default:
      return { text: s.status_label || s.status || '不清楚', tone: 'muted' }
  }
}

// slotsText：这条服务还能不能再接别的应用。被独占、接满时说清是谁占着；有上限时说还能接几个；不限时 text 是空串（不显示）。
export function slotsText(s: ServiceView, nameOf: NameOf = same): { text: string; full: boolean } {
  if (s.dedicated_to) return { text: `只给 ${nameOf(s.dedicated_to)} 用，别的应用接不了`, full: true }
  if (typeof s.slots_left === 'number') {
    if (s.slots_left <= 0) return { text: `已经接满（最多 ${s.max_consumers || 0} 个），别的应用接不了`, full: true }
    return { text: `还能接 ${s.slots_left} 个（最多 ${s.max_consumers || 0} 个）`, full: false }
  }
  return { text: '', full: false }
}

// appServiceStatus：已装应用用的一项服务的状态。没接上时 service 为空。
export function appServiceStatus(u: AppServiceUse): StatusText {
  if (!u.service) return { text: u.status_label || '没接上', tone: 'warning' }
  switch (u.status) {
    case 'ok':
      return { text: '可用', tone: 'success' }
    case 'incomplete':
      return { text: u.status_label || '还差几项没填', tone: 'warning' }
    case 'provider_stopped':
      return { text: u.status_label || '提供它的应用停着', tone: 'warning' }
    case 'provider_gone':
      return { text: u.status_label || '提供它的应用已删', tone: 'danger' }
    case 'not_ready':
      return { text: u.status_label || '还不能用', tone: 'warning' }
    default:
      return { text: u.status_label || u.status || '不清楚', tone: 'muted' }
  }
}

export function sourceText(s: ServiceView, nameOf: NameOf = same): string {
  return s.source === 'app' ? `${s.app ? nameOf(s.app) : '一个应用'} 提供的` : '你自己添加的'
}

function appsOf(list: ServiceUse[]): string[] {
  const out: string[] = []
  for (const u of list) if (u.app && !out.includes(u.app)) out.push(u.app)
  return out
}

// usedByApps：在用这条服务的应用（已装的）。已删、数据还留着的（gone）不算：它们不拦删除。
export function usedByApps(s: ServiceView): string[] {
  return appsOf((s.used_by || []).filter((u) => u.gone !== true))
}

// goneUsers：接着这条服务、已删但数据还留着的应用。
export function goneUsers(s: ServiceView): string[] {
  return appsOf((s.used_by || []).filter((u) => u.gone === true)).filter((a) => !usedByApps(s).includes(a))
}

export const goneText = (app: string) => app + '（已删除，数据还留着）'

export function usedByText(s: ServiceView, nameOf: NameOf = same): string {
  const apps = usedByApps(s).map(nameOf)
  const gone = goneUsers(s).map(nameOf)
  const head = apps.length ? apps.join('、') + ' 在用' : gone.length ? '没有已装的应用在用' : '还没有应用在用'
  return gone.length ? head + '；' + gone.map(goneText).join('、') : head
}

// providerText：一行里“谁提供”那半句。应用提供的写“应用名 提供”；自己添加的写“你添加的“名字””。
export function providerText(s: ServiceView, nameOf: NameOf = same): string {
  return s.source === 'app' ? `${s.app ? nameOf(s.app) : '一个应用'} 提供` : `你添加的“${s.name}”`
}

// usersText：展开后“谁在用”那一项。已删、数据还留着的写成“名字（已删除，数据还留着）”；一个都没有时说“还没有”。
export function usersText(s: ServiceView, nameOf: NameOf = same): string {
  return [...usedByApps(s).map(nameOf), ...goneUsers(s).map(nameOf).map(goneText)].join('、') || '还没有'
}

// ---- 平台自动填的 ----

// platformShown：“平台自动填的”展开后列出的项：这台机器上有值的；每个应用各算各的（名字以 app. 开头）不列。
export function platformShown(vars: PlatformVarView[] | null | undefined): PlatformVarView[] {
  return (vars || []).filter((v) => !!v && typeof v.name === 'string' && !v.name.startsWith('app.') && typeof v.value === 'string' && v.value.trim() !== '')
}

// platformLabel：一项的中文名，去掉括号里的补充（例如“网络区域（cn 或 global）”只写“网络区域”）。
export function platformLabel(v: PlatformVarView): string {
  return itemLabel((v.label || '').replace(/（[^）]*）/g, '').replace(/\([^)]*\)/g, '').trim())
}

// platformSummary：“平台自动填的”那一行收起时的一句话。
export function platformSummary(n: number): string {
  return n > 0 ? `时区、下载源等 ${n} 项。装应用时平台替你填好，不用管` : '这台机器上现在没有要平台替你填的项'
}

// platformValue：一项现在的值怎么给人看。网络区域的 cn、global 写成“国内网络”“海外网络”（评估 X9），别的原样。
export function platformValue(v: PlatformVarView): string {
  const raw = (v.value || '').trim()
  if (v.name === 'region') return raw === 'cn' ? '国内网络' : raw === 'global' ? '海外网络' : raw
  return raw
}

// removeConfirmText：删除前确认的那句话。有已装的应用在用：说要先把它们删掉或换掉（服务端会拒绝）；
// 只有已删、数据还留着的应用接着它：说它们的接入会一起删掉，以后装回来时要重新接服务。
export function removeConfirmText(s: ServiceView, nameOf: NameOf = same): string {
  const apps = usedByApps(s).map(nameOf)
  const gone = goneUsers(s).map(nameOf)
  if (apps.length) return `删除“${s.name}”？现在 ${apps.join('、')} 在用它，要先把它们删掉或换成别的服务。`
  if (gone.length) return `删除“${s.name}”？${gone.map(goneText).join('、')}接着它的记录会一起删掉，以后装回来时要重新接服务。`
  return `删除“${s.name}”？`
}

// canRemove：页面上给不给“删除”。自己登记的都给；应用提供的只在提供它的应用已删时给（否则删掉那个应用它才会消失）。
export function canRemove(s: ServiceView): boolean {
  return s.source !== 'app' || s.status === 'provider_gone'
}

// issuedItems：按每个使用它的应用单独发的项（名字），不是这条服务的值，也不由用户在这里填。
export function issuedItems(s: ServiceView): string[] {
  return (s.issued_per_consumer || []).filter((x) => !!x)
}

// shownValues：详情里列出来的值。按使用方单独发的项不列成“没填”（服务端本来就不放进 values，这里再保险一次）。
export function shownValues(s: ServiceView) {
  const issued = new Set(issuedItems(s))
  return (s.values || []).filter((v) => !issued.has(v.key))
}

// waitingAsk：要用户在应用里做一步、还没填的项（显示那句提示和“填这一项”）。
export function waitingAsk(s: ServiceView) {
  return shownValues(s).filter((v) => !!v.ask && !v.set)
}

// testPlain：“试一下”的结果给人看的那一句（样稿第 10 版）：不带故障编号和连的地址，这些放进 testForAI 给 AI 工作台。
export function testPlain(r: ServiceTestResult): StatusText & { kind: 'ok' | 'skipped' | 'failed' } {
  const why = r.detail || r.message || ''
  if (r.skipped) return { kind: 'skipped', tone: 'muted', text: '这次试不了' + (why ? '：' + why : '') }
  if (r.ok) return { kind: 'ok', tone: 'success', text: '通了' + (why ? '：' + why : '') }
  return { kind: 'failed', tone: 'danger', text: '没通' + (why ? '：' + why : '') }
}

export function testForAI(name: string, r: ServiceTestResult): string {
  return [`用 acornfox skill 帮我查一下这台机器上的服务“${name}”为什么试不通（acornfox services test ${name} --json）。页面上试的结果：`,
    `- ${r.ok ? '通了' : r.skipped ? '试不了' : '没通'}${r.code ? ' · ' + r.code : ''}${r.target ? ' · 连的是 ' + r.target : ''}${r.detail || r.message ? ' · ' + (r.detail || r.message) : ''}`].join('\n')
}

// testText：“试一下”的结果怎么说。skipped：这次试不了（不是没通）；target 有值时说连的是哪。
export function testText(r: ServiceTestResult): StatusText & { kind: 'ok' | 'skipped' | 'failed' } {
  const why = r.detail || r.message || ''
  const where = r.target ? `（连的是 ${r.target}）` : ''
  if (r.skipped) return { kind: 'skipped', tone: 'muted', text: '试不了' + (why ? '：' + why : '') }
  if (r.ok) return { kind: 'ok', tone: 'success', text: '通了' + where + (why ? '：' + why : '') }
  return { kind: 'failed', tone: 'danger', text: '没通' + (r.code ? ' · ' + r.code : '') + where + (why ? '：' + why : '') }
}

// ---- 按服务类型定义生成的表单 ----
// 不为某种类型写专门的表单：必填、保密、枚举、默认值、提示都照 fields 来。

export type ServiceFormValues = Record<string, string>

export function defaultText(f: ServiceField): string {
  const d = f.default
  if (d === undefined || d === null) return ''
  if (typeof d === 'boolean') return d ? 'true' : 'false'
  return String(d)
}

// fieldControl：一项用什么控件。
export function fieldControl(f: ServiceField): 'select' | 'password' | 'number' | 'text' {
  if (f.type === 'enum' || f.type === 'bool') return 'select'
  if (f.secret) return 'password'
  if (f.type === 'int') return 'number'
  return 'text'
}

// PLAIN_OPTION：枚举值给人看的说法（评估 X9：disable→不加密、tls→加密）。存的值不变，只换显示；表里没有的原样显示。
export const PLAIN_OPTION: Record<string, string> = {
  disable: '不加密', none: '不加密', require: '加密', tls: '加密', starttls: '加密（STARTTLS）', 'verify-full': '加密并核对证书', true: '是', false: '否',
}

// plainValue：展开后一项的值怎么显示。枚举、是否换成人话；别的原样（保密项本来就是遮住的）。
export function plainValue(v: { value: string; secret?: boolean }, f?: ServiceField): string {
  if (v.secret) return v.value
  const enumLike = !f || f.type === 'enum' || f.type === 'bool'
  return enumLike && Object.prototype.hasOwnProperty.call(PLAIN_OPTION, v.value) ? PLAIN_OPTION[v.value] : v.value
}

// fieldOptions：枚举和是否的选项；可以不填的项前面多一个“不填”。选项的值不变，显示的字换成人话（PLAIN_OPTION）。
export function fieldOptions(f: ServiceField): [string, string][] {
  const opts: [string, string][] = f.type === 'bool' ? [['true', '是'], ['false', '否']] : (f.options || []).map((o) => [String(o), PLAIN_OPTION[String(o)] || String(o)])
  return f.required || defaultText(f) ? opts : [['', '不填'], ...opts]
}

export function newServiceValues(k: ServiceKind): ServiceFormValues {
  const out: ServiceFormValues = {}
  for (const f of k.fields || []) out[f.key] = defaultText(f)
  return out
}

// editPlaceholder：改一项时输入框里的提示。必填项不能清掉；能清掉的写明“留空表示清掉这一项”。
export function editPlaceholder(f: ServiceField): string {
  if (f.secret) return f.required ? '新的值（保存后只显示后 4 位）' : '留空表示清掉这一项；填了保存后只显示后 4 位'
  return f.required ? '新的值' : '留空表示清掉这一项'
}

export interface FieldProblem { key: string; message: string }

// checkFieldValue：一项的值对不对（空值只在必填时算错）。
export function checkFieldValue(f: ServiceField, raw: string): string {
  const v = raw.trim()
  const label = itemLabel(f.label)
  if (!v) return f.required ? `请填写${label}` : ''
  if (f.type === 'int' && !/^-?\d+$/.test(v)) return `${label}要填整数`
  if (f.type === 'url' && !/^https?:\/\/\S+$/.test(v)) return `${label}要以 http:// 或 https:// 开头`
  if (f.type === 'bool' && v !== 'true' && v !== 'false') return `${label}只能选“是”或“否”`
  if (f.type === 'enum' && f.options?.length && !f.options.map(String).includes(v)) return `${label}只能选 ${f.options.map((o) => PLAIN_OPTION[String(o)] || String(o)).join('、')}`
  return ''
}

// checkServiceForm：提交前页面上能查的（名字、必填、格式）；其余以服务端为准。
export function checkServiceForm(k: ServiceKind, name: string, values: ServiceFormValues): FieldProblem[] {
  const out: FieldProblem[] = []
  const n = name.trim()
  if (!n) out.push({ key: 'name', message: '请给这条服务起个名字' })
  else if (n.length > 64) out.push({ key: 'name', message: '名字最多 64 个字' })
  for (const f of k.fields || []) {
    const m = checkFieldValue(f, values[f.key] || '')
    if (m) out.push({ key: f.key, message: m })
  }
  return out
}

// buildServiceRequest：添加一条服务的请求。没填的项不发；值都按字符串发（和命令行 --set 项=值 一样）。
export function buildServiceRequest(k: ServiceKind, name: string, values: ServiceFormValues): AddServiceRequest {
  const out: Record<string, string> = {}
  for (const f of k.fields || []) {
    const v = (values[f.key] || '').trim()
    if (v) out[f.key] = v
  }
  return { name: name.trim(), kind: k.kind, values: out }
}

// addCommand：同样的事用命令行怎么做（保密项不写值，命令行会从标准输入读）。
export function addCommand(k: ServiceKind, name: string, values: ServiceFormValues): string {
  const parts = ['acornfox services add', k.kind, '--name', quote(name.trim() || '名字')]
  for (const f of k.fields || []) {
    const v = (values[f.key] || '').trim()
    if (!v || f.secret) continue
    parts.push('--set', f.key + '=' + quote(v))
  }
  return parts.join(' ')
}

// ---- 安装前的计划 ----
// 计划只回答这个应用（或这个配方）自己的事：需要什么服务、这台机器上接上了哪条、还缺什么、缺的怎么登记。
// 平台不替用户装别的应用：必须的服务没有，就只能登记一条用户已有的。

export const NEED_TEXT: Record<string, StatusText> = {
  matched: { text: '已接上', tone: 'success' },
  optional_missing: { text: '没有，先跳过', tone: 'muted' },
  required_missing: { text: '没有', tone: 'danger' },
  choose: { text: '要选一条', tone: 'warning' },
}

// UNKNOWN_NEED：服务端给了页面不认识的状态（例如更新的服务端）。不把原词给用户看，也不因此拦住安装。
export const UNKNOWN_NEED: StatusText = { text: '状态未知，安装时由服务端检查', tone: 'muted' }

// pendingFrom：配方里后装的应用要接的服务，由同一次安装里先装的哪个应用（实例名）装好后登记；不是这种情况时为空。
// 这种需要算已接上，不拦安装。
export function pendingFrom(n: PlanNeed): string {
  return n.status === 'matched' && n.pending_from ? n.pending_from : ''
}

export function pendingText(n: PlanNeed): string {
  return `装好 ${pendingFrom(n)} 后接上它提供的${kindLabel(n.kind_label)}`
}

export function needText(n: PlanNeed): StatusText {
  if (pendingFrom(n)) return { text: '装好后接上', tone: 'success' }
  return Object.prototype.hasOwnProperty.call(NEED_TEXT, n.status) ? NEED_TEXT[n.status] : UNKNOWN_NEED
}

// multiApp：计划里不止一个应用（配方）。
export function multiApp(plan: InstallPlan | null | undefined): boolean {
  return (plan?.apps || []).length > 1
}

// useKey：安装请求里 use 的键。计划里不止一个应用时一律是“应用实例名.需要的名字”，免得两个应用的同名需要串在一起；
// 只装一个应用时就是需要的名字。
export function useKey(multi: boolean, appName: string, use: string): string {
  return multi ? appName + '.' + use : use
}

export function candidateNames(n: PlanNeed): string[] {
  const out: string[] = []
  for (const c of n.candidates || []) {
    const name = typeof c === 'string' ? c : c?.name
    if (name && !out.includes(name)) out.push(name)
  }
  return out
}

export interface PlanRow { app: PlanApp; need: PlanNeed; key: string }

export function planRows(plan: InstallPlan | null | undefined): PlanRow[] {
  const multi = multiApp(plan)
  const out: PlanRow[] = []
  for (const a of plan?.apps || []) for (const n of a.needs || []) out.push({ app: a, need: n, key: useKey(multi, a.name, n.use) })
  return out
}

// hasNeeds：计划里有“需要的服务”才显示那一块（不需要任何服务的应用没有）。
export function hasNeeds(plan: InstallPlan | null | undefined): boolean {
  return (plan?.apps || []).some((a) => (a.needs || []).length > 0)
}

// noCandidates：说要选一条，却一条能选的都没有。这时页面让用户登记一条，不拦安装按钮（交给服务端判断）。
export function noCandidates(n: PlanNeed): boolean {
  return n.status === 'choose' && candidateNames(n).length === 0
}

// unchosen：有多条要选、用户还没选的需要。
export function unchosen(plan: InstallPlan | null | undefined, uses: Record<string, string>): PlanRow[] {
  return planRows(plan).filter((r) => r.need.status === 'choose' && !noCandidates(r.need) && !uses[r.key])
}

// planBlock：现在不能装的原因（空表示能装）。还没选的先说；服务端说不能装的原因（problems）一律原样列出，
// 不因为还有没选的就藏起来。
export function planBlock(plan: InstallPlan | null | undefined, uses: Record<string, string>): string[] {
  if (!plan) return []
  const multi = multiApp(plan)
  const out: string[] = []
  for (const r of unchosen(plan, uses)) out.push(`先选好${multi ? ' ' + r.app.name + ' 的' : ''}“${kindLabel(r.need.kind_label)}”用哪一条`)
  if (plan.can_install === false) {
    const ps = plan.problems || []
    for (const p of ps) out.push((p.code ? p.code + ' · ' : '') + p.message)
    if (ps.length === 0 && out.length === 0) out.push('现在还不能装')
  }
  return out
}

// applyPlan：把用户选的服务写进安装请求（空的不写）。
export function applyPlan<T extends InstallRequest>(req: T, uses: Record<string, string>): T {
  const out = { ...req }
  const picked = Object.fromEntries(Object.entries(uses).filter(([, v]) => !!v))
  if (Object.keys(picked).length) out.use = picked
  return out
}

// planCli：命令行写法里要加的参数。
export function planCli(uses: Record<string, string>): string[] {
  const parts: string[] = []
  for (const [k, v] of Object.entries(uses)) if (v) parts.push('--use ' + quote(k + '=' + v))
  return parts
}
