// 从应用视图推出页面上的状态，规则和旧控制台一致：
// 期望停止 → 已停止；最新部署在进行 → 部署中；最新部署失败 → 部署失败；容器在跑 → 运行中；
// 容器停了或不在 → 没在运行；读不到 Docker → 状态未知。另外，应用中心正在装的 → 安装中。
import type { AppView, Deployment } from './types'

export type AppStatus = 'running' | 'stopped' | 'deploying' | 'installing' | 'failed' | 'crashed' | 'unknown' | 'empty'

export const PENDING = new Set(['queued', 'building', 'starting', 'checking', 'routing'])

export const STATUS: Record<AppStatus, { text: string; tone: 'success' | 'muted' | 'warning' | 'danger' }> = {
  running: { text: '运行中', tone: 'success' },
  stopped: { text: '已停止', tone: 'muted' },
  deploying: { text: '部署中', tone: 'warning' },
  installing: { text: '安装中', tone: 'warning' },
  failed: { text: '部署失败', tone: 'danger' },
  crashed: { text: '没在运行', tone: 'danger' },
  unknown: { text: '状态未知', tone: 'muted' },
  empty: { text: '还没部署', tone: 'muted' },
}

export function deriveStatus(app: AppView, installing?: Set<string>): AppStatus {
  if (installing && installing.has(app.name)) return 'installing'
  if (app.desired === 'stopped') return 'stopped'
  const latest = app.latest || app.live
  if (latest && PENDING.has(latest.status)) return 'deploying'
  if (latest && latest.status === 'failed') return 'failed'
  if (!latest && !app.current_deployment) return 'empty'
  if (app.observed_state === 'running') return 'running'
  if (app.observed_state === 'stopped' || app.observed_state === 'missing') return 'crashed'
  if (app.observed_state === 'unavailable') return 'unknown'
  return 'running'
}

// oldVersionServing：最新一次部署没上线，但之前的版本还在服务。
export function oldVersionServing(app: AppView): boolean {
  const latest = app.latest
  if (!latest || latest.status !== 'failed') return false
  if (app.live) return app.live.id !== latest.id && app.live.status === 'live'
  return !!app.current_deployment && app.current_deployment !== latest.id
}

export function failedDeployment(app: AppView): Deployment | null {
  const d = app.latest || app.live
  return d && d.status === 'failed' ? d : null
}

// 交给 AI 的部署诊断，格式和 CLI、旧控制台一致。
export function deployDiagnosisForAI(app: AppView): string {
  const d = failedDeployment(app)
  if (!d || !d.diagnosis) return ''
  const payload = {
    stage: d.diagnosis.stage,
    code: d.diagnosis.code,
    message: d.diagnosis.message,
    log_excerpt: d.diagnosis.log_excerpt || '',
    hint: d.diagnosis.hint || '',
    app: app.name,
    deployment_id: d.id,
  }
  return '请根据以下 AcornFox 部署诊断修复项目，然后重新执行 acornfox deploy：\n' + JSON.stringify(payload, null, 2)
}

// liveSeq：正在服务的是第几版，没有时是 0。列表接口没有完整的 live，只带 live_seq；
// 更早的服务端连 live_seq 都没有时，最新一版就是当前版本的也能认出来。
export function liveSeq(app: AppView): number {
  if (app.live) return app.live.seq
  if (app.live_seq) return app.live_seq
  if (app.latest && app.current_deployment && app.latest.id === app.current_deployment) return app.latest.seq
  return 0
}

export function appAddress(app: AppView): string {
  return app.url || app.lan_url || ''
}

export function shortAddress(url: string): string {
  return url.replace(/^https?:\/\//, '')
}

// isLoopbackURL：http://127.0.0.1:端口 这类地址只在服务器自己身上能用，用户在自己电脑上点不开。
export function isLoopbackURL(url?: string): boolean {
  return !!url && /^https?:\/\/(127\.\d{1,3}\.\d{1,3}\.\d{1,3}|\[::1\])(:\d+)?(\/|$)/.test(url)
}

// isLocalOnly：只本机能打开的应用（对外方式 local）。
export function isLocalOnly(app: AppView): boolean {
  return !!app.local_only || app.exposure === 'local'
}

// openableAddress：用户在自己电脑上能直接点开的地址。Linux 服务器上只本机的应用没有（要按 open_hint 打开）；
// 桌面版只本机的应用是 localhost，就在同一台电脑上，能点开。
export function openableAddress(app: AppView): string {
  if (app.open_hint) return ''
  const u = appAddress(app)
  return isLoopbackURL(u) ? '' : u
}

// gatewayTokenText：应用经本机 New API 连模型时拿到的令牌不限额、不过期，提示用户去哪里设上限。
export function gatewayTokenText(app: string, provider: string): string {
  return `${app} 用的是 ${provider} 里一把不限额的令牌；要控制花费，可在 ${provider} 后台的令牌里给它设上限。`
}

// loginAddress：首次登录卡片上的地址。任务结果和首次登录接口给的是当时的地址，改了对外方式不会跟着变；
// 知道应用现在的样子时用它现在的地址（和“打开”同一来源；只本机时是 127.0.0.1，卡片上标“只本机”），
// 原地址里的路径照留。
export function loginAddress(url: string | undefined, app?: AppView): string {
  const orig = url || ''
  if (!app) return orig
  const base = openableAddress(app) || appAddress(app)
  if (!base) return orig
  try {
    const b = new URL(base)
    let rest = ''
    if (orig) {
      const o = new URL(orig)
      rest = (o.pathname === '/' ? '' : o.pathname) + o.search + o.hash
    }
    return b.origin + rest
  } catch {
    return base
  }
}
