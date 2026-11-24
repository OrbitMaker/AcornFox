// 应用设置里的“安装设置”：一个从应用中心装的应用是用哪些值装起来的（只读）。
// 接口：GET /v1/apps/{app}/vars（?reveal=true 给保密项的完整值）。字段的意思和 acornfox apps vars -h 一样。
// 这里只做整理：哪几项和现在的不一样、哪几项是用户自己填的、其余的怎么分组、收起时那一句话。
// 样稿第 10 版（评估 H5）：访问地址、访问方式、地址里的主机三项并成一项“打开的地址”（只留 platform.app.url）；
// 平台生成、用户不用记的密钥（会话密钥、加密密钥这类）不列（hiddenVar）。要用户记的密码照列。
// 后果和怎么办一律用服务端给的 fix、notes，这里不自己编。单元测试见 app-vars.test.ts。
import { api } from './api'
import { isUnsupported, itemLabel } from './services'

// AppVar：一项。layer 是 system（平台给的）或 custom（其余）；origin 是值从哪来（见 USER_ORIGINS）；
// masked：value 被遮住了（保密项，没带 reveal）；differs：装时的值（value）和现在算出来的（value_now）不一样，
// 这时 code 是故障编号（APP-VARS-01），fix 是服务端写的怎么办。
export interface AppVar {
  name: string
  layer: 'system' | 'custom' | string
  layer_label?: string
  class?: string
  class_label?: string
  label?: string
  value: string
  secret: boolean
  masked: boolean
  origin: string
  origin_label?: string
  editable?: string
  editable_label?: string
  // 能不能在页面上改（W8-1）：服务端按清单和平台的规则算好；edit_how 是命令行的改法，页面不显示
  can_edit?: boolean
  edit_how?: string
  used?: boolean
  value_now?: string | null
  differs?: boolean
  code?: string
  fix?: string
}

// AppVars：from_catalog 为 false 的应用（不是从应用中心装的）没有这些项，页面不显示这一行。
export interface AppVars {
  app: string
  from_catalog: boolean
  catalog_id?: string
  version?: string
  revealed?: boolean
  can_change?: boolean
  vars: AppVar[] | null
  notes: string[] | null
}

export const appVarsPath = (app: string, reveal = false) => '/v1/apps/' + encodeURIComponent(app) + '/vars' + (reveal ? '?reveal=true' : '')

// fetchAppVars：旧服务端没有这个接口（404、405）时返回 null。
export async function fetchAppVars(app: string, reveal = false, signal?: AbortSignal): Promise<AppVars | null> {
  try {
    return await api.get<AppVars>(appVarsPath(app, reveal), signal)
  } catch (e) {
    if (isUnsupported(e)) return null
    throw e
  }
}

// 用户自己填的（安装时填的，或安装时改的平台给的值）。其余的（平台算的、生成的、清单默认的、配方预填的、
// 旧版本装的分不出是谁给的）都算“自动填好的”，收在“全部显示”里。
export const USER_ORIGINS = ['user', 'platform_user']

export const isMine = (v: AppVar) => USER_ORIGINS.includes(v.origin)

// 并进“打开的地址”的两项（有 platform.app.url 时不单独列）。
const MERGED_INTO_URL = ['platform.app.scheme', 'platform.app.host']

// hiddenVar：页面上不列的项。平台生成的保密值里，名字是“密钥”的（会话密钥、加密密钥）用户用不着；密码要用户记，照列。
export function hiddenVar(v: AppVar, all: AppVar[] = []): boolean {
  if (MERGED_INTO_URL.includes(v.name) && all.some((x) => x.name === 'platform.app.url')) return true
  return v.origin === 'generated' && v.secret && /密钥/.test(v.label || '') && !/密码/.test(v.label || '')
}

// visibleVars：去掉不列的项。
export function visibleVars(list: AppVar[] | null | undefined): AppVar[] {
  const all = list || []
  return all.filter((v) => !hiddenVar(v, all))
}

// varLabel：给人看的名字。“访问地址”一律叫“打开的地址”，“控制台密码”这类叫“登录密码”。去掉括号里的补充（例如“网络区域（cn 或 global）”）和开头的“这个应用的”（这一页说的就是这个应用）。
// 清单没给中文名的项：用它在清单里的名字（去掉 params.、platform. 前缀），总比“未命名”有用；连名字都没有才写“未命名”。
export function varLabel(v: AppVar): string {
  if (v.name === 'platform.app.url') return '打开的地址'
  // “控制台密码”会和 AcornFox 控制台的密码撞名，这一页说的就是这个应用，叫“登录密码”
  if (/(控制台|后台).*密码/.test(v.label || '')) return '登录密码'
  const raw = (v.label || '').replace(/[（(][^）)]*[）)]/g, '').replace(/^这个应用的?/, '').trim()
  if (raw) return raw
  return itemLabel((v.name || '').replace(/^(params|platform)\./, ''))
}

// differsHead：几项和现在不一样时，拿哪一项当代表（收起时那句话用）：有访问地址就用它，否则用第一项。
export function differsHead(d: AppVar[]): AppVar | undefined {
  return d.find((v) => v.name === 'platform.app.url') || d[0]
}

// varValue：值。空的用服务端给的来源说明（例如“没填”）。
export function varValue(v: AppVar): string {
  return v.value !== '' ? v.value : v.origin_label || '—'
}

export interface VarGroups {
  differs: AppVar[]
  mine: AppVar[]
  platform: AppVar[]
  custom: AppVar[]
}

// groupVars：differs 是和现在不一样的（提示框）；mine 是默认列出来的；platform、custom 是“全部显示”里的两组。
export function groupVars(list: AppVar[] | null | undefined): VarGroups {
  const vars = visibleVars(list)
  const rest = vars.filter((v) => !isMine(v))
  return {
    differs: vars.filter((v) => v.differs),
    mine: vars.filter(isMine),
    platform: rest.filter((v) => v.layer === 'system'),
    custom: rest.filter((v) => v.layer !== 'system'),
  }
}

// varsSummary：收起时那一句。tone 是 warning 时带黄点。
export function varsSummary(av: AppVars): { text: string; tone: 'warning' | '' } {
  const d = groupVars(av.vars).differs
  if (d.length === 1) return { text: varLabel(d[0]) + '和现在的不一样', tone: 'warning' }
  if (d.length > 1) return { text: `${varLabel(differsHead(d)!)}等 ${d.length} 项和现在的不一样`, tone: 'warning' }
  if (visibleVars(av.vars).length === 0) return { text: '没有要你管的', tone: '' }
  return { text: '都是自动配好的，不用管', tone: '' }
}

// 服务端 notes 里有几条页面上已经有了、或是写给命令行的，不重复列：
//   以某个不一样的项的故障编号开头的那条（内容就是提示框里的 fix，还带清单里的名字）；
//   “改设置的功能还没有……”（旧服务端的话）、“改一项设置：acornfox apps set……”（写给命令行的，页面上每项有“改”）；
//   “密码和 Key 只显示后 4 位……--reveal”（页面上有“显示”）。
// 其余（旧格式清单装的、清单没存、还在安装等）原样列出来。
const SHOWN_ELSEWHERE = ['改设置的功能还没有', '改一项设置：', '密码和 Key 只显示后']

export function shownNotes(av: AppVars): string[] {
  const codes = groupVars(av.vars).differs.map((v) => v.code).filter((c): c is string => !!c)
  return (av.notes || []).filter((n) => {
    const t = n.trim()
    if (!t) return false
    if (codes.some((c) => t.startsWith(c + ' '))) return false
    return !SHOWN_ELSEWHERE.some((p) => t.startsWith(p))
  })
}

// revealedValues：带 reveal=true 读回来的完整值，按项的名字放。只放保密项。
export function revealedValues(av: AppVars | null | undefined): Record<string, string> {
  const out: Record<string, string> = {}
  for (const v of av?.vars || []) if (v.secret) out[v.name] = v.value
  return out
}
