import { createFileRoute } from '@tanstack/react-router'
import { VIPPlansPage } from '@/features/billing/vip-plans-page'

export const Route = createFileRoute('/_authenticated/vip/plans')({
  component: VIPPlansPage,
})
