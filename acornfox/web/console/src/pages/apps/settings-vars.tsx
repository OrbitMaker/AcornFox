// 应用设置里的“安装设置”一行（放在“谁能打开”下面）：这个应用是用哪些值装起来的，只读。
// 只有从应用中心装的应用才有（GET /v1/apps/{app}/vars 的 from_catalog 为 true）；旧服务端没有这个接口（404）时不显示。
//   收起：一句话。都没变写“都是自动配好的，不用管”；有和现在不一样的带黄点，写是哪一项或几项。
//   展开：① 和现在不一样的项合成一个提示框（一项一句话，几项先说一句再一项一行）：装的时候是什么、现在应该是什么、应用里记的还是装时的（这一版不会自动改写应用）、故障编号；
//            服务端给的怎么办（fix）收在“怎么办”里，点开才看；
//         ② 默认只列用户自己填的项和“它连着谁”（数据同概览的“连接”卡片）；
//         ③ “全部显示”：其余的分“平台填的”“自定义的”两组，每项只有中文名和值；
//         ④ 服务端的说明（旧格式清单装的、清单没存等）用小字列出来。
// 保密项默认是服务端遮好的值；点“显示”才带 reveal=true 取完整值，只放在这一块的状态里，收起这一行就没了。
// 页面上不写“变量”，也不显示清单里的名字。整理在 lib/app-vars.ts。
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { AlertTriangle, Eye, EyeOff } from 'lucide-react'
import { differsHead, fetchAppVars, groupVars, revealedValues, shownNotes, varLabel, varValue, varsSummary, type AppVar, type AppVars } from '@/lib/app-vars'
import { useAppLinks, usesText } from '@/lib/app-links'
import type { AppView } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/controls'
import { useToast } from '@/components/ui/toast'
import { ExpandRow } from '@/components/expand-row'

// useAppVars：读这个应用的安装设置。应用的地址或对外方式变了（例如刚在“谁能打开”里改了）就重新读，
// 好让“和现在不一样”跟上。null：不显示这一行（不是从应用中心装的、旧服务端）。
export function useAppVars(a: AppView): { vars: AppVars | null; error: Error | null } {
  const [vars, setVars] = useState<AppVars | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const fromCatalog = !!a.catalog_id
  useEffect(() => {
    if (!fromCatalog) { setVars(null); return }
    const ctl = new AbortController()
    fetchAppVars(a.name, false, ctl.signal)
      .then((v) => { if (!ctl.signal.aborted) { setVars(v && v.from_catalog ? v : null); setError(null) } })
      .catch((e) => { if (!ctl.signal.aborted && (e as Error).name !== 'AbortError') setError(e as Error) })
    return () => ctl.abort()
  }, [a.name, fromCatalog, a.url, a.exposure])
  return { vars, error }
}

function KV({ k, children, testId }: { k: ReactNode; children: ReactNode; testId?: string }) {
  return (
    <div className="flex items-baseline gap-3 py-1" data-testid={testId}>
      <span className="w-28 shrink-0 text-xs text-muted-foreground">{k}</span>
      <span className="min-w-0 flex-1 break-all text-xs">{children}</span>
    </div>
  )
}

function VarLine({ v, full, busy, onReveal }: { v: AppVar; full?: string; busy: boolean; onReveal: (name: string) => void }) {
  const shown = full !== undefined
  return (
    <KV k={varLabel(v)} testId="var-item">
      <span className="mono" data-secret={v.masked ? 'true' : undefined}>{shown ? full : varValue(v)}</span>
      {v.masked && (
        <button type="button" className="ml-2 inline-flex items-center align-[-2px] text-muted-foreground hover:text-foreground disabled:opacity-50"
          aria-label={(shown ? '遮住' : '显示') + varLabel(v)} disabled={busy} onClick={() => onReveal(v.name)}>
          {busy ? <Spinner className="size-3" /> : shown ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
        </button>
      )}
    </KV>
  )
}

function Group({ title, list, ...p }: { title: string; list: AppVar[]; full: Record<string, string>; busy: string; onReveal: (name: string) => void }) {
  if (list.length === 0) return null
  return (
    <div className="mt-3" data-testid={'vars-group-' + title}>
      <div className="mb-0.5 text-2xs text-muted-foreground">{title}</div>
      {list.map((v) => <VarLine key={v.name} v={v} full={p.full[v.name]} busy={p.busy === v.name} onReveal={p.onReveal} />)}
    </div>
  )
}

// 和现在不一样的项：只给一个提示框。一项时一句话说完；几项时（例如访问地址和它里面的主机一起变）先说一句，再一项一行。
// 服务端给的“怎么办”是照着命令行写的长句，几项的也是同一句，收在“怎么办”里，要看再点开。
function Differs({ list }: { list: AppVar[] }) {
  const head = differsHead(list)!
  const was = (v: AppVar) => (
    <>
      装的时候是 <span className="mono break-all">{varValue(v)}</span>
      {v.value_now != null && v.value_now !== '' && <>，现在应该是 <span className="mono break-all">{v.value_now}</span></>}
    </>
  )
  return (
    <div className="mb-2.5 flex items-start gap-2 rounded-lg border px-3 py-2" data-testid="var-differs">
      <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-warning" />
      <div className="min-w-0 text-xs">
        {list.length === 1 ? (
          <div>
            {varLabel(head)}和现在的不一样：{was(head)}。应用里记的还是装的时候的，用到它的地方可能不对。
            {head.code && <span className="mono ml-1.5 text-2xs text-muted-foreground">{head.code}</span>}
          </div>
        ) : (
          <>
            <div>
              有 {list.length} 项和现在的不一样。应用里记的还是装的时候的，用到它们的地方可能不对。
              {head.code && <span className="mono ml-1.5 text-2xs text-muted-foreground">{head.code}</span>}
            </div>
            <ul className="mt-1 space-y-0.5 text-muted-foreground">
              {list.map((v) => <li key={v.name} data-testid="var-differs-item">{varLabel(v)}：{was(v)}</li>)}
            </ul>
          </>
        )}
        {head.fix && (
          <details className="mt-1 text-2xs text-muted-foreground">
            <summary className="cursor-pointer select-none">怎么办</summary>
            <div className="mt-1" data-testid="var-fix">{head.fix}</div>
          </details>
        )}
      </div>
    </div>
  )
}

function VarsBody({ app, vars }: { app: string; vars: AppVars }) {
  const toast = useToast()
  const links = useAppLinks(app)
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
    try {
      if (!revealed.current) revealed.current = revealedValues(await fetchAppVars(app, true))
      const v = revealed.current[name]
      if (v === undefined) throw new Error('服务端没有给这一项的完整值')
      setFull((f) => ({ ...f, [name]: v }))
    } catch (e) {
      toast('没能读取完整值', (e as Error).message, 'error')
    } finally {
      setBusy('')
    }
  }
  const rest = g.platform.length + g.custom.length
  const lineProps = { full, busy, onReveal }
  return (
    <div data-testid="vars-body">
      {g.differs.length > 0 && <Differs list={g.differs} />}
      {g.mine.map((v) => <VarLine key={v.name} v={v} full={full[v.name]} busy={busy === v.name} onReveal={onReveal} />)}
      {links?.uses.map((u) => <KV key={'l-' + u.service} k={u.kind} testId="var-link">{usesText(u)}</KV>)}
      {notes.length > 0 && (
        <ul className="mt-1.5 space-y-0.5 text-2xs text-muted-foreground" data-testid="vars-notes">
          {notes.map((n, i) => <li key={i}>{n}</li>)}
        </ul>
      )}
      {all ? (
        <>
          <Group title="平台填的" list={g.platform} {...lineProps} />
          <Group title="自定义的" list={g.custom} {...lineProps} />
          <div className="mt-2 text-2xs text-muted-foreground">现在只能看，还不能在这里改。</div>
        </>
      ) : rest > 0 ? (
        <div className="mt-2 flex items-center gap-2.5">
          <Button size="sm" variant="outline" onClick={() => setAll(true)}>全部显示</Button>
          <span className="text-2xs text-muted-foreground">其余 {rest} 项是自动填好的</span>
        </div>
      ) : (
        <div className="mt-2 text-2xs text-muted-foreground">现在只能看，还不能在这里改。</div>
      )}
    </div>
  )
}

// VarsRow：设置页里的这一行。vars 为 null 且没出错时不显示。
export function VarsRow({ app, vars, error, open, onToggle }: { app: string; vars: AppVars | null; error: Error | null; open: boolean; onToggle: () => void }) {
  if (!vars) {
    if (!error) return null
    return <ExpandRow id="set-vars" name="安装设置" summary={'读不出来：' + error.message} summaryTestId="summary-vars" tone="danger" open={false} onToggle={() => {}} />
  }
  const s = varsSummary(vars)
  return (
    <ExpandRow id="set-vars" name="安装设置" summary={s.text} summaryTestId="summary-vars" tone={s.tone} open={open} onToggle={onToggle}>
      <VarsBody app={app} vars={vars} />
    </ExpandRow>
  )
}
