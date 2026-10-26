import { StrictMode } from 'react'
import { QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRouter, RouterProvider } from '@tanstack/react-router'
import ReactDOM from 'react-dom/client'
import { toast } from 'sonner'
import { errorMessage, HttpError } from '@/lib/http'
import { useAuthStore } from '@/stores/auth-store'
import { DirectionProvider } from './context/direction-provider'
import { FontProvider } from './context/font-provider'
import { ThemeProvider } from './context/theme-provider'
// 生成的路由
import { routeTree } from './routeTree.gen'
// 样式
import './styles/index.css'

/** 会话失效（读或写请求返回 401）：清空本地状态，跳登录页并记住当前地址。 */
function toLogin() {
  useAuthStore.getState().reset()
  queryClient.removeQueries({ queryKey: ['auth'] })
  void router.navigate({
    to: '/login',
    search: { redirect: router.history.location.href },
  })
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 4xx（含未登录、无权限、不存在）不重试；网络错误与 5xx 最多重试 2 次
      retry: (failureCount, error) =>
        !(error instanceof HttpError && error.status >= 400 && error.status < 500) &&
        failureCount < 2,
      refetchOnWindowFocus: import.meta.env.PROD,
      staleTime: 30 * 1000,
    },
    mutations: {
      onError: (error) => {
        if (error instanceof HttpError && error.isUnauthorized) {
          toast.error('登录已失效，请重新登录')
          toLogin()
          return
        }
        toast.error(errorMessage(error))
      },
    },
  },
  queryCache: new QueryCache({
    onError: (error) => {
      if (!(error instanceof HttpError)) return
      if (error.isUnauthorized) {
        toLogin()
        return
      }
      if (error.status === 403) {
        void router.navigate({ to: '/403', replace: true })
      }
    },
  }),
})

const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPreload: 'intent',
  defaultPreloadStaleTime: 0,
})

// 注册路由器实例以确保类型安全
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

const rootElement = document.getElementById('root')!
if (!rootElement.innerHTML) {
  ReactDOM.createRoot(rootElement).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <ThemeProvider>
          <FontProvider>
            <DirectionProvider>
              <RouterProvider router={router} />
            </DirectionProvider>
          </FontProvider>
        </ThemeProvider>
      </QueryClientProvider>
    </StrictMode>
  )
}
