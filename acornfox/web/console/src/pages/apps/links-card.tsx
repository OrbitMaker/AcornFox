// 应用概览里“谁能打开”下面的“它用到的服务”卡片（样稿第 10 版，评估 E2：不再叫“连接”）。一行一句：
//   用别人的：“存数据用的是：PostgreSQL 16”，右边“✓ 已接上”（那条服务现在不能用时换成 lib/services.ts 的状态文字）；
//             用户自己添加的写“发邮件用的是：你添加的“我的邮箱””。
//   给别人提供、有应用在用：每个在用的应用一行“n8n 正在用它，所以现在不能删”。
//   只有后一种时卡片标题是“谁在用它”。名字一律用应用中心里的名字（name）。
// 一行都没有、或旧服务端没有服务接口（404）时整张卡片不显示。数据整理在 lib/app-links.ts。
import type { ReactNode } from 'react'
import { Check, Link2 } from 'lucide-react'
import { usesPurpose, usesWho, type AppLinks } from '@/lib/app-links'
import { TONE_BADGE } from '@/lib/services'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Tile } from '@/components/bits'

// 小图块：页面传进来时用目录里的图标；没传就用名字缩写。
export type TileOf = (app: string) => ReactNode

const defaultTile: TileOf = (name) => <Tile id={name} name={name} size="xs" />

function Line({ left, children, right, testId }: { left: ReactNode; children: ReactNode; right?: ReactNode; testId: string }) {
  return (
    <div className="flex items-center gap-2.5 py-1.5" data-testid={testId}>
      {left}
      <span className="min-w-0 flex-1 text-sm" data-testid="link-text">{children}</span>
      {right}
    </div>
  )
}

const userTile = (
  <div className="flex size-6 shrink-0 items-center justify-center rounded border text-muted-foreground" aria-hidden="true"><Link2 className="size-icon" /></div>
)

export function LinksCard({ links, tile = defaultTile, name = (x) => x }: { links: AppLinks | null; tile?: TileOf; name?: (app: string) => string }) {
  if (!links || (links.uses.length === 0 && links.usedBy.length === 0)) return null
  return (
    <Card className="px-3.5 pb-2 pt-2" data-testid="ov-links">
      <div className="pt-1 text-xs font-medium">{links.uses.length > 0 ? '它用到的服务' : '谁在用它'}</div>
      <div className="mt-0.5">
        {links.uses.map((u) => (
          <Line key={'u-' + u.service} testId="ov-link-uses" left={u.provider ? tile(u.provider) : userTile}
            right={u.status.tone === 'success'
              ? <Badge variant="success"><Check className="size-3" />已接上</Badge>
              : <Badge variant={TONE_BADGE[u.status.tone]} title={u.status.detail}>{u.status.text}</Badge>}>
            <span className="text-muted-foreground">{usesPurpose(u)}：</span><span className="font-medium">{usesWho(u, name)}</span>
          </Line>
        ))}
        {links.usedBy.map((b) => (
          <Line key={'b-' + b.service + '-' + b.app} testId="ov-link-usedby" left={tile(b.app)}>
            <span className="font-medium">{name(b.app)}</span> 正在用它，所以现在不能删
          </Line>
        ))}
      </div>
    </Card>
  )
}
