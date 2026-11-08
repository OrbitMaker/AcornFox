// ⌘K 命令面板：跳到页面、打开我的应用、从应用中心安装。
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { Search, Sparkles } from 'lucide-react'
import { cn } from '@/lib/utils'
import { appHref, go } from '@/lib/router'
import { deriveStatus, STATUS } from '@/lib/appstatus'
import { useConsole } from '@/state'
import { Dialog } from '../ui/dialog'
import { Tile } from '../bits'
import { NAV } from './sidebar'

export interface PaletteItem { group: string; label: string; hint?: string; icon: ReactNode; keywords?: string; run: () => void }

export function filterItems(items: PaletteItem[], q: string): PaletteItem[] {
  const s = q.trim().toLowerCase()
  if (!s) return items
  return items.filter((x) => (x.label + ' ' + (x.hint || '') + ' ' + (x.keywords || '')).toLowerCase().includes(s))
}

export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { apps, catalog, installing, install } = useConsole()
  const [q, setQ] = useState('')
  const [i, setI] = useState(0)
  const listRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (open) { setQ(''); setI(0) }
  }, [open])

  const items = useMemo<PaletteItem[]>(() => {
    const out: PaletteItem[] = NAV.map(({ key, label, icon: Icon }) => ({ group: '页面', label, icon: <Icon className="size-3.5" />, run: () => go('#' + key) }))
    for (const a of apps || []) {
      out.push({ group: '我的应用', label: a.name, hint: STATUS[deriveStatus(a, installing)].text, icon: <Tile id={a.catalog_id || a.name} name={a.name} size="xs" />, run: () => go(appHref(a.name)) })
    }
    for (const r of catalog?.recipes || []) {
      out.push({ group: '安装', label: r.title, hint: r.command, keywords: (r.keywords || []).join(' '), icon: <span className="flex size-6 items-center justify-center"><Sparkles className="size-3.5" /></span>, run: () => install({ kind: 'recipe', id: r.id }) })
    }
    for (const a of catalog?.apps || []) {
      if (a.installed.length > 0 || !a.supported) continue
      out.push({ group: '安装', label: a.name, hint: a.command, keywords: [a.id, a.summary, ...(a.keywords || [])].join(' '), icon: <Tile id={a.id} name={a.name} size="xs" />, run: () => install({ kind: 'app', id: a.id }) })
    }
    return out
  }, [apps, catalog, installing, install])

  const list = filterItems(items, q)
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
        <Search className="size-3.5 text-muted-foreground" />
        <input
          data-autofocus
          value={q}
          onChange={(e) => { setQ(e.target.value); setI(0) }}
          onKeyDown={onKey}
          placeholder="搜索页面、应用，或输入要装的应用"
          aria-label="搜索"
          className="flex h-10 w-full bg-transparent pr-6 text-sm outline-none placeholder:text-muted-foreground"
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
                  className={cn('flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm', i === idx && 'bg-accent')}
                >
                  {x.icon}
                  <span className="flex-1 truncate">{x.label}</span>
                  {x.hint && <span className="mono hidden truncate text-2xs text-muted-foreground sm:inline">{x.hint}</span>}
                </button>
              </div>
            )
          })
        )}
      </div>
    </Dialog>
  )
}
