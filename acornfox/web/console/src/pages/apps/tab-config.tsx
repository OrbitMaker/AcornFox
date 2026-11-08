// 配置：环境变量（密钥类不回显，留空表示不改）和运行设置（容器端口、健康检查路径）。
// 改动先保存到期望状态，重新部署后生效。接口：PUT/DELETE /v1/apps/{app}/env/{key}、PATCH /v1/apps/{app}、POST /v1/apps/{app}/redeploy。
import { useEffect, useState } from 'react'
import { Lock, Plus, Trash2, Unlock } from 'lucide-react'
import { api, appPath } from '@/lib/api'
import type { AppView } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Spinner } from '@/components/ui/controls'
import { Input, Label } from '@/components/ui/input'
import { useToast } from '@/components/ui/toast'
import type { TabProps } from './app-page'

export interface EnvRow { key: string; value: string; secret: boolean; orig?: { key: string; secret: boolean; value?: string }; removed?: boolean }

export const ENV_KEY = /^[A-Za-z_][A-Za-z0-9_]{0,127}$/

export function rowsFrom(a: AppView): EnvRow[] {
  return (a.env || []).map((e) => ({ key: e.key, value: e.secret ? '' : e.value || '', secret: e.secret, orig: { key: e.key, secret: e.secret, value: e.value } }))
}

// envChanges：要写的（新增、改值、改密钥属性）和要删的（删掉的、改了名的旧名）。
export function envChanges(rows: EnvRow[]): { put: EnvRow[]; del: string[]; problems: string[] } {
  const put: EnvRow[] = []
  const del: string[] = []
  const problems: string[] = []
  const seen = new Set<string>()
  for (const r of rows) {
    const k = r.key.trim()
    if (r.removed) {
      if (r.orig) del.push(r.orig.key)
      continue
    }
    if (!k) { if (r.orig) del.push(r.orig.key); continue }
    if (!ENV_KEY.test(k)) { problems.push(`变量名 ${k} 不合法：字母或下划线开头，只用字母、数字、下划线`); continue }
    if (seen.has(k)) { problems.push(`变量 ${k} 重复了`); continue }
    seen.add(k)
    if (r.orig && r.orig.key !== k) del.push(r.orig.key)
    const renamed = !r.orig || r.orig.key !== k
    if (renamed) {
      if (r.secret && !r.value) { problems.push(`密钥 ${k} 要填值`); continue }
      put.push({ ...r, key: k })
      continue
    }
    // 原来是密钥：留空表示不改值；改成非密钥时必须给值。
    if (r.orig!.secret) {
      if (r.value) put.push({ ...r, key: k })
      else if (!r.secret) problems.push(`把 ${k} 改成普通变量时要重新填值`)
      continue
    }
    if (r.value !== (r.orig!.value || '') || r.secret !== r.orig!.secret) put.push({ ...r, key: k })
  }
  return { put, del: [...new Set(del)].filter((k) => !put.some((p) => p.key === k)), problems }
}

export function ConfigTab({ app: a, reload }: TabProps) {
  const toast = useToast()
  const [rows, setRows] = useState<EnvRow[]>(() => rowsFrom(a))
  const [port, setPort] = useState(String(a.port || ''))
  const [health, setHealth] = useState(a.health_path || '/')
  const [busy, setBusy] = useState('')
  const [problems, setProblems] = useState<string[]>([])
  const envKey = JSON.stringify(a.env || [])
  useEffect(() => { setRows(rowsFrom(a)) }, [envKey]) // eslint-disable-line react-hooks/exhaustive-deps
  const changes = envChanges(rows)
  const dirty = changes.put.length > 0 || changes.del.length > 0
  const set = (i: number, patch: Partial<EnvRow>) => setRows((rs) => rs.map((r, j) => (j === i ? { ...r, ...patch } : r)))

  const saveEnv = async (redeploy: boolean) => {
    if (changes.problems.length) { setProblems(changes.problems); return }
    setProblems([])
    setBusy(redeploy ? 'env-re' : 'env')
    try {
      for (const k of changes.del) await api.del(appPath(a.name, '/env/' + encodeURIComponent(k)))
      for (const r of changes.put) await api.put(appPath(a.name, '/env/' + encodeURIComponent(r.key)), { value: r.value, secret: r.secret })
      if (redeploy && a.current_deployment) {
        await api.post(appPath(a.name, '/redeploy'), {})
        toast('已保存，正在重新部署', a.name)
      } else toast('已保存', '重新部署后生效')
      await reload()
    } catch (e) {
      toast('没能保存', (e as Error).message, 'error')
    } finally {
      setBusy('')
    }
  }

  const saveRun = async () => {
    const p = Number(port)
    if (!Number.isInteger(p) || p < 1 || p > 65535) { toast('端口不对', '要是 1～65535 的整数', 'error'); return }
    if (!health.startsWith('/')) { toast('健康检查路径不对', '要以 / 开头', 'error'); return }
    setBusy('run')
    try {
      await api.patch(appPath(a.name), { port: p, health_path: health })
      toast('已保存', '重新部署后生效')
      await reload()
    } catch (e) {
      toast('没能保存', (e as Error).message, 'error')
    } finally {
      setBusy('')
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <Card>
        <div className="flex items-center justify-between px-3.5 py-2.5">
          <span className="text-sm font-medium">环境变量</span>
          <Button variant="ghost" size="sm" onClick={() => setRows((rs) => [...rs, { key: '', value: '', secret: false }])}><Plus className="size-3.5" />添加</Button>
        </div>
        <div className="flex flex-col gap-1.5 px-3.5 pb-3">
          {rows.every((r) => r.removed) && <div className="py-2 text-xs text-muted-foreground">还没有环境变量</div>}
          {rows.map((r, i) => r.removed ? null : (
            <div key={i} className="grid grid-cols-1 gap-1.5 sm:grid-cols-[minmax(0,2fr)_minmax(0,3fr)_auto]">
              <Input className="mono text-xs" aria-label="变量名" placeholder="变量名" value={r.key} onChange={(e) => set(i, { key: e.target.value })} />
              <Input
                className="mono text-xs"
                aria-label={'变量 ' + r.key + ' 的值'}
                type={r.secret ? 'password' : 'text'}
                autoComplete="off"
                placeholder={r.orig?.secret ? '已保存（不显示），留空不改' : '值'}
                value={r.value}
                onChange={(e) => set(i, { value: e.target.value })}
              />
              <div className="flex">
                <Button variant="ghost" size="icon" onClick={() => set(i, { secret: !r.secret })} aria-label={r.secret ? '改成普通变量' : '改成密钥'} title={r.secret ? '密钥：不回显，日志里会遮住' : '普通变量'}>
                  {r.secret ? <Lock className="size-3.5" /> : <Unlock className="size-3.5 text-muted-foreground" />}
                </Button>
                <Button variant="ghost" size="icon" onClick={() => set(i, { removed: true })} aria-label={'删除 ' + r.key}><Trash2 className="size-3.5" /></Button>
              </div>
            </div>
          ))}
          {problems.map((p) => <p key={p} className="text-2xs text-danger">{p}</p>)}
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2 border-t px-3.5 py-2.5">
          <span className="mr-auto text-2xs text-muted-foreground">改动在重新部署后生效</span>
          <Button variant="outline" disabled={!dirty || !!busy} onClick={() => saveEnv(false)}>{busy === 'env' && <Spinner />}只保存</Button>
          <Button disabled={!dirty || !!busy || !a.current_deployment} onClick={() => saveEnv(true)}>{busy === 'env-re' && <Spinner />}保存并重新部署</Button>
        </div>
      </Card>
      <Card>
        <div className="px-3.5 py-2.5 text-sm font-medium">运行设置</div>
        <div className="grid gap-3 px-3.5 pb-3 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5"><Label htmlFor="cfg-port">容器端口</Label><Input id="cfg-port" className="tnum" inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value)} /></div>
          <div className="flex flex-col gap-1.5"><Label htmlFor="cfg-health">健康检查路径</Label><Input id="cfg-health" className="mono" value={health} onChange={(e) => setHealth(e.target.value)} /></div>
        </div>
        <div className="flex justify-end border-t px-3.5 py-2.5">
          <Button variant="outline" disabled={!!busy || (String(a.port) === port && a.health_path === health)} onClick={saveRun}>{busy === 'run' && <Spinner />}保存</Button>
        </div>
      </Card>
    </div>
  )
}
