import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { listSearch } from '@/features/content/search'
import { APIKeyPage } from '@/features/platform/apikey-page'

const searchSchema = z.object({
  ...listSearch,
  status: z.enum(['active', 'inactive']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/apikeys')({
  component: APIKeyPage,
  validateSearch: searchSchema,
})
