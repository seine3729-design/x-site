import { createFileRoute } from '@tanstack/react-router'
import { RolesPage } from '@/features/access/roles-page'

export const Route = createFileRoute('/_authenticated/roles')({
  component: RolesPage,
})
