// 设置 → 数据（样稿第 10 版，评估 H7）：收起时一句“存在这台机器上，删应用时默认留着”。
//   展开：一句话说数据在哪、删应用时怎么办；存数据的位置（应用里的路径、名字、是不是应用自带的）收在小的“技术信息”里，
//   页面上不写“数据卷”“镜像自带”。
//   “附加服务”（PostgreSQL、MySQL、Redis，平台内置的那一套）只给开发者自己部署的项目；应用中心装的应用用的服务在概览“它用到的服务”。
//   以后“备份”放在这一行（W10 还没做）。
// 接口：GET /v1/apps/{app}（volumes、addons）、POST /v1/apps/{app}/addons、DELETE /v1/apps/{app}/addons/{kind}[?volumes=true]。
import { useState } from 'react'
import { Database, HardDrive, Plus } from 'lucide-react'
import { api, appPath } from '@/lib/api'
import { fromStore } from '@/lib/display'
import { Fold } from '@/components/fold'
import { timeText } from '@/lib/format'
import type { Addon, AppView } from '@/lib/types'
import { ErrorLine } from '@/components/bits'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { useToast } from '@/components/ui/toast'
import type { TabProps } from './app-page'

export const ADDONS: Record<string, { name: string; desc: string }> = {
  postgres: { name: 'PostgreSQL', desc: '关系型数据库' },
  mysql: { name: 'MySQL', desc: '关系型数据库' },
  redis: { name: 'Redis', desc: '缓存和队列' },
}
const ADDON_STATE: Record<string, [string, 'success' | 'warning' | 'danger' | 'outline']> = {
  running: ['正常', 'success'], starting: ['正在准备', 'outline'], unhealthy: ['还没准备好', 'warning'], stopped: ['停了', 'outline'], missing: ['正在准备', 'outline'], unavailable: ['看不到状态', 'outline'],
}

export function DataSection({ app: a, reload }: TabProps) {
  const toast = useToast()
  const [busy, setBusy] = useState('')
  const [removing, setRemoving] = useState<Addon | null>(null)
  const [dropData, setDropData] = useState(false)
  // 没加上：出错的话留在那几个按钮下面；没关掉：留在确认窗里（样稿第 14 版，不弹提示）
  const [addErr, setAddErr] = useState<Error | null>(null)
  const [removeErr, setRemoveErr] = useState<Error | null>(null)
  const addons = a.addons || []
  const volumes = a.volumes || []
  const missing = Object.keys(ADDONS).filter((k) => !addons.some((x) => x.kind === k))

  const add = async (kind: string) => {
    setBusy(kind)
    setAddErr(null)
    try {
      await api.post(appPath(a.name, '/addons'), { kind })
      toast('已添加 ' + ADDONS[kind].name, '应用会自动重装一遍')
      await reload()
    } catch (e) {
      setAddErr(new Error('没能添加 ' + ADDONS[kind].name + '：' + (e as Error).message))
    } finally {
      setBusy('')
    }
  }
  const remove = async () => {
    if (!removing) return
    const kind = removing.kind
    setBusy(kind)
    setRemoveErr(null)
    try {
      await api.del(appPath(a.name, '/addons/' + encodeURIComponent(kind) + (dropData ? '?volumes=true' : '')))
      toast(dropData ? `已关掉 ${ADDONS[kind]?.name || kind}，数据也删了` : `已关掉 ${ADDONS[kind]?.name || kind}，数据留着`)
      setRemoving(null)
      await reload()
    } catch (e) {
      setRemoveErr(new Error('没能关闭：' + (e as Error).message))
    } finally {
      setBusy('')
    }
  }

  // 附加服务只给开发者自己部署的项目；数据库这类只在内部网络的应用自己就是别的应用的服务，不能再给它加附加服务（W5-6）
  const dev = !fromStore(a) && !a.internal_only
  return (
    <div className={dev ? 'grid gap-5 md:grid-cols-2' : 'flex flex-col'}>
      <section aria-label="数据" className="flex flex-col gap-2">
        {volumes.length === 0
          ? <p className="text-sm">这个应用没有单独存数据的地方，重装以后它里面写的文件会丢。</p>
          : <p className="text-sm">数据存在这台机器上。删除应用时默认留着，以后重装能接着用。</p>}
        {volumes.length > 0 && (
          <Fold title="技术信息">
            {volumes.map((v) => (
              <div key={v.volume_name} className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs">
                <HardDrive className="size-icon shrink-0 text-muted-foreground" />
                <span className="mono min-w-0 truncate" title="应用里的路径">{v.path}</span>
                <span className="mono min-w-0 truncate text-2xs text-muted-foreground" title="名字">{v.volume_name}</span>
                {v.auto && <Badge variant="outline">应用自带的</Badge>}
              </div>
            ))}
          </Fold>
        )}
      </section>
      {dev && <section aria-label="附加服务">
        <div className="mb-1.5 text-2xs text-muted-foreground">附加服务</div>
        {addons.length === 0 && <div className="mb-2 text-xs text-muted-foreground">没有附加服务</div>}
        {addons.map((x) => {
          const [t, v] = ADDON_STATE[x.observed_state] || ['看不到状态', 'outline']
          return (
            <div key={x.kind} className="flex items-center gap-2 border-b py-1.5 text-sm last:border-0">
              <Database className="size-icon shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5">{ADDONS[x.kind]?.name || x.kind}<Badge variant={v}>{t}</Badge></div>
                <div className="mono truncate text-2xs text-muted-foreground" title={x.env_var}>{x.env_var} · {x.host}:{x.port} · {timeText(x.created_at)}</div>
              </div>
              <Button variant="ghost" size="sm" disabled={!!busy} onClick={() => { setDropData(false); setRemoveErr(null); setRemoving(x) }}>关闭</Button>
            </div>
          )
        })}
        {missing.length > 0 && (
          <div className="mt-2 flex flex-wrap gap-1.5">
            {missing.map((k) => (
              <Button key={k} variant="outline" size="sm" disabled={!!busy} loading={busy === k} onClick={() => add(k)} title={ADDONS[k].desc}>
                <Plus className="size-icon" />{ADDONS[k].name}
              </Button>
            ))}
          </div>
        )}
        <ErrorLine error={addErr} className="mt-2" />
        <p className="mt-2 text-2xs text-muted-foreground">添加后地址写进环境变量，应用会自动重装一遍。</p>
      </section>}
      <Dialog open={!!removing} onClose={() => setRemoving(null)} labelledBy="addon-title">
        {removing && (
          <>
            <DialogHead id="addon-title" title={'关闭 ' + (ADDONS[removing.kind]?.name || removing.kind)} desc={`${a.name} 不再用它，应用会自动重装一遍。默认留着它的数据，以后再打开还能接着用。`} />
            <label className="tap-h flex items-center gap-2 text-sm"><input type="checkbox" className="size-icon accent-[hsl(var(--destructive))]" checked={dropData} onChange={(e) => setDropData(e.target.checked)} />数据也删掉（找不回来）</label>
            <ErrorLine error={removeErr} className="mt-3" />
            <div className="mt-5 flex justify-end gap-2">
              <Button variant="outline" onClick={() => setRemoving(null)}>取消</Button>
              <Button variant={dropData ? 'destructive' : 'default'} onClick={remove} loading={!!busy}>关闭</Button>
            </div>
          </>
        )}
      </Dialog>
    </div>
  )
}

// dataSummary：设置里“数据”那一行收起时的一句。开发者的项目后面带上附加服务，例如“… · 附加服务 Redis”。
export function dataSummary(a: AppView): string {
  const vols = (a.volumes || []).length
  const addons = fromStore(a) ? [] : (a.addons || []).map((x) => ADDONS[x.kind]?.name || x.kind)
  return (vols ? '存在这台机器上，删应用时默认留着' : '没有单独存数据的地方') + (addons.length ? ' · 附加服务 ' + addons.join('、') : '')
}
