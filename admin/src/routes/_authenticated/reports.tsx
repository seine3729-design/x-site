import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { listSearch } from '@/features/content/search'
import { ReportsPage } from '@/features/community/reports-page'

const searchSchema = z.object({
  ...listSearch,
  status: z.enum(['pending', 'resolved', 'rejected']).optional().catch(undefined),
  target_type: z.enum(['comment', 'danmaku', 'drama', 'comment_image']).optional().catch(undefined),
  reason: z.enum(['attack', 'vulgar', 'illegal', 'minor', 'ad', 'spoiler', 'other']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/reports')({
  component: ReportsPage,
  validateSearch: searchSchema,
})
