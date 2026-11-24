// 应用的启停、重启、重装一遍程序；名片的“更多”菜单、我的应用卡片的菜单、“更新”页签共用，提示文字只在这里写一次。
// 接口：POST /v1/apps/{app}/start|stop|restart|redeploy。“重装一遍”就是 redeploy：用现在的程序重新装一次，数据不动。
import { api, appPath } from '@/lib/api'
import type { ToastOpt } from '@/components/ui/toast'

export type AppAction = 'start' | 'stop' | 'restart' | 'redeploy'

export const ACTION_DONE: Record<AppAction, string> = {
  start: '正在启动',
  stop: '已停了',
  restart: '已重启',
  redeploy: '正在重装一遍',
}

// 没做成时写在那一项上的话的开头（后面接服务端给的原因）。
export const ACTION_FAIL: Record<AppAction, string> = {
  start: '没能启动',
  stop: '没能停下',
  restart: '没能重启',
  redeploy: '没能重装',
}

export async function runAction(name: string, action: AppAction) {
  await api.post(appPath(name, '/' + action), {})
}

// actionToast：动作做完以后提示的第三个参数（样稿第 12 版）。“停止”能原样做回来，提示里带“撤销”（再启动起来）；别的动作没有。
// after：撤销以后要刷新哪些数据。
export function actionToast(name: string, action: AppAction, after: () => Promise<unknown>): ToastOpt | undefined {
  if (action !== 'stop') return undefined
  return { undo: async () => { await runAction(name, 'start'); await after() }, undone: '已重新启动' }
}
