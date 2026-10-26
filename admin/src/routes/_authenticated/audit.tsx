import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { listSearch, optionalId } from '@/features/content/search'
import { AuditPage } from '@/features/platform/audit-page'

const searchSchema = z.object({
  ...listSearch,
  action: z.string().min(1).max(100).optional().catch(undefined),
  target_type: z.string().min(1).max(50).optional().catch(undefined),
  admin_id: optionalId,
  // 日期输入框的值（YYYY-MM-DD，浏览器本地时区），请求时转成带时区的 RFC3339
  from: z.string().regex(/^\d{4}-\d{2}-\d{2}$/).optional().catch(undefined),
  to: z.string().regex(/^\d{4}-\d{2}-\d{2}$/).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/audit')({
  component: AuditPage,
  validateSearch: searchSchema,
})
