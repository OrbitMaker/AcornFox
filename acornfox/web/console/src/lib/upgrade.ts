// AcornFox 自身的升级（有没有新版本、这台机器该用哪条命令），和“应用要更新版本的 AcornFox 才能装”的判断。
// 都是纯函数和常量，测试见 upgrade.test.ts。
import type { AppDetail, Recipe } from './types'

// 升级命令和 README、Skill 里公开的一致（管道运行就视为同意，不用 -y）。设置的“升级”分区和应用中心的提示共用。
export const UPGRADE_CN = 'curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash -s -- upgrade'
export const UPGRADE_GLOBAL = 'curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | sudo bash -s -- upgrade'

// upgradeCommand：这台机器该用的那一条升级命令。china 取自体检结果（GET /v1/runtime/checks 的 sources.china），
// 页面知道这台机器在国内还是海外，就只给一条（评估 N13）；不知道（null）时两条都给。
export function upgradeCommand(china: boolean | null | undefined): string[] {
  if (china === true) return [UPGRADE_CN]
  if (china === false) return [UPGRADE_GLOBAL]
  return [UPGRADE_CN, UPGRADE_GLOBAL]
}

// upgradeState：现在的版本和线上最新的比（latest 取自 GET /v1/status 的 latest_version，样稿第 10 版的假字段，要服务端配合）。
//   new：有新版本；latest：已是最新；unknown：不知道线上最新是哪个（旧服务端没给）。
export function upgradeState(version: string | undefined, latest: string | undefined): 'new' | 'latest' | 'unknown' {
  if (!version || !latest) return 'unknown'
  return latest !== version && higherVersion(latest, version) === latest ? 'new' : 'latest'
}

// upgradeForAI：“复制给 AI 工作台帮我升级”复制的那段话，带上该用的命令。
export function upgradeForAI(version: string | undefined, latest: string | undefined, cmds: string[]): string {
  const to = latest && upgradeState(version, latest) === 'new' ? latest : '最新版'
  return [
    `用 acornfox skill 帮我把这台机器上的 AcornFox 从 ${version || '现在的版本'} 升级到 ${to}。`,
    cmds.length === 1 ? '在这台机器上运行（会先打印要做的事；升级没成功会自动恢复原样）：' : '这台机器在国内用第一条，在海外用第二条（会先打印要做的事；升级没成功会自动恢复原样）：',
    ...cmds,
  ].join('\n')
}

// needsPlatform：这个应用或配方要先把 AcornFox 升到哪个版本；不用升级（空串）或旧服务端没给这一项时是 ''。
export function needsPlatform(x: { needs_platform?: string } | null | undefined): string {
  return typeof x?.needs_platform === 'string' ? x.needs_platform.trim() : ''
}

// 页面上统一用这一句。
export function needsPlatformText(version: string): string {
  return `要先把 AcornFox 升级到 ${version} 才能装`
}

// higherVersion：两个版本号里取高的（按点分的数字比；带 -rc.1 这类后缀的只比前面的数字）。
export function higherVersion(a: string, b: string): string {
  if (!a) return b
  if (!b) return a
  const nums = (v: string) => v.replace(/^v/, '').split(/[-+]/)[0].split('.').map((n) => parseInt(n, 10) || 0)
  const x = nums(a)
  const y = nums(b)
  for (let i = 0; i < Math.max(x.length, y.length); i++) {
    const d = (x[i] || 0) - (y[i] || 0)
    if (d !== 0) return d > 0 ? a : b
  }
  return a
}

// installNeedsPlatform：安装弹窗用。单个应用看它自己；配方以服务端给配方算的为准，
// 服务端没给时看其中要新装的应用（已经装过、会被复用的不重装，不算）。
export function installNeedsPlatform(kind: 'app' | 'recipe', details: AppDetail[], recipe: Recipe | null): string {
  if (kind === 'app') return needsPlatform(details[0])
  const own = needsPlatform(recipe)
  if (own) return own
  let v = ''
  for (const d of details) {
    if (d.installed.length > 0) {
      const ra = recipe?.apps.find((x) => x.app === d.id)
      if (!ra || ra.reuse_installed !== false) continue
    }
    v = higherVersion(v, needsPlatform(d))
  }
  return v
}

// 错误下面的那一句（和命令行 catalog list 里的说法一样）。
export function upgradeFirstText(version: string): string {
  return `要先升级到 ${version}`
}

// faultNeedsPlatform：安装被拒绝、失败，或者应用详情读不了，原因是不是“AcornFox 版本不够”。
// 是就返回要升到的版本，页面在错误下面说“要先升级到 x.y.z”并给升级命令；不是返回 ''，页面只原样显示服务端的说明。
// 只认服务端在错误里带的 needs_platform（接口错误是 ApiError.needsPlatform，任务视图是 fault.needs_platform），
// 不看错误编号，也不看说明里的字：同一个编号可能是该升级，也可能是目录本身有问题。
export function faultNeedsPlatform(fault: object | null | undefined): string {
  if (!fault) return ''
  const f = fault as { needs_platform?: unknown; needsPlatform?: unknown }
  const v = typeof f.needs_platform === 'string' ? f.needs_platform : typeof f.needsPlatform === 'string' ? f.needsPlatform : ''
  return v.trim()
}
