// 被截断的文字，悬停时显示全文（样稿第 12 版，全站只装这一处）。
// 带 truncate 类名的元素，字真的被截掉了（scrollWidth > clientWidth）而且自己没写 title 时，鼠标移上去临时补一个 title；
// 没被截的不补，免得到处都是多余的气泡。只在鼠标移上去时量一次，不轮询。
const MARK = 'data-auto-title'

export function installTruncateTitles() {
  if (typeof document === 'undefined') return
  document.addEventListener('mouseover', (e) => {
    const el = e.target instanceof Element ? e.target.closest<HTMLElement>('.truncate') : null
    if (!el) return
    const cut = el.scrollWidth > el.clientWidth + 1
    if (cut && !el.title) {
      el.title = (el.textContent || '').trim()
      el.setAttribute(MARK, '')
    } else if (!cut && el.hasAttribute(MARK)) {
      el.removeAttribute('title')
      el.removeAttribute(MARK)
    } else if (cut && el.hasAttribute(MARK)) {
      el.title = (el.textContent || '').trim()
    }
  }, { passive: true })
}
