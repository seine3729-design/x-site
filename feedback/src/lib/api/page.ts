import type { Page } from './types'

const EMPTY_PAGE: Page<never> = { list: [], total: 0, page: 1, page_size: 0 }

/** 列表接口的兜底：后端把空列表序列化成 null 时也能安全渲染。 */
export function pageOf<T>(page: Page<T> | null | undefined): Page<T> {
  if (!page) return EMPTY_PAGE as unknown as Page<T>
  return { ...page, list: page.list ?? [] }
}
