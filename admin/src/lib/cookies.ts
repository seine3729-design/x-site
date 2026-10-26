/**
 * 使用手动 document.cookie 方法实现 Cookie 实用功能
 * 替换 js-cookie 依赖项，以提高一致性
 */

const DEFAULT_MAX_AGE = 60 * 60 * 24 * 7 // 7 days

/**
 * 通过名称获取 Cookie 值
 */
export function getCookie(name: string): string | undefined {
  if (typeof document === 'undefined') return undefined

  const value = `; ${document.cookie}`
  const parts = value.split(`; ${name}=`)
  if (parts.length === 2) {
    const cookieValue = parts.pop()?.split(';').shift()
    return cookieValue
  }
  return undefined
}

/**
 * 设置 Cookie，包含名称、值和可选的最大年龄
 */
export function setCookie(
  name: string,
  value: string,
  maxAge: number = DEFAULT_MAX_AGE
): void {
  if (typeof document === 'undefined') return

  document.cookie = `${name}=${value}; path=/; max-age=${maxAge}`
}

/**
 * 通过将最大年龄设置为 0 来删除 Cookie
 */
export function removeCookie(name: string): void {
  if (typeof document === 'undefined') return

  document.cookie = `${name}=; path=/; max-age=0`
}
