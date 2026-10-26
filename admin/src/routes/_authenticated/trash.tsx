import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { listSearch } from '@/features/content/search'
import { TrashPage } from '@/features/platform/trash-page'

const searchSchema = z.object({ ...listSearch })

export const Route = createFileRoute('/_authenticated/trash')({
  component: TrashPage,
  validateSearch: searchSchema,
})
