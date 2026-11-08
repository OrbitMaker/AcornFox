// 资源：现在用了多少（GET /v1/metrics/apps/{app}），上限能调多少（GET /v1/resources），改上限（PATCH /v1/apps/{app}）。
// 内存和 CPU 上限要放得进整机给应用的上限；能直接改到运行中的容器上就立即生效，不能就下次部署生效。
import { useEffect, useState } from 'react'
import { coresText, mbText, pctText } from '@/lib/format'
import { api, appPath } from '@/lib/api'
import type { Resources } from '@/lib/types'
import { useConsole } from '@/state'
import { KV } from '@/components/bits'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Select, Spinner } from '@/components/ui/controls'
import { Label } from '@/components/ui/input'
import { useToast } from '@/components/ui/toast'
import type { TabProps } from './app-page'

const MEM = [256, 512, 768, 1024, 1536, 2048, 3072, 4096, 6144, 8192, 12288, 16384, 24576, 32768]
const CPU = [250, 500, 1000, 1500, 2000, 3000, 4000, 6000, 8000, 12000, 16000]

// limitOptions：这个应用可选的上限。内存不超过“整机上限 − 别的已分配”，CPU 不超过整机上限；现在的值总在里面。
export function limitOptions(res: Resources | null, app: string, curMem: number, curCPU: number) {
  const others = (res?.items || []).filter((i) => !(i.app === app && i.kind === 'app')).reduce((s, i) => s + i.memory_mb, 0)
  const memTotal = res ? (res.memory.cap_mb || res.memory.host_mb) : 0
  const memMax = memTotal ? memTotal - others : Infinity
  const memMin = res?.min_app_memory_mb || 256
  const cpuMax = res ? (res.cpu.cap_milli || res.cpu.host_milli) || Infinity : Infinity
  const cpuMin = res?.min_app_cpu_milli || 250
  const mem = [...new Set([...MEM.filter((m) => m >= memMin && m <= memMax), curMem].filter((x) => x > 0))].sort((a, b) => a - b)
  const cpu = [...new Set([...CPU.filter((c) => c >= cpuMin && c <= cpuMax), curCPU].filter((x) => x > 0))].sort((a, b) => a - b)
  return { mem, cpu, memMax, cpuMax }
}

export function ResourcesTab({ app: a, metrics: m, reload }: TabProps) {
  const { resources, refreshResources } = useConsole()
  const toast = useToast()
  const [mem, setMem] = useState(String(a.memory_mb))
  const [cpu, setCpu] = useState(String(a.cpu_milli))
  const [busy, setBusy] = useState(false)
  useEffect(() => { setMem(String(a.memory_mb)); setCpu(String(a.cpu_milli)) }, [a.memory_mb, a.cpu_milli])
  const opts = limitOptions(resources, a.name, a.memory_mb, a.cpu_milli)
  const dirty = mem !== String(a.memory_mb) || cpu !== String(a.cpu_milli)
  const used = m?.available && m.memory_limit_mb ? (m.memory_usage_mb || 0) / m.memory_limit_mb : null

  const save = async () => {
    setBusy(true)
    try {
      const r = await api.patch<{ applied_now?: boolean; apply_error?: string }>(appPath(a.name), { memory_mb: Number(mem), cpu_milli: Number(cpu) })
      if (r.apply_error) toast('已保存', r.apply_error)
      else toast('已保存', r.applied_now ? '已经生效，没有重启' : '下次部署时生效')
      await Promise.all([reload(), refreshResources()])
    } catch (e) {
      toast('没能保存', (e as Error).message, 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid gap-3 md:grid-cols-2">
      <Card className="px-3.5 py-2">
        <div className="pt-1 text-xs font-medium">现在 <span className="font-normal text-muted-foreground">每 5 秒更新</span></div>
        {m && !m.available ? (
          <div className="py-3 text-xs text-muted-foreground">应用没在运行，读不到资源用量</div>
        ) : (
          <div className="divide-y">
            <KV k="CPU"><span className="tnum">{m && m.cpu_available !== false ? pctText(m.cpu_percent) : '采样中'}</span></KV>
            <KV k="内存"><span className="tnum">{m ? mbText(m.memory_usage_mb) : '—'}{used !== null && <span className="text-muted-foreground">（上限的 {Math.round(used * 100)}%）</span>}</span></KV>
            <KV k="进程数"><span className="tnum">{m?.pids ?? '—'}</span></KV>
            <KV k="网络收 / 发"><span className="tnum">{m ? mbText(m.network_rx_mb) + ' / ' + mbText(m.network_tx_mb) : '—'}</span></KV>
          </div>
        )}
      </Card>
      <Card className="flex flex-col">
        <div className="grid grid-cols-2 gap-3 p-3.5">
          <div className="flex flex-col gap-1.5"><Label htmlFor="lim-mem">内存上限</Label><Select id="lim-mem" value={mem} onChange={(e) => setMem(e.target.value)} options={opts.mem.map((v) => [String(v), mbText(v)])} /></div>
          <div className="flex flex-col gap-1.5"><Label htmlFor="lim-cpu">CPU 上限</Label><Select id="lim-cpu" value={cpu} onChange={(e) => setCpu(e.target.value)} options={opts.cpu.map((v) => [String(v), coresText(v)])} /></div>
        </div>
        {resources && (
          <p className="px-3.5 text-2xs text-muted-foreground">
            整机给应用的内存：已分配 {mbText(resources.memory.allocated_mb)} / {resources.memory.cap_mb ? mbText(resources.memory.cap_mb) : '不限'}
            {resources.cpu.cap_milli ? ` · CPU 上限 ${coresText(resources.cpu.cap_milli)}` : ''}
          </p>
        )}
        <div className="mt-auto flex justify-end border-t px-3.5 py-2.5">
          <Button disabled={!dirty || busy} onClick={save}>{busy && <Spinner />}保存</Button>
        </div>
      </Card>
    </div>
  )
}
