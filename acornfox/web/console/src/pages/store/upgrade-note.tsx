// 应用或配方要更新版本的 AcornFox 才能装时的提示：卡片上一行（UpgradeHint），详情页和安装弹窗里一块说明（UpgradeNeeded）。
// 安装被拒绝或失败、原因是版本不够时（错误里带 needs_platform），错误下面用 UpgradeFirst 说“要先升级到 x.y.z”。
// 样稿第 10 版（评估 X4）：页面上不直接摆命令，升级命令收进默认收起的“用命令行”；主路是“去升级”（设置 › 升级）。
import { ArrowUpCircle } from 'lucide-react'
import { cn } from '@/lib/utils'
import { go } from '@/lib/router'
import { UPGRADE_CN, UPGRADE_GLOBAL, needsPlatformText, upgradeFirstText } from '@/lib/upgrade'
import { useConsole } from '@/state'
import { Button } from '@/components/ui/button'
import { CopyLine } from '@/components/ui/copy'
import { CliFold } from '@/components/fold'

// UpgradeCommands：“去升级”（设置 › 升级），升级命令收在“用命令行”里（中国大陆和其他地区各一条，和设置的“升级”分区一样）。
export function UpgradeCommands({ className }: { className?: string }) {
  return (
    <div className={cn('flex flex-col items-start gap-1.5 text-xs', className)} data-testid="upgrade-commands">
      <Button variant="outline" size="sm" onClick={(e) => { e.stopPropagation(); go('#prefs/update') }}>去升级</Button>
      <CliFold>
        <p className="text-muted-foreground">在这台机器上运行下面的命令升级（二选一）。升级期间控制台会暂时连不上，升级完再回来装。</p>
        <span className="text-2xs text-muted-foreground">中国大陆</span>
        <CopyLine text={UPGRADE_CN} label="复制升级命令（中国大陆）" />
        <span className="text-2xs text-muted-foreground">其他地区</span>
        <CopyLine text={UPGRADE_GLOBAL} label="复制升级命令（其他地区）" />
      </CliFold>
    </div>
  )
}

// UpgradeFirst：放在错误说明下面，一句“要先升级到 x.y.z”加升级命令。服务端的说明在它上面原样显示，这里不重复。
export function UpgradeFirst({ version, className }: { version: string; className?: string }) {
  return (
    <div className={cn('text-xs', className)} data-testid="upgrade-first">
      <div className="flex items-center gap-1.5 font-medium text-warning">
        <ArrowUpCircle className="size-icon shrink-0" />{upgradeFirstText(version)}
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
        <ArrowUpCircle className="size-icon shrink-0" />{needsPlatformText(version)}
      </div>
      {status?.version && <div className="mt-1 text-muted-foreground">这台机器现在是 <span className="tnum">{status.version}</span>。</div>}
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
      <Button variant="link" size="sm" data-arrow="" className="h-auto shrink-0 p-0 text-2xs" onClick={(e) => { e.stopPropagation(); go('#prefs/update') }}>怎么升级</Button>
    </div>
  )
}
