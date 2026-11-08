// 复制到剪贴板。控制台经 SSH 隧道走 http://127.0.0.1，属于安全上下文，clipboard 接口可用；
// 局域网的 http 地址不是安全上下文，退回到选中文本再 execCommand('copy')。
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    /* 退回旧做法 */
  }
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.className = 'fixed left-0 top-0 opacity-0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    ta.remove()
    return ok
  } catch {
    return false
  }
}
