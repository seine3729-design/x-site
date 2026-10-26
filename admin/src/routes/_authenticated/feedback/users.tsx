import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'
import { UsersPage } from '@/features/feedback/users-page'
import { listSearch } from '@/features/content/search'

const searchSchema = z.object({
  ...listSearch,
  status: z.enum(['active', 'restricted', 'suspended']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/feedback/users')({
  component: UsersPage,
  validateSearch: searchSchema,
})
