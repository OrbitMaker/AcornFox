// 高级：访问方式、域名、常用命令、删除应用。
// 接口：PATCH /v1/apps/{app}（exposure）、GET/POST/DELETE /v1/apps/{app}/domains、DELETE /v1/apps/{app}?volumes=true|false。
import { useState } from 'react'
import { Globe, Trash2 } from 'lucide-react'
import { api, appPath } from '@/lib/api'
import { go } from '@/lib/router'
import { useConsole } from '@/state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Select, Spinner } from '@/components/ui/controls'
import { CopyLine } from '@/components/ui/copy'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { Input, Label } from '@/components/ui/input'
import { useToast } from '@/components/ui/toast'
import type { AppView } from '@/lib/types'
import type { TabProps } from './app-page'

// 页面上能选的访问方式。应用当前是别的值（例如经 AFX 云）时只显示、不让在这里改。
const EXPOSURES: [string, string][] = [['local', '只本机'], ['lan', '局域网'], ['public', '公网']]
const EXPOSURE_TEXT: Record<string, string> = { local: '只本机', lan: '局域网', public: '公网', afx_cloud: '经 AFX 云' }
// 下拉框里“跟随默认”的取值；保存时发 exposure: ""（和 acornfox app set --expose default 一样）。
export const FOLLOW_DEFAULT = 'default'

// exposureChoices：下拉框的选项和当前选中的值。服务端告诉了平台默认（default_exposure）时多一项“跟随默认”，
// 应用没单独设过时选中它。
export function exposureChoices(a: AppView): { options: [string, string][]; current: string } {
  if (!a.default_exposure) return { options: EXPOSURES, current: a.exposure }
  const label = '跟随默认（现在是' + (EXPOSURE_TEXT[a.default_exposure] || a.default_exposure) + '）'
  return { options: [[FOLLOW_DEFAULT, label], ...EXPOSURES], current: a.exposure_is_default ? FOLLOW_DEFAULT : a.exposure }
}

// exposurePatch：选中的值换成 PATCH 的 exposure，以及改完实际是哪种（判断要不要先确认改成公网）。
export function exposurePatch(a: AppView, choice: string): { exposure: string; effective: string } {
  if (choice === FOLLOW_DEFAULT) return { exposure: '', effective: a.default_exposure || '' }
  return { exposure: choice, effective: choice }
}

const DOMAIN_STATE: Record<string, [string, 'success' | 'warning' | 'danger' | 'outline']> = {
  ready: ['已生效', 'success'], pending: ['等待证书', 'warning'], failed: ['没生效', 'danger'],
}

export function AdvancedTab({ app: a, reload }: TabProps) {
  const { refreshApps, refreshCatalog, refreshResources } = useConsole()
  const toast = useToast()
  const choices = exposureChoices(a)
  const [exposure, setExposure] = useState(choices.current)
  const [domain, setDomain] = useState('')
  const [busy, setBusy] = useState('')
  const [delOpen, setDelOpen] = useState(false)
  const [keep, setKeep] = useState(true)
  const [typed, setTyped] = useState('')
  const [pubOpen, setPubOpen] = useState(false)
  const editable = EXPOSURES.some(([v]) => v === a.exposure)

  const run = async (key: string, fn: () => Promise<void>) => {
    setBusy(key)
    try { await fn() } catch (e) { toast('没能完成', (e as Error).message, 'error') } finally { setBusy('') }
  }
  const saveExposure = () => run('exp', async () => {
    setPubOpen(false)
    await api.patch(appPath(a.name), { exposure: exposurePatch(a, exposure).exposure })
    toast('已保存访问方式', '路由马上跟着改')
    await reload()
  })
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
  const remove = () => run('del', async () => {
    const r = await api.del<{ warnings?: string[] }>(appPath(a.name, '?volumes=' + (keep ? 'false' : 'true')))
    setDelOpen(false)
    toast('已删除 ' + a.name, r.warnings?.length ? '注意：' + r.warnings.join('；') : keep ? '数据卷已保留' : '数据卷已删除')
    await Promise.all([refreshApps(), refreshCatalog(), refreshResources()])
    go('#apps')
  })

  return (
    <div className="flex flex-col gap-3">
      {editable ? (
        <Card className="flex flex-wrap items-end gap-3 p-3.5">
          <div className="flex min-w-[200px] flex-1 flex-col gap-1.5">
            <Label htmlFor="adv-exp">访问方式</Label>
            <Select id="adv-exp" value={exposure} onChange={(e) => setExposure(e.target.value)} options={choices.options} />
          </div>
          {/* 改成公网先确认一次：所有网卡都能打开，带管理后台的应用要先设好登录密码。跟随默认、默认是公网时也一样。 */}
          <Button variant="outline" disabled={exposure === choices.current || !!busy} onClick={() => (exposurePatch(a, exposure).effective === 'public' && a.exposure !== 'public' ? setPubOpen(true) : saveExposure())}>{busy === 'exp' && <Spinner />}保存</Button>
          {exposure === FOLLOW_DEFAULT && <p className="basis-full text-2xs text-muted-foreground">不单独设，用这台服务器的默认访问方式；命令行写法是 <span className="mono">acornfox app set --app {a.name} --expose default</span>。</p>}
        </Card>
      ) : (
        <Card className="flex flex-col gap-1 p-3.5" data-testid="exposure-readonly">
          <div className="text-sm font-medium">访问方式</div>
          <div className="text-sm">{EXPOSURE_TEXT[a.exposure] || a.exposure || '默认'}</div>
          <div className="text-2xs text-muted-foreground">这里不能改这种访问方式，要改用命令行 <span className="mono">acornfox app set --app {a.name} --expose …</span></div>
        </Card>
      )}
      <Dialog open={pubOpen} onClose={() => setPubOpen(false)} labelledBy="pub-title">
        <DialogHead id="pub-title" title={'把 ' + a.name + ' 改成公网访问？'} desc="改完马上生效。" />
        <ul className="flex list-disc flex-col gap-1 pl-5 text-sm">
          <li>这台服务器的所有网卡都能打开它；如果服务器有公网地址，互联网上的任何人都能访问。</li>
          <li>绑定的域名会自动申请证书。</li>
          <li>应用带管理后台时，先确认已经改掉默认密码、设好登录密码。</li>
        </ul>
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" onClick={() => setPubOpen(false)}>取消</Button>
          <Button variant="destructive" disabled={busy === 'exp'} onClick={saveExposure}>{busy === 'exp' && <Spinner />}改成公网</Button>
        </div>
      </Dialog>
      <Card className="p-3.5">
        <div className="mb-2 text-sm font-medium">域名</div>
        {(a.domains || []).length === 0 && <div className="mb-2 text-xs text-muted-foreground">没有绑定域名</div>}
        {(a.domains || []).map((d) => {
          const [t, v] = DOMAIN_STATE[d.status] || [d.status, 'outline']
          return (
            <div key={d.name} className="flex items-center gap-2 border-b py-1.5 text-sm last:border-0">
              <Globe className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="mono min-w-0 flex-1 truncate text-xs">{d.name}</span>
              <Badge variant={v} title={d.diagnosis?.message}>{t}</Badge>
              <Button variant="ghost" size="sm" disabled={!!busy} onClick={() => removeDomain(d.name)} aria-label={'删除域名 ' + d.name}>删除</Button>
            </div>
          )
        })}
        <div className="mt-2 flex gap-2">
          <Input placeholder="例如 app.example.com" aria-label="新域名" value={domain} onChange={(e) => setDomain(e.target.value)} />
          <Button variant="outline" disabled={!domain.trim() || !!busy} onClick={addDomain}>{busy === 'dom' && <Spinner />}添加</Button>
        </div>
      </Card>
      <Card className="flex flex-col gap-2 p-3.5">
        <div className="text-sm font-medium">命令行</div>
        <CopyLine text={'acornfox status ' + a.name} />
        <CopyLine text={'acornfox logs ' + a.name + ' -f'} />
      </Card>
      <Card className="flex flex-wrap items-center gap-3 border-danger/40 p-3.5">
        <div className="min-w-0 flex-1 text-sm">删除 {a.name} <span className="text-xs text-muted-foreground">· 默认保留数据卷</span></div>
        <Button variant="destructive" size="sm" onClick={() => { setKeep(true); setTyped(''); setDelOpen(true) }}><Trash2 className="size-3.5" />删除</Button>
      </Card>
      <Dialog open={delOpen} onClose={() => setDelOpen(false)} labelledBy="del-title">
        <DialogHead id="del-title" title={'删除 ' + a.name} desc="容器、路由和版本记录都会删掉。" />
        <label className="flex items-center gap-2 text-sm"><input type="checkbox" className="size-3.5 accent-[hsl(var(--primary))]" checked={keep} onChange={(e) => setKeep(e.target.checked)} />保留数据卷（以后同名重装可以接上旧数据）</label>
        {!keep && (
          <div className="mt-3 flex flex-col gap-1.5">
            <Label htmlFor="confirm-name">输入 <span className="mono">{a.name}</span> 确认连数据一起删除</Label>
            <Input id="confirm-name" value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" />
          </div>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" onClick={() => setDelOpen(false)}>取消</Button>
          <Button variant="destructive" disabled={busy === 'del' || (!keep && typed !== a.name)} onClick={remove}>{busy === 'del' && <Spinner />}删除</Button>
        </div>
      </Dialog>
    </div>
  )
}
