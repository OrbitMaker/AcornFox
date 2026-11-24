// 首页（样稿第 14 版：“首页不需要显示我的应用，就是显示一些图表状态就行了。”“首页的这个模块也不要”——指“需要你处理”那一块）：
//   有应用时只有图表看板（chart-board.tsx）：应用状态、CPU、内存、磁盘、一条 CPU 曲线、最近发生的事、各应用用量；能编辑。
//   要处理的事不在首页单独摆一块了，入口有三个：右上角铃铛（琥珀点，一件一句话、“去看看”）、左侧“我的应用”后面的琥珀点、
//   看板里“应用状态”那张的“要处理 N”（能点，去“我的应用”）。
// 刚装好、还没有应用的机器仍是“第 1 步　装一个应用 / 第 2 步　打开它”，下面是这台机器三行。
// 接口：GET /v1/apps（共享数据）；GET /v1/metrics/host（10 秒，只在没有应用时用）；看板自己的接口见 chart-board.tsx。
import { api } from '@/lib/api'
import { usePoll } from '@/lib/hooks'
import type { HostView } from '@/lib/types'
import { useConsole } from '@/state'
import { Loading } from '@/components/ui/skeleton'
import { SkHome } from '@/components/skeletons'
import { ChartBoard } from './chart-board'
import { FirstSteps, MachineBlock } from './home-blocks'

export function DashboardPage() {
  const { apps } = useConsole()
  const empty = !!apps && apps.length === 0
  const host = usePoll((s) => (empty ? api.get<HostView>('/v1/metrics/host', s).catch(() => null) : Promise.resolve(null)), 10000, [empty])
  const hostLoading = host.loading && !host.data
  return (
    <Loading when={!apps} skeleton={<SkHome />} className="flex flex-col gap-6">
      {() => (empty ? (
        <>
          <FirstSteps />
          <MachineBlock host={host.data} loading={hostLoading} />
        </>
      ) : (
        <div data-testid="home-charts"><ChartBoard /></div>
      ))}
    </Loading>
  )
}
