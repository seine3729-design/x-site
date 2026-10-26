import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { listSearch } from '@/features/content/search'
import { UsersPage } from '@/features/community/users-page'

const searchSchema = z.object({
  ...listSearch,
  kind: z.enum(['user', 'guest']).optional().catch(undefined),
  status: z.enum(['normal', 'muted', 'banned']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/users')({
  component: UsersPage,
  validateSearch: searchSchema,
})
