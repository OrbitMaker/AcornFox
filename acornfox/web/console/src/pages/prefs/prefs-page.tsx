// 设置：通用、访问、运行环境、存储、下载源、升级、开发者与 AI。没有后端支撑的项（提醒、账号）不显示。
// 接口：GET /v1/lan-console、GET /v1/public-console（控制台入口只读；开关只能在服务器上用命令改）、
//       GET /v1/runtime/checks（数据目录、磁盘、下载顺序）、GET /v1/metrics/host、GET /v1/status（版本）、
//       GET /v1/catalog（目录来源，含私有目录）、POST /v1/catalog/connect（连接私有目录，这一版藏起，见 PRIVATE_CATALOG_UI）。
// 升级只有升级命令：平台只能升级、不能回滚（升级没成功时脚本自动恢复原样）。
import * as React from 'react'
import { Bot, Globe, HardDrive, Link2, Lock, Server, SlidersHorizontal, ArrowUpCircle } from 'lucide-react'
import { cn } from '@/lib/utils'
import { api } from '@/lib/api'
import { usePoll } from '@/lib/hooks'
import { go } from '@/lib/router'
import { timeText } from '@/lib/format'
import { PRIVATE_CATALOG_UI } from '@/lib/features'
import { UPGRADE_CN, UPGRADE_GLOBAL } from '@/lib/upgrade'
import type { HostView, LANConsoleStatus, LANDevice, PublicConsoleStatus, RuntimeChecks } from '@/lib/types'
import { ACCENTS, type Theme } from '@/lib/theme'
import { useConsole } from '@/state'
import { KV, PageHead } from '@/components/bits'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Spinner, Switch, Tabs } from '@/components/ui/controls'
import { CopyLine } from '@/components/ui/copy'
import { Progress } from '@/components/ui/progress'
import { useToast } from '@/components/ui/toast'
import { ConnectCatalogDialog } from '../store/connect-dialog'

export const PREF_NAV = [
  ['general', '通用', SlidersHorizontal],
  ['access', '访问', Lock],
  ['engine', '运行环境', Server],
  ['storage', '存储', HardDrive],
  ['mirror', '下载源', Globe],
  ['update', '升级', ArrowUpCircle],
  ['dev', '开发者与 AI', Bot],
] as const

const CLI_CN = 'curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/install-cli.sh | bash'
const CLI_GLOBAL = 'curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/install-cli.sh | bash'

function Row({ title, desc, children }: { title: React.ReactNode; desc?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 py-2.5">
      <div className="min-w-0">
        <div className="text-sm">{title}</div>
        {desc && <div className="text-2xs text-muted-foreground">{desc}</div>}
      </div>
      {children && <div className="shrink-0">{children}</div>}
    </div>
  )
}

function Block({ title, desc, children }: { title: React.ReactNode; desc?: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5 py-2.5">
      <div className="text-sm">{title}</div>
      {desc && <div className="text-2xs text-muted-foreground">{desc}</div>}
      {children}
    </div>
  )
}

function useChecks() {
  return usePoll((s) => api.get<RuntimeChecks>('/v1/runtime/checks', s).catch(() => null), 0)
}

function General() {
  const { theme, setTheme, accent, setAccent } = useConsole()
  return (
    <>
      <Row title="外观"><Tabs<Theme> value={theme} onChange={setTheme} label="外观" items={[['light', '浅色'], ['dark', '深色'], ['system', '跟随系统']]} /></Row>
      <Row title="主色" desc="按钮、开关这些主要动作的颜色；只记在这个浏览器里">
        <div role="radiogroup" aria-label="主色" className="flex items-center gap-0.5 rounded-md bg-muted p-0.5">
          {ACCENTS.map(([k, l, c]) => (
            <button key={k} type="button" role="radio" aria-checked={accent === k} title={'主色：' + l} aria-label={'主色：' + l} onClick={() => setAccent(k)}
              className={cn('size-6 rounded border-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring', accent === k ? 'border-foreground' : 'border-transparent')}>
              <span className="block size-full rounded-sm" style={{ background: c }} />
            </button>
          ))}
        </div>
      </Row>
      <Row title="语言" desc="这一版只有中文"><Badge variant="outline">简体中文</Badge></Row>
    </>
  )
}

function lanDesc(l: LANConsoleStatus): string {
  if (!l.enabled) return '没开：控制台只在服务器本机（经 SSH 隧道）能打开'
  const where = l.url || '端口 ' + l.port
  if (l.password_set === false) return where + ' · 还没设密码：请现在就用局域网里的电脑打开它设一个'
  if (l.generated_password) return where + ' · 用开启时生成的密码登录'
  if (l.password_set_at) return `${where} · 密码是 ${timeText(l.password_set_at)} 从 ${l.password_set_from || '未知地址'} 设的`
  return where + ' · 要密码登录'
}

// 记住的设备：列表和移除。移除只会让那台设备重新输密码，所以页面上可以点（要带会话的校验值）。
function LANDevices() {
  const toast = useToast()
  const list = usePoll((s) => api.get<{ devices: LANDevice[] }>('/v1/lan-console/devices', s).then((b) => b.devices || []).catch(() => null), 0)
  const [busy, setBusy] = React.useState('')
  if (!list.data) return null
  const remove = async (id: string) => {
    setBusy(id || '*')
    try {
      await api.send('DELETE', '/v1/lan-console/devices' + (id ? '/' + encodeURIComponent(id) : ''))
      toast(id ? '已移除这台设备' : '已移除全部设备', '它们要重新输密码才能进')
      await list.refresh()
    } catch (e) {
      toast('没能移除', e instanceof Error ? e.message : '', 'error')
    } finally {
      setBusy('')
    }
  }
  return (
    <div className="mb-2.5 rounded-md border" data-testid="lan-devices">
      <div className="flex items-center justify-between border-b px-3 py-2 text-xs font-medium">
        <span>记住的设备 <span className="font-normal text-muted-foreground">登录时勾了“记住这台设备 90 天”的</span></span>
        {list.data.length > 1 && <Button size="sm" variant="ghost" disabled={!!busy} onClick={() => remove('')}>全部移除</Button>}
      </div>
      {list.data.length === 0 && <div className="px-3 py-2.5 text-xs text-muted-foreground">没有</div>}
      {list.data.map((d) => (
        <div key={d.id} className="flex items-center gap-3 border-b px-3 py-2 text-xs last:border-b-0">
          <div className="min-w-0 flex-1">
            <div className="truncate font-medium">{d.name}</div>
            <div className="truncate text-2xs text-muted-foreground">上次使用 {timeText(d.last_used_at)}{d.last_ip ? ' · ' + d.last_ip : ''} · {d.expires_at.slice(0, 10)} 到期</div>
          </div>
          <Button size="sm" variant="outline" disabled={!!busy} aria-label={'移除 ' + d.name} onClick={() => remove(d.id)}>移除</Button>
        </div>
      ))}
    </div>
  )
}

function Access() {
  const lan = usePoll((s) => api.get<LANConsoleStatus>('/v1/lan-console', s), 0)
  const pub = usePoll((s) => api.get<PublicConsoleStatus>('/v1/public-console', s), 0)
  const loading = lan.loading || pub.loading
  return (
    <>
      {loading && <div className="flex items-center gap-2 py-2.5 text-xs text-muted-foreground"><Spinner />正在读取…</div>}
      {lan.data && (
        <Row title="局域网控制台" desc={lanDesc(lan.data)}>
          <Switch checked={lan.data.enabled} disabled label="局域网控制台" />
        </Row>
      )}
      {lan.data?.enabled && lan.data.password_set_at && (
        <p className="pb-2 text-2xs text-muted-foreground">设密码的时间和地址不是你？在服务器上运行下面的命令清掉密码，再打开局域网地址重新设；所有登录和记住的设备会一起作废。</p>
      )}
      {lan.data?.enabled && <CopyLine className="mb-2.5" text="acornfox lan-console reset-password" />}
      {lan.data?.enabled && <LANDevices />}
      {lan.data && <CopyLine className="mb-2.5" text={lan.data.enabled ? 'acornfox lan-console disable' : 'acornfox lan-console enable'} />}
      {pub.data && (
        <Row title="公网控制台" desc={pub.data.enabled ? `${pub.data.url || pub.data.domain} · HTTPS · 要密码登录${pub.data.enabled_at ? ' · ' + timeText(pub.data.enabled_at) + ' 开的' : ''}` : pub.data.verifying ? `正在验证域名 ${pub.data.verifying}` : '没开：要用自己的域名'}>
          <Switch checked={pub.data.enabled} disabled label="公网控制台" />
        </Row>
      )}
      {pub.data && <CopyLine className="mb-2.5" text={pub.data.enabled ? 'acornfox public-console disable' : 'acornfox public-console enable 你的域名'} />}
      {!loading && !lan.data && !pub.data && <Row title="访问" desc="这台服务器不支持把控制台开到局域网或公网" />}
      {!loading && (lan.data || pub.data) && (
        <p className="pb-2.5 text-2xs text-muted-foreground">开关和清密码只能在服务器上用上面的命令做：控制台页面不能把自己开放给更多人。局域网控制台第一次打开时在浏览器里设密码，只给局域网里的地址用，云主机开不了；公网控制台开启时给一次密码，只显示这一次。</p>
      )}
    </>
  )
}

function Engine() {
  const { status } = useConsole()
  return (
    <>
      <div className="py-1">
        <KV k="Docker">{status?.docker_version || '—'}</KV>
        <KV k="执行器">{status?.runner === 'ok' ? '正常' : status ? '没有应答' : '—'}</KV>
        {status?.edge?.state && <KV k="网关（Caddy）">{status.edge.state === 'running' ? '运行中' : status.edge.state === 'preparing' ? '正在准备' : status.edge.message || '有问题'}</KV>}
      </div>
      <div className="flex gap-1.5 py-2.5">
        <Button variant="outline" size="sm" onClick={() => go('#runtime')}>查看自检</Button>
      </div>
    </>
  )
}

function Storage() {
  const checks = useChecks()
  const host = usePoll((s) => api.get<HostView>('/v1/metrics/host', s), 0)
  const h = host.data
  const disk = checks.data?.items.find((i) => i.text.startsWith('磁盘：'))
  const GB = 1024 ** 3
  return (
    <>
      {checks.data?.data_dir && <Row title="数据目录" desc="应用的数据卷之外，AcornFox 自己的数据都在这里"><span className="mono text-xs">{checks.data.data_dir}</span></Row>}
      {h?.available && h.disk_total && h.disk_used !== undefined && (
        <Block title="磁盘" desc={`已用 ${(h.disk_used / GB).toFixed(1)} GB / ${(h.disk_total / GB).toFixed(1)} GB`}>
          <Progress value={(h.disk_used / h.disk_total) * 100} tone={h.disk_total - h.disk_used < 3 * GB ? 'danger' : h.disk_total - h.disk_used < 10 * GB ? 'warning' : undefined} label="磁盘已用" />
        </Block>
      )}
      {disk && <Row title="剩余空间" desc={disk.text.replace(/^磁盘：/, '')}>{disk.level !== 'ok' && <Badge variant={disk.level === 'fail' ? 'danger' : 'warning'} className="mono">{disk.code}</Badge>}</Row>}
      <Row title="可回退版本" desc="除了正在运行的版本，每个应用再留 5 个不同的镜像用来回退，更早的自动删掉" />
      <Block title="可清理的内容" desc="控制台还不能清理。在服务器上先看占用，确认不需要再清理（会删掉停掉的容器和没用的镜像）：">
        <CopyLine text="sudo docker system df" />
      </Block>
    </>
  )
}

function Mirror({ onConnect }: { onConnect: () => void }) {
  const checks = useChecks()
  const { catalog } = useConsole()
  const src = checks.data?.sources
  const cats = catalog?.sources || []
  return (
    <>
      {src && (
        <>
          <Row title="网络" desc={src.region_source === 'install.env' ? '安装时选的' : src.region_source === 'default' ? '默认' : '自动判断'}>
            <Badge variant="outline">{src.china ? '中国大陆：先用国内源' : '海外：先用官方源'}</Badge>
          </Row>
          <Block title="Docker Hub 镜像" desc="按这个顺序试，一个不通换下一个">
            <ol className="flex flex-col gap-1" data-testid="mirror-order">
              {src.docker_hub.map((h, i) => (
                <li key={h} className="flex items-center gap-2 text-xs"><span className="tnum flex size-4 shrink-0 items-center justify-center rounded-full border text-2xs">{i + 1}</span><span className="mono">{h === 'registry-1.docker.io' ? 'Docker Hub 官方' : h}</span></li>
              ))}
            </ol>
          </Block>
          {src.ghcr && src.ghcr.length > 0 && <Row title="GitHub 镜像（ghcr.io）的国内源"><span className="mono text-xs">{src.ghcr.join('、')}</span></Row>}
          <Row title="下载网关" desc="AcornFox 自己存的安装文件和大镜像包从这里取"><span className="mono text-xs">{src.gateway ? src.gateway.replace(/^https?:\/\//, '') : '已关掉'}</span></Row>
        </>
      )}
      {!src && checks.loading && <div className="flex items-center gap-2 py-2.5 text-xs text-muted-foreground"><Spinner />正在读取…</div>}
      <Block title="应用目录">
        <ul className="flex flex-col gap-1.5" data-testid="catalog-sources">
          {cats.map((c) => (
            <li key={c.id} className="flex flex-wrap items-center gap-2 text-xs">
              <span className="font-medium">{c.name}</span>
              {c.kind === 'private' && <span className="mono text-2xs text-muted-foreground">{c.id}</span>}
              {c.disconnected ? <Badge variant="danger">已断开</Badge>
                : c.code === 'CAT-SYNC-01' ? <Badge variant="outline" title={c.message}>只用程序自带的目录</Badge>
                : c.code ? <Badge variant="warning" className="mono" title={c.message}>{c.code}</Badge>
                : <Badge variant="success">正常</Badge>}
              <span className="ml-auto text-2xs text-muted-foreground">{c.serial ? `第 ${c.serial} 版 · ${c.apps} 个应用` : '还没同步下来'}{c.kind === 'public' && c.origin === 'snapshot' ? ' · 程序自带' : ''}</span>
            </li>
          ))}
        </ul>
      </Block>
      {PRIVATE_CATALOG_UI && <div className="py-2.5"><Button variant="outline" size="sm" onClick={onConnect}><Link2 className="size-3.5" />连接私有目录</Button></div>}
    </>
  )
}

function Update() {
  const { status } = useConsole()
  return (
    <>
      <Row title="当前版本"><span className="tnum text-sm">{status?.version || '—'}</span></Row>
      <Block title="升级（中国大陆）" desc="在服务器上运行（会先打印要做的事）">
        <CopyLine text={UPGRADE_CN} />
      </Block>
      <Block title="升级（其他地区）"><CopyLine text={UPGRADE_GLOBAL} /></Block>
    </>
  )
}

function Dev() {
  return (
    <>
      <Block title="AI 工作台的 Skill" desc="装进 Claude Code、Codex 等 AI 工作台；版本和命令行一致">
        <CopyLine text="acornfox skill install" />
        <CopyLine text="acornfox skill print" />
      </Block>
      <Block title="在自己电脑上装命令行" desc="Linux、Mac；用来管理这台服务器">
        <CopyLine text={CLI_CN} />
        <CopyLine text={CLI_GLOBAL} />
      </Block>
      <Block title="常用命令" desc="页面能做的事命令行都能做，加 --json 输出给程序或 AI 读">
        <CopyLine text="acornfox apps" />
        <CopyLine text="acornfox catalog list" />
        <CopyLine text="acornfox install new-api" />
        <CopyLine text="sudo acornfox doctor --json" />
      </Block>
    </>
  )
}

export function PrefsPage({ sec }: { sec?: string }) {
  const cur = PREF_NAV.find((x) => x[0] === sec) || PREF_NAV[0]
  const [connect, setConnect] = React.useState(false)
  const body: Record<string, React.ReactNode> = {
    general: <General />, access: <Access />, engine: <Engine />, storage: <Storage />,
    mirror: <Mirror onConnect={() => setConnect(true)} />, update: <Update />, dev: <Dev />,
  }
  return (
    <div>
      <PageHead title="设置" />
      <div className="flex flex-col gap-4 md:flex-row">
        <nav aria-label="设置分区" className="no-scrollbar flex gap-0.5 overflow-x-auto md:w-40 md:shrink-0 md:flex-col">
          {PREF_NAV.map(([k, l, Icon]) => (
            <a key={k} href={'#prefs/' + k} aria-current={cur[0] === k ? 'page' : undefined}
              className={cn('flex h-7 shrink-0 items-center gap-2 whitespace-nowrap rounded-md px-2 text-sm hover:bg-accent', cur[0] === k ? 'bg-accent font-medium' : 'text-muted-foreground')}>
              <Icon className="hidden size-3.5 md:block" />{l}
            </a>
          ))}
        </nav>
        <Card className="min-w-0 max-w-2xl flex-1 px-4 py-1"><div className="divide-y" data-testid={'prefs-' + cur[0]}>{body[cur[0]]}</div></Card>
      </div>
      {PRIVATE_CATALOG_UI && <ConnectCatalogDialog open={connect} onClose={() => setConnect(false)} />}
    </div>
  )
}
