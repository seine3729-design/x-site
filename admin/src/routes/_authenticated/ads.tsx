import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { listSearch } from '@/features/content/search'
import { AD_SLOTS } from '@/features/ops/api'
import { AdsPage } from '@/features/ops/ads-page'

const searchSchema = z.object({
  ...listSearch,
  slot: z.enum(AD_SLOTS).optional().catch(undefined),
  status: z.enum(['active', 'inactive']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/ads')({
  component: AdsPage,
  validateSearch: searchSchema,
})
