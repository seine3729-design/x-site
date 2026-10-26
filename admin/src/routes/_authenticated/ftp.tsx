import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { listSearch } from '@/features/content/search'
import { FTPPage } from '@/features/resource/ftp-page'

const searchSchema = z.object({
  ...listSearch,
  status: z.enum(['active', 'inactive']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/ftp')({
  component: FTPPage,
  validateSearch: searchSchema,
})
