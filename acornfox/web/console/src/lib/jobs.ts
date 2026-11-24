// 后台任务做完以后怎么告诉人（样稿第 14 版，照“提示还是通知”那一期的判断）。
// 后台任务：从应用中心安装、重装一遍、退回某一版——点了以后要过一会儿才有结果。
//   人还在这一页（这个应用的详情、这个应用在应用中心的那一页，或安装弹窗还开着）：结果在原位出现（状态、按钮、弹窗里的字自己变），不另外告知；
//   人已经去了别的页：记成一条“刚做完的”，放在右上角铃铛里“需要你处理”的下面，铃铛上多一个灰点；点开过铃铛、关上以后就清掉。
//   没做成的不在这里：它是要人处理的事，本来就会进“需要你处理”。
// 只记在这次打开的页面里（刷新就没有了；发生过什么在首页“最近发生的事”里一直能看到）。
// 这里是纯函数和类型，取数和状态在 state.tsx（ConsoleProvider）；铃铛在 components/shell/header.tsx。
import { PENDING } from './appstatus'
import { appHref, parseHash } from './router'
import type { AppView, TaskView } from './types'

export type JobKind = 'redeploy' | 'rollback' | 'update'
export interface DoneItem { key: string; title: string; href?: string; at: number }
// 一件点过的后台任务：from 是点的那一刻最新一次部署的编号（新的一次部署出现以后才开始算），detail 是“第 3 版”这类补充。
export interface Job { kind: JobKind; from: string; detail?: string; at: number }

const DONE_WORD: Record<JobKind, (detail?: string) => string> = {
  redeploy: () => '重装好了',
  rollback: (d) => (d ? '退回到' + d + '了' : '退回好了'),
  update: () => '更新好了',
}

// jobStep：一件后台任务现在到哪了。wait 还没出结果；ok 做成了；gone 不用再看（没做成、应用没了、等太久）。
export function jobStep(job: Job, a: AppView | undefined, now: number): 'wait' | 'ok' | 'gone' {
  if (!a) return 'gone'
  const d = a.latest
  if (!d || d.id === job.from) return now - job.at > 120000 ? 'gone' : 'wait'
  if (PENDING.has(d.status)) return 'wait'
  return d.status === 'live' ? 'ok' : 'gone'
}

// lookingAt：人现在是不是正看着这个应用（它的详情页）。hash 是地址栏 # 那一段。
export function lookingAtApp(hash: string, app: string): boolean {
  const r = parseHash(hash)
  return r.page === 'app' && r.id === app
}

// lookingAtItem：人现在是不是正看着应用中心里这个应用或解决方案的那一页。
export function lookingAtItem(hash: string, kind: string, id: string): boolean {
  const r = parseHash(hash)
  return (kind === 'recipe' ? r.page === 'solution' : r.page === 'item') && r.id === id
}

export function jobDone(job: Job, app: string, name: string, deploymentId: string, now: number): DoneItem {
  return { key: 'j-' + deploymentId, title: name + ' ' + DONE_WORD[job.kind](job.detail), href: appHref(app), at: now }
}

// taskDone：一次安装做成了。装了几个应用的解决方案写“一起装好了”，点了去第一个新装的应用。
export function taskDone(t: TaskView, name: string, now: number): DoneItem {
  const first = (t.apps || []).find((a) => !a.reused) || (t.apps || [])[0]
  return { key: 't-' + t.id, title: name + (t.kind === 'recipe' ? ' 一起装好了' : ' 装好了'), href: first ? appHref(first.app) : '#apps', at: now }
}

// doneTime：铃铛里那一行右边的时间（刚刚、几分钟前、几点几分）。
export function doneTime(at: number, now: number): string {
  const min = Math.floor((now - at) / 60000)
  if (min < 1) return '刚刚'
  if (min < 60) return min + ' 分钟前'
  const d = new Date(at)
  return String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0')
}
