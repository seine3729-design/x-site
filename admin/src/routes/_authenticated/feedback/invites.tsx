import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'
import { InvitesPage } from '@/features/feedback/invites-page'
import { listSearch, optionalId } from '@/features/content/search'

const searchSchema = z.object({
  ...listSearch,
  project_id: optionalId,
})

export const Route = createFileRoute('/_authenticated/feedback/invites')({
  component: InvitesPage,
  validateSearch: searchSchema,
})
