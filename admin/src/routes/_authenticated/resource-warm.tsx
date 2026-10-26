import { createFileRoute } from '@tanstack/react-router'
import { ResourceWarmPage } from '@/features/content/resource-warm-page'

export const Route = createFileRoute('/_authenticated/resource-warm')({
  component: ResourceWarmPage,
})
