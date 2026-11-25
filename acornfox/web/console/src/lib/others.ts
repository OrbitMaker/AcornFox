// 这台机器上不是从 AcornFox 装的容器（用户自己 docker run、docker compose 装的）：只读，只显示，不管理。
// 接口 GET /v1/host/containers（服务端 internal/apiserver/others.go）；旧服务端没有这个接口、或 Docker 读不到时当作“没有这一组”。
// 用在两处：“我的应用”最下面默认收起的一组（pages/apps/others-group.tsx）、首页各应用内存和 CPU 图里合成的一项“其他容器”（lib/dashboard.ts）。
import { api } from './api'
import { usePoll } from './hooks'
import { mbText } from './format'
import type { OtherContainer, OtherContainersView } from './types'

export const OTHERS_NAME = '其他容器'

// fetchOthers：读不到（旧服务端、Docker 没回应）是 null，不报错、不显示这一组。
export function fetchOthers(signal?: AbortSignal): Promise<OtherContainersView | null> {
  return api.get<OtherContainersView>('/v1/host/containers', signal).catch(() => null)
}

// useOthers：每 15 秒读一次；页面在后台时暂停。
export function useOthers(ms = 15000) {
  return usePoll((s) => fetchOthers(s), ms)
}

export type OtherTone = 'success' | 'muted' | 'warning'

// otherState：一句人话和圆点的颜色。别人的容器出了什么事不算“要处理”，只有反复重启用琥珀提醒一下。
export function otherState(c: Pick<OtherContainer, 'state' | 'running'>): { text: string; tone: OtherTone } {
  switch (c.state) {
    case 'running': return { text: '在跑', tone: 'success' }
    case 'paused': return { text: '暂停了', tone: 'muted' }
    case 'restarting': return { text: '在反复重启', tone: 'warning' }
    case 'created': return { text: '没启动过', tone: 'muted' }
  }
  return { text: c.running ? '在跑' : '停了', tone: c.running ? 'success' : 'muted' }
}

// otherPorts：对外的端口一句话；没有对外端口的不写。
export function otherPorts(c: Pick<OtherContainer, 'ports' | 'host_network'>): string {
  if (c.host_network) return '用本机的网络'
  const hosts = [...new Set((c.ports || []).map((p) => p.host))]
  return hosts.length ? '端口 ' + hosts.join('、') : ''
}

// otherUsage：在跑、读到了用量的才写。
export function otherUsage(c: Pick<OtherContainer, 'stats_available' | 'memory_usage_mb'>): string {
  return c.stats_available ? mbText(c.memory_usage_mb) : ''
}

// othersSummary：收起时的一句话：“在跑 2 个，停了 1 个，用了 120 MB 内存”。
export function othersSummary(v: Pick<OtherContainersView, 'running' | 'stopped' | 'memory_mb' | 'containers'>): string {
  const parts: string[] = []
  if (v.running > 0) parts.push(`在跑 ${v.running} 个`)
  if (v.stopped > 0) parts.push(`停了 ${v.stopped} 个`)
  const usage = v.memory_mb > 0 && v.containers.some((c) => c.stats_available) ? `用了 ${mbText(v.memory_mb)} 内存` : ''
  return parts.join('，') + (usage ? (parts.length ? '，' : '') + usage : '')
}

// othersForChart：首页“各应用内存、CPU”图里合成的一项：在跑、读到用量的加起来。没有就不加这一项。
export function othersForChart(v: OtherContainersView | null | undefined, kind: 'memory' | 'cpu'): number | null {
  if (!v || v.running <= 0 || !v.containers.some((c) => c.running && c.stats_available)) return null
  if (kind === 'memory') return Math.round(v.memory_mb * 10) / 10
  if (!v.containers.some((c) => c.running && c.stats_available && c.cpu_available)) return null
  return Math.round(v.cpu_percent * 100) / 100
}
