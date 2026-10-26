#!/usr/bin/env node
/**
 * 把首屏 JS 预算变成可运行的检查（feedback/docs/06 第 11 节）。
 *
 * 量的是「关键路径」：HTML 里会阻塞 hydration 的那些 script。
 * 刻意排除两类：
 * - 带 nomodule 的 polyfill chunk（34.3 KB）：支持 ES module 的浏览器根本不下它，
 *   算进来会凭空多出 34 KB，让预算看着超标；
 * - hydration 之后才按需取的 chunk（如懒加载的 sonner）：它们不阻塞可交互。
 *
 * 为什么要起服务再抓 HTML，而不是读构建清单：
 * build-manifest.json 只给出共享基座和路由 chunk，和浏览器真实取的列表差了 6 个文件
 * （实测基座 5 个 vs 首页实际 11 个）。清单推算出的绝对值没法用来对预算。
 *
 * 用法：
 *   1) bun run build
 *   2) bun run start            # 另一个终端，默认 3000 端口
 *   3) BASE=http://localhost:3000 bun run check:budget
 */
import { statSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const BASE = process.env.BASE || 'http://localhost:3000'

/**
 * 预算（Brotli 后，KB）。
 *
 * 这些数字是在 Next.js 16.4 + React 19.3 的实测基线上定的，不是拍的：
 * react-dom 60.0 KB + Next.js 客户端运行时 37.5 KB = 97.5 KB 是框架地板，
 * 我们一行代码都不写也得付。所以 total 的预算必须把这部分算进去，
 * 真正能控制的是 app 这一项。
 *
 * 改大任何一个数字之前，先确认不是代码引入了不必要的客户端依赖。
 */
const FRAMEWORK_FLOOR_KB = 97.5
const BUDGET = {
  /** 关键路径总量上限 */
  total: 180,
  /** 扣掉框架地板后，属于我们自己的代码 */
  app: 80,
}

/** 要检查的路由。覆盖最轻（长文页）到最重（带筛选器与表单的页）。 */
const ROUTES = ['/', '/guide', '/feedback', '/feedback/new', '/topics', '/me']

const size = (p) => {
  try {
    return statSync(p).size
  } catch {
    return 0
  }
}

/**
 * 从 HTML 里取出会阻塞 hydration 的脚本。
 *
 * 注意 nomodule 的判断：Next.js 输出的字面量是 `noModule=""`（React 的 prop 名），
 * HTML 属性名大小写不敏感，浏览器按 nomodule 处理，所以两种写法都要认。
 */
function criticalScripts(html) {
  const out = []
  for (const tag of html.match(/<script[^>]*>/g) ?? []) {
    if (/nomodule/i.test(tag)) continue
    const m = tag.match(/src="([^"]+)"/)
    if (!m) continue
    // 接了 CDN（next.config.ts 的 assetPrefix）之后 src 是绝对地址
    // https://cdn.example.com/feedback/_next/static/...，不再以 /_next/ 开头。
    // 只按 /_next/ 之后的部分去本地产物里找，两种形式都能量到。
    const at = m[1].indexOf('/_next/')
    if (at !== -1) out.push(m[1].slice(at))
  }
  return [...new Set(out)]
}

async function measure(route) {
  const res = await fetch(BASE + route, { headers: { 'accept-encoding': 'identity' } })
  if (!res.ok) throw new Error(`${route} 返回 ${res.status}`)
  const html = await res.text()

  let raw = 0
  let br = 0
  let gz = 0
  let counted = 0
  for (const src of criticalScripts(html)) {
    const file = join(root, '.next', src.replace('/_next/', ''))
    const r = size(file)
    if (r === 0) continue
    counted += 1
    raw += r
    br += size(`${file}.br`) || r
    gz += size(`${file}.gz`) || r
  }
  if (counted === 0) {
    throw new Error(
      `${route} 没匹配到任何产物。先跑 bun run build（并确认 BASE 指向的是同一份构建）。`,
    )
  }
  return { route, counted, raw: raw / 1024, br: br / 1024, gz: gz / 1024 }
}

let results
try {
  results = await Promise.all(ROUTES.map(measure))
} catch (err) {
  console.error(`首屏预算检查失败：${err.message}`)
  console.error(`BASE=${BASE}。需要先 bun run build，再 bun run start，然后跑这个检查。`)
  process.exit(1)
}

console.log(`首屏关键路径 JS（BASE=${BASE}，大小为预压缩后的实际传输量）\n`)
console.log('  Brotli      Gzip     原始   脚本  路由')

let failed = 0
for (const r of results.sort((a, b) => b.br - a.br)) {
  const app = r.br - FRAMEWORK_FLOOR_KB
  const ok = r.br <= BUDGET.total && app <= BUDGET.app
  if (!ok) failed += 1
  console.log(
    `${ok ? ' ok ' : 'FAIL'}${r.br.toFixed(1).padStart(7)} ${r.gz.toFixed(1).padStart(9)} ` +
      `${r.raw.toFixed(1).padStart(9)} ${String(r.counted).padStart(5)}   ${r.route}` +
      (ok ? '' : `  ← 超预算（应用代码 ${app.toFixed(1)} KB / 上限 ${BUDGET.app} KB）`),
  )
}

console.log(
  `\n预算：关键路径 ≤ ${BUDGET.total} KB，其中应用代码 ≤ ${BUDGET.app} KB` +
    `（框架地板 ${FRAMEWORK_FLOOR_KB} KB 不计入应用）`,
)
const worst = Math.max(...results.map((r) => r.br))
console.log(
  failed === 0
    ? `全部达标，最重的路由 ${worst.toFixed(1)} KB，余量 ${(BUDGET.total - worst).toFixed(1)} KB`
    : `${failed} 个路由超预算`,
)
process.exit(failed === 0 ? 0 : 1)
