import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { listSearch } from '@/features/content/search'
import { TagsPage } from '@/features/content/tags-page'

const searchSchema = z.object({
  ...listSearch,
  status: z.enum(['active', 'hidden']).optional().catch(undefined),
  merged: z.enum(['true', 'false']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/tags')({
  component: TagsPage,
  validateSearch: searchSchema,
})
