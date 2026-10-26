import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { listSearch } from '@/features/content/search'
import { TopicsPage } from '@/features/content/topics-page'

const searchSchema = z.object({
  ...listSearch,
  status: z.enum(['draft', 'published']).optional().catch(undefined),
  source_type: z.enum(['manual', 'auto']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/topics')({
  component: TopicsPage,
  validateSearch: searchSchema,
})
