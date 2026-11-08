// 数据：数据卷（应用自己的数据放在哪）和附加服务（PostgreSQL、MySQL、Redis）。
// 接口：GET /v1/apps/{app}（volumes、addons）、POST /v1/apps/{app}/addons、DELETE /v1/apps/{app}/addons/{kind}[?volumes=true]。
import { useState } from 'react'
import { Database, HardDrive, Plus } from 'lucide-react'
import { api, appPath } from '@/lib/api'
import { timeText } from '@/lib/format'
import type { Addon } from '@/lib/types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Spinner } from '@/components/ui/controls'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { useToast } from '@/components/ui/toast'
import type { TabProps } from './app-page'

export const ADDONS: Record<string, { name: string; desc: string }> = {
  postgres: { name: 'PostgreSQL', desc: '关系型数据库' },
  mysql: { name: 'MySQL', desc: '关系型数据库' },
  redis: { name: 'Redis', desc: '缓存和队列' },
}
const ADDON_STATE: Record<string, [string, 'success' | 'warning' | 'danger' | 'outline']> = {
  running: ['运行中', 'success'], starting: ['初始化中', 'warning'], unhealthy: ['未就绪', 'warning'], stopped: ['已停止', 'outline'], missing: ['等待创建', 'outline'], unavailable: ['读不到状态', 'outline'],
}

export function DataTab({ app: a, reload }: TabProps) {
  const toast = useToast()
  const [busy, setBusy] = useState('')
  const [removing, setRemoving] = useState<Addon | null>(null)
  const [dropData, setDropData] = useState(false)
  const addons = a.addons || []
  const volumes = a.volumes || []
  const missing = Object.keys(ADDONS).filter((k) => !addons.some((x) => x.kind === k))

  const add = async (kind: string) => {
    setBusy(kind)
    try {
      await api.post(appPath(a.name, '/addons'), { kind })
      toast('已添加 ' + ADDONS[kind].name, '应用会自动重新部署')
      await reload()
    } catch (e) {
      toast('没能添加', (e as Error).message, 'error')
    } finally {
      setBusy('')
    }
  }
  const remove = async () => {
    if (!removing) return
    const kind = removing.kind
    setBusy(kind)
    try {
      await api.del(appPath(a.name, '/addons/' + encodeURIComponent(kind) + (dropData ? '?volumes=true' : '')))
      toast(dropData ? `已关闭 ${ADDONS[kind]?.name || kind} 并删除数据` : `已关闭 ${ADDONS[kind]?.name || kind}，数据已保留`)
      setRemoving(null)
      await reload()
    } catch (e) {
      toast('没能关闭', (e as Error).message, 'error')
    } finally {
      setBusy('')
    }
  }

  return (
    <div className="grid gap-3 md:grid-cols-2">
      <Card className="p-3.5">
        <div className="mb-2 text-sm font-medium">数据卷</div>
        {volumes.length === 0 && <div className="text-xs text-muted-foreground">这个应用没有数据卷，容器重建后里面写的文件会丢</div>}
        {volumes.map((v) => (
          <div key={v.volume_name} className="flex items-center gap-2 py-1 text-sm">
            <HardDrive className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="mono min-w-0 flex-1 truncate text-xs" title={v.volume_name}>{v.volume_name}</span>
            <span className="mono truncate text-2xs text-muted-foreground" title="容器里的路径">{v.path}</span>
            {v.auto && <Badge variant="outline">镜像自带</Badge>}
          </div>
        ))}
        {volumes.length > 0 && <p className="mt-2 text-2xs text-muted-foreground">删除应用时默认保留数据卷。</p>}
      </Card>
      <Card className="p-3.5">
        <div className="mb-2 text-sm font-medium">附加服务</div>
        {addons.length === 0 && <div className="mb-2 text-xs text-muted-foreground">没有附加服务</div>}
        {addons.map((x) => {
          const [t, v] = ADDON_STATE[x.observed_state] || ['状态未知', 'outline']
          return (
            <div key={x.kind} className="flex items-center gap-2 border-b py-1.5 text-sm last:border-0">
              <Database className="size-3.5 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5">{ADDONS[x.kind]?.name || x.kind}<Badge variant={v}>{t}</Badge></div>
                <div className="mono truncate text-2xs text-muted-foreground" title={x.env_var}>{x.env_var} · {x.host}:{x.port} · {timeText(x.created_at)}</div>
              </div>
              <Button variant="ghost" size="sm" disabled={!!busy} onClick={() => { setDropData(false); setRemoving(x) }}>关闭</Button>
            </div>
          )
        })}
        {missing.length > 0 && (
          <div className="mt-2 flex flex-wrap gap-1.5">
            {missing.map((k) => (
              <Button key={k} variant="outline" size="sm" disabled={!!busy} onClick={() => add(k)} title={ADDONS[k].desc}>
                {busy === k ? <Spinner /> : <Plus className="size-3.5" />}{ADDONS[k].name}
              </Button>
            ))}
          </div>
        )}
        <p className="mt-2 text-2xs text-muted-foreground">添加后连接地址写进环境变量，应用会自动重新部署。</p>
      </Card>
      <Dialog open={!!removing} onClose={() => setRemoving(null)} labelledBy="addon-title">
        {removing && (
          <>
            <DialogHead id="addon-title" title={'关闭 ' + (ADDONS[removing.kind]?.name || removing.kind)} desc={`${a.name} 将不再连接它，应用会自动重新部署。默认保留数据卷，之后重新打开可恢复原来的数据。`} />
            <label className="flex items-center gap-2 text-sm"><input type="checkbox" className="size-3.5 accent-[hsl(var(--destructive))]" checked={dropData} onChange={(e) => setDropData(e.target.checked)} />同时删除数据（不可恢复）</label>
            <div className="mt-5 flex justify-end gap-2">
              <Button variant="outline" onClick={() => setRemoving(null)}>取消</Button>
              <Button variant={dropData ? 'destructive' : 'default'} disabled={!!busy} onClick={remove}>{busy && <Spinner />}关闭</Button>
            </div>
          </>
        )}
      </Dialog>
    </div>
  )
}
