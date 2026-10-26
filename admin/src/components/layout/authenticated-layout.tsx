import { Outlet } from '@tanstack/react-router'
import { getCookie } from '@/lib/cookies'
import { cn } from '@/lib/utils'
import { LayoutProvider } from '@/context/layout-provider'
import { SearchProvider } from '@/context/search-provider'
import { AccountSecurityDialog } from '@/features/auth/account-security-dialog'
import { ReauthProvider } from '@/features/auth/reauth-provider'
import { useAuthStore } from '@/stores/auth-store'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { AppSidebar } from '@/components/layout/app-sidebar'
import { PageHeader } from '@/components/layout/page-header'

type AuthenticatedLayoutProps = {
  children?: React.ReactNode
}

export function AuthenticatedLayout({ children }: AuthenticatedLayoutProps) {
  const defaultOpen = getCookie('sidebar_state') !== 'false'
  const user = useAuthStore((s) => s.user)

  // S2-6 切片 7：超级管理员必须开启二次验证。未开启时只给强制开启弹窗，不渲染会触发 403 的后台页面。
  if (user?.is_super && !user.mfa_enabled) {
    return (
      <main className='flex min-h-svh items-center justify-center bg-muted/40 p-4'>
        <AccountSecurityDialog open mandatory onOpenChange={() => undefined} />
      </main>
    )
  }

  return (
    <SearchProvider>
      <LayoutProvider>
        <SidebarProvider defaultOpen={defaultOpen}>
          <AppSidebar />
          <SidebarInset
            className={cn(
              // 设置内容容器，以便我们可以使用容器查询
              '@container/content',

              // 如果布局是固定的，请将高度设置为100svh以防止溢出
              'has-data-[layout=fixed]:h-svh',

              // 如果布局是固定的且侧边栏是内嵌的，
              // 请将高度设置为100svh - spacing (总边距) 以防止溢出
              'peer-data-[variant=inset]:has-data-[layout=fixed]:h-[calc(100svh-(var(--spacing)*4))]'
            )}
          >
            <PageHeader />
            <div className="flex flex-1 flex-col gap-4 p-4 pt-0">
              {children ?? <Outlet />}
            </div>
          </SidebarInset>
        </SidebarProvider>
        <ReauthProvider />
      </LayoutProvider>
    </SearchProvider>
  )
}
