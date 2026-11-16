// 平台域名页面用的小工具：把用户填的域名理一理，算出到买域名的地方要加的那条记录叫什么名字。
// 后端设了域名以后会在 record.name 里给准的名字；这里只在还没保存（用户正在填）时用来示意。

// 常见的两段式后缀：apps.example.com.cn 的“根”是 example.com.cn，记录名字是 *.apps。
const TWO_PART = new Set(['com.cn', 'net.cn', 'org.cn', 'gov.cn', 'edu.cn', 'ac.cn', 'com.hk', 'com.tw', 'co.uk', 'org.uk', 'co.jp', 'ne.jp', 'or.jp', 'com.au', 'co.kr', 'com.sg'])

// normDomain：去掉首尾空白、http(s)://、路径和末尾的点，转小写。
export function normDomain(s: string): string {
  return s.trim().toLowerCase().replace(/^https?:\/\//, '').replace(/[/?#].*$/, '').replace(/\.$/, '')
}

// recordName：apps.example.com → *.apps；example.com → *；a.b.example.com.cn → *.a.b。填得不像域名时返回空。
export function recordName(domain: string): string {
  const labels = normDomain(domain).split('.').filter(Boolean)
  if (labels.length < 2) return ''
  const root = TWO_PART.has(labels.slice(-2).join('.')) ? 3 : 2
  if (labels.length <= root) return '*'
  return '*.' + labels.slice(0, labels.length - root).join('.')
}
