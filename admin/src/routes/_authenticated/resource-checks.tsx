import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { ResourceChecksPage } from '@/features/content/resource-checks-page'
import { optionalId } from '@/features/content/search'

const searchSchema = z.object({
  q: z.string().trim().min(1).max(50).optional().catch(undefined),
  drama_id: optionalId,
  bad_only: z.coerce.boolean().optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/resource-checks')({
  component: ResourceChecksPage,
  validateSearch: searchSchema,
})
