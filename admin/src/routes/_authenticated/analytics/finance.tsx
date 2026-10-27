import { createFileRoute } from '@tanstack/react-router'
import { FinancePage } from '@/features/analytics/finance-page'

export const Route = createFileRoute('/_authenticated/analytics/finance')({
  component: FinancePage,
})
