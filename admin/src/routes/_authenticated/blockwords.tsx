import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { listSearch } from '@/features/content/search'
import { BlockwordsPage } from '@/features/community/blockwords-page'

const searchSchema = z.object({
  ...listSearch,
  group: z.string().trim().min(1).max(50).optional().catch(undefined),
  action: z.enum(['block', 'mask', 'review']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/blockwords')({
  component: BlockwordsPage,
  validateSearch: searchSchema,
})
