// 计算控制台源码哈希，写进构建产物 build-info.json。
// 规则（Go 测试 internal/console/console_test.go 按同样的规则重算）：
//   1. 取 build-inputs.json 里 files 列的文件，加上 dirs 列的目录下的所有文件；
//   2. 路径统一用 / 分隔、相对本目录，匹配 exclude 里任一正则的去掉；
//   3. 按路径字节序排序，每个文件拼一行 “路径\n内容的 sha256 十六进制\n”；
//   4. 对拼好的整段文字再算一次 sha256。
// 用法：node scripts/source-hash.mjs（打印哈希）。
import { createHash } from 'node:crypto'
import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

export const consoleRoot = join(dirname(fileURLToPath(import.meta.url)), '..')

function walk(root, rel, out) {
  const abs = join(root, rel)
  for (const name of readdirSync(abs)) {
    const r = rel + '/' + name
    if (statSync(join(root, r)).isDirectory()) walk(root, r, out)
    else out.push(r)
  }
}

export function sourceFiles(root = consoleRoot) {
  const spec = JSON.parse(readFileSync(join(root, 'build-inputs.json'), 'utf8'))
  const files = []
  for (const f of spec.files) if (existsSync(join(root, f))) files.push(f)
  for (const d of spec.dirs) if (existsSync(join(root, d))) walk(root, d, files)
  const ex = spec.exclude.map((p) => new RegExp(p))
  return [...new Set(files)].filter((f) => !ex.some((re) => re.test(f))).sort((a, b) => (a < b ? -1 : a > b ? 1 : 0))
}

export function sourceHash(root = consoleRoot) {
  const h = createHash('sha256')
  for (const f of sourceFiles(root)) {
    const digest = createHash('sha256').update(readFileSync(join(root, f))).digest('hex')
    h.update(f + '\n' + digest + '\n')
  }
  return h.digest('hex')
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  console.log(sourceHash())
}
