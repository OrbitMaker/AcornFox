// 谁能打开这个应用：只本机、局域网、公网三档，点一档就改（PATCH /v1/apps/{app} 的 exposure）。
// 三档只在概览最上面的“谁能打开”卡片里（who-card.tsx）；设置里那一行只管域名（样稿第 10 版，评估 H2）。
//   这台机器不在公网上（GET /v1/status 的 on_public 为 false，家里的机器是这样）：“公网”那一档灰着，旁边写一句原因；
//   能开的机器上改成公网先弹确认，两句人话；别的档直接改。
//   ToPublicButton：只本机、又没有局域网那条路的应用（云服务器上）卡片里只给一个“改成公网”，确认窗同一个。
// 对用到应用地址的应用（清单里的设置读它的访问地址），改了以后应用要重启才用上新地址：服务端不带“同意重启”会 409，
//   体里带计划，页面弹确认窗（pages/apps/reapply.tsx），同意了才改；也可以“先改，稍后再让应用跟上”。弹了确认窗或应用正在跟着变的不给“撤销”。
// “跟随默认”不在页面上出现（评估 H1），要跟着整台机器走的人用命令行 acornfox app set --app 名字 --expose default。
// 应用当前是别的访问方式（经 AFX 云、只在内部网络）时不显示三档，由调用方显示说明。
import { useRef, useState } from 'react'
import { api, appPath } from '@/lib/api'
import { onPublic } from '@/lib/display'
import { ACCESS_LEVELS } from '@/lib/overview'
import type { AppView } from '@/lib/types'
import { cn } from '@/lib/utils'
import { useIndicator } from '@/lib/hooks'
import { useConsole } from '@/state'
import { useReapply } from './reapply'
import { ErrorLine } from '@/components/bits'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/controls'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { useToast } from '@/components/ui/toast'

export const EXPOSURE_TEXT: Record<string, string> = { local: '只本机', lan: '局域网', public: '公网', afx_cloud: '经 AFX 云', internal: '给别的应用用' }
// 选“跟随默认”时的取值；保存时发 exposure: ""（和 acornfox app set --expose default 一样）。页面上不再给这个选项，留给命令行和老测试。
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

// useExposure：改访问方式（带“改成公网先确认”）。页面上的三档和“改成公网”按钮共用。
// 样稿第 12 版：改好以后的提示带“撤销”（原样改回刚才那一档）；出错不用会消失的提示，留在三档旁边（err）。
function useExposure(a: AppView, name: string, reload: () => Promise<void>) {
  const toast = useToast()
  const flow = useReapply(a.name, name, reload)
  const [busy, setBusy] = useState('')
  const [pending, setPending] = useState('')
  const [err, setErr] = useState<Error | null>(null)
  const save = async (choice: string) => {
    setPending('')
    setBusy(choice)
    setErr(null)
    const before = a.exposure
    const lv = ACCESS_LEVELS.find((l) => l.key === choice)
    try {
      const body = exposurePatch(a, choice).exposure
      const r = await flow.address('把 ' + name + ' 改成' + (lv ? lv.text : '这一档'), (mode) =>
        api.patch(appPath(a.name), { exposure: body, ...(mode === 'confirm' ? { confirm_restart: true } : {}), ...(mode === 'skip' ? { reapply: 'skip' } : {}) }))
      if (r === 'plain') {
        const was = ACCESS_LEVELS.find((l) => l.key === before)
        toast('已改好', lv ? lv.who : name, was ? {
          undo: async () => { await api.patch(appPath(a.name), { exposure: before }); await reload() },
          undone: '改回“' + was.text + '”了',
        } : undefined)
      }
    } catch (e) {
      setErr(new Error('没能改：' + (e as Error).message))
    } finally {
      setBusy('')
    }
  }
  const pick = (choice: string) => (needsPublicConfirm(a, choice) ? setPending(choice) : save(choice))
  const dialog = (
    <Dialog open={!!pending} onClose={() => setPending('')} labelledBy="pub-title">
      <DialogHead id="pub-title" title={'让互联网上的人也能打开 ' + name + '？'} />
      <p className="text-sm">互联网上任何人都能打开它的登录页。先确认已经给它设了自己的密码。</p>
      <div className="mt-5 flex justify-end gap-2">
        <Button variant="outline" onClick={() => setPending('')}>取消</Button>
        <Button variant="destructive" loading={!!busy} onClick={() => save(pending)}>改成公网</Button>
      </div>
    </Dialog>
  )
  return { busy, pick, dialog, err }
}

export function ExposureSwitch({ app: a, name = a.name, reload }: {
  app: AppView
  // 给人看的名字（应用中心里的名字）
  name?: string
  reload: () => Promise<void>
}) {
  const { status } = useConsole()
  const { busy, pick, dialog, err } = useExposure(a, name, reload)
  const inner = useRef<HTMLSpanElement>(null)
  const ind = useIndicator(inner, '[aria-pressed="true"]', [a.exposure])
  if (!exposureEditable(a)) return null
  const publicOK = onPublic(status)
  // 只在“公网”没点亮时灰掉；已经是公网的照样显示亮着（改回来要能点别的档）
  const off = (key: string) => key === 'public' && !publicOK && a.exposure !== 'public'
  return (
    <>
      <span className="seg inline-flex h-ctl-sm shrink-0 rounded-md bg-muted p-0.5 text-xs" role="group" aria-label={'谁能打开 ' + name}>
        <span ref={inner} className="relative inline-flex h-full items-center">
          {ind.box && <span aria-hidden="true" className="seg-pill" data-ready={ind.ready ? '' : undefined} data-dir={ind.dir} style={{ left: ind.box.left, right: ind.box.right }} />}
          {ACCESS_LEVELS.map((l) => {
            const on = l.key === a.exposure
            return (
              <button key={l.key} type="button" aria-pressed={on} data-on={on ? '1' : '0'}
                disabled={!!busy || on || off(l.key)}
                title={off(l.key) ? '这台机器不在公网上' : undefined}
                onClick={() => pick(l.key)}
                className={cn('tap relative inline-flex h-full items-center gap-1 whitespace-nowrap rounded-[6px] px-3 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-default',
                  on ? 'font-medium text-foreground' : off(l.key) ? 'text-muted-foreground/50' : 'text-muted-foreground hover:text-foreground')}>
                {busy === l.key && <Spinner className="size-3" />}{l.text}
              </button>
            )
          })}
        </span>
      </span>
      {!publicOK && a.exposure !== 'public' && <span className="text-2xs text-muted-foreground" data-testid="not-public">这台机器不在公网上</span>}
      <ErrorLine error={err} className="basis-full text-right" />
      {dialog}
    </>
  )
}

// ToPublicButton：只给一个“改成公网”（确认窗和三档的一样）。这台机器不在公网上时不显示。
export function ToPublicButton({ app: a, name = a.name, reload }: { app: AppView; name?: string; reload: () => Promise<void> }) {
  const { status } = useConsole()
  const { busy, pick, dialog, err } = useExposure(a, name, reload)
  if (!onPublic(status) || !exposureEditable(a)) return null
  return (
    <>
      <Button size="sm" className="self-start" disabled={!!busy} loading={busy === 'public'} onClick={() => pick('public')}>改成公网</Button>
      <ErrorLine error={err} />
      {dialog}
    </>
  )
}
