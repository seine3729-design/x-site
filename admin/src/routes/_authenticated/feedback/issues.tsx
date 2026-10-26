import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'
import { IssuesPage } from '@/features/feedback/issues-page'
import { listSearch, optionalId } from '@/features/content/search'

const searchSchema = z.object({
  ...listSearch,
  project_id: optionalId,
  status: z
    .enum(['submitted', 'triaged', 'planned', 'in_progress', 'verifying', 'shipped', 'closed', 'duplicate', 'declined'])
    .optional()
    .catch(undefined),
  kind: z
    .enum(['bug', 'experience', 'feature', 'content', 'access', 'competitor', 'other'])
    .optional()
    .catch(undefined),
  priority: z.enum(['critical', 'high', 'medium', 'low', 'unset']).optional().catch(undefined),
  visibility: z.enum(['public', 'members', 'private']).optional().catch(undefined),
  source: z.enum(['community', 'open_api']).optional().catch(undefined),
  sort: z.enum(['latest', 'newest', 'votes', 'oldest']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/feedback/issues')({
  component: IssuesPage,
  validateSearch: searchSchema,
})
