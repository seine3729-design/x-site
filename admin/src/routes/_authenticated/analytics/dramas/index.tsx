import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { listSearch } from '@/features/content/search'
import { DramaAnalyticsPage } from '@/features/analytics/dramas-page'

const searchSchema = z.object({
  days: z
    .coerce.number()
    .int()
    .refine((v) => [7, 30, 90].includes(v))
    .optional()
    .catch(undefined),
  sort: z.enum(['viewers', 'play_starts', 'watch_seconds']).optional().catch(undefined),
  page: listSearch.page,
  page_size: listSearch.page_size,
})

export const Route = createFileRoute('/_authenticated/analytics/dramas/')({
  component: DramaAnalyticsPage,
  validateSearch: searchSchema,
})
