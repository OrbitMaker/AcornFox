// 设置（样稿第 10 版，评估 N1、N2、N13、N14、R3；第 11 版把体检并了进来）。左边分页分两组：
//   常用：外观（路由 general，以前叫“通用”）、服务、访问、这台机器（machine：升级和体检两行，见 machine.tsx）；
//   高级（默认收起，点开才显示；地址直接进高级分区时自动展开）：平台自动填的（platform）、开发者与 AI（dev）。
// 地址 #prefs/update、#prefs/checkup 进“这台机器”并展开升级、体检那一行。
// 体检以前是单独一页（#runtime），更早还有设置里的“运行环境”“存储”“下载源”三个分页：#runtime 和 #prefs/engine、storage、mirror、
// runtime、sources 进来时直接换成 #prefs/checkup（不留在浏览历史里）。不认识的分区显示第一页“外观”。
// “服务”分区在 services.tsx；“访问”分区在 access.tsx（最下面一行是退出登录）；“平台自动填的”在 platform.tsx。
import * as React from 'react'
import { Bot, ChevronRight, Lock, Palette, Plug, Server, Wand2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { ACCENTS, type Theme } from '@/lib/theme'
import { WORDS } from '@/lib/display'
import { useConsole } from '@/state'
import { Card } from '@/components/ui/card'
import { Tabs } from '@/components/ui/controls'
import { CopyLine } from '@/components/ui/copy'
import { Services } from './services'
import { Access } from './access'
import { PlatformFilled } from './platform'
import { Machine, type MachineRow } from './machine'

type NavItem = readonly [string, string, typeof Plug]

export const PREF_COMMON: readonly NavItem[] = [
  ['general', '外观', Palette],
  ['services', '服务', Plug],
  ['access', '访问', Lock],
  ['machine', WORDS.machine, Server],
] as const

export const PREF_ADVANCED: readonly NavItem[] = [
  ['platform', WORDS.platformFilled, Wand2],
  ['dev', '开发者与 AI', Bot],
] as const

export const PREF_NAV: readonly NavItem[] = [...PREF_COMMON, ...PREF_ADVANCED]

// “这台机器”里直接展开某一行的地址。
const MACHINE_ROWS: Record<string, MachineRow> = { update: 'update', checkup: 'checkup' }

// 并进体检的旧地址（旧链接、书签照样能用）：换成 #prefs/checkup。
export const MOVED_TO_CHECKUP = ['engine', 'storage', 'mirror', 'runtime', 'sources']

// 只有一种语言时不显示“语言”这一行（评估 N2）；以后加了语言再显示。
const LANGUAGES = ['简体中文']

const CLI_CN = 'curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash -s -- cli'
const CLI_GLOBAL = 'curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | bash -s -- cli'

function Row({ title, desc, children }: { title: React.ReactNode; desc?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <div className="flex min-h-ctl flex-wrap items-center justify-between gap-3 py-2.5">
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

function General() {
  const { theme, setTheme, accent, setAccent } = useConsole()
  return (
    <>
      <Row title="明暗"><Tabs<Theme> value={theme} onChange={setTheme} label="明暗" items={[['light', '浅色'], ['dark', '深色'], ['system', '跟随系统']]} /></Row>
      <Row title="主色">
        <div role="radiogroup" aria-label="主色" className="flex items-center gap-0.5 rounded-md bg-muted p-0.5">
          {ACCENTS.map(([k, l, c]) => (
            <button key={k} type="button" role="radio" aria-checked={accent === k} title={'主色：' + l} aria-label={'主色：' + l} onClick={() => setAccent(k)}
              className={cn('tap size-ctl-xs rounded border-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring', accent === k ? 'border-foreground' : 'border-transparent')}>
              <span className="block size-full rounded-sm" style={{ background: c }} />
            </button>
          ))}
        </div>
      </Row>
      {LANGUAGES.length > 1 && <Row title="语言"><span className="text-xs">{LANGUAGES.join('、')}</span></Row>}
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
      <Block title="在自己电脑上装命令行" desc="Linux、Mac；用来管理这台机器">
        <CopyLine text={CLI_CN} />
        <CopyLine text={CLI_GLOBAL} />
      </Block>
      <Block title="部署自己的项目" desc="把自己写的程序放到这台机器上跑：在项目目录里运行，装好后出现在“我的应用”里">
        <CopyLine text="acornfox deploy" />
      </Block>
      <Block title="常用命令" desc="页面能做的事命令行都能做，加 --json 输出给程序或 AI 读">
        <CopyLine text="acornfox apps" />
        <CopyLine text="acornfox catalog list" />
        <CopyLine text="acornfox install 应用名 --plan" />
        <CopyLine text="sudo acornfox doctor --json" />
      </Block>
    </>
  )
}

function NavLink({ item, cur }: { item: NavItem; cur: string }) {
  const [k, l, Icon] = item
  return (
    <a href={'#prefs/' + k} aria-current={cur === k ? 'page' : undefined}
      className={cn('flex h-nav shrink-0 items-center gap-2 whitespace-nowrap rounded-md px-2 text-sm hover:bg-accent', cur === k ? 'bg-accent font-medium' : 'text-muted-foreground')}>
      <Icon className="hidden size-icon md:block" />{l}
    </a>
  )
}

export function PrefsPage({ sec }: { sec?: string }) {
  const moved = !!sec && MOVED_TO_CHECKUP.includes(sec)
  React.useEffect(() => { if (moved) location.replace('#prefs/checkup') }, [moved])
  const row = sec ? MACHINE_ROWS[sec] : undefined
  const cur = PREF_NAV.find((x) => x[0] === (row ? 'machine' : sec)) || PREF_NAV[0]
  const inAdvanced = PREF_ADVANCED.some((x) => x[0] === cur[0])
  const [adv, setAdv] = React.useState(inAdvanced)
  React.useEffect(() => { if (inAdvanced) setAdv(true) }, [inAdvanced])
  if (moved) return null
  const body: Record<string, React.ReactNode> = {
    general: <General />, services: <Services />, access: <Access />, machine: <Machine want={row} />, platform: <PlatformFilled />, dev: <Dev />,
  }
  return (
    <div>
      <div className="flex flex-col gap-4 md:flex-row">
        <nav aria-label="设置分区" className="no-scrollbar flex items-center gap-0.5 overflow-x-auto md:w-40 md:shrink-0 md:flex-col md:items-stretch">
          {PREF_COMMON.map((x) => <NavLink key={x[0]} item={x} cur={cur[0]} />)}
          <button type="button" aria-expanded={adv} onClick={() => setAdv((o) => !o)} data-testid="prefs-advanced-toggle"
            className="flex h-nav shrink-0 items-center gap-1 whitespace-nowrap rounded-md px-2 text-2xs text-muted-foreground hover:text-foreground md:mt-3">
            <ChevronRight className={cn('size-3 transition-transform duration-[var(--dur-sm-in)] ease-out-strong', adv && 'rotate-90')} aria-hidden="true" />高级
          </button>
          {adv && PREF_ADVANCED.map((x) => <NavLink key={x[0]} item={x} cur={cur[0]} />)}
        </nav>
        <Card className="min-w-0 max-w-2xl flex-1 px-4 md:self-start py-1"><div className="divide-y" data-testid={'prefs-' + cur[0]}>{body[cur[0]]}</div></Card>
      </div>
    </div>
  )
}
