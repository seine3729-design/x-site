import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { DramasPage } from '@/features/content/dramas-page'
import { listSearch, optionalId } from '@/features/content/search'

const searchSchema = z.object({
  ...listSearch,
  status: z.enum(['draft', 'published', 'scheduled', 'offline']).optional().catch(undefined),
  tag_id: optionalId,
  term_id: optionalId,
  sort: z.enum(['latest', 'updated', 'hot', 'recommend']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/dramas/')({
  component: DramasPage,
  validateSearch: searchSchema,
})
