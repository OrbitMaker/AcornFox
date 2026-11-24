// 让应用用上新的设置（后端叫“重新生效”，W8-1、W8-2、W8-3、W6-3）：改一项安装设置、平台自动填的值变了、改了谁能打开或域名以后，
// 应用里记的地址和设置要跟着变。页面上的做法固定：先读“计划”（会改什么、要不要重启，只读），用户同意了再做，做的时候看一列步骤。
// 接口（acornfox/internal/apiserver/reapply.go）：
//   PATCH /v1/apps/{app}/vars        {set, reset, plan, confirm_restart}  改设置并生效；plan 为 true 只看计划
//   POST  /v1/apps/{app}/reapply     {plan, confirm_restart}              让应用跟上现在的平台设置和服务
//   GET   /v1/platform/affected                                          哪些应用落在后面（平台设置变了、服务变了）
//   PATCH /v1/apps/{app}（exposure）、POST/DELETE …/domains              改了谁能打开或域名；对用到应用地址的应用不带 confirm_restart 会 409（APP-APPLY-05，
//                                                                          体里带 plan，什么都没改）：页面拿着 plan 问用户，同意了带 confirm_restart 再来
// 答复：计划 200 {plan}；没有要改的 200 {changed:false, plan}；开始做 202 {changed:true, plan, task}（task 是安装任务，进度读 /v1/catalog/install/{task}）。
// 页面上不写“变量”“环境变量”这些词；服务端的 summary 是写给命令行的（有“环境变量”“配置文件”），页面自己按结构化的计划写（planFacts）。
import { api, ApiError, appPath } from './api'
import { itemLabel } from './services'
import type { TaskLike } from './run'

export interface ReapplyVarChange {
  // params.<名>、platform.<名> 或 uses.<需要名>.<项>
  name: string
  label?: string
  old: string
  new: string
  secret?: boolean
  // user 这次改的；platform 平台自动填的变了；service 用的服务变了；upgrade 升级带来的
  from: string
}

export interface ReapplyPlan {
  app: string
  needed: boolean
  restart: boolean
  vars: ReapplyVarChange[]
  notes: string[]
  summary: string
  // 它提供的服务值变了，用着这些服务的应用也会落后（只告知，不替它们改）
  dependents?: string[]
}

export interface AffectedApp {
  app: string
  catalog_id?: string
  differs: boolean
  vars: ReapplyVarChange[]
  restart: boolean
  // 现在不能让它跟上的原因（一句话）
  blocked?: string
  summary?: string
}

export interface Affected { var: string; apps: AffectedApp[]; notes: string[] }

// 一次改动的答复（改安装设置、改谁能打开或域名、让它跟上都是这个样子）
export interface ReapplyAnswer {
  changed?: boolean
  plan?: ReapplyPlan
  task?: TaskLike | null
}

export const CONFIRM_CODE = 'APP-APPLY-05'

// confirmPlan：这次改动要重启应用、需要用户同意（409 APP-APPLY-05），体里带着计划；不是这种错误返回 null。
export function confirmPlan(e: unknown): ReapplyPlan | null {
  if (!(e instanceof ApiError) || e.code !== CONFIRM_CODE) return null
  const p = e.raw?.plan
  return p && typeof p === 'object' ? (p as ReapplyPlan) : null
}

// ---- 接口 ----

export const varsPath = (app: string) => appPath(app, '/vars')

export async function planVars(app: string, set: Record<string, unknown>, reset: string[] = []): Promise<ReapplyPlan> {
  const r = await api.patch<{ plan: ReapplyPlan }>(varsPath(app), { set, reset, plan: true })
  return r.plan
}

export function applyVars(app: string, set: Record<string, unknown>, reset: string[] = [], confirmRestart = false): Promise<ReapplyAnswer> {
  return api.patch<ReapplyAnswer>(varsPath(app), { set, reset, confirm_restart: confirmRestart })
}

export async function planReapply(app: string): Promise<ReapplyPlan> {
  const r = await api.post<{ plan: ReapplyPlan }>(appPath(app, '/reapply'), { plan: true })
  return r.plan
}

export function applyReapply(app: string, confirmRestart = false): Promise<ReapplyAnswer> {
  return api.post<ReapplyAnswer>(appPath(app, '/reapply'), { confirm_restart: confirmRestart })
}

// fetchAffected：哪些应用落在后面。旧服务端没有这个接口（404、405）时是 null。
export async function fetchAffected(signal?: AbortSignal): Promise<Affected | null> {
  try {
    return await api.get<Affected>('/v1/platform/affected', signal)
  } catch (e) {
    if (e instanceof ApiError && (e.status === 404 || e.status === 405)) return null
    throw e
  }
}

// behindApps：落在后面的应用（只算和现在不一样的）。
export function behindApps(a: Affected | null | undefined): AffectedApp[] {
  return (a?.apps || []).filter((x) => x.differs)
}

// ---- 说人话 ----

// 访问地址里的协议和主机并进“打开的地址”，计划里不单独列
const MERGED = ['platform.app.scheme', 'platform.app.host']

// changeName：计划里一项叫什么（和“安装设置”那一行同一个叫法）。
export function changeName(v: ReapplyVarChange): string {
  if (v.name === 'platform.app.url') return '打开的地址'
  const raw = (v.label || '').replace(/[（(][^）)]*[）)]/g, '').replace(/^这个应用的?/, '').trim()
  return raw || itemLabel(v.name.replace(/^(params|platform)\./, '').replace(/^uses\./, ''))
}

// visibleChanges：页面上列出来的改动（去掉并进“打开的地址”的两项）。
export function visibleChanges(vars: ReapplyVarChange[] | null | undefined): ReapplyVarChange[] {
  const all = vars || []
  const hasURL = all.some((v) => v.name === 'platform.app.url')
  return all.filter((v) => !(hasURL && MERGED.includes(v.name)))
}

// planFacts：确认窗里的几句话（label 是应用的名字）。第一句说要不要重启；改动的项另外一项一行列出（visibleChanges）。
export function planFacts(plan: ReapplyPlan, label: string): string[] {
  const out = [plan.restart ? `${label} 会重启一下，中间有一小段时间用不了，数据不动。没成功会自动退回原样。` : `${label} 不用重启，改完马上生效。`]
  if (plan.dependents && plan.dependents.length > 0) out.push('用着它的服务的应用，设置之后也要再跟上一次。')
  return out
}

// behindSummary：一个应用落后了，一句话说是哪几项（“打开的地址、时区”）。
export function behindSummary(a: AffectedApp): string {
  const names = [...new Set(visibleChanges(a.vars).map(changeName))]
  return names.length ? names.join('、') : '设置'
}

// reapplyForAI：复制给 AI 工作台的文字（编号、命令都在这里）。
export function reapplyForAI(app: string, label: string, what: string, detail: string): string {
  return `AcornFox 上的应用 ${label}（${app}）${what}。${detail}\n请帮我处理，可以先运行 acornfox apps vars ${app} 和 acornfox apps reapply ${app} --plan 看现在的情况。`
}
