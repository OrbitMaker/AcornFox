// 介绍文档（“介绍性的东西一律内置为一篇文档的形式，不要搞这种多个层级”）。
// 应用和解决方案的介绍只有一个字段：show.doc，一篇 Markdown 文档。页面不再有“它能帮你做什么”“装好后你会得到”
// “多帮你做了这些”“几段视频加截图”这些分开维护的栏目——想写什么、分几节，都由写文档的人在这一篇里定。
//
// 认的写法（故意只留最常用的几种，在经营系统里用一个文本框就能写）：
//   ## 标题、### 小标题（# 也当 ##）
//   一段话；空一行分段
//   - 一条        无序列表（* 也行）
//   1. 一步       有序列表
//   > 一句提醒
//   **加粗**、`原样的字`、[文字](链接)
//   ![图的说明](图的链接)        单独一行：一张图。链接是空的（图还没放进来）就不显示这一张
//   [视频的标题](哔哩哔哩的视频链接)   单独一行：变成播放器（只写一个链接也行）
// 别的写法（表格、嵌套列表、HTML）不认，原样当成一段话。
//
// 小标题定一套（评估 K2）：写介绍的人照着填，页面也按这几个名字取内容。
//   应用：     ## 它能帮你做什么、## 装好以后
//   解决方案： ## 它能帮你做什么、## 它帮你配好了什么、## 装好以后
// “装好以后”那一节里的列表，安装弹窗装好时当“接下来”列出来（docSection）。
//
// 视频有两种（videoKind）：有的（带 BV 号的哔哩哔哩链接、短链接或视频文件）；还没有（只写到
// bilibili.com/video/、没有 BV 号）——页面上当作没有：不画播放键、文档里不显示这一块（评估 I1）。

export type DocBlock =
  | { t: 'h'; level: 2 | 3; text: string }
  | { t: 'p'; text: string }
  | { t: 'ul' | 'ol'; items: string[] }
  | { t: 'quote'; text: string }
  | { t: 'video'; title: string; url: string }
  | { t: 'img'; alt: string; src: string }

// isVideoUrl：哔哩哔哩的视频页（含短链接）或视频文件。
export function isVideoUrl(url: string): boolean {
  return /^https?:\/\/([\w-]+\.)*(bilibili\.com\/video|b23\.tv)\b/i.test(url) || /^https?:\/\/\S+\.(mp4|webm|mov)(\?\S*)?$/i.test(url)
}

// bilibiliEmbed：哔哩哔哩的视频链接（https://www.bilibili.com/video/BV…，可以带 ?p=2）换成能嵌进页面的播放器地址；
// 不是这种链接（包括 b23.tv 短链接、还没填 BV 号的）返回空串。
export function bilibiliEmbed(url?: string): string {
  const m = /bilibili\.com\/video\/(BV[0-9A-Za-z]+)/.exec(url || '')
  if (!m) return ''
  const part = /[?&]p=(\d+)/.exec(url || '')
  return 'https://player.bilibili.com/player.html?bvid=' + m[1] + (part ? '&p=' + part[1] : '') + '&autoplay=1&high_quality=1&danmaku=0'
}

export function parseDoc(text: string): DocBlock[] {
  const out: DocBlock[] = []
  let para: string[] = []
  const flush = () => { if (para.length) { out.push({ t: 'p', text: para.join('') }); para = [] } }
  const list = (t: 'ul' | 'ol', item: string) => {
    const last = out[out.length - 1]
    if (last && last.t === t && para.length === 0) last.items.push(item)
    else { flush(); out.push({ t, items: [item] }) }
  }
  for (const raw of (text || '').replace(/\r/g, '').split('\n')) {
    const line = raw.trim()
    let m: RegExpExecArray | null
    if (!line) { flush(); continue }
    if ((m = /^(#{1,3})\s+(.+)$/.exec(line))) { flush(); out.push({ t: 'h', level: m[1].length === 3 ? 3 : 2, text: m[2] }); continue }
    if ((m = /^[-*]\s+(.+)$/.exec(line))) { list('ul', m[1]); continue }
    if ((m = /^\d+[.、]\s*(.+)$/.exec(line))) { list('ol', m[1]); continue }
    if ((m = /^>\s?(.+)$/.exec(line))) { flush(); out.push({ t: 'quote', text: m[1] }); continue }
    if ((m = /^!\[([^\]]*)\]\(([^)]*)\)$/.exec(line))) { flush(); out.push({ t: 'img', alt: m[1], src: m[2].trim() }); continue }
    if ((m = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(line)) && isVideoUrl(m[2].trim())) { flush(); out.push({ t: 'video', title: m[1], url: m[2].trim() }); continue }
    if (/^\S+$/.test(line) && isVideoUrl(line)) { flush(); out.push({ t: 'video', title: '', url: line }); continue }
    para.push(line)
  }
  flush()
  return out
}

export const DOC_HEADINGS = { what: '它能帮你做什么', setup: '它帮你配好了什么', after: '装好以后' } as const

// videoKind：real 有视频；none 还没有视频（页面上当作没有）。
export function videoKind(url: string): 'real' | 'none' {
  if (bilibiliEmbed(url) || /\.(mp4|webm|mov)(\?|$)/i.test(url)) return 'real'
  if (/bilibili\.com\/video\/?$/i.test(url)) return 'none'
  return 'real' // b23.tv 短链接等：有链接就算有，点了去哔哩哔哩看
}

// docHasVideo：文档里有没有视频（卡片封面上要不要画播放钮）。还没有视频的那种不算。
export function docHasVideo(text?: string): boolean {
  return !!text && parseDoc(text).some((b) => b.t === 'video' && videoKind(b.url) !== 'none')
}

// docSection：某个小标题下面的列表（有序、无序都行），例如“装好以后”的几步。没有这一节或这一节里没有列表时是空的。
export function docSection(text: string | undefined, heading: string): string[] {
  if (!text) return []
  const blocks = parseDoc(text)
  const at = blocks.findIndex((b) => b.t === 'h' && b.text.trim() === heading)
  if (at < 0) return []
  for (const b of blocks.slice(at + 1)) {
    if (b.t === 'h') break
    if (b.t === 'ul' || b.t === 'ol') return b.items.map((x) => x.replace(/\*\*([^*]+)\*\*|`([^`]+)`/g, '$1$2').replace(/\[([^\]]+)\]\([^)]+\)/g, '$1'))
  }
  return []
}
