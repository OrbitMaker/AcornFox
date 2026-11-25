// 图标一律琥珀色（样稿第 11 版）。
// “需要你处理”：顶栏铃铛、左下角“这台机器有 N 件事要处理”用同一个来源、同一句话（样稿第 10 版，评估 B4、C2、X8）。
// 首页原来也有这一块，样稿第 14 版拿掉了（“首页的这个模块也不要”）；看板里可选的“需要你处理”那张图用的也是这个来源。
// 数据：应用和安装任务用共享数据（useConsole）；体检项 GET /v1/runtime/checks（60 秒；几处同时要时只发一次请求，30 秒内复用）。
// 规则在 lib/dashboard.ts 的 todoItems、machineTodo（纯函数）；这里只负责取数和画一行。
import * as React from 'react'
import { AlertTriangle, Bell, CircleArrowUp, Package, Unlink, Wrench } from 'lucide-react'
import { cn } from '@/lib/utils'
import { api } from '@/lib/api'
import { usePoll } from '@/lib/hooks'
import { go } from '@/lib/router'
import { machineTodo, todoItems, type ListItem } from '@/lib/dashboard'
import { displayName } from '@/lib/display'
import { useUpdateSeen } from '@/lib/update-seen'
import type { RuntimeCheck, RuntimeChecks } from '@/lib/types'
import { showCheck } from '@/pages/runtime/checkup'
import { useConsole } from '@/state'

let cache: { at: number; data: RuntimeChecks | null } | null = null
let inflight: Promise<RuntimeChecks | null> | null = null

function loadChecks(): Promise<RuntimeChecks | null> {
  if (cache && Date.now() - cache.at < 30000) return Promise.resolve(cache.data)
  if (inflight) return inflight
  inflight = api.get<RuntimeChecks>('/v1/runtime/checks')
    .catch(() => null)
    .then((d) => {
      cache = { at: Date.now(), data: d }
      inflight = null
      return d
    })
  return inflight
}

// useChecks：体检项（读不到时是 null）。
export function useChecks(): RuntimeChecks | null {
  return usePoll(() => loadChecks(), 60000).data
}

// useNameOf：应用代号、应用中心里的应用编号、解决方案编号 → 页面上显示的名字（评估 X3）。
export function useNameOf(): (id: string) => string {
  const { apps, catalog } = useConsole()
  return React.useCallback((id: string) => {
    const a = (apps || []).find((x) => x.name === id)
    if (a) return displayName(catalog, a)
    const c = (catalog?.apps || []).find((x) => x.id === id)
    if (c) return c.name
    const r = (catalog?.recipes || []).find((x) => x.id === id)
    if (r) return r.title
    return id
  }, [apps, catalog])
}

// useTodo：需要你处理的全部事（items），其中这台机器自己的（machine，体检里要处理的项）。
export function useTodo(): { items: ListItem[]; machine: RuntimeCheck[] } {
  const { apps, installing, tasks, behind, catalog, status } = useConsole()
  const updateSeen = useUpdateSeen()
  const platformUpdate = status?.platform_update
  const fetched = useChecks()
  // 展示内容这次没能用上，也算体检里一项要注意的（和“设置 › 这台机器”里体检那一行同一个说法）
  const sc = showCheck(catalog?.sync?.show)
  const checks = React.useMemo(() => (fetched && sc ? { ...fetched, items: [...fetched.items, sc] } : fetched), [fetched, sc?.code]) // eslint-disable-line react-hooks/exhaustive-deps
  const nameOf = useNameOf()
  return React.useMemo(() => ({
    items: todoItems({ apps: apps || [], installing, checks, tasks, now: new Date(), nameOf, behind, platformUpdate, updateSeen }),
    machine: machineTodo(checks),
  }), [apps, installing, checks, tasks, nameOf, behind, platformUpdate, updateSeen])
}

const ICON: Record<string, React.ComponentType<{ className?: string }>> = { alert: AlertTriangle, wrench: Wrench, package: Package, unlink: Unlink, up: CircleArrowUp }

export function TodoIcon({ it, small }: { it: ListItem; small?: boolean }) {
  const Icon = ICON[it.icon] || Bell
  return (
    <span className={cn('flex shrink-0 items-center justify-center rounded-md border', small ? 'size-6' : 'size-7', (it.tone === 'warning' || it.tone === 'danger') && 'border-warning/30 bg-warning/10 text-warning')}>
      <Icon className="size-icon" />
    </span>
  )
}

// useTodoGo：点“去看看”：安装没成功的打开安装弹窗看那一次的结果，其余跳到对应的页面。
export function useTodoGo(): (it: ListItem) => void {
  const { install } = useConsole()
  return (it: ListItem) => {
    if (it.task) install(it.task)
    else if (it.href) go(it.href)
  }
}
