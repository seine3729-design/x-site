import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'
import { ProjectsPage } from '@/features/feedback/projects-page'
import { listSearch } from '@/features/content/search'

const searchSchema = z.object({
  ...listSearch,
  status: z.enum(['draft', 'recruiting', 'active', 'paused', 'archived']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/feedback/projects')({
  component: ProjectsPage,
  validateSearch: searchSchema,
})
