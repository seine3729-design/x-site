import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'
import { TopicsPage } from '@/features/feedback/topics-page'
import { listSearch, optionalId } from '@/features/content/search'

const searchSchema = z.object({
  ...listSearch,
  project_id: optionalId,
  status: z.enum(['draft', 'open', 'closed', 'archived']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/feedback/topics')({
  component: TopicsPage,
  validateSearch: searchSchema,
})
