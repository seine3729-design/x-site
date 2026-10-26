import { createFileRoute } from '@tanstack/react-router'
import { FeaturesPage } from '@/features/platform/features-page'

export const Route = createFileRoute('/_authenticated/features')({
  component: FeaturesPage,
})
