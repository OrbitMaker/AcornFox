// 设置 → 域名（样稿第 10 版，评估 H2、H3、N9）：这一行只管这个应用自己的域名，“谁能打开”的三档只在概览最上面的卡片里。
//   两种域名：“自动给的”（应用详情里的 platform_domain，跟着“设置 › 访问”里这台机器的域名，没设就没有这一行）、
//   “你自己的”（添加、删除）。两个都有时用你自己的。状态只写“已生效”“正在生效”“没生效”（服务端的 active、ready 都算已生效）。
// 设置 → 删除应用：确认框“会删掉这个应用。☑ 留着它的数据（以后重装能接着用）”，不留数据要输入应用的代号确认；
//   被别的应用用着时按钮灰着（blockedBy，原因由设置页写在这一行里）。
// 加或删域名后，对用到应用地址的应用，服务端要用户同意重启（409，体里带计划）：页面弹确认窗，同意了再改（pages/apps/reapply.tsx）。
// 接口：GET/POST/DELETE /v1/apps/{app}/domains、DELETE /v1/apps/{app}?volumes=true|false。
import { useState } from 'react'
import { Globe } from 'lucide-react'
import { api, appPath } from '@/lib/api'
import { displayName } from '@/lib/display'
import { go } from '@/lib/router'
import type { AppView } from '@/lib/types'
import { cn } from '@/lib/utils'
import { useConsole } from '@/state'
import { ErrorLine } from '@/components/bits'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { Input, Label } from '@/components/ui/input'
import { useToast } from '@/components/ui/toast'
import type { TabProps } from './app-page'
import { useReapply } from './reapply'

const LINE = 'flex flex-wrap items-center gap-x-3 gap-y-1.5 border-t py-2'
const LABEL = 'w-20 shrink-0 text-xs text-muted-foreground'

const DOMAIN_STATE: Record<string, [string, 'success' | 'warning' | 'danger' | 'outline']> = {
  ready: ['已生效', 'success'], active: ['已生效', 'success'], pending: ['正在生效', 'outline'], failed: ['没生效', 'danger'],
}

// accessSummary：设置里“域名”那一行收起时的一句，例如“www.example.com”“n8n.afx.example.com（自动给的）”“没有，用地址打开”。
export function accessSummary(a: AppView): string {
  const ds = (a.domains || []).map((d) => d.name).filter(Boolean)
  if (ds.length) return ds.join('、')
  if (a.platform_domain?.active && a.platform_domain.host) return a.platform_domain.host + '（自动给的）'
  return '没有，用地址打开'
}

export function AccessSection({ app: a, reload }: TabProps) {
  const toast = useToast()
  const [domain, setDomain] = useState('')
  const [busy, setBusy] = useState('')
  // 没加上、没删掉：出错的话留在那个域名（或输入框）下面（样稿第 14 版，不弹提示）。key 是哪一项：dom 是输入框，dom-<域名> 是那一个域名。
  const [err, setErr] = useState<{ key: string; error: Error } | null>(null)
  const pd = a.platform_domain?.host ? a.platform_domain : undefined

  const run = async (key: string, what: string, fn: () => Promise<void>) => {
    setBusy(key)
    setErr(null)
    try { await fn() } catch (e) { setErr({ key, error: new Error(what + '：' + (e as Error).message) }) } finally { setBusy('') }
  }
  const { catalog } = useConsole()
  const flow = useReapply(a.name, displayName(catalog, a), reload)
  const addDomain = () => run('dom', '没能添加', async () => {
    const name = domain.trim().toLowerCase()
    const r = await flow.address('给 ' + displayName(catalog, a) + ' 加域名 ' + name, (mode) =>
      api.post(appPath(a.name, '/domains'), { name, ...(mode === 'confirm' ? { confirm_restart: true } : {}), ...(mode === 'skip' ? { reapply: 'skip' } : {}) }))
    setDomain('')
    if (r === 'plain') toast('已添加 ' + name, '把这个域名指向这台机器以后自动生效，地址前面会有小锁')
  })
  const removeDomain = (name: string) => run('dom-' + name, '没能删掉', async () => {
    const r = await flow.address('删掉域名 ' + name, (mode) =>
      api.del(appPath(a.name, '/domains/' + encodeURIComponent(name) + (mode === 'confirm' ? '?confirm_restart=true' : mode === 'skip' ? '?reapply=skip' : ''))))
    if (r === 'plain') toast('已删掉 ' + name)
  })

  return (
    <div className="flex flex-col" data-testid="domains">
      <div className="mb-1 text-xs text-muted-foreground">这个应用自己的域名</div>
      {pd && (
        <div className={LINE} data-testid="platform-domain">
          <span className={LABEL}>自动给的</span>
          <span className={cn('mono min-w-0 flex-1 truncate text-xs', !pd.active && 'text-muted-foreground')}>{pd.host}</span>
          <Badge variant={pd.active ? 'success' : 'outline'}>{pd.active ? '已生效' : '没生效'}</Badge>
          {!pd.active && <span className="basis-full text-2xs text-muted-foreground sm:basis-auto">{pd.note || '改成“公网”后自动生效'}</span>}
        </div>
      )}
      <div className={cn(LINE, 'items-start')} data-testid="own-domains">
        <span className={cn(LABEL, 'pt-1.5')}>你自己的</span>
        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
          {(a.domains || []).map((d) => {
            const [t, v] = DOMAIN_STATE[d.status] || ['没生效', 'outline']
            return (
              <div key={d.name} className="flex flex-wrap items-center gap-x-2 text-sm">
                <Globe className="size-icon shrink-0 text-muted-foreground" />
                <span className="mono min-w-0 flex-1 truncate text-xs">{d.name}</span>
                <Badge variant={v} title={d.diagnosis?.message}>{t}</Badge>
                <Button variant="ghost" size="sm" disabled={!!busy} onClick={() => removeDomain(d.name)} loading={busy === 'dom-' + d.name} aria-label={'删掉域名 ' + d.name}>删掉</Button>
                {err?.key === 'dom-' + d.name && <ErrorLine error={err.error} className="basis-full" />}
              </div>
            )
          })}
          <div className="flex max-w-lg flex-wrap items-center gap-2 sm:flex-nowrap">
            {(a.domains || []).length === 0 && <span className="shrink-0 pr-2 text-xs">没有</span>}
            <Input className="min-w-0 flex-1" placeholder="例如 app.example.com" aria-label="新域名" value={domain} onChange={(e) => setDomain(e.target.value)} />
            <Button variant="outline" disabled={!domain.trim() || !!busy} onClick={addDomain} loading={busy === 'dom'}>添加</Button>
          </div>
          {err?.key === 'dom' && <ErrorLine error={err.error} />}
        </div>
      </div>
      {pd && (a.domains || []).length > 0 && <div className="mt-1.5 text-2xs text-muted-foreground" data-testid="domain-precedence">两个都有，现在用你自己的。</div>}
    </div>
  )
}

// DeleteAppButton：设置最下面“删除应用”那一行右边的按钮和确认框。blockedBy：正在用它的应用（显示名），有就灰着。
export function DeleteAppButton({ app: a, blockedBy = [] }: { app: AppView; blockedBy?: string[] }) {
  const { refreshApps, refreshCatalog, refreshResources, catalog } = useConsole()
  const toast = useToast()
  const [open, setOpen] = useState(false)
  const [keep, setKeep] = useState(true)
  const [typed, setTyped] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<Error | null>(null)
  const label = displayName(catalog, a)
  const remove = async () => {
    setBusy(true)
    setErr(null)
    try {
      const r = await api.del<{ warnings?: string[] }>(appPath(a.name, '?volumes=' + (keep ? 'false' : 'true')))
      setOpen(false)
      toast('已删掉 ' + label, r.warnings?.length ? '注意：' + r.warnings.join('；') : keep ? '数据留着，以后重装能接着用' : '数据也删掉了')
      await Promise.all([refreshApps(), refreshCatalog(), refreshResources()])
      go('#apps')
    } catch (e) {
      setErr(new Error('没能删掉：' + (e as Error).message))
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <Button variant="outline" size="sm" className="text-danger hover:text-danger" disabled={blockedBy.length > 0}
        title={blockedBy.length ? blockedBy.join('、') + ' 正在用它，现在不能删' : undefined}
        onClick={() => { setKeep(true); setTyped(''); setErr(null); setOpen(true) }}>删除</Button>
      <Dialog open={open} onClose={() => setOpen(false)} labelledBy="del-title">
        <DialogHead id="del-title" title={'删除 ' + label} desc="会删掉这个应用。" />
        <label className="tap-h flex items-center gap-2 text-sm"><input type="checkbox" className="size-icon accent-[hsl(var(--primary))]" checked={keep} onChange={(e) => setKeep(e.target.checked)} />留着它的数据（以后重装能接着用）</label>
        {!keep && (
          <div className="mt-3 flex flex-col gap-1.5">
            <Label htmlFor="confirm-name">数据删掉就找不回来了。输入 <span className="mono">{a.name}</span> 确认</Label>
            <Input id="confirm-name" value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" />
          </div>
        )}
        <ErrorLine error={err} className="mt-3" />
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" onClick={() => setOpen(false)}>取消</Button>
          <Button variant="destructive" disabled={(!keep && typed !== a.name)} onClick={remove} loading={busy}>删除</Button>
        </div>
      </Dialog>
    </>
  )
}
