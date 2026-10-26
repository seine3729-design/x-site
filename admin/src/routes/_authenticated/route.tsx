import { createFileRoute, redirect } from '@tanstack/react-router'
import { meQuery } from '@/features/auth/api'
import { HttpError } from '@/lib/http'
import { useAuthStore } from '@/stores/auth-store'
import { AuthenticatedLayout } from '@/components/layout/authenticated-layout'

export const Route = createFileRoute('/_authenticated')({
  // 进入任何后台页面前确认会话；未登录跳转登录页并记录原地址。
  // 缓存过期（5 分钟）后先用缓存渲染，同时后台重取：会话已失效时由全局错误处理跳转登录页。
  beforeLoad: async ({ context, location }) => {
    try {
      const session = await context.queryClient.ensureQueryData({ ...meQuery, revalidateIfStale: true })
      useAuthStore.getState().setSession(session.user, session.csrf_token, session.permissions)
    } catch (error) {
      if (error instanceof HttpError && error.status !== 0 && error.status < 500) {
        useAuthStore.getState().reset()
        throw redirect({ to: '/login', search: { redirect: location.href } })
      }
      throw error
    }
  },
  component: AuthenticatedLayout,
})
