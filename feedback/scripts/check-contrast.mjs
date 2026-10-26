#!/usr/bin/env node
/**
 * 把「对比度达标」变成可运行的检查，而不是靠眼睛看。
 *
 * 从 src/app/globals.css 解析三层 token（含 var() 引用链），对照下面的配对表
 * 计算 WCAG 2.2 对比度：正文 4.5:1，大号文字与非文字控件边界 3:1。
 * 浅色与深色分别算 —— 深色不是反色，必须各自验证（feedback/docs/06 第 9 节）。
 *
 * 用法：node scripts/check-contrast.mjs
 */
import { readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const cssPath = join(here, '..', 'src', 'app', 'globals.css')

/** 需要保证的配对。min 取 4.5（正文）或 3（大号文字 / 控件边界）。 */
const PAIRS = [
  // 正文与背景
  { name: '正文 / 页面背景', fg: 'fg', bg: 'canvas', min: 4.5 },
  { name: '正文 / 卡片', fg: 'fg', bg: 'surface', min: 4.5 },
  { name: '正文 / 抬升面', fg: 'fg', bg: 'surface-raised', min: 4.5 },
  { name: '次要文字 / 页面背景', fg: 'fg-muted', bg: 'canvas', min: 4.5 },
  { name: '次要文字 / 卡片', fg: 'fg-muted', bg: 'surface', min: 4.5 },
  { name: '次要文字 / 抬升面', fg: 'fg-muted', bg: 'surface-raised', min: 4.5 },
  { name: '辅助文字 / 卡片', fg: 'fg-subtle', bg: 'surface', min: 4.5 },
  { name: '辅助文字 / 页面背景', fg: 'fg-subtle', bg: 'canvas', min: 4.5 },
  { name: '辅助文字 / 抬升面', fg: 'fg-subtle', bg: 'surface-raised', min: 4.5 },

  // 浮层（下拉、弹窗、菜单）自成一档背景，必须单独验证：
  // 深色下它比卡片更亮，卡片上刚好达标的颜色放到浮层里可能就不达标了
  { name: '正文 / 浮层', fg: 'fg', bg: 'surface-overlay', min: 4.5 },
  { name: '次要文字 / 浮层', fg: 'fg-muted', bg: 'surface-overlay', min: 4.5 },
  { name: '辅助文字 / 浮层', fg: 'fg-subtle', bg: 'surface-overlay', min: 4.5 },
  { name: '品牌文字 / 浮层', fg: 'primary-fg', bg: 'surface-overlay', min: 4.5 },

  // 品牌色作文字 / 作填充
  { name: '品牌文字 / 卡片', fg: 'primary-fg', bg: 'surface', min: 4.5 },
  { name: '品牌文字 / 页面背景', fg: 'primary-fg', bg: 'canvas', min: 4.5 },
  { name: '品牌文字 / 品牌浅底', fg: 'primary-fg', bg: 'primary-surface', min: 4.5 },
  { name: '填充按钮文字 / 品牌填充', fg: 'fg-on-fill', bg: 'primary', min: 4.5 },
  { name: '填充按钮文字 / 品牌填充(hover)', fg: 'fg-on-fill', bg: 'primary-hover', min: 4.5 },
  { name: '填充按钮文字 / 强调填充', fg: 'fg-on-fill', bg: 'accent', min: 4.5 },
  { name: '填充按钮文字 / 危险填充', fg: 'fg-on-fill', bg: 'danger', min: 4.5 },

  // 状态色：作文字的那一档必须达标，作背景的那一档只要求能撑起文字
  { name: '成功文字 / 卡片', fg: 'success-fg', bg: 'surface', min: 4.5 },
  { name: '成功文字 / 成功浅底', fg: 'success-fg', bg: 'success-surface', min: 4.5 },
  { name: '警告文字 / 卡片', fg: 'warning-fg', bg: 'surface', min: 4.5 },
  { name: '警告文字 / 警告浅底', fg: 'warning-fg', bg: 'warning-surface', min: 4.5 },
  { name: '危险文字 / 卡片', fg: 'danger-fg', bg: 'surface', min: 4.5 },
  { name: '危险文字 / 危险浅底', fg: 'danger-fg', bg: 'danger-surface', min: 4.5 },
  { name: '信息文字 / 卡片', fg: 'info-fg', bg: 'surface', min: 4.5 },
  { name: '信息文字 / 信息浅底', fg: 'info-fg', bg: 'info-surface', min: 4.5 },

  // 非文字对比（WCAG 1.4.11）：表单控件边界与焦点环
  { name: '控件边框 / 卡片', fg: 'control-border', bg: 'surface', min: 3 },
  { name: '控件边框 / 页面背景', fg: 'control-border', bg: 'canvas', min: 3 },
  { name: '控件边框 / 抬升面', fg: 'control-border', bg: 'surface-raised', min: 3 },
  { name: '控件边框 / 浮层', fg: 'control-border', bg: 'surface-overlay', min: 3 },
  { name: '控件边框(hover) / 卡片', fg: 'control-border-hover', bg: 'surface', min: 3 },
  // 分段控件（外观切换器）：未选中的图标画在 sunken 轨道上
  { name: '未选中图标 / 凹陷面', fg: 'fg-subtle', bg: 'surface-sunken', min: 3 },
  { name: '焦点环 / 卡片', fg: 'ring', bg: 'surface', min: 3 },
  { name: '焦点环 / 页面背景', fg: 'ring', bg: 'canvas', min: 3 },
  { name: '焦点环 / 浮层', fg: 'ring', bg: 'surface-overlay', min: 3 },
  { name: '状态图标(成功) / 卡片', fg: 'success', bg: 'surface', min: 3 },
  { name: '状态图标(警告) / 卡片', fg: 'warning', bg: 'surface', min: 3 },
  { name: '状态图标(危险) / 卡片', fg: 'danger', bg: 'surface', min: 3 },
]

/** 解析 @theme / :root / .dark 三个块里的 `--name: value;`。 */
function parseBlocks(css) {
  const grab = (header) => {
    const start = css.indexOf(header)
    if (start < 0) throw new Error(`globals.css 里找不到 ${header}`)
    const open = css.indexOf('{', start)
    let depth = 0
    let i = open
    for (; i < css.length; i += 1) {
      if (css[i] === '{') depth += 1
      else if (css[i] === '}') {
        depth -= 1
        if (depth === 0) break
      }
    }
    const body = css.slice(open + 1, i)
    const vars = new Map()
    for (const m of body.matchAll(/--([\w-]+)\s*:\s*([^;]+);/g)) {
      vars.set(m[1], m[2].trim())
    }
    return vars
  }
  return { theme: grab('@theme {'), light: grab(':root {'), dark: grab('.dark {') }
}

/** 顺着 var(--x) 引用链求出最终颜色字面量。 */
function resolve(name, scope, theme, seen = new Set()) {
  if (seen.has(name)) throw new Error(`token ${name} 存在循环引用`)
  seen.add(name)
  const raw = scope.get(name) ?? theme.get(name)
  if (!raw) throw new Error(`找不到 token --${name}`)
  const ref = raw.match(/^var\(--([\w-]+)\)$/)
  if (ref) return resolve(ref[1], scope, theme, seen)
  return raw
}

function toRgb(value) {
  const hex = value.trim()
  const m3 = hex.match(/^#([\da-f])([\da-f])([\da-f])$/i)
  if (m3) return [0, 1, 2].map((i) => parseInt(m3[i + 1].repeat(2), 16))
  const m6 = hex.match(/^#([\da-f]{2})([\da-f]{2})([\da-f]{2})$/i)
  if (m6) return [0, 1, 2].map((i) => parseInt(m6[i + 1], 16))
  throw new Error(`无法解析颜色 ${value}（本检查只支持十六进制）`)
}

/** WCAG 相对亮度。 */
function luminance([r, g, b]) {
  const f = (c) => {
    const s = c / 255
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b)
}

function contrast(a, b) {
  const la = luminance(toRgb(a))
  const lb = luminance(toRgb(b))
  const [hi, lo] = la > lb ? [la, lb] : [lb, la]
  return (hi + 0.05) / (lo + 0.05)
}

const css = await readFile(cssPath, 'utf8')
const { theme, light, dark } = parseBlocks(css)

let failed = 0
for (const [label, scope] of [
  ['浅色', light],
  ['深色', dark],
]) {
  console.log(`\n== ${label} ==`)
  for (const pair of PAIRS) {
    const fg = resolve(pair.fg, scope, theme)
    const bg = resolve(pair.bg, scope, theme)
    const ratio = contrast(fg, bg)
    const ok = ratio >= pair.min
    if (!ok) failed += 1
    const mark = ok ? 'ok  ' : 'FAIL'
    console.log(
      `  ${mark} ${pair.name.padEnd(28, '　')} ${ratio.toFixed(2)}:1 ` +
        `(需 ${pair.min}:1, ${fg} on ${bg})`,
    )
  }
}

console.log(failed === 0 ? '\n全部达标' : `\n${failed} 项未达标`)
process.exit(failed === 0 ? 0 : 1)
