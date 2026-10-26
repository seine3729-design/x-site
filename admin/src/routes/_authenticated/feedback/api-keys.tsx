import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'
import { ApiKeysPage } from '@/features/feedback/api-keys-page'
import { listSearch, optionalId } from '@/features/content/search'

const searchSchema = z.object({
  ...listSearch,
  project_id: optionalId,
})

export const Route = createFileRoute('/_authenticated/feedback/api-keys')({
  component: ApiKeysPage,
  validateSearch: searchSchema,
})
