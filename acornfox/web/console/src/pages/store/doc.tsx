// Doc：把一篇介绍文档（show.doc，Markdown）显示出来。应用页和解决方案页的介绍都只有这一块。
// 认哪些写法、小标题定哪一套见 lib/doc.ts。正文 14px，和控制台别的页面一样（评估 J4、X1）；图最高 320px。
// 视频：单独一行的哔哩哔哩链接变成播放器，下面留“去哔哩哔哩看”（嵌不出来时用）；链接是视频文件就直接播。
// 还没有视频的（链接只写到 bilibili.com/video/、没有 BV 号）不显示这一块。
// 图：链接是空的（图还没放进来）不显示。
import { useState } from 'react'
import { ExternalLink, Play } from 'lucide-react'
import { cn, safeHref } from '@/lib/utils'
import { bilibiliEmbed, parseDoc, videoKind } from '@/lib/doc'

// 行内：**加粗**、`原样的字`、[文字](链接)。
function inline(text: string): React.ReactNode[] {
  const out: React.ReactNode[] = []
  const re = /\*\*([^*]+)\*\*|`([^`]+)`|\[([^\]]+)\]\(([^)]+)\)/g
  let last = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(text))) {
    if (m.index > last) out.push(text.slice(last, m.index))
    if (m[1]) out.push(<strong key={m.index}>{m[1]}</strong>)
    else if (m[2]) out.push(<code key={m.index}>{m[2]}</code>)
    else {
      const url = m[4].trim()
      const href = url.startsWith('#') ? url : safeHref(url)
      out.push(href
        ? <a key={m.index} href={href} {...(url.startsWith('#') ? {} : { target: '_blank', rel: 'noopener noreferrer' })}>{m[3]}</a>
        : m[3])
    }
    last = re.lastIndex
  }
  if (last < text.length) out.push(text.slice(last))
  return out
}

function DocVideo({ title, url, color, autoplay }: { title: string; url: string; color: React.CSSProperties; autoplay: boolean }) {
  const [playing, setPlaying] = useState(autoplay)
  const link = safeHref(url)
  const embed = bilibiliEmbed(link)
  const file = !!link && /\.(mp4|webm|mov)(\?|$)/i.test(link)
  const play = () => { if (link && !embed) window.open(link, '_blank', 'noopener,noreferrer'); else setPlaying(true) }
  return (
    <figure data-testid="doc-video">
      <div className="store-cover relative aspect-video max-h-80 overflow-hidden rounded-lg border" style={color}>
        {link && file ? (
          <video className="absolute inset-0 size-full bg-black" src={link} controls autoPlay={playing} />
        ) : embed && playing ? (
          <iframe className="absolute inset-0 size-full border-0 bg-black" src={embed} title={title || '演示视频'} allow="autoplay; fullscreen; picture-in-picture" allowFullScreen referrerPolicy="no-referrer" />
        ) : (
          <button type="button" className="absolute inset-0" onClick={play} aria-label={'播放' + (title ? '：' + title : '')}>
            <span className="store-play absolute left-1/2 top-1/2 flex size-16 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full"><Play className="size-6 translate-x-[1px] fill-current" /></span>
            {title && <span className="store-time absolute bottom-3 left-3 rounded-md px-2 py-1 text-xs font-medium">{title}</span>}
          </button>
        )}
      </div>
      {link && !file && (
        <figcaption className="mt-1.5 text-right">
          <a href={link} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-2xs text-muted-foreground !no-underline hover:text-foreground">播不出来？去哔哩哔哩看<ExternalLink className="size-3" /></a>
        </figcaption>
      )}
    </figure>
  )
}

function DocImage({ alt, src }: { alt: string; src: string }) {
  const href = safeHref(src)
  if (!href) return null
  return (
    <figure>
      <img alt={alt} src={href} loading="lazy" className="max-h-80 w-full rounded-lg border object-contain" />
      {alt && <figcaption className="mt-1.5 text-center text-2xs text-muted-foreground">{alt}</figcaption>}
    </figure>
  )
}

// color：应用或解决方案的主色（视频还没开播时的底色）；autoplay：从卡片封面点进来的，第一段视频打开就播。
export function Doc({ text, color, autoplay = false, className }: { text: string; color?: string; autoplay?: boolean; className?: string }) {
  const blocks = parseDoc(text)
  const style = { '--c': color || '#64748b' } as React.CSSProperties
  const firstVideo = blocks.findIndex((b) => b.t === 'video' && videoKind(b.url) !== 'none')
  return (
    <article className={cn('store-doc', className)} data-testid="store-doc">
      {blocks.map((b, i) => {
        switch (b.t) {
          case 'h': return b.level === 3 ? <h3 key={i}>{inline(b.text)}</h3> : <h2 key={i}>{inline(b.text)}</h2>
          case 'p': return <p key={i}>{inline(b.text)}</p>
          case 'ul': return <ul key={i}>{b.items.map((x, k) => <li key={k}>{inline(x)}</li>)}</ul>
          case 'ol': return <ol key={i}>{b.items.map((x, k) => <li key={k}>{inline(x)}</li>)}</ol>
          case 'quote': return <blockquote key={i}>{inline(b.text)}</blockquote>
          case 'video': return videoKind(b.url) === 'none' ? null : <DocVideo key={i} title={b.title} url={b.url} color={style} autoplay={autoplay && i === firstVideo} />
          case 'img': return <DocImage key={i} alt={b.alt} src={b.src} />
        }
      })}
    </article>
  )
}
