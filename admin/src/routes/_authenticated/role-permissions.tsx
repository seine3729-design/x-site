import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { RolePermissionsPage } from '@/features/access/role-permissions-page'

// ?role=<角色 ID>：从「角色」页的「配置权限」直接打开对应角色
const searchSchema = z.object({
  role: z.coerce.number().int().positive().optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/role-permissions')({
  component: RolePermissionsPage,
  validateSearch: searchSchema,
})
