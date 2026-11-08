// 应用的启停、重启、重新部署；按钮和菜单共用，提示文字和旧控制台一致。
import { api, appPath } from '@/lib/api'

export type AppAction = 'start' | 'stop' | 'restart' | 'redeploy'

export const ACTION_DONE: Record<AppAction, string> = {
  start: '已提交启动',
  stop: '已停止',
  restart: '已重启',
  redeploy: '已提交重新部署',
}

export async function runAction(name: string, action: AppAction) {
  await api.post(appPath(name, '/' + action), {})
}
