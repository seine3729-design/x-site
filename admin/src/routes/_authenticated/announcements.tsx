import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { listSearch } from '@/features/content/search'
import { AnnouncementsPage } from '@/features/ops/announcements-page'

const searchSchema = z.object({
  ...listSearch,
  kind: z.enum(['topbar', 'popup', 'inbox']).optional().catch(undefined),
  status: z.enum(['active', 'inactive']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/announcements')({
  component: AnnouncementsPage,
  validateSearch: searchSchema,
})
