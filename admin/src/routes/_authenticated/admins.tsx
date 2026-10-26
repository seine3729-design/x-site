import { createFileRoute } from '@tanstack/react-router'
import { AdminsPage } from '@/features/access/admins-page'

export const Route = createFileRoute('/_authenticated/admins')({
  component: AdminsPage,
})
