// “我的应用”最下面默认收起的一组：这台机器上不是从 AcornFox 装的容器（用户自己 docker run、docker compose 装的）。
// 只看，不管理：这里没有启动、停止、删除的按钮，服务端也没有这样的接口。没有这样的容器、读不到（旧服务端、Docker 没回应）时整组不显示。
// 收起时一句话（在跑几个、停了几个、用了多少内存），展开一行一个：名字、在跑还是停了、对外的端口、内存；
// 镜像名这类技术内容不上页面（命令行 acornfox other-containers 里有）。
// 接口：GET /v1/host/containers（15 秒）；规则见 lib/others.ts。
import { useState } from 'react'
import { ExpandRow } from '@/components/expand-row'
import { StatusDot, TONE_TEXT } from '@/components/bits'
import { cn } from '@/lib/utils'
import { OTHERS_NAME, otherPorts, otherState, otherUsage, othersSummary, useOthers } from '@/lib/others'

export function OthersGroup() {
  const { data } = useOthers()
  const [open, setOpen] = useState(false)
  if (!data || data.containers.length === 0) return null
  const n = data.containers.length + (data.truncated ? '+' : '')
  return (
    <section className="mt-6 rounded-lg border bg-card" data-testid="others-group" aria-label={'这台机器上的' + OTHERS_NAME}>
      <ExpandRow id="others-body" fluid name={`这台机器上的${OTHERS_NAME}（${n}）`} summary={othersSummary(data)} summaryTestId="others-summary" open={open} onToggle={() => setOpen((o) => !o)}>
        <p className="mb-2 text-xs text-muted-foreground" data-testid="others-note">
          这些不是从 AcornFox 装的，这里只显示，不管理。在 Windows、Mac 上只能看到 AcornFox 自己那套环境里的，看不到系统外面 Docker Desktop 里的。
        </p>
        <ul className="divide-y" data-testid="others-list">
          {data.containers.map((c) => {
            const st = otherState(c)
            const ports = otherPorts(c)
            const usage = otherUsage(c)
            return (
              <li key={c.id} className="flex flex-wrap items-center gap-x-3 gap-y-0.5 py-2 text-xs" data-testid="other-container">
                <span className="min-w-0 flex-1 basis-40 truncate text-sm font-medium">{c.name}</span>
                <span className={cn('inline-flex shrink-0 items-center gap-1 text-2xs font-medium', TONE_TEXT[st.tone])}>
                  <StatusDot tone={st.tone} />
                  {st.text}
                </span>
                {ports && <span className="tnum shrink-0 text-muted-foreground">{ports}</span>}
                {usage && <span className="tnum w-16 shrink-0 text-right text-muted-foreground">{usage}</span>}
              </li>
            )
          })}
        </ul>
      </ExpandRow>
    </section>
  )
}
