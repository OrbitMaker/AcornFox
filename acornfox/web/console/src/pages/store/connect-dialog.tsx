// 连接私有目录：输入别人给的连接码（AFXC-XXXX-XXXX-XXXX-XXXX），调 POST /v1/catalog/connect。
// 连接码只发给服务端，页面不留存；格式先在页面上查一遍，不对的不发出去（服务端还会再查，编号 CAT-CONN-01）。
import * as React from 'react'
import { Link2 } from 'lucide-react'
import { api, type ApiError } from '@/lib/api'
import { useConsole } from '@/state'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/controls'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { useToast } from '@/components/ui/toast'

// normalizeCode：和服务端 NormalizeCode 同一规则：去掉空白和连字符、转大写，0→O、1→I、8→B，
// 只剩 A–Z 和 2–7；前缀 AFXC 可有可无，去掉后要正好 16 位。
export function normalizeCode(code: string): string | null {
  let out = ''
  for (let ch of code.toUpperCase()) {
    if (ch === '-' || ch === ' ' || ch === '\t' || ch === '\n' || ch === '\r') continue
    if (ch === '0') ch = 'O'
    else if (ch === '1') ch = 'I'
    else if (ch === '8') ch = 'B'
    if (!/^[A-Z2-7]$/.test(ch)) return null
    out += ch
  }
  if (out.length === 20 && out.startsWith('AFXC')) out = out.slice(4)
  return out.length === 16 ? out : null
}

interface ConnectView { catalog: { id: string; name: string }; again: boolean; sync?: { ok: boolean; message?: string } }

export function ConnectCatalogDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { refreshCatalog } = useConsole()
  const toast = useToast()
  const [code, setCode] = React.useState('')
  const [busy, setBusy] = React.useState(false)
  const [err, setErr] = React.useState<{ code?: string; message: string; hint?: string } | null>(null)
  React.useEffect(() => {
    if (!open) {
      setCode('')
      setErr(null)
    }
  }, [open])
  const submit = async (e?: React.FormEvent) => {
    e?.preventDefault()
    if (!normalizeCode(code)) {
      setErr({ code: 'CAT-CONN-01', message: '连接码格式不对：应该是 AFXC- 加 16 个字母数字（每 4 个一组）' })
      return
    }
    setBusy(true)
    setErr(null)
    try {
      const v = await api.post<ConnectView>('/v1/catalog/connect', { code: code.trim() })
      await refreshCatalog()
      toast(v.again ? '已重新连上' : '已连接私有目录', v.catalog?.name + (v.sync && !v.sync.ok ? '：' + (v.sync.message || '目录还没同步下来，稍后会再试') : ''))
      onClose()
    } catch (x) {
      const a = x as ApiError
      // 没有 HTTP 状态的是网络错误（后台服务连不上），浏览器给的英文原因不直接显示。
      if (!a.status) setErr({ message: '连不上后台服务，没有连接。请稍后再试，或在服务器上运行 acornfox catalog connect <连接码>' })
      else setErr({ code: a.code, message: a.message, hint: a.hint })
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open={open} onClose={onClose} labelledBy="connect-title">
      <DialogHead id="connect-title" title="连接私有目录" desc="输入别人发给你的连接码，连上后他们给你准备的应用会出现在应用中心，标着“来自：目录名”。" />
      <form onSubmit={submit} className="flex flex-col gap-3">
        <Input
          className="mono"
          autoComplete="off"
          spellCheck={false}
          placeholder="AFXC-XXXX-XXXX-XXXX-XXXX"
          aria-label="连接码"
          value={code}
          onChange={(e) => setCode(e.target.value)}
          aria-invalid={!!err}
        />
        {err && (
          <p className="text-xs text-danger" role="alert">
            {err.code && <span className="mono mr-1">{err.code}</span>}{err.message}
            {err.hint && <span className="block text-muted-foreground">{err.hint}</span>}
          </p>
        )}
        <p className="text-2xs text-muted-foreground">命令行写法：acornfox catalog connect &lt;连接码&gt;。连接码不会存在这台机器上。</p>
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button type="submit" disabled={busy || !code.trim()}>{busy ? <Spinner /> : <Link2 className="size-3.5" />}连接</Button>
        </div>
      </form>
    </Dialog>
  )
}
