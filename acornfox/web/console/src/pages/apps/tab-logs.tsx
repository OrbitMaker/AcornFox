// 日志：现在的版本每 2 秒接着读（GET /v1/apps/{app}/log-batch，带游标），以前的版本读存档（GET /v1/deployments/{id}/logs）。
// 日志在服务端已按应用的密钥遮住；“只看出错”按关键词筛；下载保存成文本文件。
// 样稿第 10 版（评估 F1）：右上角“复制给 AI 工作台”（复制最近 80 行和应用状态）；底部的命令收进默认收起的“用命令行”；
// 下拉里从应用中心装的应用写“现在的”“以前的（11-10）”，自己部署的项目写“第 2 版 · 现在的”“第 1 版 · 以前的版本”。
import { useEffect, useMemo, useRef, useState } from 'react'
import { Download, Pause, Play } from 'lucide-react'
import { cn } from '@/lib/utils'
import { api, appPath, type ApiError } from '@/lib/api'
import type { Deployment } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Select, Switch } from '@/components/ui/controls'
import { Loading } from '@/components/ui/skeleton'
import { SkLogLines } from '@/components/skeletons'
import { CopyLine } from '@/components/ui/copy'
import { AskAI, CliFold } from '@/components/fold'
import { deriveStatus } from '@/lib/appstatus'
import { displayName, fromStore, plainStatus } from '@/lib/display'
import { useConsole } from '@/state'
import type { TabProps } from './app-page'
import { parseHash } from '@/lib/router'

export const ERROR_RE = /\b(error|err|fatal|panic|exception|fail(ed|ure)?|critical|traceback)\b|错误|失败|异常/i
const WARN_RE = /\b(warn(ing)?)\b|警告/i
const MAX_LINES = 2000

export function lineTone(l: string): 'error' | 'warn' | '' {
  if (ERROR_RE.test(l)) return 'error'
  if (WARN_RE.test(l)) return 'warn'
  return ''
}

interface Batch { lines: string[]; cursor: string; has_more: boolean; reset: boolean }

const DEP_WORD: Record<string, string> = { live: '现在的', failed: '没换上的', retired: '以前的版本', superseded: '以前的版本' }
const md = (iso: string) => { const d = new Date(iso); return Number.isFinite(d.getTime()) ? String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0') : '' }

export function LogsTab({ app: a }: TabProps) {
  const { catalog, installing } = useConsole()
  const initialV = parseHash(location.hash).query?.v
  const [deps, setDeps] = useState<Deployment[]>([])
  const [sel, setSel] = useState<string>('live')
  const [lines, setLines] = useState<string[] | null>(null)
  const [err, setErr] = useState<string>('')
  const [follow, setFollow] = useState(true)
  const [onlyErr, setOnlyErr] = useState(false)
  const [note, setNote] = useState('')
  const cursor = useRef('')
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => {
    api.get<{ deployments: Deployment[] }>(appPath(a.name, '/deployments?limit=20')).then((b) => {
      const ds = b.deployments || []
      setDeps(ds)
      if (initialV) {
        const d = ds.find((x) => String(x.seq) === initialV)
        if (d && d.status !== 'live') setSel(d.id)
      }
    }).catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [a.name])

  // 当前版本：先读最后 200 行，之后带游标接着读；暂停时停止轮询，继续时从游标接上。
  const followRef = useRef(follow)
  followRef.current = follow
  const resume = useRef<() => void>(() => {})
  useEffect(() => {
    if (sel !== 'live') return
    let alive = true
    let timer = 0
    cursor.current = ''
    setLines(null)
    setErr('')
    setNote('')
    const tick = async () => {
      window.clearTimeout(timer)
      try {
        const q = new URLSearchParams({ tail: '200' })
        if (cursor.current) q.set('cursor', cursor.current)
        const b = await api.get<Batch>(appPath(a.name, '/log-batch?' + q.toString()))
        if (!alive) return
        cursor.current = b.cursor || ''
        setLines((prev) => (b.reset || !prev ? b.lines || [] : prev.concat(b.lines || [])).slice(-MAX_LINES))
        setErr('')
        if (followRef.current) timer = window.setTimeout(tick, b.has_more ? 0 : 2000)
      } catch (e) {
        if (!alive) return
        const ae = e as ApiError
        setErr(ae.message)
        if (ae.code === 'no_live_deployment') setLines([])
        if (followRef.current && ae.status !== 404 && ae.status !== 501 && ae.status !== 401) timer = window.setTimeout(tick, 4000)
      }
    }
    resume.current = tick
    tick()
    return () => { alive = false; window.clearTimeout(timer); resume.current = () => {} }
  }, [a.name, sel])
  const toggleFollow = () => {
    const next = !follow
    setFollow(next)
    followRef.current = next
    if (next) resume.current()
  }

  // 旧版本：读存档的最后 500 行。
  useEffect(() => {
    if (sel === 'live') return
    let alive = true
    setLines(null)
    setErr('')
    api.get<{ lines: string[]; truncated?: boolean; source?: string }>('/v1/deployments/' + encodeURIComponent(sel) + '/logs?tail=500')
      .then((b) => {
        if (!alive) return
        setLines(b.lines || [])
        setNote(b.truncated ? '只显示最后 500 行' : '')
      })
      .catch((e) => { if (alive) { setErr((e as Error).message); setLines([]) } })
    return () => { alive = false }
  }, [sel])

  const shown = useMemo(() => (lines || []).filter((l) => !onlyErr || lineTone(l) !== ''), [lines, onlyErr])

  useEffect(() => {
    const el = box.current
    if (el && sel === 'live' && follow) el.scrollTop = el.scrollHeight
  }, [shown, sel, follow])

  const download = () => {
    const blob = new Blob([(lines || []).join('\n') + '\n'], { type: 'text/plain;charset=utf-8' })
    const link = document.createElement('a')
    link.href = URL.createObjectURL(blob)
    const d = deps.find((x) => x.id === sel)
    link.download = `${a.name}-${d ? 'v' + d.seq : 'live'}.log`
    link.click()
    window.setTimeout(() => URL.revokeObjectURL(link.href), 1000)
  }

  const store = fromStore(a)
  const live = deps.find((d) => d.status === 'live')
  const options: [string, string][] = [['live', store || !live ? '现在的' : `第 ${live.seq} 版 · 现在的`]]
  for (const d of deps) {
    if (d.status === 'live' || !(d.logs?.run || d.logs?.build)) continue
    const word = DEP_WORD[d.status] || '以前的版本'
    options.push([d.id, store ? `${word.replace(/版本$/, '')}（${md(d.created_at)}）` : `第 ${d.seq} 版 · ${word}`])
  }
  const selDep = deps.find((d) => d.id === sel)
  const cli = 'acornfox logs ' + a.name + (selDep ? ' --version ' + selDep.seq : ' -f')
  // 复制给 AI 工作台：应用、状态、最近 80 行（故障编号这类原话都在日志里）
  const forAI = () => {
    const label = displayName(catalog, a)
    const st = plainStatus(a, deriveStatus(a, installing))
    const tail = (lines || []).slice(-80).join('\n')
    return `AcornFox 上的应用 ${label}（${a.name}）现在${st.text}${st.note ? '（' + st.note + '）' : ''}。下面是${selDep ? '第 ' + selDep.seq + ' 版' : '现在的'}日志最后 ${Math.min(80, (lines || []).length)} 行，请帮我看看有没有问题、该怎么办：\n\n${tail}`
  }

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center gap-2 border-b p-2.5">
        <Select className="w-48" aria-label="哪个版本的日志" value={sel} onChange={(e) => setSel(e.target.value)} options={options} />
        <label className="flex items-center gap-1.5 text-xs"><Switch id="only-err" checked={onlyErr} onChange={setOnlyErr} label="只看出错" />只看出错</label>
        <div className="ml-auto flex items-center gap-1">
          {sel === 'live' && (
            <Button variant="ghost" size="icon-sm" onClick={toggleFollow} aria-label={follow ? '暂停' : '继续'} title={follow ? '暂停' : '继续'}>
              {follow ? <Pause className="size-icon" /> : <Play className="size-icon" />}
            </Button>
          )}
          <Button variant="ghost" size="icon-sm" onClick={download} disabled={!lines || lines.length === 0} aria-label="下载" title="下载"><Download className="size-icon" /></Button>
          <AskAI text={forAI} className="ml-1" />
        </div>
      </div>
      <div ref={box} className="max-h-[420px] min-h-[160px] overflow-auto bg-muted/20 p-3 font-mono text-xs leading-relaxed" data-testid="log-box">
        <Loading when={lines === null && !err} skeleton={<SkLogLines />}>{null}</Loading>
        {err && <div className="text-danger">{err}</div>}
        {lines !== null && shown.length === 0 && !err && <div className="text-muted-foreground">{onlyErr ? '没有出错记录' : '还没有日志'}</div>}
        {shown.map((l, i) => {
          const t = lineTone(l)
          return <div key={i} className={cn('whitespace-pre-wrap break-all', t === 'error' && 'text-danger', t === 'warn' && 'text-warning')}>{l}</div>
        })}
      </div>
      <div className="flex flex-col gap-1.5 border-t px-3 py-1.5">
        <span className="text-2xs text-muted-foreground">{note || (sel === 'live' ? (follow ? '实时 · 每 2 秒更新' : '已暂停') : '以前的，不再更新')}</span>
        <CliFold><CopyLine className="max-w-full sm:max-w-sm" text={cli} /></CliFold>
      </div>
    </Card>
  )
}
