// Vite 插件：构建时做两件事。
//   1. 列出真正打进产物的 npm 包（含许可证全文），写到本目录 third-party-licenses.json，
//      scripts/gen-licenses.py 读它生成 docs/licenses；
//   2. 把源码哈希写进产物目录的 build-info.json（见 source-hash.mjs）。
import { readFileSync, readdirSync, writeFileSync, existsSync } from 'node:fs'
import { join, sep } from 'node:path'
import { sourceHash, consoleRoot } from './source-hash.mjs'

const NOTICE_RE = /^(LICEN[CS]E|COPYING|NOTICE)([._-].*)?$/i
// 不以模块形式出现、但内容进了产物的包：CSS 里 @import 的字体、Tailwind 生成的基础样式、Vite 的预加载辅助代码。
const ALWAYS = ['@fontsource/geist-mono', '@fontsource/geist-sans', 'tailwindcss', 'vite']

function packageRoot(id) {
  const norm = id.split(sep).join('/')
  const i = norm.lastIndexOf('/node_modules/')
  if (i < 0) return null
  const rest = norm.slice(i + '/node_modules/'.length).split('/')
  const name = rest[0].startsWith('@') ? rest[0] + '/' + rest[1] : rest[0]
  return { dir: norm.slice(0, i) + '/node_modules/' + name, name }
}

function describe(dir) {
  const pkg = JSON.parse(readFileSync(join(dir, 'package.json'), 'utf8'))
  const names = readdirSync(dir).filter((n) => NOTICE_RE.test(n)).sort()
  if (names.length === 0) throw new Error(`${pkg.name} 没有许可文件，不能打进产物`)
  const notice = names.map((n) => {
    const t = readFileSync(join(dir, n), 'utf8')
    return n + '\n' + (t.endsWith('\n') ? t : t + '\n')
  }).join('')
  return {
    name: pkg.name,
    version: pkg.version,
    license: typeof pkg.license === 'string' ? pkg.license : '',
    source: `https://www.npmjs.com/package/${pkg.name}/v/${pkg.version}`,
    notice,
  }
}

export default function acornfoxBuildMeta() {
  let outDir = ''
  return {
    name: 'acornfox-build-meta',
    apply: 'build',
    configResolved(cfg) { outDir = cfg.build.outDir },
    generateBundle() {
      const dirs = new Map()
      for (const id of this.getModuleIds()) {
        const p = packageRoot(id)
        if (p && !dirs.has(p.name)) dirs.set(p.name, p.dir)
      }
      for (const name of ALWAYS) {
        const d = join(consoleRoot, 'node_modules', name)
        if (existsSync(d)) dirs.set(name, d)
      }
      const list = [...dirs.values()].map(describe).sort((a, b) => (a.name < b.name ? -1 : 1))
      writeFileSync(join(consoleRoot, 'third-party-licenses.json'), JSON.stringify(list, null, 1) + '\n')
    },
    closeBundle() {
      const info = { source_sha256: sourceHash(), inputs: 'web/console/build-inputs.json' }
      writeFileSync(join(outDir, 'build-info.json'), JSON.stringify(info, null, 2) + '\n')
    },
  }
}
