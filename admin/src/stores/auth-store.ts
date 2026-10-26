import { create } from 'zustand'
import { setCsrfToken } from '@/lib/http'
import { can as canDo, type PermissionSubject } from '@/lib/permissions'

/** 当前管理员（来自 GET /api/admin/me）。不包含任何令牌。 */
export type AdminUser = {
  id: number
  username: string
  /** 角色编码 */
  role: string
  role_name: string
  is_super: boolean
  mfa_enabled: boolean
}

type AuthState = {
  user: AdminUser | null
  subject: PermissionSubject | null
  setSession: (user: AdminUser, csrfToken: string, permissions: string[]) => void
  reset: () => void
}

export const useAuthStore = create<AuthState>()((set) => ({
  user: null,
  subject: null,
  setSession: (user, csrfToken, permissions) => {
    setCsrfToken(csrfToken)
    set({ user, subject: { isSuper: user.is_super, permissions: new Set(permissions) } })
  },
  reset: () => {
    setCsrfToken(null)
    set({ user: null, subject: null })
  },
}))

/** 返回判断当前管理员是否拥有某权限点的函数（只用于显示控制）。 */
export function useCan(): (code: string) => boolean {
  const subject = useAuthStore((s) => s.subject)
  return (code: string) => canDo(subject, code)
}
