import { z } from 'zod'
import { PAGE_SIZES } from './components/pager'

/** 列表页地址栏参数的公共部分：页码、每页条数、关键词。非法值一律忽略（回到默认值）。 */
export const listSearch = {
  q: z.string().trim().min(1).max(50).optional().catch(undefined),
  page: z.coerce.number().int().min(1).max(10000).optional().catch(undefined),
  page_size: z.coerce
    .number()
    .int()
    .refine((v) => (PAGE_SIZES as readonly number[]).includes(v))
    .optional()
    .catch(undefined),
}

export const DEFAULT_PAGE_SIZE = 20

/** 可选的正整数 ID 参数（如 tag_id）。 */
export const optionalId = z.coerce.number().int().positive().optional().catch(undefined)

/** 改筛选条件后回到第 1 页。 */
export function withFilter<T extends Record<string, unknown>>(prev: T, patch: Partial<T>): T {
  return { ...prev, ...patch, page: undefined }
}
