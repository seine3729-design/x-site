import { createFileRoute } from '@tanstack/react-router'
import { ConfigPage } from '@/features/platform/config-page'

export const Route = createFileRoute('/_authenticated/config')({
  component: ConfigPage,
})
