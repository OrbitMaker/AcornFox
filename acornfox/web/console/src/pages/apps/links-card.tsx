// 应用概览里的“连接”卡片（放在“访问”下面）：一行一句，说这个应用和别的应用怎么连着。
//   用别人的：“数据库用的是 postgres16 的”，右边“已接上”（那条服务现在不能用时换成 lib/services.ts 的状态文字）；
//             用户自己登记的写“大模型接口用的是你登记的“名字””。
//   给别人提供、有应用在用：每个在用的应用一行，“n8n 在用它的数据库”，右边小字“所以现在不能删它”。
// 一行都没有、或旧服务端没有服务接口（404）时整张卡片不显示。数据整理在 lib/app-links.ts。
import type { ReactNode } from 'react'
import { Link2 } from 'lucide-react'
import { usesText, type AppLinks } from '@/lib/app-links'
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
  <div className="flex size-6 shrink-0 items-center justify-center rounded border text-muted-foreground" aria-hidden="true"><Link2 className="size-3.5" /></div>
)

export function LinksCard({ links, tile = defaultTile }: { links: AppLinks | null; tile?: TileOf }) {
  if (!links || (links.uses.length === 0 && links.usedBy.length === 0)) return null
  return (
    <Card className="px-3.5 pb-2 pt-2" data-testid="ov-links">
      <div className="pt-1 text-xs font-medium">连接</div>
      <div className="mt-0.5">
        {links.uses.map((u) => (
          <Line key={'u-' + u.service} testId="ov-link-uses" left={u.provider ? tile(u.provider) : userTile}
            right={u.status.tone === 'success'
              ? <Badge variant="success">已接上</Badge>
              : <Badge variant={TONE_BADGE[u.status.tone]} title={u.status.detail}>{u.status.text}</Badge>}>
            {u.kind}{u.provider ? <>用的是 <span className="font-medium">{u.provider}</span> 的</> : usesText(u)}
          </Line>
        ))}
        {links.usedBy.map((b) => (
          <Line key={'b-' + b.service + '-' + b.app} testId="ov-link-usedby" left={tile(b.app)}
            right={<span className="shrink-0 text-2xs text-muted-foreground">所以现在不能删它</span>}>
            <span className="font-medium">{b.app}</span> 在用它的{b.kind}
          </Line>
        ))}
      </div>
    </Card>
  )
}
