// 数字、大小、时间的中文显示。

export function mbText(mb: number | undefined | null): string {
  if (mb === undefined || mb === null || !Number.isFinite(mb)) return '—'
  if (mb < 1024) return Math.round(mb) + ' MB'
  const gb = mb / 1024
  return (Number.isInteger(gb) ? String(gb) : gb.toFixed(1).replace(/\.0$/, '')) + ' GB'
}

export function coresText(milli: number | undefined | null): string {
  if (!milli) return '—'
  const c = milli / 1000
  return (Number.isInteger(c) ? String(c) : c.toFixed(2).replace(/0$/, '')) + ' 核'
}

export function pctText(v: number | undefined | null): string {
  if (v === undefined || v === null || !Number.isFinite(v)) return '—'
  return (v >= 10 ? Math.round(v) : Math.round(v * 10) / 10) + '%'
}

const p2 = (n: number) => String(n).padStart(2, '0')

// timeText：今年的显示“11-02 21:04”，更早的带年份。
export function timeText(iso: string | undefined | null, now = new Date()): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  const md = `${p2(d.getMonth() + 1)}-${p2(d.getDate())} ${p2(d.getHours())}:${p2(d.getMinutes())}`
  return d.getFullYear() === now.getFullYear() ? md : `${d.getFullYear()}-${md}`
}

export function durationText(seconds: number | undefined | null): string {
  if (seconds === undefined || seconds === null || !Number.isFinite(seconds) || seconds < 0) return '—'
  const s = Math.round(seconds)
  if (s < 60) return `${s} 秒`
  if (s < 3600) return `${Math.floor(s / 60)} 分 ${p2(s % 60)} 秒`
  return `${Math.floor(s / 3600)} 小时 ${Math.floor((s % 3600) / 60)} 分`
}

// sinceText：从某个时刻到现在过了多久，用于“已运行 3 天”。
export function sinceText(iso: string | undefined | null, now = Date.now()): string {
  if (!iso) return '—'
  const t = Date.parse(iso)
  if (!Number.isFinite(t)) return '—'
  const m = Math.max(0, Math.floor((now - t) / 60000))
  if (m < 1) return '不到 1 分钟'
  if (m < 60) return `${m} 分钟`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} 小时`
  return `${Math.floor(h / 24)} 天`
}
