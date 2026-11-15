// 谁能打开这个应用：只本机、局域网、公网三档，点一档就改（PATCH /v1/apps/{app} 的 exposure）。
// 概览的访问卡片和设置里的“谁能打开”用的是同一个组件、同一套确认：
//   改成公网（包括跟随默认、默认是公网）先弹确认，说明后果；别的档直接改。
//   服务端给了平台默认（default_exposure）时，设置里多一个“跟随默认”的勾选，取消勾选就固定成现在的这一档。
// 应用当前是别的访问方式（经 AFX 云、只在内部网络）时不显示三档，由调用方显示只读的说明。
import { useState, type ReactNode } from 'react'
import { api, appPath } from '@/lib/api'
import { ACCESS_LEVELS } from '@/lib/overview'
import type { AppView } from '@/lib/types'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/controls'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { useToast } from '@/components/ui/toast'

export const EXPOSURE_TEXT: Record<string, string> = { local: '只本机', lan: '局域网', public: '公网', afx_cloud: '经 AFX 云', internal: '只在内部网络' }
// 选“跟随默认”时的取值；保存时发 exposure: ""（和 acornfox app set --expose default 一样）。
export const FOLLOW_DEFAULT = 'default'

// exposureEditable：这个应用的访问方式能不能在页面上改（只有三档之一时能改）。
export function exposureEditable(a: AppView): boolean {
  return ACCESS_LEVELS.some((l) => l.key === a.exposure)
}

// exposurePatch：选中的值换成 PATCH 的 exposure，以及改完实际是哪种（判断要不要先确认改成公网）。
export function exposurePatch(a: AppView, choice: string): { exposure: string; effective: string } {
  if (choice === FOLLOW_DEFAULT) return { exposure: '', effective: a.default_exposure || '' }
  return { exposure: choice, effective: choice }
}

// needsPublicConfirm：这次改动会把应用变成公网（原来不是），要先确认。
export function needsPublicConfirm(a: AppView, choice: string): boolean {
  return exposurePatch(a, choice).effective === 'public' && a.exposure !== 'public'
}

export function ExposureSwitch({ app: a, reload, withDefault = false, after }: {
  app: AppView
  reload: () => Promise<void>
  // 设置里显示“跟随默认”的勾选（服务端给了平台默认时）
  withDefault?: boolean
  // 紧跟在三档后面的东西（设置里放“这一档谁能打开”那句话），在“跟随默认”那一行之前
  after?: ReactNode
}) {
  const toast = useToast()
  const [busy, setBusy] = useState('')
  const [pending, setPending] = useState('')
  if (!exposureEditable(a)) return null
  const save = async (choice: string) => {
    setPending('')
    setBusy(choice)
    try {
      await api.patch(appPath(a.name), { exposure: exposurePatch(a, choice).exposure })
      toast('已保存访问方式', '路由马上跟着改')
      await reload()
    } catch (e) {
      toast('没能完成', (e as Error).message, 'error')
    } finally {
      setBusy('')
    }
  }
  const pick = (choice: string) => (needsPublicConfirm(a, choice) ? setPending(choice) : save(choice))
  const following = !!a.exposure_is_default
  return (
    <>
      <span className="inline-flex shrink-0 overflow-hidden rounded-md border text-xs" role="group" aria-label="访问方式">
        {ACCESS_LEVELS.map((l, i) => {
          const on = l.key === a.exposure
          return (
            <button key={l.key} type="button" aria-pressed={on} data-on={on ? '1' : '0'}
              // 跟随默认时点亮着的那一档 = 固定成这一档；没跟随默认时点亮着的那一档什么都不做
              disabled={!!busy || (on && !following)}
              onClick={() => pick(l.key)}
              className={cn('inline-flex h-7 items-center gap-1 whitespace-nowrap px-3.5 transition-colors disabled:cursor-default', i > 0 && 'border-l',
                on ? 'bg-primary font-medium text-primary-foreground' : 'text-muted-foreground hover:bg-accent hover:text-foreground')}>
              {busy === l.key && <Spinner className="size-3" />}{l.text}
            </button>
          )
        })}
      </span>
      {after}
      {withDefault && a.default_exposure && (
        <div className="basis-full text-2xs text-muted-foreground" data-testid="exposure-default">
          <label className="inline-flex items-center gap-1.5 text-xs text-foreground">
            <input type="checkbox" className="size-3.5 accent-[hsl(var(--primary))]" checked={following} disabled={!!busy}
              onChange={(e) => pick(e.target.checked ? FOLLOW_DEFAULT : a.exposure)} />
            跟随默认（现在是{EXPOSURE_TEXT[a.default_exposure] || a.default_exposure}）
          </label>
          <span className="ml-2">不单独设，用这台服务器的默认访问方式；命令行写法是 <span className="mono">acornfox app set --app {a.name} --expose default</span>。</span>
        </div>
      )}
      {/* 改成公网先确认一次：所有网卡都能打开，带管理后台的应用要先设好登录密码。跟随默认、默认是公网时也一样。 */}
      <Dialog open={!!pending} onClose={() => setPending('')} labelledBy="pub-title">
        <DialogHead id="pub-title" title={'把 ' + a.name + ' 改成公网访问？'} desc="改完马上生效。" />
        <ul className="flex list-disc flex-col gap-1 pl-5 text-sm">
          <li>这台服务器的所有网卡都能打开它；如果服务器有公网地址，互联网上的任何人都能访问。</li>
          <li>绑定的域名会自动申请证书。</li>
          <li>应用带管理后台时，先确认已经改掉默认密码、设好登录密码。</li>
        </ul>
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="outline" onClick={() => setPending('')}>取消</Button>
          <Button variant="destructive" disabled={!!busy} onClick={() => save(pending)}>{!!busy && <Spinner />}改成公网</Button>
        </div>
      </Dialog>
    </>
  )
}
