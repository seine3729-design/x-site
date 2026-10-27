import { createFileRoute } from '@tanstack/react-router'
import { DashboardPage } from '@/features/analytics/dashboard-page'

export const Route = createFileRoute('/_authenticated/')({
  component: DashboardPage,
})
