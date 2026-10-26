import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { listSearch } from '@/features/content/search'
import { PagesPage } from '@/features/site/pages-page'

const searchSchema = z.object({
  ...listSearch,
  status: z.enum(['draft', 'published']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/pages')({
  component: PagesPage,
  validateSearch: searchSchema,
})
