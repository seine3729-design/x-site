#!/usr/bin/env node
/**
 * 构建后把静态产物预压缩成 .br 与 .gz，供边缘层直接回源（feedback/docs/06 第 11 节）。
 *
 * 为什么要预压缩，而不是让 Next.js / nginx 在请求时压：
 * - 运行时压缩为了不拖慢响应，只能用中等档位（Brotli 4 左右）。离线压可以用 11，
 *   同一份文件能再小一截，而且这份代价只付一次，不是每个请求付一次。
 * - 静态产物带内容哈希、永不变更，压一次就能一直用。
 *
 * 两种格式都出：Brotli 优先，Gzip 只是回退。边缘层按 Accept-Encoding 选
 * （nginx 的 brotli_static + gzip_static，配置见 deploy/edge/feedback.conf）。
 * 没有预压缩文件时边缘会落回实时压缩，所以这个脚本失败不会让站点挂掉，
 * 只是会变慢 —— 但它仍然按非 0 退出，让 CI 能发现。
 *
 * 用法：node scripts/precompress.mjs（已串在 package.json 的 build 后面）
 */
import { constants, brotliCompress, gzip } from 'node:zlib'
import { promisify } from 'node:util'
import { readdir, readFile, writeFile, stat } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { dirname, extname, join, relative } from 'node:path'

const br = promisify(brotliCompress)
const gz = promisify(gzip)

const root = join(dirname(fileURLToPath(import.meta.url)), '..')

/** 要扫的目录。.next/static 带内容哈希，public 是我们自己放的静态文件。 */
const TARGETS = ['.next/static', 'public']

/**
 * 只压这些类型。
 * 刻意不含 .map：source map 体积大、只给排错用，压它白费构建时间。
 * 也刻意不含图片与字体：png/jpg/webp/woff2 本身已是压缩格式，再压基本没有收益。
 */
const EXTENSIONS = new Set(['.js', '.mjs', '.css', '.svg', '.json', '.txt', '.xml', '.webmanifest'])

/**
 * 小于这个大小就不压。
 * 一个 TCP 包装得下的内容，压缩省下的字节换不回多一次解压的开销，
 * 而且很小的文件压完经常反而更大。
 */
const MIN_BYTES = 1024

async function* walk(dir) {
  let entries
  try {
    entries = await readdir(dir, { withFileTypes: true })
  } catch (err) {
    if (err.code === 'ENOENT') return
    throw err
  }
  for (const entry of entries) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) yield* walk(full)
    else if (entry.isFile()) yield full
  }
}

async function collect() {
  const files = []
  for (const target of TARGETS) {
    for await (const file of walk(join(root, target))) {
      if (!EXTENSIONS.has(extname(file))) continue
      const info = await stat(file)
      if (info.size < MIN_BYTES) continue
      files.push({ file, size: info.size })
    }
  }
  return files
}

async function compressOne({ file, size }) {
  const raw = await readFile(file)
  const [brBuf, gzBuf] = await Promise.all([
    br(raw, {
      params: {
        // 11 是最高档。只在构建时付这个代价，运行时不受影响。
        [constants.BROTLI_PARAM_QUALITY]: 11,
        // 24 = 16MB 窗口，对大 JS chunk 有额外收益
        [constants.BROTLI_PARAM_LGWIN]: 24,
        // 告诉编码器输入多大，它能据此挑更好的策略
        [constants.BROTLI_PARAM_SIZE_HINT]: size,
      },
    }),
    gz(raw, { level: 9 }),
  ])

  // 压完反而更大就不写：边缘拿到更大的预压缩文件会白白多传字节
  const wrote = { br: false, gz: false }
  if (brBuf.length < size) {
    await writeFile(`${file}.br`, brBuf)
    wrote.br = true
  }
  if (gzBuf.length < size) {
    await writeFile(`${file}.gz`, gzBuf)
    wrote.gz = true
  }
  return { size, br: brBuf.length, gz: gzBuf.length, wrote }
}

/** 并发跑，但不要一次把所有文件读进内存。 */
async function pool(items, limit, worker) {
  const results = []
  let cursor = 0
  const runners = Array.from({ length: Math.min(limit, items.length) }, async () => {
    while (cursor < items.length) {
      const index = cursor++
      results[index] = await worker(items[index])
    }
  })
  await Promise.all(runners)
  return results
}

const kb = (n) => `${(n / 1024).toFixed(1)} KB`

const files = await collect()
if (files.length === 0) {
  console.error(
    '预压缩：没有找到可压缩的产物。先跑 next build —— 只有 .next/static 存在时这一步才有意义。',
  )
  process.exit(1)
}

const started = Date.now()
const results = await pool(files, 8, compressOne)

let raw = 0
let brTotal = 0
let gzTotal = 0
for (const r of results) {
  raw += r.size
  brTotal += r.wrote.br ? r.br : r.size
  gzTotal += r.wrote.gz ? r.gz : r.size
}

// 列出最大的几个，构建体积涨回去时能一眼看出是谁
const top = results
  .map((r, i) => ({ ...r, file: relative(root, files[i].file) }))
  .sort((a, b) => b.br - a.br)
  .slice(0, 5)

console.log(`预压缩完成：${files.length} 个文件，${((Date.now() - started) / 1000).toFixed(1)}s`)
console.log(`  原始     ${kb(raw)}`)
console.log(`  Brotli   ${kb(brTotal)}  (${((1 - brTotal / raw) * 100).toFixed(1)}% 省下)`)
console.log(`  Gzip     ${kb(gzTotal)}  (${((1 - gzTotal / raw) * 100).toFixed(1)}% 省下)`)
console.log('  最大的几个产物（按 Brotli 后大小）：')
for (const r of top) {
  console.log(`    ${kb(r.br).padStart(10)}  ${r.file}`)
}
