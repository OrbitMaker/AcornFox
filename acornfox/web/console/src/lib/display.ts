// 样稿第 10 版的统一叫法和显示名（评估 2026-11-23 的 X2、X3）。各页面都从这里取，别各写各的。
//
// 应用的名字：从应用中心装的应用，装好以后也用应用中心里的名字（“Hermes 智能体”），英文代号（hermes-agent）只在
// “技术信息”和命令行里出现；开发者自己部署的项目没有别的名字，就用它的项目名。
import { catalogAppOf } from './catalog'
import { oldVersionServing, type AppStatus } from './appstatus'
import type { AppView, CatalogView, ServerStatus } from './types'
import { useConsole } from '@/state'

// 全站统一的叫法（改名字只改这里和用到它的地方）。
export const WORDS = {
  machine: '这台机器',
  service: '服务', // 应用要用到的、别的应用提供的东西。统一叫“服务”，不叫“配件”“连接”
  whoCanOpen: '谁能打开',
  fromStore: '来自应用中心',
  installed: '已装好',
  todo: '需要你处理',
  platformFilled: '平台自动填的',
  rollback: '退回上一版',
  addMine: '添加我已有的',
  cli: '用命令行',
  askAI: '复制给 AI 工作台',
  internalApp: '给别的应用用，不用打开',
} as const

// fromStore：从应用中心装的应用（小白管的）；不是的就是开发者自己部署的项目。
// 两种应用的详情和设置分开显示（评估 R5）：环境变量、端口、健康检查、附加服务、版本记录和回退只给后一种。
export function fromStore(a: { catalog_id?: string }): boolean {
  return !!a.catalog_id
}

export function displayName(catalog: CatalogView | null | undefined, a: { name: string; catalog_id?: string; catalog_origin?: string }): string {
  if (!a.catalog_id) return a.name
  return catalogAppOf(catalog, a)?.name || a.name
}

// useAppNames：按“装好的应用的代号”取显示名（服务页、连接、通知里只有代号时用）。
export function useAppNames(): (name: string) => string {
  const { apps, catalog } = useConsole()
  return (name: string) => {
    const a = (apps || []).find((x) => x.name === name)
    return a ? displayName(catalog, a) : name
  }
}

export function useDisplayName(a: { name: string; catalog_id?: string; catalog_origin?: string }): string {
  const { catalog } = useConsole()
  return displayName(catalog, a)
}

// 给小白看的状态只有三种：正常、停了、要处理（另有两种过渡：安装中、更新中）。
// 颜色（样稿第 11 版）：正常绿、停了灰、要处理琥珀；安装中、更新中是 busy：灰色加转圈。红色不用来表示应用的状态
// （红色只给删除和出错）。danger 留在类型里只为了别处的出错提示还能用同一套写法。
export type PlainTone = 'success' | 'muted' | 'warning' | 'danger' | 'busy'
export interface PlainStatus { text: string; tone: PlainTone; note?: string }

export function plainStatus(a: AppView, st: AppStatus): PlainStatus {
  switch (st) {
    case 'running': return { text: '正常', tone: 'success' }
    case 'stopped': return { text: '停了', tone: 'muted' }
    case 'installing': return { text: '安装中', tone: 'busy' }
    case 'deploying': return { text: '更新中', tone: 'busy' }
    case 'failed':
      return oldVersionServing(a)
        ? { text: '新版本没换上', tone: 'warning', note: '旧版本照常用' }
        : { text: '要处理', tone: 'warning', note: '没启动成功' }
    case 'crashed': return { text: '要处理', tone: 'warning', note: '停了，正在自动重新启动' }
    case 'empty': return { text: '还没装好', tone: 'muted' }
    default: return { text: '看不到状态', tone: 'muted' }
  }
}

// canOpenNow：现在点“打开”有没有东西可看。新版本没换上、旧版本还在用时也能打开（评估 Y3、E4）。
export function canOpenNow(a: AppView, st: AppStatus): boolean {
  return st === 'running' || (st === 'failed' && oldVersionServing(a))
}

// needsYou：算不算“需要你处理”的事（首页、铃铛、菜单上的琥珀点用同一个判断）。
export function needsYou(st: AppStatus): boolean {
  return st === 'failed' || st === 'crashed'
}

// problemLine：一句人话说出了什么事。故障编号、/health 这类只放进“复制给 AI 工作台”的文字里。
export function problemLine(name: string, a: AppView, st: AppStatus): string {
  if (st === 'failed') return oldVersionServing(a) ? `${name} 的新版本没启动成功，旧版本照常用` : `${name} 没启动成功`
  if (st === 'crashed') return `${name} 停了，正在自动重新启动`
  return ''
}

// onPublic：这台机器在不在公网上。家里、公司局域网里的机器不在：这是正常情况，不算问题、不计数（评估 X8），
// 只在用户去点“公网”时说一句“这台机器不在公网上”。
// 样稿里读 GET /v1/status 的 on_public（假数据；要服务端配合：现在只能从平台域名、公网控制台两个接口的 unsupported 侧面看出来）。
export function onPublic(status: ServerStatus | null | undefined): boolean {
  return status?.on_public !== false
}

// gap：把一个名字接在中文后面时用。名字以英文或数字开头才加空格（“我自己有 MySQL 数据库”“我自己有大模型接口”），免得出现“有 大模型”这种多余的空格。
export function gap(name: string): string {
  return /^[A-Za-z0-9]/.test(name) ? ' ' + name : name
}
// gapBoth：名字夹在两段中文中间时，结尾是英文或数字的后面也补一个空格。
export function gapBoth(name: string): string {
  return gap(name) + (/[A-Za-z0-9]$/.test(name) ? ' ' : '')
}
