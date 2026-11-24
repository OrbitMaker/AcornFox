// 应用概览最上面的“谁能打开”卡片（样稿第 10 版，评估 R4、E1、E5、E6、E7、H4）：所有应用都有，位置固定。
// “怎么打开、谁能打开、第一次登录用什么”只在这一张卡片里，设置里那一行只管域名。按应用的样子分三种：
//   能打开的：地址、复制、手机扫码、三档（access.tsx），地址下面固定一行“第一次登录用：…”（login-line.tsx），
//            右下角一句这一档谁能打开；
//   只本机、在这台机器以外点不开的（有 open_hint）：“现在只有这台机器自己能打开。”加主按钮（open-hint.tsx）——
//            家里的机器是“让家里的电脑、手机也能打开”，云服务器上是“改成公网”；命令收在“我在外面，用命令行打开”；
//   数据库这类（internal_only）：一句“它是给别的应用用的，没有网页可以打开。”
// 名片上的“怎么打开”按钮滚到这张卡片（id who-can-open）。“打开”按钮只在名片上。
import { useMemo, useState } from 'react'
import { encode } from 'uqr'
import { Check, Copy, QrCode } from 'lucide-react'
import { copyText } from '@/lib/clipboard'
import { WORDS } from '@/lib/display'
import { ACCESS_LEVELS, accessInfo } from '@/lib/overview'
import type { AppView, FirstLogin } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { ExposureSwitch, ToPublicButton } from './access'
import { OpenHintBlock } from './open-hint'
import { LoginLine, hasLoginFields } from './login-line'

export const WHO_CARD_ID = 'who-can-open'

// 二维码：在页面里自己画成 SVG，不联网、不用图片。
function QR({ text }: { text: string }) {
  // border 0：四周的留白自己画（pad），方便控制大小
  const cells = useMemo(() => encode(text, { ecc: 'M', border: 0 }).data, [text])
  const n = cells.length
  const pad = 2
  return (
    <svg data-testid="ov-qr" viewBox={`0 0 ${n + pad * 2} ${n + pad * 2}`} className="size-52 rounded-md" shapeRendering="crispEdges" role="img" aria-label={'二维码：' + text}>
      <rect width={n + pad * 2} height={n + pad * 2} fill="#fff" />
      {cells.map((row, r) => row.map((dark, c) => (dark ? <rect key={r + '-' + c} x={c + pad} y={r + pad} width="1" height="1" fill="#000" /> : null)))}
    </svg>
  )
}

export function WhoCard({ app: a, name, reload, login }: { app: AppView; name: string; reload: () => Promise<void>; login?: FirstLogin | null }) {
  const [qr, setQr] = useState(false)
  const [copied, setCopied] = useState(false)
  // 没能复制：出错的话留在地址那一行下面（样稿第 14 版，不弹提示）
  const [copyErr, setCopyErr] = useState(false)
  const x = accessInfo(a)
  const level = x ? ACCESS_LEVELS.find((l) => l.key === x.level) : undefined
  const loginRow = hasLoginFields(login) ? <div className="mt-2" data-testid="ov-login"><LoginLine fl={login} /></div> : null
  const copy = async () => {
    if (!x) return
    if (!(await copyText(x.url))) { setCopyErr(true); return }
    setCopyErr(false)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1200)
  }

  let body
  if (a.internal_only) {
    body = <p className="mt-1.5 text-sm text-muted-foreground" data-testid="ov-internal">它是给别的应用用的，没有网页可以打开。</p>
  } else if (a.open_hint) {
    body = (
      <>
        <OpenHintBlock bare className="mt-1.5" name={a.name} hint={a.open_hint} onChanged={reload}
          extra={!a.open_hint.can_lan ? <ToPublicButton app={a} name={name} reload={reload} /> : undefined} />
        {loginRow}
      </>
    )
  } else if (x) {
    body = (
      <>
        <div className="mt-2 flex flex-wrap items-center gap-2.5">
          <span className="mono tnum mr-1 min-w-0 max-w-full basis-full select-all truncate text-lg font-medium leading-7 sm:basis-auto" title={x.url}>{x.address}</span>
          <Button size="sm" variant="outline" onClick={copy}>{copied ? <Check className="size-icon" /> : <Copy className="size-icon" />}复制地址</Button>
          {x.openable && <Button size="sm" variant="outline" onClick={() => setQr(true)}><QrCode className="size-icon" />手机扫码</Button>}
          <span className="flex-1" />
          <span className="flex flex-wrap items-center gap-2"><ExposureSwitch app={a} name={name} reload={reload} /></span>
        </div>
        {copyErr && <p className="mt-1 text-xs text-danger" role="alert">没能复制，请手动选中上面的地址</p>}
        {x.lanURL && <div className="mono mt-0.5 truncate text-2xs text-muted-foreground" title={x.lanURL}>同一个网络里也能用 {x.lanURL.replace(/^https?:\/\//, '')}</div>}
        <div className="flex flex-wrap items-end justify-between gap-x-3 gap-y-1">
          {loginRow}
          {level && <span className="ml-auto mt-2 text-2xs text-muted-foreground" data-testid="ov-access-who">{level.who}</span>}
        </div>
        <Dialog open={qr} onClose={() => setQr(false)} labelledBy="ov-qr-title" className="sm:max-w-xs">
          <DialogHead id="ov-qr-title" title="用手机扫码打开" desc={x.level === 'public' ? '手机连着网就能打开' : '手机要连着家里（同一个网络）的 Wi-Fi'} />
          <div className="flex flex-col items-center gap-2">
            {qr && <QR text={x.url} />}
            <code className="mono max-w-full truncate text-xs text-muted-foreground">{x.address}</code>
          </div>
        </Dialog>
      </>
    )
  } else {
    body = <p className="mt-1.5 text-sm text-muted-foreground">还没有打开的地址，启动以后这里会有。</p>
  }

  return (
    <Card id={WHO_CARD_ID} data-testid="ov-access" className="scroll-mt-4 px-3.5 pb-3 pt-2">
      <div className="pt-1 text-xs font-medium">{WORDS.whoCanOpen}</div>
      {body}
    </Card>
  )
}
