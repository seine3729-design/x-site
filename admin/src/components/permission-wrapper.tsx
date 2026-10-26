import { type ReactNode } from 'react'
import { useCan } from '@/stores/auth-store'

interface PermissionWrapperProps {
  /** 需要的权限点，如 dramas.edit */
  permission: string
  children: ReactNode
  fallback?: ReactNode
}

/** 只在当前管理员拥有该权限点时渲染子元素（仅显示控制，后端会再次校验）。 */
export function PermissionWrapper({ permission, children, fallback = null }: PermissionWrapperProps) {
  const can = useCan()
  return <>{can(permission) ? children : fallback}</>
}
