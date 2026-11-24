// ⌘K 搜索面板（全站唯一的搜索）：跳到页面、打开我的应用、去应用中心里的某个应用或解决方案；最后一行“在应用中心里找…”带着搜索词去应用中心。
// 样稿第 10 版（评估 B3、X3）：我的应用那组用应用中心里的名字和真图标（代号放进 keywords，照样搜得到），旁边写状态；
// 应用中心的应用和解决方案旁边写它的一句话（show.tagline，没有就 summary），不再显示命令；只装一个应用、什么都没加的配方不列（和应用中心一致）。
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { Search } from 'lucide-react'
import { cn } from '@/lib/utils'
import { catalogAppOf } from '@/lib/catalog'
import { appHref, go, itemHref } from '@/lib/router'
import { deriveStatus } from '@/lib/appstatus'
import { displayName, plainStatus } from '@/lib/display'
import { useConsole } from '@/state'
import { isSolution } from '@/pages/store/shelf'
import { Dialog } from '../ui/dialog'
import { Tile } from '../bits'
import { PALETTE_PAGES } from './sidebar'

export interface PaletteItem { group: string; label: string; hint?: string; icon: ReactNode; keywords?: string; run: () => void }

export function filterItems(items: PaletteItem[], q: string): PaletteItem[] {
  const s = q.trim().toLowerCase()
  if (!s) return items
  return items.filter((x) => (x.label + ' ' + (x.hint || '') + ' ' + (x.keywords || '')).toLowerCase().includes(s))
}

export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { apps, catalog, installing } = useConsole()
  const [q, setQ] = useState('')
  const [i, setI] = useState(0)
  const listRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (open) { setQ(''); setI(0) }
  }, [open])

  const items = useMemo<PaletteItem[]>(() => {
    const out: PaletteItem[] = PALETTE_PAGES.map(({ href, label, icon: Icon }) => ({ group: '页面', label, icon: <Icon className="size-icon" />, run: () => go(href) }))
    for (const a of apps || []) {
      const name = displayName(catalog, a)
      const c = catalogAppOf(catalog, a)
      out.push({ group: '我的应用', label: name, hint: plainStatus(a, deriveStatus(a, installing)).text, keywords: [a.name, a.catalog_id || ''].join(' '), icon: <Tile id={a.catalog_id || a.name} name={name} icon={c?.icon} iconType={c?.icon_type} size="xs" />, run: () => go(appHref(a.name)) })
    }
    for (const r of catalog?.recipes || []) {
      if (!isSolution(r)) continue
      const first = (catalog?.apps || []).find((x) => x.id === r.apps[0]?.app)
      out.push({ group: '解决方案', label: r.title, hint: r.show?.tagline || r.summary, keywords: [r.id, r.summary, ...(r.keywords || [])].join(' '), icon: <Tile id={r.id} name={r.title} icon={first?.icon} iconType={first?.icon_type} size="xs" />, run: () => go('#solution-' + encodeURIComponent(r.id)) })
    }
    for (const a of catalog?.apps || []) {
      if (a.installed.length > 0 || !a.supported) continue
      out.push({ group: '应用中心', label: a.name, hint: a.show?.tagline || a.summary, keywords: [a.id, a.summary, ...(a.keywords || [])].join(' '), icon: <Tile id={a.id} name={a.name} icon={a.icon} iconType={a.icon_type} size="xs" />, run: () => go(itemHref(a.id)) })
    }
    return out
  }, [apps, catalog, installing])

  // 最后总有一行“在应用中心里找…”：带着搜索词去应用中心（那一页读地址里的 q 来过滤）。
  const kw = q.trim()
  const list = filterItems(items, q).concat(kw ? [{ group: '应用中心', label: '在应用中心里找“' + kw + '”', icon: <span className="flex size-6 items-center justify-center"><Search className="size-icon" /></span>, run: () => go('#store?q=' + encodeURIComponent(kw)) }] : [])
  const pick = (x: PaletteItem) => { onClose(); window.setTimeout(x.run, 10) }
  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setI((v) => Math.min(list.length - 1, v + 1)) }
    if (e.key === 'ArrowUp') { e.preventDefault(); setI((v) => Math.max(0, v - 1)) }
    if (e.key === 'Enter' && list[i]) { e.preventDefault(); pick(list[i]) }
  }
  useEffect(() => {
    listRef.current?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [i])

  let lastGroup = ''
  return (
    <Dialog open={open} onClose={onClose} className="overflow-hidden p-0 sm:max-w-lg" label="搜索">
      <div className="flex items-center gap-2 border-b px-3">
        <Search className="size-icon text-muted-foreground" />
        <input
          data-autofocus
          value={q}
          onChange={(e) => { setQ(e.target.value); setI(0) }}
          onKeyDown={onKey}
          placeholder="搜索我的应用、应用中心，或要去的页面"
          aria-label="搜索"
          className="flex h-10 w-full bg-transparent pr-6 text-field outline-none placeholder:text-muted-foreground"
        />
      </div>
      <div ref={listRef} className="max-h-[340px] overflow-y-auto p-1">
        {list.length === 0 ? (
          <div className="py-6 text-center text-xs text-muted-foreground">没有结果</div>
        ) : (
          list.map((x, idx) => {
            const head = x.group !== lastGroup
            lastGroup = x.group
            return (
              <div key={x.group + x.label + idx}>
                {head && <div className="px-2 pb-0.5 pt-2 text-2xs text-muted-foreground">{x.group}</div>}
                <button
                  type="button"
                  data-active={i === idx}
                  onMouseEnter={() => setI(idx)}
                  onClick={() => pick(x)}
                  className={cn('flex min-h-ctl w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm', i === idx && 'bg-accent')}
                >
                  {x.icon}
                  <span className="flex-1 truncate">{x.label}</span>
                  {x.hint && <span className="hidden max-w-[55%] truncate text-2xs text-muted-foreground sm:inline">{x.hint}</span>}
                </button>
              </div>
            )
          })
        )}
      </div>
    </Dialog>
  )
}
