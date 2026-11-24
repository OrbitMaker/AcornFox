// 应用页（应用中心里的一个应用；样稿第 9 版：介绍就是一篇文档）。
// “介绍性的东西一律内置为一篇文档的形式，不要搞这种多个层级，因为这样后期维护起来比较麻烦。”
// 左边只有一块：应用的介绍文档（show.doc，一篇 Markdown；小标题固定“它能帮你做什么”“装好以后”，见 lib/doc.ts）。
// 右边是一直跟着的“安装栏”：图标、名字、一句话、主按钮，下面几行由程序算出来的话（这台机器装不装得下、要等多久、
// 它要用到的服务有没有）；安装栏下面是“用到它的解决方案”。
// 文档下面是收起的“技术信息”（版本、许可证、它用到的服务、命令行，都从清单里读，不用人维护）。
// 最下面一排“再逛逛”（同一货架的，不够再补热门的）。手机上的顺序是：安装栏 → 文档 → 其余。
//
// 样稿第 10 版（评估 J1、J2、J3、J4、X6、R8）：
// - 主按钮：没装是“安装”；装好了是“打开”（直接开应用现在的地址），下面小字“去管理”；打不开时是“去管理”。
// - 缺服务：它要用到的服务这台机器上没有、又有解决方案能一起补上时，主按钮变成“和 MySQL 一起装好”（装那个解决方案），
//   下面一行小字“只装 WordPress”（点了才是原来的安装弹窗，弹窗里再说怎么办）。没有解决方案能补时仍是“安装”。
//   平台不提示“先装某某”，一起装靠解决方案（AGENTS.md“单机版很纯粹”）。
// - 技术信息里许可证写成人话（原名在悬停提示里），支持的 CPU 写“这台机器能装”。
// - 标题、正文和控制台别的页面同一档字号。
// 服务都用接口给的中文名（uses 每项的 kind_label、provides_detail），页面不为某种服务写专门的文字。
// 要更新版本的 AcornFox 才能装时（needs_platform 有值）：安装按钮不可点，下面说明要升到哪个版本，升级命令收在“用命令行”里。
// 这台 AcornFox 版本不够、连应用包都读不了时，详情接口返回 409（错误里带 needs_platform）：
// 用目录列表里已有的基本信息顶上（LimitedItem），说明要先升级，不当成加载失败。
// 目录里还是旧格式清单的应用，详情接口返回 409（CAT-INST-36）：同样用列表里的基本信息顶上，服务端的话原样显示，
// 安装不可点；这不是版本不够，不给升级命令。
import { useEffect, useState } from 'react'
import { Download, ExternalLink, MemoryStick, Play, Plug, Sparkles } from 'lucide-react'
import { cn, safeHref } from '@/lib/utils'
import { api, type ApiError } from '@/lib/api'
import { PUBLIC, appKey, appMemory, askInstallText, findApp, formerNote, freeMB, groupOf, isFormerFault, licenseText, memoryCheck, shortName, sourceId } from '@/lib/catalog'
import { mbText } from '@/lib/format'
import { faultNeedsPlatform, needsPlatform, upgradeFirstText } from '@/lib/upgrade'
import { WORDS, gap } from '@/lib/display'
import { appHref, go } from '@/lib/router'
import type { AppDetail, ProvideView, Use } from '@/lib/types'
import { useConsole } from '@/state'
import { KindName } from '../prefs/service-form'
import { Empty, ErrorLine, KV, TierBadge, Tile } from '@/components/bits'
import { AskAI, CliFold } from '@/components/fold'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Spinner } from '@/components/ui/controls'
import { SkZone, useSkeleton } from '@/components/ui/skeleton'
import { SkItem } from '@/components/skeletons'
import { CopyLine } from '@/components/ui/copy'
import { ExpertButton } from './shared'
import { Doc } from './doc'
import { Installed, OpenButtons, ProductCard, Shelf, SolutionCard, isSolution, SourceBadge, StoreIcon, fitText, groupName, playIntent, runningTaskFor, solutionFor, useOpenTarget, useServiceInfo } from './shelf'
import { AISLE } from './store-page'
import { UpgradeCommands, UpgradeNeeded } from './upgrade-note'

// ServicesCard：技术信息里的“它要用到的服务”（必须的、可选的，可选的写没有时会怎样）和“它能提供的服务”。两样都没有时不显示。
function ServicesCard({ uses, provides }: { uses: Use[]; provides: ProvideView[] }) {
  if (uses.length === 0 && provides.length === 0) return null
  return (
    <div className="flex flex-col gap-3" data-testid="item-services">
      {uses.length > 0 && (
        <div>
          <div className="text-xs font-medium">它要用到的{WORDS.service}</div>
          <ul className="mt-1.5 flex flex-col gap-1.5" data-testid="item-uses">
            {uses.map((u) => {
              const required = u.required ?? !u.optional
              return (
                <li key={u.use || u.name} className="flex flex-col gap-0.5 text-sm">
                  <span className="flex flex-wrap items-center gap-1.5">
                    <KindName label={u.kind_label} kind={u.kind} />
                    <Badge variant={required ? 'secondary' : 'outline'} className="font-normal">{required ? '必须有' : '可选'}</Badge>
                  </span>
                  {!required && u.without && <span className="text-2xs text-muted-foreground">没有时：{u.without}</span>}
                </li>
              )
            })}
          </ul>
          <p className="mt-1.5 text-2xs text-muted-foreground">安装时自动接上这台机器上已有的；没有时，用解决方案一起装好，或者在“设置 › 服务”里点“{WORDS.addMine}”。</p>
        </div>
      )}
      {provides.length > 0 && (
        <div>
          <div className="text-xs font-medium">它能提供的{WORDS.service}</div>
          <ul className="mt-1.5 flex flex-wrap gap-1" data-testid="item-provides">
            {provides.map((p) => <li key={p.kind}><Badge variant="secondary" className="font-normal"><KindName label={p.kind_label} kind={p.kind} /></Badge></li>)}
          </ul>
          <p className="mt-1.5 text-2xs text-muted-foreground">装好以后，别的应用安装时可以接上它。</p>
        </div>
      )}
    </div>
  )
}

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

// Fact：安装栏里的一行。
function Fact({ icon, tone, children }: { icon: React.ReactNode; tone?: 'ok' | 'warn'; children: React.ReactNode }) {
  return (
    <li className="flex items-start gap-2 text-xs">
      <span className={cn('mt-px shrink-0 [&_svg]:size-icon', tone === 'warn' ? 'text-warning' : 'text-muted-foreground')}>{icon}</span>
      <span className="min-w-0">{children}</span>
    </li>
  )
}

export function ItemPage({ id, catalog: src }: { id: string; catalog?: string }) {
  const { detail: d, error } = useAppDetail(id, src)
  const sk = useSkeleton(!d && !error)
  const { catalog, resources, tasks, install } = useConsole()
  const info = useServiceInfo()
  const opened = useOpenTarget(d && d.installed.length > 0 ? d.installed[0] : undefined)
  // 从卡片的封面点进来的，文档里的第一段视频打开就开始播。
  const [autoplay] = useState(() => { const hit = playIntent.id === id; playIntent.id = ''; return hit })
  const tooOld = faultNeedsPlatform(error)
  if (error && tooOld) return <LimitedItem id={id} src={src} version={tooOld} message={error.message} />
  if (error && isFormerFault(error)) return <LimitedItem id={id} src={src} former message={error.message} />
  if (error) return <Empty>{error.status === 404 ? '应用中心里没有这个应用。' : error.message} <a href="#store" className="underline">回应用中心</a></Empty>
  if (sk.wait || !d) return <SkZone show={sk.show} className="block"><SkItem /></SkZone>
  const free = freeMB(resources)
  const mem = appMemory(d)
  const check = memoryCheck(mem.need, mem.min, free)
  const running = runningTaskFor(tasks, 'app', d.id, sourceId(d))
  const installed = d.installed.length > 0
  const needs = !installed && !running && d.supported ? needsPlatform(d) : ''
  const homepage = safeHref(d.homepage)
  const legacyVideo = safeHref(d.delivery.video)
  const minutes = d.delivery.minutes
  const uses = d.uses || []
  // 它要用到的服务：必须有、这台机器上又还没有的。known：读得到这台机器有哪些。
  const missing = installed ? [] : uses.filter((u) => (u.required ?? !u.optional) && info.have(u.kind) === false)
  const known = uses.every((u) => info.have(u.kind) !== null)
  // 缺服务时能一起补上的解决方案（评估 J2、R8）。
  const fix = missing.length > 0 ? solutionFor(catalog, d.id, missing.map((u) => u.kind), sourceId(d)) : null
  const missingNames = missing.map((u) => u.kind_label || info.label(u.kind)).join('、')
  const group = groupOf(d.category)
  // 用到它的解决方案：几个应用一起的排前面。
  const bundles = (catalog?.recipes || []).filter((r) => sourceId(r) === sourceId(d) && isSolution(r) && r.apps.some((x) => x.app === d.id)).sort((x, y) => y.apps.length - x.apps.length)
  // 再逛逛：先摆同一货架的，不够一排再补热门的。
  const others = (catalog?.apps || []).filter((x) => x.id !== d.id && sourceId(x) === sourceId(d))
  const same = others.filter((x) => groupOf(x.category) === group)
  const peers = [...same, ...others.filter((x) => groupOf(x.category) !== group && x.show?.flag === 'hot')].slice(0, Math.max(same.length, 8))
  const installApp = () => install({ kind: 'app', id: d.id, catalog: src })
  const action = installed ? (
    <OpenButtons t={opened} layout="block" />
  ) : running ? (
    <Button size="lg" className="w-full" onClick={() => install({ kind: 'app', id: d.id, taskId: running.id, catalog: src })}><Spinner />安装中，看进度</Button>
  ) : fix && d.supported && !needs ? (
    <div className="flex flex-col items-center gap-2" data-testid="item-install-with">
      <Button size="lg" className="w-full" onClick={() => install({ kind: 'recipe', id: fix.r.id, catalog: src })}>和 {shortName(fix.provider?.name || missingNames)} 一起装好</Button>
      <button type="button" onClick={installApp} className="text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">只装 {d.name}</button>
    </div>
  ) : (
    <Button size="lg" className="w-full" disabled={!d.supported || !!needs} onClick={installApp}>{d.supported ? '安装' : '这台机器用不了'}</Button>
  )
  return (
    <div>
      <div className={cn('grid gap-x-6 gap-y-4 lg:grid-cols-3', sk.reveal && 'sk-reveal')}>
        <aside className="min-w-0 lg:col-start-3 lg:row-start-1">
          <div className="flex flex-col gap-3 lg:sticky lg:top-16">
            <Card className="p-4" data-testid="item-buy">
              <div className="flex min-w-0 items-center gap-3">
                <StoreIcon a={d} size={44} />
                <div className="min-w-0">
                  <h1 className="truncate text-title font-semibold">{d.name}</h1>
                  <div className="mt-1 flex flex-wrap items-center gap-x-1.5 text-2xs text-muted-foreground">
                    <span>{groupName(d.category)}</span><SourceBadge x={d} />
                    {installed && <Installed />}
                  </div>
                </div>
              </div>
              <p className="mt-3 text-sm">{d.show?.tagline || d.summary}</p>
              <div className="mt-4">{action}</div>
              {!installed && d.supported && !needs && (
                <AskAI size="default" className="mt-2 w-full" label="让 AI 帮我装" done={['已复制', '粘贴给 AI 工作台，它会帮你装']}
                  text={askInstallText('app', { id: d.id, command: d.command, show: d.show, source: d.source }, d.name)} />
              )}
              {needs && <UpgradeNeeded version={needs} className="mt-2" />}
              <ErrorLine error={d.supported ? null : new Error('这台机器装不了这个应用')} className="mt-2" />
              <ul className="mt-4 flex flex-col gap-2.5 border-t pt-4">
                <Fact icon={<Sparkles />}>免费{d.delivery.tier === 'steps' ? '' : ' · 全自动安装'}{minutes ? <> · 约 <span className="tnum">{minutes}</span> 分钟装好</> : null}</Fact>
                {!installed && free !== null && (
                  <Fact icon={<MemoryStick />} tone={check.tone === 'low' ? 'warn' : 'ok'}>
                    <span title={check.text}>{fitText(check.tone)}{check.tone === 'low' ? '，先删掉一个不用的应用再装' : ''}</span>
                  </Fact>
                )}
                {d.size_mb > 0 && !installed && <Fact icon={<Download />}><span>要下载 {mbText(d.size_mb)}</span></Fact>}
                {!installed && uses.length > 0 && missing.length === 0 && known && <Fact icon={<Plug />}>它要用到的{WORDS.service}这台机器上已经有了，会自动接上</Fact>}
                {missing.length > 0 && (
                  <Fact icon={<Plug />} tone={fix ? 'ok' : 'warn'}>
                    {fix ? `它还要用到${gap(missingNames)}，会一起装好并接上` : `它还要用到${gap(missingNames)}，这台机器上还没有`}
                  </Fact>
                )}
              </ul>
            </Card>
            {bundles.length > 0 && (
              <div data-testid="item-solutions">
                <div className="mb-1.5 text-2xs font-medium text-muted-foreground">用到它的解决方案</div>
                <div className="flex flex-col gap-2">{bundles.map((r) => <SolutionCard key={r.id} r={r} size="sm" />)}</div>
              </div>
            )}
            <ExpertButton />
          </div>
        </aside>

        <div className="flex min-w-0 flex-col gap-4 lg:col-span-2 lg:col-start-1 lg:row-start-1">
          {d.show?.doc ? (
            <Card className="p-4 sm:p-5"><Doc text={d.show.doc} color={d.show.color} autoplay={autoplay} /></Card>
          ) : (
            <Card className="p-4 text-sm sm:p-5">
              {d.summary}
              {legacyVideo && (
                <a href={legacyVideo} target="_blank" rel="noopener noreferrer" className="mt-3 flex aspect-video max-h-80 items-center justify-center rounded-lg border bg-muted/50 text-xs text-muted-foreground hover:bg-muted/70"><Play className="mr-1.5 size-4" />看视频（新窗口打开）</a>
              )}
            </Card>
          )}
          <details className="rounded-lg border bg-card" data-testid="item-more">
            <summary className="cursor-pointer select-none px-4 py-3 text-sm font-medium">技术信息</summary>
            <div className="flex flex-col gap-3 border-t px-4 py-3">
              <ServicesCard uses={uses} provides={d.provides_detail || []} />
              <div>
                <div className="text-2xs text-muted-foreground">用命令行装</div>
                <CopyLine className="mt-1" text={d.command} />
                <div className="mt-2 divide-y">
                  <KV k="版本"><span className="font-mono">{d.version}</span></KV>
                  <KV k="内存"><span className="tnum">{mbText(mem.need)}{mem.min < mem.need ? `（最少 ${mbText(mem.min)}）` : ''}</span></KV>
                  {d.disk_mb?.min > 0 && <KV k="磁盘"><span className="tnum">{mbText(d.disk_mb.recommended || d.disk_mb.min)}</span></KV>}
                  <KV k="能不能装"><span title={d.platforms.map((p) => p.replace('linux/', '')).join('、')}>{d.supported ? '这台机器能装' : '这台机器装不了'}</span></KV>
                  {d.license && <KV k="许可证"><span title={d.license}>{licenseText(d.license, d.name)}</span></KV>}
                  {homepage && (
                    <KV k="主页">
                      <a href={homepage} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 underline-offset-4 hover:underline">
                        {d.homepage.replace(/^https?:\/\//, '').replace(/\/$/, '')}<ExternalLink className="size-3" />
                      </a>
                    </KV>
                  )}
                </div>
              </div>
            </div>
          </details>
        </div>
      </div>

      {peers.length > 0 && (
        <Shelf title="再逛逛" icon={AISLE[group]?.[0]} count={peers.length}>
          {peers.map((a) => <ProductCard key={appKey(a)} a={a} free={free} className="w-[13.5rem] shrink-0" />)}
        </Shelf>
      )}
    </div>
  )
}

// LimitedItem：读不了应用包时的详情页。只有目录列表里的基本信息（名称、简介、版本、内存、下载大小、支持的 CPU），加一块说明。
// 两种原因：这台 AcornFox 版本不够（version：要先升级到哪个版本、服务端的原话、升级命令）；
// 目录里还是旧格式的清单（former：服务端的原话，不给升级命令、也不给安装命令，升级也装不了）。已经装好的照常能进“管理”。
function LimitedItem({ id, src, version = '', former = false, message }: { id: string; src?: string; version?: string; former?: boolean; message: string }) {
  const { catalog, tasks, install } = useConsole()
  const a = findApp(catalog, id, src || PUBLIC)
  const running = runningTaskFor(tasks, 'app', id, src || PUBLIC)
  const installed = a?.installed || []
  const mem = a ? appMemory(a) : null
  return (
    <div className="grid gap-4 lg:grid-cols-3" data-testid={former ? 'item-limited-former' : 'item-limited'}>
      <div className="flex min-w-0 flex-col gap-4 lg:col-span-2">
        <div className="flex items-center gap-3">
          <Tile id={id} name={a?.name || id} icon={a?.icon} iconType={a?.icon_type} size="lg" />
          <div className="min-w-0">
            <h1 className="text-base font-semibold">{a?.name || id}</h1>
            {a && <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">{a.summary}<TierBadge tier={a.delivery.tier} /><SourceBadge x={a} /></div>}
          </div>
        </div>
        {former ? (
          <Card className="p-3.5 text-sm" role="note" data-testid="item-former">
            <div className="font-medium">{formerNote({ former: true, note: a?.note })}</div>
            <p className="mt-1.5 break-words text-xs text-muted-foreground" data-testid="item-former-message">{message}</p>
          </Card>
        ) : (
          <Card className="p-3.5 text-sm" role="note">
            <div className="font-medium">{upgradeFirstText(version)}，才能看这个应用的完整介绍</div>
            <p className="mt-1.5 text-xs text-muted-foreground">
              这台机器上的 AcornFox 版本不够，读不了这个应用现在的安装包，所以这一页只有基本信息。升级后再打开，就能看到完整介绍{installed.length > 0 ? '。已经装好的照常运行，不受影响。' : '并安装。'}
            </p>
            <p className="mt-1.5 break-words text-xs text-muted-foreground" data-testid="item-limited-message">{message}</p>
            {installed.length > 0 && <UpgradeCommands className="mt-2" />}
          </Card>
        )}
      </div>
      <div className="flex min-w-0 flex-col gap-3">
        <Card className="p-3.5">
          {installed.length > 0 ? (
            <Button className="w-full" onClick={() => go(appHref(installed[0]))}>去管理</Button>
          ) : running ? (
            <Button className="w-full" size="lg" onClick={() => install({ kind: 'app', id, taskId: running.id, catalog: src })}><Spinner />安装中，看进度</Button>
          ) : (
            <Button className="w-full" size="lg" disabled>{a && !a.supported ? '这台机器用不了' : '安装'}</Button>
          )}
          {installed.length === 0 && !running && !former && <UpgradeNeeded version={version} className="mt-2" />}
          {a && (
            <>
              {/* 旧格式的清单装不了：照着复制的安装命令跑只会被拒，所以不给 */}
              {!former && <CliFold className="mt-2"><CopyLine text={a.command} /></CliFold>}
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
