import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { ImportPage } from '@/features/content/import-page'
import { listSearch } from '@/features/content/search'

const searchSchema = z.object({
  page: listSearch.page,
  page_size: listSearch.page_size,
  // 日期输入框的值（YYYY-MM-DD，浏览器本地时区），请求时换算成带时区的时间戳
  from: z.string().regex(/^\d{4}-\d{2}-\d{2}$/).optional().catch(undefined),
  to: z.string().regex(/^\d{4}-\d{2}-\d{2}$/).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/import')({
  component: ImportPage,
  validateSearch: searchSchema,
})
