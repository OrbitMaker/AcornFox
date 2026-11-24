// 应用设置里的“安装设置”一行（放在“域名”下面；数据库这类没有“域名”时是第一行）：这个应用是用哪些值装起来的，能改的每项有“改”。
// 只有从应用中心装的应用才有（GET /v1/apps/{app}/vars 的 from_catalog 为 true）；旧服务端没有这个接口（404）时不显示。
//   收起：一句话。都没变写“都是自动配好的，不用管”；有和现在不一样的带黄点，写是哪一项或几项。
//   展开：① 和现在不一样的项合成一个提示框（一项一句话，几项先说一句再一项一行）：装的时候是什么、现在应该是什么、
//            应用里记的还是装时的；故障编号不显示，放进“复制给 AI 工作台”的文字里；服务端给的怎么办（fix）收在“怎么办”里；
//         ② 默认只列用户自己填的项和它用到的服务（“存数据用的是：PostgreSQL 16”，同概览的“它用到的服务”卡片）；
//         ③ “全部显示”：其余的分“平台自动填的”“安装设置”两组，每项只有中文名和值；
//            访问地址那几项并成“打开的地址”，会话密钥这类平台生成、用户不用记的不列（lib/app-vars.ts）；
//         ④ 服务端的说明（旧格式清单装的、清单没存等）用小字列出来。不讲还没做的功能（样稿第 10 版，评估 H5）。
// 改一项（W8-1、W8-2）：点“改”就在那一行换成输入框（服务端说这一项 can_edit 才有），“保存”先读计划（会改什么、要不要重启），
//   有变化才弹确认窗，同意了再做，做的时候看一列步骤，失败自动退回原样（pages/apps/reapply.tsx）；
//   只改这个应用的平台自动填的项（时区这类）多一个“跟着这台机器”。落在后面的（和现在不一样）提示框里有“让它跟上”。
// 保密项默认是服务端遮好的值；点“显示”才带 reveal=true 取完整值，只放在这一块的状态里，收起这一行就没了。
// 页面上不写“变量”，也不显示清单里的名字。整理在 lib/app-vars.ts。
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { AlertTriangle, ChevronRight, Eye, EyeOff } from 'lucide-react'
import { differsHead, fetchAppVars, groupVars, revealedValues, shownNotes, varLabel, varValue, varsSummary, type AppVar, type AppVars } from '@/lib/app-vars'
import { useAppLinks, usesPurpose, usesWho } from '@/lib/app-links'
import { useAppNames } from '@/lib/display'
import { AskAI } from '@/components/fold'
import type { AppView } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/controls'
import { Sk } from '@/components/ui/skeleton'
import { ErrorLine } from '@/components/bits'
import { cn } from '@/lib/utils'
import { ExpandRow } from '@/components/expand-row'
import type { useReapply } from './reapply'

type Flow = ReturnType<typeof useReapply>

// useAppVars：读这个应用的安装设置。应用的地址或对外方式变了（例如刚在“谁能打开”里改了）就重新读，
// 好让“和现在不一样”跟上。null：不显示这一行（不是从应用中心装的、旧服务端）。
// loading：第一次还没读到（这时那一行先画名字和一段占位形状，样稿第 14 版；以前是读到了才冒出来，下面几行被顶下去）。
export function useAppVars(a: AppView): { vars: AppVars | null; error: Error | null; loading: boolean; refresh: () => void } {
  const [vars, setVars] = useState<AppVars | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const fromCatalog = !!a.catalog_id
  const [loading, setLoading] = useState(fromCatalog)
  const [again, setAgain] = useState(0)
  const refresh = useCallback(() => setAgain((n) => n + 1), [])
  // 应用的地址、对外方式变了，或者重装过（改设置、让它跟上以后换了新的一版）就重新读，好让“和现在不一样”跟上
  useEffect(() => {
    if (!fromCatalog) { setVars(null); setLoading(false); return }
    const ctl = new AbortController()
    fetchAppVars(a.name, false, ctl.signal)
      .then((v) => { if (!ctl.signal.aborted) { setVars(v && v.from_catalog ? v : null); setError(null); setLoading(false) } })
      .catch((e) => { if (!ctl.signal.aborted && (e as Error).name !== 'AbortError') { setError(e as Error); setLoading(false) } })
    return () => ctl.abort()
  }, [a.name, fromCatalog, a.url, a.exposure, a.current_deployment, again])
  return { vars, error, loading, refresh }
}

function KV({ k, children, testId }: { k: ReactNode; children: ReactNode; testId?: string }) {
  return (
    <div className="flex items-baseline gap-3 py-1" data-testid={testId}>
      <span className="w-28 shrink-0 text-xs text-muted-foreground">{k}</span>
      <span className="min-w-0 flex-1 break-all text-xs">{children}</span>
    </div>
  )
}

function VarLine({ v, full, busy, onReveal, edit }: { v: AppVar; full?: string; busy: boolean; onReveal: (name: string) => void; edit?: EditCtl }) {
  const shown = full !== undefined
  if (edit?.editing === v.name) return <VarEditor v={v} flow={edit.flow} onClose={edit.close} />
  return (
    <KV k={varLabel(v)} testId="var-item">
      <span className="mono" data-secret={v.masked ? 'true' : undefined}>{shown ? full : varValue(v)}</span>
      {v.masked && (
        <button type="button" className="ml-2 inline-flex items-center align-[-2px] text-muted-foreground hover:text-foreground disabled:opacity-50"
          aria-label={(shown ? '遮住' : '显示') + varLabel(v)} disabled={busy} onClick={() => onReveal(v.name)}>
          {busy ? <Spinner className="size-3" /> : shown ? <EyeOff className="size-icon" /> : <Eye className="size-icon" />}
        </button>
      )}
      {edit && v.can_edit && (
        <button type="button" className="ml-2.5 text-2xs text-primary hover:underline" aria-label={'改' + varLabel(v)} data-testid="var-edit" onClick={() => edit.open(v.name)}>改</button>
      )}
    </KV>
  )
}

// 改一项的控制：editing 是正在改的那一项的名字；只有从应用中心装的、清单还在、能改设置的应用有（vars.can_change）。
interface EditCtl { editing: string; open: (name: string) => void; close: () => void; flow: Flow }

// VarEditor：这一行换成输入框。保存先读计划（出错写在输入框下面）；保密项留空不让保存（不显示原来的值）。
function VarEditor({ v, flow, onClose }: { v: AppVar; flow: Flow; onClose: () => void }) {
  const [draft, setDraft] = useState(v.secret ? '' : v.value)
  const [busy, setBusy] = useState<'' | 'save' | 'reset'>('')
  const [err, setErr] = useState<Error | null>(null)
  const key = v.name.replace(/^params\./, '')
  const label = varLabel(v)
  const mine = v.layer === 'system' && v.origin === 'platform_user'
  const run = async (kind: 'save' | 'reset') => {
    if (kind === 'save' && v.secret && !draft) { setErr(new Error('新的值不能是空的')); return }
    setBusy(kind)
    setErr(null)
    try {
      if (kind === 'save') await flow.changeVars({ [key]: draft })
      else await flow.changeVars({}, [v.name])
      onClose()
    } catch (e) {
      setErr(new Error('没能保存：' + (e as Error).message))
    } finally {
      setBusy('')
    }
  }
  return (
    <div className="flex flex-col gap-1.5 py-1.5" data-testid="var-editor">
      <label className="text-xs text-muted-foreground" htmlFor={'ve-' + key}>{label}</label>
      <div className="flex flex-wrap items-center gap-2">
        <Input id={'ve-' + key} className="mono min-w-0 flex-1 text-xs" autoComplete="off" type={v.secret ? 'password' : 'text'} value={draft}
          placeholder={v.secret ? '新的值' : ''} onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter' && !busy) void run('save') }} />
        <Button size="sm" disabled={!!busy || (!v.secret && draft === v.value)} loading={busy === 'save'} onClick={() => run('save')}>保存</Button>
        <Button size="sm" variant="outline" disabled={!!busy} onClick={onClose}>取消</Button>
        {mine && <Button size="sm" variant="ghost" disabled={!!busy} loading={busy === 'reset'} onClick={() => run('reset')}>跟着这台机器</Button>}
      </div>
      <ErrorLine error={err} />
    </div>
  )
}

function Group({ title, list, ...p }: { title: string; list: AppVar[]; full: Record<string, string>; busy: string; onReveal: (name: string) => void; edit?: EditCtl }) {
  if (list.length === 0) return null
  return (
    <div className="mt-3" data-testid={'vars-group-' + title}>
      <div className="mb-0.5 text-2xs text-muted-foreground">{title}</div>
      {list.map((v) => <VarLine key={v.name} v={v} full={p.full[v.name]} busy={p.busy === v.name} onReveal={p.onReveal} edit={p.edit} />)}
    </div>
  )
}

// 和现在不一样的项：只给一个提示框。一项时一句话说完；几项时先说一句，再一项一行。
// 服务端给的“怎么办”是照着命令行写的长句，几项的也是同一句，收在“怎么办”里，要看再点开。
// 故障编号（APP-VARS-01）只放进“复制给 AI 工作台”的文字里。
function Differs({ app, list, flow }: { app: string; list: AppVar[]; flow?: Flow }) {
  const head = differsHead(list)!
  const aiText = () => `AcornFox 上的应用 ${app} 的安装设置里有 ${list.length} 项和现在的不一样${head.code ? '（' + head.code + '）' : ''}：\n` +
    list.map((v) => `- ${varLabel(v)}（${v.name}）：装的时候是 ${varValue(v)}${v.value_now ? '，现在应该是 ' + v.value_now : ''}`).join('\n') +
    (head.fix ? `\n服务端给的办法：${head.fix}` : '') + `\n请帮我处理，可以先运行 acornfox apps vars ${app}。`
  const was = (v: AppVar) => (
    <>
      装的时候是 <span className="mono break-all">{varValue(v)}</span>
      {v.value_now != null && v.value_now !== '' && <>，现在应该是 <span className="mono break-all">{v.value_now}</span></>}
    </>
  )
  return (
    <div className="mb-2.5 flex items-start gap-2 rounded-lg border px-3 py-2" data-testid="var-differs">
      <AlertTriangle className="mt-0.5 size-icon shrink-0 text-warning" />
      <div className="min-w-0 text-xs">
        {list.length === 1 ? (
          <div>
            {varLabel(head)}和现在的不一样：{was(head)}。应用里记的还是装的时候的，用到它的地方可能不对。
          </div>
        ) : (
          <>
            <div>
              有 {list.length} 项和现在的不一样。应用里记的还是装的时候的，用到它们的地方可能不对。
            </div>
            <ul className="mt-1 space-y-0.5 text-muted-foreground">
              {list.map((v) => <li key={v.name} data-testid="var-differs-item">{varLabel(v)}：{was(v)}</li>)}
            </ul>
          </>
        )}
        <div className="mt-1.5 flex flex-wrap items-center gap-3">
          {flow && <CatchUp flow={flow} />}
          <AskAI text={aiText} />
          {!flow && head.fix && (
            <details className="text-2xs text-muted-foreground">
              <summary className="cursor-pointer select-none">怎么办</summary>
              <div className="mt-1" data-testid="var-fix">{head.fix}</div>
            </details>
          )}
        </div>
      </div>
    </div>
  )
}

// CatchUp：“让它跟上”：先读计划，有变化才弹确认窗。出错写在按钮旁边。
function CatchUp({ flow }: { flow: Flow }) {
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<Error | null>(null)
  const run = async () => {
    setBusy(true)
    setErr(null)
    try { await flow.catchUp() } catch (e) { setErr(new Error('没能让它跟上：' + (e as Error).message)) } finally { setBusy(false) }
  }
  return (
    <>
      <Button size="sm" loading={busy} onClick={run} data-testid="var-catchup">让它跟上</Button>
      <ErrorLine error={err} />
    </>
  )
}

function VarsBody({ app, vars, flow }: { app: string; vars: AppVars; flow: Flow }) {
  // 正在改的那一项（同一时间只改一项）
  const [editing, setEditing] = useState('')
  const edit: EditCtl | undefined = vars.can_change ? { editing, open: setEditing, close: () => setEditing(''), flow } : undefined
  // 点“显示”没读到完整值：出错的话留在这一块最上面（样稿第 14 版，不弹提示）。
  const [revealErr, setRevealErr] = useState<Error | null>(null)
  const links = useAppLinks(app)
  const name = useAppNames()
  const g = groupVars(vars.vars)
  const notes = shownNotes(vars)
  const [all, setAll] = useState(false)
  // 完整值：只在这里（组件状态），这一行收起时组件卸掉，跟着没了。
  const [full, setFull] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState('')
  const revealed = useRef<Record<string, string> | null>(null)
  const onReveal = async (name: string) => {
    if (full[name] !== undefined) {
      setFull((f) => {
        const next = { ...f }
        delete next[name]
        return next
      })
      return
    }
    setBusy(name)
    setRevealErr(null)
    try {
      if (!revealed.current) revealed.current = revealedValues(await fetchAppVars(app, true))
      const v = revealed.current[name]
      if (v === undefined) throw new Error('服务端没有给这一项的完整值')
      setFull((f) => ({ ...f, [name]: v }))
    } catch (e) {
      setRevealErr(new Error('没能读取完整值：' + (e as Error).message))
    } finally {
      setBusy('')
    }
  }
  const rest = g.platform.length + g.custom.length
  const lineProps = { full, busy, onReveal, edit }
  return (
    <div data-testid="vars-body">
      <ErrorLine error={revealErr} className="mb-2" />
      {g.differs.length > 0 && <Differs app={app} list={g.differs} flow={vars.can_change ? flow : undefined} />}
      {g.mine.map((v) => <VarLine key={v.name} v={v} full={full[v.name]} busy={busy === v.name} onReveal={onReveal} edit={edit} />)}
      {links?.uses.map((u) => <KV key={'l-' + u.service} k={usesPurpose(u)} testId="var-link">{usesWho(u, name)}</KV>)}
      {notes.length > 0 && (
        <ul className="mt-1.5 space-y-0.5 text-2xs text-muted-foreground" data-testid="vars-notes">
          {notes.map((n, i) => <li key={i}>{n}</li>)}
        </ul>
      )}
      {all ? (
        <>
          <Group title="平台自动填的" list={g.platform} {...lineProps} />
          <Group title="安装设置" list={g.custom} {...lineProps} />
        </>
      ) : rest > 0 ? (
        <div className="mt-2 flex items-center gap-2.5">
          <Button size="sm" variant="outline" onClick={() => setAll(true)}>全部显示</Button>
          <span className="text-2xs text-muted-foreground">还有 {rest} 项是自动填好的</span>
        </div>
      ) : null}
    </div>
  )
}

// VarsRow：设置页里的这一行。vars 为 null 且没出错时不显示。
export function VarsRow({ app, vars, error, loading, open, onToggle, wide, flow }: { app: string; vars: AppVars | null; error: Error | null; loading?: boolean; open: boolean; onToggle: () => void; wide?: boolean; flow: Flow }) {
  if (!vars) {
    // 还在读：名字先出来，现状那一句是占位形状（和一行一项的行同样的留白、同样宽的名字一列）
    if (loading && !error) {
      return (
        <div className="flex items-center gap-3 px-4 py-row" aria-busy="true" data-testid="vars-loading">
          <span className={cn('shrink-0 text-sm font-medium', wide ? 'w-28 sm:w-36' : 'w-24 sm:w-28')}>安装设置</span>
          <span className="flex h-[var(--lh-xs)] min-w-0 flex-1 items-center"><Sk className="h-2.5 w-36" /></span>
          <ChevronRight className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        </div>
      )
    }
    if (!error) return null
    return <ExpandRow id="set-vars" name="安装设置" summary={'没能读出来：' + error.message} summaryTestId="summary-vars" tone="danger" open={false} onToggle={() => {}} wide={wide} />
  }
  const s = varsSummary(vars)
  return (
    <ExpandRow id="set-vars" name="安装设置" summary={s.text} summaryTestId="summary-vars" tone={s.tone} open={open} onToggle={onToggle} wide={wide}>
      <VarsBody app={app} vars={vars} flow={flow} />
    </ExpandRow>
  )
}
