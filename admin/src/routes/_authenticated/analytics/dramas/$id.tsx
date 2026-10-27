import { createFileRoute } from '@tanstack/react-router'
import { DramaAnalyticsDetailPage } from '@/features/analytics/drama-page'

export const Route = createFileRoute('/_authenticated/analytics/dramas/$id')({
  component: DramaAnalyticsDetailPage,
})
