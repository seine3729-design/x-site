import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'
import { PostsPage } from '@/features/feedback/posts-page'
import { listSearch, optionalId } from '@/features/content/search'

const searchSchema = z.object({
  ...listSearch,
  project_id: optionalId,
  status: z.enum(['draft', 'published', 'hidden']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/feedback/posts')({
  component: PostsPage,
  validateSearch: searchSchema,
})
