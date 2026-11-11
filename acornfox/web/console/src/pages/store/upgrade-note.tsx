// 应用或配方要更新版本的 AcornFox 才能装时的提示：卡片上一行（UpgradeHint），详情页和安装弹窗里一块带升级命令的说明（UpgradeNeeded）。
// 安装被拒绝或失败、原因是版本不够时（错误里带 needs_platform），错误下面用 UpgradeFirst 说“要先升级到 x.y.z”并给同样的命令。
import { ArrowUpCircle } from 'lucide-react'
import { cn } from '@/lib/utils'
import { go } from '@/lib/router'
import { UPGRADE_CN, UPGRADE_GLOBAL, needsPlatformText, upgradeFirstText } from '@/lib/upgrade'
import { useConsole } from '@/state'
import { Button } from '@/components/ui/button'
import { CopyLine } from '@/components/ui/copy'

// UpgradeCommands：升级命令，中国大陆和其他地区各一条（和设置的“升级”分区一样）。
export function UpgradeCommands({ className }: { className?: string }) {
  return (
    <div className={cn('flex flex-col gap-1.5 text-xs', className)} data-testid="upgrade-commands">
      <p className="text-muted-foreground">在服务器上运行下面的命令升级（二选一）。升级期间控制台会暂时连不上，升级完再回来装。</p>
      <span className="text-2xs text-muted-foreground">中国大陆</span>
      <CopyLine text={UPGRADE_CN} label="复制升级命令（中国大陆）" />
      <span className="text-2xs text-muted-foreground">其他地区</span>
      <CopyLine text={UPGRADE_GLOBAL} label="复制升级命令（其他地区）" />
    </div>
  )
}

// UpgradeFirst：放在错误说明下面，一句“要先升级到 x.y.z”加升级命令。服务端的说明在它上面原样显示，这里不重复。
export function UpgradeFirst({ version, className }: { version: string; className?: string }) {
  return (
    <div className={cn('text-xs', className)} data-testid="upgrade-first">
      <div className="flex items-center gap-1.5 font-medium text-warning">
        <ArrowUpCircle className="size-3.5 shrink-0" />{upgradeFirstText(version)}
      </div>
      <UpgradeCommands className="mt-1.5" />
    </div>
  )
}

// UpgradeNeeded：一句结论、现在的版本、升级命令。
export function UpgradeNeeded({ version, className }: { version: string; className?: string }) {
  const { status } = useConsole()
  return (
    <div className={cn('rounded-md border border-warning/40 px-3 py-2.5 text-xs', className)} role="note" data-testid="needs-platform">
      <div className="flex items-center gap-1.5 font-medium text-warning">
        <ArrowUpCircle className="size-3.5 shrink-0" />{needsPlatformText(version)}
      </div>
      {status?.version && <div className="mt-1 text-muted-foreground">这台服务器现在是 <span className="tnum">{status.version}</span>。</div>}
      <UpgradeCommands className="mt-1.5" />
    </div>
  )
}

// UpgradeHint：卡片上的一行，“怎么升级”跳到设置的“升级”分区。
export function UpgradeHint({ version, className }: { version: string; className?: string }) {
  const text = needsPlatformText(version)
  return (
    <div className={cn('flex min-w-0 items-center gap-1.5 text-2xs text-warning', className)} data-testid="needs-platform">
      <span className="min-w-0 truncate" title={text}>{text}</span>
      <Button variant="link" size="sm" className="h-auto shrink-0 p-0 text-2xs" onClick={(e) => { e.stopPropagation(); go('#prefs/update') }}>怎么升级</Button>
    </div>
  )
}
