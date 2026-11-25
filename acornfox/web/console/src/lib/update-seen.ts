// “AcornFox 有新版本”在铃铛里同一个版本只提醒一次（W12-3）：用户点进“设置 › 这台机器”看过升级那一行，就记下这个版本号，
// 铃铛里不再出这一条；线上再出更新的版本才会再提醒。记在这个浏览器里（localStorage；读写不了时只记在这次打开的页面里）。
import { useSyncExternalStore } from 'react'

const KEY = 'afx-update-seen'
const listeners = new Set<() => void>()
let mem = ''

function read(): string {
  try {
    return localStorage.getItem(KEY) || mem
  } catch {
    return mem
  }
}

function subscribe(fn: () => void): () => void {
  listeners.add(fn)
  window.addEventListener('storage', fn)
  return () => {
    listeners.delete(fn)
    window.removeEventListener('storage', fn)
  }
}

// markUpdateSeen：用户看过这个版本的提醒了。
export function markUpdateSeen(version: string): void {
  if (!version || read() === version) return
  mem = version
  try {
    localStorage.setItem(KEY, version)
  } catch {
    /* 存不了就只记在内存里 */
  }
  listeners.forEach((f) => f())
}

// useUpdateSeen：看过的那个版本号（没有是空串）；记下以后用到它的页面马上更新。
export function useUpdateSeen(): string {
  return useSyncExternalStore(subscribe, read, () => '')
}
