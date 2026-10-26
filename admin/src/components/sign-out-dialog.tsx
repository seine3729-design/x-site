import { useQueryClient } from '@tanstack/react-query'
import { useLocation, useNavigate } from '@tanstack/react-router'
import { authApi } from '@/features/auth/api'
import { useAuthStore } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'

interface SignOutDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function SignOutDialog({ open, onOpenChange }: SignOutDialogProps) {
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  const reset = useAuthStore((s) => s.reset)

  const handleSignOut = async () => {
    // 无论后端是否成功吊销会话，本地都清除状态并回到登录页
    await authApi.logout().catch(() => undefined)
    reset()
    queryClient.clear()
    await navigate({ to: '/login', search: { redirect: location.href }, replace: true })
  }

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title='退出登录'
      desc='确定要退出登录吗？'
      cancelBtnText='取消'
      confirmText='退出登录'
      destructive
      handleConfirm={handleSignOut}
      className='sm:max-w-sm'
    />
  )
}
