// 设置 › 高级 › 平台自动填的（样稿第 10 版，评估 N4）：平台按这台机器替应用填好的几项（时区、下载源等）和现在的值。
// 以前是“服务”页最后一行；它不是服务，挪到高级里单独一页。每个应用各算各的（名字以 app. 开头）不列；
// 网络区域写“国内网络”“海外网络”，不写 cn、global（lib/services.ts 的 platformValue）。
// 接口：GET /v1/catalog/platform-vars。旧服务端或没启用应用中心（404、405）时说“这个版本还没有这项功能”。
import { usePoll } from '@/lib/hooks'
import { listPlatformVars, platformLabel, platformShown, platformValue } from '@/lib/services'
import { WORDS } from '@/lib/display'
import { Loading } from '@/components/ui/skeleton'
import { SkLines } from '@/components/skeletons'
import { ApiErrorBox, NOT_SUPPORTED } from './service-form'

export function PlatformFilled() {
  const pv = usePoll((s) => listPlatformVars(s), 0)
  const shown = platformShown(pv.data)
  return (
    <div className="flex flex-col gap-2 py-3" data-testid="platform-auto">
      <div className="text-sm">{WORDS.platformFilled}</div>
      <Loading when={pv.loading && !pv.data} skeleton={<SkLines n={4} />}>{null}</Loading>
      {pv.error && !pv.data && <ApiErrorBox err={pv.error} />}
      {!pv.loading && !pv.error && pv.data === null && <p className="text-xs text-muted-foreground">{NOT_SUPPORTED}</p>}
      {pv.data && shown.length === 0 && <p className="text-xs text-muted-foreground">这台机器上现在没有要平台替你填的项。</p>}
      {shown.length > 0 && (
        <dl className="flex flex-col text-xs" data-testid="platform-auto-values">
          {shown.map((v) => (
            <div key={v.name} className="flex items-baseline gap-3 py-1.5">
              <dt className="w-28 shrink-0 text-muted-foreground">{platformLabel(v)}</dt>
              <dd className={v.name === 'region' ? 'min-w-0 flex-1' : 'mono min-w-0 flex-1 break-all'}>{platformValue(v)}</dd>
            </div>
          ))}
        </dl>
      )}
      {shown.length > 0 && <p className="text-2xs text-muted-foreground">装应用时平台替你填好；想给某个应用用别的值，装它的时候可以改。</p>}
    </div>
  )
}
