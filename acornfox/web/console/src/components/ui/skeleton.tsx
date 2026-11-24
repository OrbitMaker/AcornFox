// 等数据时的占位形状（样稿第 12 版，照“加载状态”那一期）。全站只用这一套：
//   <Sk className="h-3 w-24" />            一块形状（一行字、一个按钮）；round 是圆的（头像、圆点）
//   <Loading when={!data} skeleton={…}>    内容区的等待：不依赖数据的（标题、页签、按钮）照常画在它外面；
//       {() => 真内容}                      头 300 毫秒什么都不画（很快就来的不闪一下）；过了 300 毫秒画占位形状，至少留 500 毫秒；
//   </Loading>                             数据来了，真内容在原位淡入（从上到下，每块 200 毫秒、相隔 40 毫秒），版面不跳。
// 占位形状要按真内容的位置和大小画：同样的行高、同样大的图标块，字的那一行比真字短一点、长短错开。
// 各页的占位形状在 components/skeletons.tsx。亮带、淡入的样子在 index.css 的 .sk、.sk-zone、.sk-reveal；“减少动效”时没有亮带。
// 刷新已有的数据不用它：旧数据照常显示。空页面正中放一个转圈的写法不要再用。
import * as React from 'react'
import { cn } from '@/lib/utils'
import { useDelayedFlag } from '@/lib/hooks'

export function Sk({ className, round }: { className?: string; round?: boolean }) {
  return <span className={cn('sk', round && 'sk-round', className)} aria-hidden="true" />
}

// useSkeleton：一处等待现在该画什么。show 画占位形状；wait 还不能画真内容（数据没到，或占位形状还没留够 500 毫秒）；
// reveal 真内容是接在占位形状后面出来的（要淡入）。<Loading> 用它；整页提前 return 的页面也可以直接用：
//   const sk = useSkeleton(!data)
//   if (sk.wait) return <SkZone show={sk.show}><SkStore /></SkZone>
//   return <div className={cn(sk.reveal && 'sk-reveal')}>…</div>
export function useSkeleton(when: boolean) {
  const show = useDelayedFlag(when, 300, 500)
  const had = React.useRef(false)
  if (show) had.current = true
  return { show, wait: when || show, reveal: had.current && !show }
}

// SkZone：占位形状外面那一层。show 是 false 时（头 300 毫秒）什么都不画。
export function SkZone({ show = true, children, className }: { show?: boolean; children: React.ReactNode; className?: string }) {
  // 头 300 毫秒什么都不画，也不留空壳（留一个空的兄弟节点会让 divide-y 这类“相邻才画线”的样式多画一条线）。
  if (!show) return null
  return (
    <div className={cn('sk-zone', className || 'contents')} aria-busy="true" data-testid="skeleton">
      <span className="sr-only">正在读取</span>
      {children}
    </div>
  )
}

export function Loading({ when, skeleton, children, className }: {
  when: boolean
  skeleton: React.ReactNode
  // 真内容；写成函数时只在数据到了以后才算（里面可以放心用数据）
  children: React.ReactNode | (() => React.ReactNode)
  // 包在外面的那一层的类名；默认 contents（不占版面，里面的东西照原来的排法）
  className?: string
}) {
  const sk = useSkeleton(when)
  if (sk.wait) return <SkZone show={sk.show} className={className}>{skeleton}</SkZone>
  const body = typeof children === 'function' ? children() : children
  // 只用来放占位形状（真内容在别处画）时，数据到了就什么都不留。
  if (body === null || body === undefined || body === false) return null
  return <div className={cn(sk.reveal && 'sk-reveal', className || 'contents')}>{body}</div>
}
