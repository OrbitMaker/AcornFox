// 设置 → 最多能用多少内存（样稿第 10 版，评估 H6；原来叫“资源上限”）：收起时“内存 1 GB · CPU 1 核”；
// 展开先写一句现在用了多少、够不够（用应用页传进来的指标），下面才是两个下拉。
// 能调多少看 GET /v1/resources，改用 PATCH /v1/apps/{app}；要放得进这台机器给应用的总量；
// 能直接改到正在跑的应用上就立即生效，不能就下次重启后生效。
import { useEffect, useState } from 'react'
import { ErrorLine } from '@/components/bits'
import { coresText, mbText } from '@/lib/format'
import { api, appPath } from '@/lib/api'
import type { AppView, Resources } from '@/lib/types'
import { useConsole } from '@/state'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/controls'
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

// usageLine：“现在用了 289 MB，够用”。用到八成以上提醒调大一点；读不到指标时不写。
export function usageLine(usedMb: number | undefined | null, limitMb: number): { text: string; warn: boolean } | null {
  if (usedMb === undefined || usedMb === null || !Number.isFinite(usedMb)) return null
  const warn = !!limitMb && usedMb / limitMb >= 0.8
  return { text: '现在用了 ' + mbText(usedMb) + (warn ? '，快用满了，可以调大一点' : '，够用'), warn }
}

export function LimitsSection({ app: a, metrics: m, reload }: TabProps) {
  const { resources, refreshResources } = useConsole()
  const toast = useToast()
  const [mem, setMem] = useState(String(a.memory_mb))
  const [cpu, setCpu] = useState(String(a.cpu_milli))
  const [busy, setBusy] = useState(false)
  // 出错留在这一项上（保存按钮下面），不用会消失的提示（样稿第 12 版）
  const [err, setErr] = useState<Error | null>(null)
  useEffect(() => { setMem(String(a.memory_mb)); setCpu(String(a.cpu_milli)) }, [a.memory_mb, a.cpu_milli])
  const opts = limitOptions(resources, a.name, a.memory_mb, a.cpu_milli)
  const dirty = mem !== String(a.memory_mb) || cpu !== String(a.cpu_milli)
  const usage = m?.available ? usageLine(m.memory_usage_mb, a.memory_mb) : null

  const save = async () => {
    setBusy(true)
    setErr(null)
    // 改之前的值：提示里的“撤销”把它原样改回去
    const before = { memory_mb: a.memory_mb, cpu_milli: a.cpu_milli }
    const undo = async () => { await api.patch(appPath(a.name), before); await Promise.all([reload(), refreshResources()]) }
    try {
      const r = await api.patch<{ applied_now?: boolean; apply_error?: string }>(appPath(a.name), { memory_mb: Number(mem), cpu_milli: Number(cpu) })
      if (r.apply_error) toast('已保存', r.apply_error, { undo, undone: '改回原来的了' })
      else toast('已保存', r.applied_now ? '已经生效，不用重启' : '下次重启后生效', { undo, undone: '改回原来的了' })
      await Promise.all([reload(), refreshResources()])
    } catch (e) {
      setErr(new Error('没能保存：' + (e as Error).message))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-col gap-2.5">
      {usage && <p className={usage.warn ? 'text-sm text-warning' : 'text-sm'} data-testid="limits-usage">{usage.text}</p>}
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end">
        <div className="flex flex-col gap-1.5"><Label htmlFor="lim-mem">最多用的内存</Label><Select id="lim-mem" value={mem} onChange={(e) => setMem(e.target.value)} options={opts.mem.map((v) => [String(v), mbText(v)])} /></div>
        <div className="flex flex-col gap-1.5"><Label htmlFor="lim-cpu">最多用的 CPU</Label><Select id="lim-cpu" value={cpu} onChange={(e) => setCpu(e.target.value)} options={opts.cpu.map((v) => [String(v), coresText(v)])} /></div>
        <Button disabled={!dirty} onClick={save} loading={busy}>保存</Button>
      </div>
      <ErrorLine error={err} />
      {resources && (
        <p className="text-2xs text-muted-foreground">
          {resources.memory.cap_mb
            ? `这台机器给应用的内存还剩 ${mbText(Math.max(0, resources.memory.cap_mb - resources.memory.allocated_mb))}（一共 ${mbText(resources.memory.cap_mb)}）`
            : `这台机器上的应用一共分了 ${mbText(resources.memory.allocated_mb)} 内存`}
        </p>
      )}
    </div>
  )
}

// limitsSummary：设置里“最多能用多少内存”那一行的摘要，例如“内存 640 MB · CPU 2 核”。
export function limitsSummary(a: AppView): string {
  return '内存 ' + (a.memory_mb ? mbText(a.memory_mb) : '不限') + ' · CPU ' + (a.cpu_milli ? coresText(a.cpu_milli) : '不限')
}
