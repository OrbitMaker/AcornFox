// 守门：页面的源码（不含测试和测试数据）不认识任何一个具体的应用，也不为某种服务类型写专门的文字或分支，
// 不再有“一并装”“先装某某”这类说法。查的名字取自测试数据里的应用（id、名字、关键词）和服务类型（名字、中文名），
// 注释不算（说明里可以举例）。
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, it } from 'vitest'
import { catalog, serviceKinds } from '@/test/fixtures'

// Vitest 在控制台目录（acornfox/web/console）下运行；测试环境是 jsdom，import.meta.url 不是文件地址，所以按工作目录找。
const SRC = join(process.cwd(), 'src')

function sourceFiles(dir: string): string[] {
  const out: string[] = []
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) {
      if (relative(SRC, p) !== 'test') out.push(...sourceFiles(p))
    } else if (/\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name)) {
      out.push(p)
    }
  }
  return out
}

// 去掉注释：/* … */（含 JSX 里的 {/* … */}）和行尾的 // …（前面是行首或空白，免得误伤 https://）。
function code(text: string): string {
  return text.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|\s)\/\/.*$/gm, '$1')
}

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

describe('页面源码里没有具体的应用和服务类型', () => {
  const files = sourceFiles(SRC)
  const words = new Set<string>()
  for (const a of catalog.apps) for (const w of [a.id, a.name, ...(a.keywords || [])]) if (w && w.length >= 3) words.add(w)
  for (const k of serviceKinds) for (const w of [k.kind, k.label]) if (w) words.add(w)
  const phrases = ['一并装', '一并安装', '要先装', '先装好', '还要先装', 'with_providers', 'will_install']
  const pattern = new RegExp([...words, ...phrases].map(escape).join('|'))

  it('找到了要查的文件和名字', () => {
    expect(files.length).toBeGreaterThan(40)
    expect(files.some((f) => f.endsWith(join('pages', 'store', 'install-dialog.tsx')))).toBe(true)
    expect(words.size).toBeGreaterThanOrEqual(6)
  })

  it('不含应用名、服务类型名和“一并装”的说法', () => {
    const hits: string[] = []
    for (const f of files) {
      code(readFileSync(f, 'utf8')).split('\n').forEach((line, i) => {
        const m = line.match(pattern)
        if (m) hits.push(`${relative(SRC, f)}:${i + 1}: ${m[0]}`)
      })
    }
    expect(hits).toEqual([])
  })

  it('去注释不会把代码一起去掉（自检）', () => {
    expect(code("const u = 'https://x' // 例如 new-api")).toBe("const u = 'https://x' ")
    expect(code('a /* new-api */ b')).toBe('a  b')
  })
})
