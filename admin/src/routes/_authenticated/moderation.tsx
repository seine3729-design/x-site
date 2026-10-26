import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { listSearch } from '@/features/content/search'
import { ModerationPage } from '@/features/community/moderation-page'

const searchSchema = z.object({
  ...listSearch,
  kind: z.enum(['comment', 'danmaku']).optional().catch(undefined),
  status: z.enum(['pending', 'visible', 'hidden']).optional().catch(undefined),
  reported: z.boolean().optional().catch(undefined),
  blockword: z.boolean().optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/moderation')({
  component: ModerationPage,
  validateSearch: searchSchema,
})
