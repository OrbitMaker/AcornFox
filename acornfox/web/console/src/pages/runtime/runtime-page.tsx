// 运行环境：后台服务、Docker、网关、磁盘、端口的自检结果，每项带故障编号和建议。
// 接口：GET /v1/runtime/checks（和 acornfox doctor 同一套检查和编号）、GET /v1/setup/faults（编号的说明，单一来源在服务端）。
// Mac、Windows 本机运行这一版没有开放，所以不显示样稿里的安装步骤。
import * as React from 'react'
import { Bot, CheckCircle2, CircleAlert, CircleX, RefreshCw, Stethoscope } from 'lucide-react'
import { cn } from '@/lib/utils'
import { api, type ApiError } from '@/lib/api'
import { usePoll } from '@/lib/hooks'
import { timeText } from '@/lib/format'
import type { FaultDef, RuntimeCheck, RuntimeChecks } from '@/lib/types'
import { Empty, KV, PageHead } from '@/components/bits'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Spinner } from '@/components/ui/controls'
import { CopyButton, CopyLine } from '@/components/ui/copy'

export function useFaults() {
  return usePoll((s) => api.get<{ faults: FaultDef[] }>('/v1/setup/faults', s).then((b) => b.faults || []).catch(() => [] as FaultDef[]), 0)
}

// checksForAI：交给 AI 工作台的文字，带不正常的项和编号。
export function checksForAI(c: RuntimeChecks): string {
  const bad = c.items.filter((i) => i.level !== 'ok')
  const head = '用 acornfox skill 帮我检查这台服务器的运行环境（可以运行 sudo acornfox doctor --json 看详情）'
  if (!bad.length) return head + '。控制台自检全部正常。'
  return head + '。控制台自检发现：\n' + bad.map((i) => `- ${i.code ? i.code + ' ' : ''}${i.text}`).join('\n')
}

function CheckRow({ c, fault }: { c: RuntimeCheck; fault?: FaultDef }) {
  const [open, setOpen] = React.useState(c.level === 'fail')
  const Icon = c.level === 'ok' ? CheckCircle2 : c.level === 'warn' ? CircleAlert : CircleX
  return (
    <li className="flex gap-3 py-2.5" data-testid="runtime-check">
      <Icon className={cn('mt-0.5 size-4 shrink-0', c.level === 'ok' && 'text-success', c.level === 'warn' && 'text-warning', c.level === 'fail' && 'text-danger')} aria-label={c.level === 'ok' ? '正常' : c.level === 'warn' ? '要注意' : '有问题'} />
      <div className="min-w-0 flex-1">
        <div className="text-sm">{c.text}</div>
        {c.code && (
          <div className="mt-1 flex flex-wrap items-center gap-1.5">
            <Badge variant={c.level === 'fail' ? 'danger' : 'warning'} className="mono">{c.code}</Badge>
            {fault && <span className="text-2xs text-muted-foreground">{fault.title}</span>}
            {fault && <button type="button" className="text-2xs underline-offset-4 hover:underline" onClick={() => setOpen((o) => !o)}>{open ? '收起' : '怎么办'}</button>}
          </div>
        )}
        {open && fault && (
          <div className="mt-2 rounded-md bg-muted/50 p-2.5 text-xs">
            <div className="mb-1 font-medium">怎么办</div>
            <p className="text-muted-foreground">{fault.next}</p>
            {fault.causes.length > 0 && (
              <>
                <div className="mb-1 mt-2 font-medium">常见原因（按先查的顺序）</div>
                <ol className="list-decimal pl-4 text-muted-foreground">{fault.causes.map((x) => <li key={x}>{x}</li>)}</ol>
              </>
            )}
            {fault.needs_user && <p className="mt-2 text-warning">这一项要你自己确认或操作。</p>}
          </div>
        )}
      </div>
    </li>
  )
}

// checksErrorText：自检接口出错时页面上的说明。旧服务端没有这个接口（404）、服务端出错（带编号）、
// 连不上后台服务（网络错误，没有 HTTP 状态）分开说，都指向在服务器上运行 doctor。
export function checksErrorText(err: Partial<ApiError> & { message?: string }): string {
  if (err.status === 404) return '这个版本的服务端还不能在控制台里自检，请在服务器上运行 sudo acornfox doctor。'
  if (!err.status) return '连不上后台服务，没能自检。请在服务器上运行 sudo acornfox doctor。'
  const code = err.code && err.code !== 'server_error' ? err.code + ' ' : ''
  return `自检没做完：${code}${err.message || ''}。可以在服务器上运行 sudo acornfox doctor。`
}

export function RuntimePage() {
  const checks = usePoll((s) => api.get<RuntimeChecks>('/v1/runtime/checks', s), 60000)
  const faults = useFaults()
  const [busy, setBusy] = React.useState(false)
  const recheck = async () => {
    setBusy(true)
    try { await checks.refresh() } finally { setBusy(false) }
  }
  const c = checks.data
  const err = checks.error as ApiError | null
  const byCode = new Map((faults.data || []).map((f) => [f.code, f]))
  const bad = c ? c.items.filter((i) => i.level !== 'ok').length : 0
  return (
    <div>
      <PageHead title="运行环境" meta={c ? (bad ? `${bad} 项要处理` : '全部正常') : undefined}>
        <Button variant="outline" size="sm" onClick={recheck} disabled={busy}>{busy ? <Spinner /> : <RefreshCw className="size-3.5" />}重新检查</Button>
      </PageHead>
      {err && !c && <Empty>{checksErrorText(err)}</Empty>}
      {!c && !err && <div className="flex items-center gap-2 text-xs text-muted-foreground"><Spinner />正在检查…</div>}
      {c && (
        <div className="grid gap-3 lg:grid-cols-3">
          <Card className="min-w-0 px-3.5 lg:col-span-2">
            <ul className="divide-y">{c.items.map((it, i) => <CheckRow key={i} c={it} fault={it.code ? byCode.get(it.code) : undefined} />)}</ul>
            {c.os !== 'linux' && (
              <p className="border-t py-2.5 text-2xs text-muted-foreground">Mac、Windows 本机运行这一版还没有开放，这里只显示后台服务自己能查到的项。</p>
            )}
          </Card>
          <div className="flex min-w-0 flex-col gap-3">
            <Card className="p-3.5">
              <div className="mb-1.5 flex items-center gap-1.5 text-xs font-medium"><Stethoscope className="size-3.5" />在服务器上查得更全</div>
              <CopyLine text="sudo acornfox doctor" />
              <p className="mt-2 text-2xs text-muted-foreground">后台服务不是 root，看不到别的程序占了哪个端口；用 sudo 运行能看到是谁占用。</p>
            </Card>
            <Card className="p-3.5">
              <div className="mb-1.5 flex items-center gap-1.5 text-xs font-medium"><Bot className="size-3.5" />发给 AI 工作台<CopyButton className="ml-auto" text={checksForAI(c)} label="复制给 AI 的文字" /></div>
              <pre className="whitespace-pre-wrap rounded-md bg-muted/40 p-2.5 text-xs">{checksForAI(c)}</pre>
            </Card>
            <Card className="divide-y px-3.5 py-1">
              <KV k="系统">{c.os === 'linux' ? 'Linux' : c.os === 'darwin' ? 'macOS' : c.os === 'windows' ? 'Windows' : c.os}</KV>
              {c.docker_version && <KV k="Docker"><span className="tnum">{c.docker_version}</span></KV>}
              {c.data_dir && <KV k="数据目录"><span className="mono text-xs">{c.data_dir}</span></KV>}
              <KV k="检查时间"><span className="tnum">{timeText(c.checked_at)}</span></KV>
            </Card>
          </div>
        </div>
      )}
    </div>
  )
}
