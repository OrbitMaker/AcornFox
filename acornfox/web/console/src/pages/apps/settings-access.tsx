// 设置 → 谁能打开：三档（同概览访问卡片那一套，选公网要确认；有平台默认时可以跟随默认）、域名的添加和删除。
// 设置 → 删除应用：确认框（默认保留数据卷，不保留要输入应用名）。
// 接口：PATCH /v1/apps/{app}（exposure）、GET/POST/DELETE /v1/apps/{app}/domains、DELETE /v1/apps/{app}?volumes=true|false。
import { useState } from 'react'
import { Globe } from 'lucide-react'
import { api, appPath } from '@/lib/api'
import { ACCESS_LEVELS } from '@/lib/overview'
import { go } from '@/lib/router'
import type { AppView } from '@/lib/types'
import { useConsole } from '@/state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/controls'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { Input, Label } from '@/components/ui/input'
import { useToast } from '@/components/ui/toast'
import { EXPOSURE_TEXT, ExposureSwitch, exposureEditable } from './access'
import type { TabProps } from './app-page'

const DOMAIN_STATE: Record<string, [string, 'success' | 'warning' | 'danger' | 'outline']> = {
  ready: ['已生效', 'success'], pending: ['等待证书', 'warning'], failed: ['没生效', 'danger'],
}

// accessSummary：设置里“谁能打开”那一行的摘要，例如“局域网 · 没有绑定域名”。
export function accessSummary(a: AppView): string {
  const how = (EXPOSURE_TEXT[a.exposure] || a.exposure || '默认') + (a.exposure_is_default && a.default_exposure ? '（跟随默认）' : '')
  if (a.internal_only) return how
  const ds = (a.domains || []).map((d) => d.name).filter(Boolean)
  return how + ' · ' + (ds.length ? '域名 ' + ds.join('、') : '没有绑定域名')
}

export function AccessSection({ app: a, reload }: TabProps) {
  const toast = useToast()
  const [domain, setDomain] = useState('')
  const [busy, setBusy] = useState('')
  const level = ACCESS_LEVELS.find((l) => l.key === a.exposure)

  const run = async (key: string, fn: () => Promise<void>) => {
    setBusy(key)
    try { await fn() } catch (e) { toast('没能完成', (e as Error).message, 'error') } finally { setBusy('') }
  }
  const addDomain = () => run('dom', async () => {
    const name = domain.trim().toLowerCase()
    await api.post(appPath(a.name, '/domains'), { name })
    setDomain('')
    toast('已添加域名', '把域名解析到这台服务器后会自动申请证书')
    await reload()
  })
  const removeDomain = (name: string) => run('dom-' + name, async () => {
    await api.del(appPath(a.name, '/domains/' + encodeURIComponent(name)))
    toast('已删除域名', name)
    await reload()
  })

  return (
    <div className="flex flex-col gap-3">
      {exposureEditable(a) ? (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <ExposureSwitch app={a} reload={reload} withDefault after={level && <span className="text-2xs text-muted-foreground">{level.who}</span>} />
        </div>
      ) : (
        <div className="flex flex-col gap-1" data-testid="exposure-readonly">
          <div className="text-sm">{EXPOSURE_TEXT[a.exposure] || a.exposure || '默认'}</div>
          {a.internal_only ? (
            <div className="text-2xs text-muted-foreground">{a.internal_note || '只在内部网络，给别的应用用'}：没有网页可以打开，访问方式不能改。别的应用按名字 <span className="mono">{a.name}</span> 在内部网络里连它。</div>
          ) : (
            <div className="text-2xs text-muted-foreground">这里不能改这种访问方式，要改用命令行 <span className="mono">acornfox app set --app {a.name} --expose …</span></div>
          )}
        </div>
      )}
      {!a.internal_only && (
        <div className="flex flex-col gap-1.5" data-testid="domains">
          <div className="text-2xs text-muted-foreground">域名</div>
          {(a.domains || []).length === 0 && <div className="text-xs">没有绑定域名</div>}
          {(a.domains || []).map((d) => {
            const [t, v] = DOMAIN_STATE[d.status] || [d.status, 'outline']
            return (
              <div key={d.name} className="flex items-center gap-2 border-b py-1 text-sm last:border-0">
                <Globe className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="mono min-w-0 flex-1 truncate text-xs">{d.name}</span>
                <Badge variant={v} title={d.diagnosis?.message}>{t}</Badge>
                <Button variant="ghost" size="sm" disabled={!!busy} onClick={() => removeDomain(d.name)} aria-label={'删除域名 ' + d.name}>删除</Button>
              </div>
            )
          })}
          <div className="flex max-w-md gap-2">
            <Input placeholder="例如 app.example.com" aria-label="新域名" value={domain} onChange={(e) => setDomain(e.target.value)} />
            <Button variant="outline" disabled={!domain.trim() || !!busy} onClick={addDomain}>{busy === 'dom' && <Spinner />}添加域名</Button>
          </div>
        </div>
      )}
    </div>
  )
}

// DeleteAppButton：设置最下面“删除应用”那一行右边的按钮和确认框。
export function DeleteAppButton({ app: a }: { app: AppView }) {
  const { refreshApps, refreshCatalog, refreshResources } = useConsole()
  const toast = useToast()
  const [open, setOpen] = useState(false)
  const [keep, setKeep] = useState(true)
  const [typed, setTyped] = useState('')
  const [busy, setBusy] = useState(false)
  const remove = async () => {
    setBusy(true)
    try {
      const r = await api.del<{ warnings?: string[] }>(appPath(a.name, '?volumes=' + (keep ? 'false' : 'true')))
      setOpen(false)
      toast('已删除 ' + a.name, r.warnings?.length ? '注意：' + r.warnings.join('；') : keep ? '数据卷已保留' : '数据卷已删除')
      await Promise.all([refreshApps(), refreshCatalog(), refreshResources()])
      go('#apps')
    } catch (e) {
      toast('没能完成', (e as Error).message, 'error')
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <Button variant="outline" size="sm" className="text-danger hover:text-danger" onClick={() => { setKeep(true); setTyped(''); setOpen(true) }}>删除</Button>
      <Dialog open={open} onClose={() => setOpen(false)} labelledBy="del-title">
        <DialogHead id="del-title" title={'删除 ' + a.name} desc="容器、路由和版本记录都会删掉。" />
        <label className="flex items-center gap-2 text-sm"><input type="checkbox" className="size-3.5 accent-[hsl(var(--primary))]" checked={keep} onChange={(e) => setKeep(e.target.checked)} />保留数据卷（以后同名重装可以接上旧数据）</label>
        {!keep && (
          <div className="mt-3 flex flex-col gap-1.5">
            <Label htmlFor="confirm-name">输入 <span className="mono">{a.name}</span> 确认连数据一起删除</Label>
            <Input id="confirm-name" value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" />
          </div>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" onClick={() => setOpen(false)}>取消</Button>
          <Button variant="destructive" disabled={busy || (!keep && typed !== a.name)} onClick={remove}>{busy && <Spinner />}删除</Button>
        </div>
      </Dialog>
    </>
  )
}
