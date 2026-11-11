// 控制台调接口的唯一入口：带 Cookie 会话，非 GET 请求带 CSRF 头（和旧控制台一样）。
// 错误统一成 ApiError：HTTP 状态、错误编号、说明，应用中心的错误还带建议和任务编号；
// 原因是“AcornFox 版本不够”的带 needsPlatform（要升到的版本，服务端的 needs_platform）。
import type { Session } from './types'

export class ApiError extends Error {
  status: number
  code: string
  hint?: string
  taskId?: string
  diagnosis?: unknown
  needsPlatform: string
  constructor(status: number, code: string, message: string, extra?: { hint?: string; task_id?: string; diagnosis?: unknown; needs_platform?: unknown }) {
    super(message)
    this.status = status
    this.code = code
    this.hint = extra?.hint
    this.taskId = extra?.task_id
    this.diagnosis = extra?.diagnosis
    this.needsPlatform = typeof extra?.needs_platform === 'string' ? extra.needs_platform.trim() : ''
  }
}

export async function toApiError(r: Response): Promise<ApiError> {
  let code = 'server_error'
  let message = '服务器出错（HTTP ' + r.status + '）'
  let extra: { hint?: string; task_id?: string; diagnosis?: unknown; needs_platform?: unknown } | undefined
  try {
    const body = await r.json()
    const e = body && (body.error || body)
    if (e && typeof e === 'object') {
      if (typeof e.code === 'string') code = e.code
      if (typeof e.message === 'string') message = e.message
      extra = { hint: e.hint, task_id: e.task_id, diagnosis: e.diagnosis, needs_platform: e.needs_platform }
    }
  } catch {
    /* 不是 JSON */
  }
  return new ApiError(r.status, code, message, extra)
}

type Listener = (e: ApiError) => void

class Api {
  csrf = ''
  private expiredListeners: Listener[] = []

  onSessionExpired(fn: Listener) {
    this.expiredListeners.push(fn)
  }

  private async check(r: Response) {
    if (r.ok) return
    const e = await toApiError(r)
    if (r.status === 401) this.expiredListeners.forEach((fn) => fn(e))
    throw e
  }

  async session(): Promise<Session> {
    const s = await this.get<Session>('/v1/console/session')
    this.csrf = s.csrf || ''
    return s
  }

  async get<T>(path: string, signal?: AbortSignal): Promise<T> {
    const r = await fetch(path, { headers: { Accept: 'application/json' }, credentials: 'same-origin', signal })
    await this.check(r)
    return r.json() as Promise<T>
  }

  async send<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json', 'Content-Type': 'application/json' }
    if (this.csrf) headers['X-AcornFox-CSRF'] = this.csrf
    const r = await fetch(path, {
      method,
      headers,
      credentials: 'same-origin',
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    await this.check(r)
    if (r.status === 204) return {} as T
    return r.json().catch(() => ({})) as Promise<T>
  }

  post<T>(path: string, body: unknown = {}) { return this.send<T>('POST', path, body) }
  put<T>(path: string, body: unknown) { return this.send<T>('PUT', path, body) }
  patch<T>(path: string, body: unknown) { return this.send<T>('PATCH', path, body) }
  del<T>(path: string) { return this.send<T>('DELETE', path) }
}

export const api = new Api()

export const appPath = (name: string, rest = '') => '/v1/apps/' + encodeURIComponent(name) + rest
