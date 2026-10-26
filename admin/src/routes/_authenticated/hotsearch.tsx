import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { listSearch } from '@/features/content/search'
import { HotsearchPage } from '@/features/ops/hotsearch-page'

// 路由把 ?visible=true 解析成布尔值（查询串按 JSON 解析），所以这里用布尔，手输的地址与页面内筛选写出的地址一致
const searchSchema = z.object({
  ...listSearch,
  visible: z.boolean().optional().catch(undefined),
  pinned: z.boolean().optional().catch(undefined),
  missing: z.literal(true).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/hotsearch')({
  component: HotsearchPage,
  validateSearch: searchSchema,
})
