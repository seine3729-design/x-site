import { createFileRoute } from '@tanstack/react-router'
import { AdminMenusPage } from '@/features/access/admin-menus-page'

export const Route = createFileRoute('/_authenticated/admin-menus')({
  component: AdminMenusPage,
})
