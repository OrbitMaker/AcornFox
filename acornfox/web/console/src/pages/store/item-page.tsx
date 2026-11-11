// 应用详情页：左栏介绍、视频（有才显示）、要用户做的几步、组件；右栏安装、命令行、版本内存磁盘。
// 要更新版本的 AcornFox 才能装时（needs_platform 有值）：安装按钮不可点，下面说明要升到哪个版本并给升级命令。
// 这台 AcornFox 版本不够、连应用包都读不了时，详情接口返回 409（错误里带 needs_platform）：
// 用目录列表里已有的基本信息顶上（LimitedItem），说明要先升级并给升级命令，不当成加载失败。
import { useEffect, useState } from 'react'
import { ExternalLink, Package, PlayCircle } from 'lucide-react'
import { cn, safeHref } from '@/lib/utils'
import { api, type ApiError } from '@/lib/api'
import { PUBLIC, appMemory, findApp, freeMB, memoryCheck, sourceId } from '@/lib/catalog'
import { mbText } from '@/lib/format'
import { faultNeedsPlatform, needsPlatform, upgradeFirstText } from '@/lib/upgrade'
import { appHref, go } from '@/lib/router'
import type { AppDetail } from '@/lib/types'
import { useConsole } from '@/state'
import { Empty, ErrorLine, KV, TierBadge, Tile } from '@/components/bits'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Spinner } from '@/components/ui/controls'
import { CopyLine } from '@/components/ui/copy'
import { ExpertButton } from './shared'
import { SourceBadge, runningTaskFor } from './store-page'
import { UpgradeCommands, UpgradeNeeded } from './upgrade-note'

const PROVIDE_TEXT: Record<string, string> = { llm_gateway: '提供大模型接口' }
const USE_TEXT: Record<string, string> = { llm_gateway: '可接本机大模型网关' }

export function useAppDetail(id: string | undefined, catalogId?: string) {
  const [detail, setDetail] = useState<AppDetail | null>(null)
  const [error, setError] = useState<ApiError | null>(null)
  const { catalog } = useConsole()
  useEffect(() => {
    if (!id) return
    let alive = true
    setError(null)
    const q = catalogId && catalogId !== 'public' ? '?catalog=' + encodeURIComponent(catalogId) : ''
    api.get<AppDetail>('/v1/catalog/apps/' + encodeURIComponent(id) + q)
      .then((d) => { if (alive) setDetail(d) })
      .catch((e) => { if (alive) setError(e) })
    return () => { alive = false }
    // 目录刷新后（例如装完）重新读，已安装状态跟着变。
  }, [id, catalogId, catalog])
  return { detail, error }
}

export function ItemPage({ id, catalog: src }: { id: string; catalog?: string }) {
  const { detail: d, error } = useAppDetail(id, src)
  const { resources, tasks, install } = useConsole()
  const tooOld = faultNeedsPlatform(error)
  if (error && tooOld) return <LimitedItem id={id} src={src} version={tooOld} message={error.message} />
  if (error) return <Empty>{error.status === 404 ? '目录里没有这个应用。' : error.message} <a href="#store" className="underline">回应用中心</a></Empty>
  if (!d) return <div className="flex items-center gap-2 text-xs text-muted-foreground"><Spinner />正在读取…</div>
  const mem = appMemory(d)
  const check = memoryCheck(mem.need, mem.min, freeMB(resources))
  const running = runningTaskFor(tasks, 'app', d.id, sourceId(d))
  const needs = d.installed.length === 0 && !running && d.supported ? needsPlatform(d) : ''
  const steps = d.delivery.user_steps || []
  const video = safeHref(d.delivery.video)
  const homepage = safeHref(d.homepage)
  const parts = [d.image ? d.image.split(':')[0].split('/').pop() || d.name : d.name, ...(d.provides || []).map((p) => PROVIDE_TEXT[p] || p), ...(d.uses || []).map((u) => USE_TEXT[u.kind] || u.name)]
  return (
    <div className="grid gap-4 lg:grid-cols-3">
      <div className="flex min-w-0 flex-col gap-4 lg:col-span-2">
        <div className="flex items-center gap-3">
          <Tile id={d.id} name={d.name} icon={d.icon} iconType={d.icon_type} size="lg" />
          <div className="min-w-0">
            <h1 className="text-lg font-semibold">{d.name}</h1>
            <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">{d.summary}<TierBadge tier={d.delivery.tier} /><SourceBadge x={d} /></div>
          </div>
        </div>
        {video && (
          <a href={video} target="_blank" rel="noopener noreferrer" className="relative flex aspect-video max-w-full items-center justify-center rounded-lg border bg-muted/50 hover:bg-muted/70">
            <span className="flex flex-col items-center gap-2 text-center">
              <span className="flex size-11 items-center justify-center rounded-full bg-background shadow-sm"><PlayCircle className="size-5" /></span>
              <span className="text-2xs text-muted-foreground">看视频（新窗口打开）</span>
            </span>
          </a>
        )}
        {steps.length > 0 && (
          <Card className="p-3.5">
            <div className="mb-2 text-sm font-medium">{d.delivery.tier === 'steps' ? `要你做的 ${steps.length} 步` : `装好后要你做的 ${steps.length} 步`}</div>
            <ol className="flex flex-col gap-1.5">
              {steps.map((s, i) => (
                <li key={i} className="flex gap-2 text-sm">
                  <span className="tnum flex size-4 shrink-0 items-center justify-center rounded-full border text-2xs">{i + 1}</span>{s}
                </li>
              ))}
            </ol>
          </Card>
        )}
        <div className="flex flex-wrap gap-1">
          {parts.map((p) => <Badge key={p} variant="secondary" className="font-normal"><Package className="size-3" />{p}</Badge>)}
        </div>
      </div>
      <div className="flex min-w-0 flex-col gap-3">
        <Card className="p-3.5">
          {d.installed.length > 0 ? (
            <Button className="w-full" onClick={() => go(appHref(d.installed[0]))}>已安装 · 管理</Button>
          ) : running ? (
            <Button className="w-full" size="lg" onClick={() => install({ kind: 'app', id: d.id, taskId: running.id, catalog: src })}><Spinner />安装中，看进度</Button>
          ) : (
            <Button className="w-full" size="lg" disabled={!d.supported || !!needs} onClick={() => install({ kind: 'app', id: d.id, catalog: src })}>{d.supported ? '安装' : '这台机器用不了'}</Button>
          )}
          {needs && <UpgradeNeeded version={needs} className="mt-2" />}
          <CopyLine className="mt-2" text={d.command} />
          <div className="mt-2 divide-y">
            <KV k="版本"><span className="tnum">{d.version}</span></KV>
            <KV k="内存"><span className={cn('tnum', check.tone !== 'ok' && 'text-warning')} title={check.text}>{mbText(mem.need)}{mem.min < mem.need ? `（最少 ${mbText(mem.min)}）` : ''}</span></KV>
            {d.disk_mb?.min > 0 && <KV k="磁盘"><span className="tnum">{mbText(d.disk_mb.recommended || d.disk_mb.min)}</span></KV>}
            {d.size_mb > 0 && <KV k="下载"><span className="tnum">{mbText(d.size_mb)}</span></KV>}
            <KV k="支持"><span className="tnum">{d.platforms.map((p) => p.replace('linux/', '')).join('、')}</span></KV>
            {d.license && <KV k="许可证">{d.license}</KV>}
            {homepage && (
              <KV k="主页">
                <a href={homepage} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 underline-offset-4 hover:underline">
                  {d.homepage.replace(/^https?:\/\//, '').replace(/\/$/, '')}<ExternalLink className="size-3" />
                </a>
              </KV>
            )}
          </div>
          <ErrorLine error={d.supported ? null : new Error('这个应用只支持 ' + d.platforms.join('、'))} className="mt-2" />
        </Card>
        <ExpertButton />
      </div>
    </div>
  )
}

// LimitedItem：这台 AcornFox 版本不够、读不了应用包时的详情页。只有目录列表里的基本信息（名称、简介、版本、内存、下载大小、支持的 CPU），
// 加一块说明：要先升级到哪个版本、服务端的原话、升级命令。已经装好的照常能进“管理”。
function LimitedItem({ id, src, version, message }: { id: string; src?: string; version: string; message: string }) {
  const { catalog, tasks, install } = useConsole()
  const a = findApp(catalog, id, src || PUBLIC)
  const running = runningTaskFor(tasks, 'app', id, src || PUBLIC)
  const installed = a?.installed || []
  const mem = a ? appMemory(a) : null
  return (
    <div className="grid gap-4 lg:grid-cols-3" data-testid="item-limited">
      <div className="flex min-w-0 flex-col gap-4 lg:col-span-2">
        <div className="flex items-center gap-3">
          <Tile id={id} name={a?.name || id} icon={a?.icon} iconType={a?.icon_type} size="lg" />
          <div className="min-w-0">
            <h1 className="text-lg font-semibold">{a?.name || id}</h1>
            {a && <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">{a.summary}<TierBadge tier={a.delivery.tier} /><SourceBadge x={a} /></div>}
          </div>
        </div>
        <Card className="p-3.5 text-sm" role="note">
          <div className="font-medium">{upgradeFirstText(version)}，才能看这个应用的完整介绍</div>
          <p className="mt-1.5 text-xs text-muted-foreground">
            这台 AcornFox 版本不够，读不了这个应用现在的安装包，所以这一页只有基本信息。升级后再打开，就能看到完整介绍{installed.length > 0 ? '。已经装好的照常运行，不受影响。' : '并安装。'}
          </p>
          <p className="mt-1.5 break-words text-xs text-muted-foreground" data-testid="item-limited-message">{message}</p>
          {installed.length > 0 && <UpgradeCommands className="mt-2" />}
        </Card>
      </div>
      <div className="flex min-w-0 flex-col gap-3">
        <Card className="p-3.5">
          {installed.length > 0 ? (
            <Button className="w-full" onClick={() => go(appHref(installed[0]))}>已安装 · 管理</Button>
          ) : running ? (
            <Button className="w-full" size="lg" onClick={() => install({ kind: 'app', id, taskId: running.id, catalog: src })}><Spinner />安装中，看进度</Button>
          ) : (
            <Button className="w-full" size="lg" disabled>{a && !a.supported ? '这台机器用不了' : '安装'}</Button>
          )}
          {installed.length === 0 && !running && <UpgradeNeeded version={version} className="mt-2" />}
          {a && (
            <>
              <CopyLine className="mt-2" text={a.command} />
              <div className="mt-2 divide-y">
                <KV k="版本"><span className="tnum">{a.version}</span></KV>
                {mem && <KV k="内存"><span className="tnum">{mbText(mem.need)}{mem.min < mem.need ? `（最少 ${mbText(mem.min)}）` : ''}</span></KV>}
                {a.size_mb > 0 && <KV k="下载"><span className="tnum">{mbText(a.size_mb)}</span></KV>}
                <KV k="支持"><span className="tnum">{a.platforms.map((p) => p.replace('linux/', '')).join('、')}</span></KV>
              </div>
            </>
          )}
        </Card>
        <ExpertButton />
      </div>
    </div>
  )
}
