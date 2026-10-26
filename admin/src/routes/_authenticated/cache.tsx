import { createFileRoute } from '@tanstack/react-router'
import { CachePage } from '@/features/platform/cache-page'

export const Route = createFileRoute('/_authenticated/cache')({
  component: CachePage,
})
