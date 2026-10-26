import messages from '#messages/zh-CN.json'

/**
 * 文案访问入口（feedback/docs/06 第 1.1 节：组件内不硬编码文案）。
 *
 * 首发只有简体中文，不做语言切换与 RTL，所以这里不引 i18n 运行时：
 * 一个带类型的点号取值函数就够了。类型从 JSON 推导，key 写错会在 typecheck 阶段报错，
 * 这是不硬编码文案能带来的主要好处，必须保住。
 */

type Dict = { [key: string]: string | Dict }

/** 把嵌套对象的所有叶子路径展开成 'a.b.c' 这样的字面量联合类型。 */
type LeafPath<T> = T extends string
  ? never
  : {
      [K in keyof T & string]: T[K] extends string ? K : `${K}.${LeafPath<T[K]>}`
    }[keyof T & string]

export type MessageKey = LeafPath<typeof messages>

/** 插值变量，如 t('x.y', { count: 3 })。 */
export type Vars = Record<string, string | number>

function lookup(key: string): string {
  let node: string | Dict = messages as Dict
  for (const part of key.split('.')) {
    if (typeof node === 'string') return key
    const next: string | Dict | undefined = node[part]
    if (next === undefined) return key
    node = next
  }
  return typeof node === 'string' ? node : key
}

/**
 * 取文案。`{name}` 形式的占位会被 vars 替换。
 * 找不到 key 时返回 key 本身——页面上会直接看到 `home.heroTitle` 这样的字符串，
 * 比静默显示空白更容易在验收时发现。
 */
export function t(key: MessageKey, vars?: Vars): string {
  const raw = lookup(key)
  if (!vars) return raw
  return raw.replace(/\{(\w+)\}/g, (whole, name: string) =>
    name in vars ? String(vars[name]) : whole,
  )
}

/**
 * 业务错误码 → 用户文案。
 *
 * 策略：
 * - 后端给的 message 是我们自己写的、面向用户的具体原因（例如「这个议题已经结束征集」），
 *   对业务类错误直接用它，比通用文案有用得多；
 * - 但 INTERNAL / UNAVAILABLE 这类可能带内部细节，一律换成通用文案，只保留问题编号。
 */
const SAFE_TO_SHOW = new Set([
  'BAD_REQUEST',
  'FORBIDDEN',
  'CONFLICT',
  'RATE_LIMITED',
  'NOT_FOUND',
  'NEED_LOGIN',
  'TOKEN_EXPIRED',
])

export function errorText(code: string | undefined, backendMessage?: string): string {
  if (code && SAFE_TO_SHOW.has(code) && backendMessage) return backendMessage
  const key = `error.${code ?? 'INTERNAL'}`
  const text = lookup(key)
  return text === key ? lookup('error.INTERNAL') : text
}
