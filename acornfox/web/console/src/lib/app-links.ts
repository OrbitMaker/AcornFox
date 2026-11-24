// 一个应用和别的应用之间的连接：它用着谁的服务、谁在用它提供的服务。
// 数据只来自 GET /v1/services（每条服务的 source、app、used_by、kind_label），这里只做整理，结论（能不能用）沿用 lib/services.ts。
// 用在应用概览的“它用到的服务”卡片和应用设置“安装设置”里，以及删除应用时“谁正在用它”。单元测试见 app-links.test.ts。
// 给人看的句子（评估 E2）：“PostgreSQL 数据库用的是：PostgreSQL 16”，用户自己添加的写“发信邮箱用的是：你添加的“我的邮箱””。
import { useEffect, useState } from 'react'
import { kindLabel, listServices, serviceStatus, usedByApps, type StatusText } from './services'
import type { ServiceView } from './types'

// UsesLink：这个应用用着的一条服务。provider 是提供它的应用（用户自己登记的为空，这时 service 是登记时起的名字）。
export interface UsesLink { kind: string; service: string; provider: string; status: StatusText }

// UsedByLink：在用这个应用提供的服务的一个应用。
export interface UsedByLink { kind: string; service: string; app: string }

export interface AppLinks { uses: UsesLink[]; usedBy: UsedByLink[] }

// appLinks：从服务列表里挑出和 app 有关的。
//   用着的：used_by 里有它（已删、数据还留着的不算）。
//   提供的：source 是 app 而且 app 是它；每个在用它的应用（同样不算已删的）一行。
export function appLinks(list: ServiceView[] | null | undefined, app: string): AppLinks {
  const uses: UsesLink[] = []
  const usedBy: UsedByLink[] = []
  for (const s of list || []) {
    const kind = kindLabel(s.kind_label)
    if (usedByApps(s).includes(app)) {
      uses.push({ kind, service: s.name, provider: s.source === 'app' ? s.app || '' : '', status: serviceStatus(s) })
    }
    if (s.source === 'app' && s.app === app) {
      for (const user of usedByApps(s)) if (user !== app) usedBy.push({ kind, service: s.name, app: user })
    }
  }
  return { uses, usedBy }
}

export function hasLinks(l: AppLinks | null | undefined): l is AppLinks {
  return !!l && (l.uses.length > 0 || l.usedBy.length > 0)
}

// usesPurpose：“PostgreSQL 数据库用的是”这一截。说法只用服务类型自带的中文名（目录里定义的），页面不为某一种服务类型写专门的话。
export function usesPurpose(u: UsesLink): string {
  return u.kind + '用的是'
}

// usesWho：冒号后面那一截。提供方是装好的应用时给它的显示名（name 把代号换成应用中心里的名字），
// 用户自己添加的写“你添加的“名字””。
export function usesWho(u: UsesLink, name: (app: string) => string = (x) => x): string {
  return u.provider ? name(u.provider) : `你添加的“${u.service}”`
}

// usesText：一整句“PostgreSQL 数据库用的是：PostgreSQL 16”。
export function usesText(u: UsesLink, name?: (app: string) => string): string {
  return usesPurpose(u) + '：' + usesWho(u, name)
}

// useAppLinks：读服务列表并整理出和 app 有关的连接。旧服务端没有服务接口（404）或读不出来时是 null（页面不显示）。
export function useAppLinks(app: string): AppLinks | null {
  const [links, setLinks] = useState<AppLinks | null>(null)
  useEffect(() => {
    const ctl = new AbortController()
    listServices(ctl.signal)
      .then((v) => { if (!ctl.signal.aborted) setLinks(v ? appLinks(v.services, app) : null) })
      .catch(() => { if (!ctl.signal.aborted) setLinks(null) })
    return () => ctl.abort()
  }, [app])
  return links
}
