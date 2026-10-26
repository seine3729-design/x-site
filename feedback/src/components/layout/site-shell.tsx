import { t } from '@/lib/i18n'
import { BottomNav } from './bottom-nav'
import { SiteFooter } from './site-footer'
import { SiteHeader } from './site-header'

/**
 * 页面壳。
 *
 * 两处容易出错的细节固定在这里，不留给各页面自己处理：
 * - 「跳到主要内容」：键盘用户第一个 Tab 就能跳过顶栏（WCAG 2.4.1）；
 * - 底部导航是 fixed 的，所以最外层预留等高内边距 + safe-area，
 *   保证页面最后一个元素（表单提交按钮、最后一条评论）不会被压住。
 */
export function SiteShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-dvh flex-col pb-[calc(3.5rem+env(safe-area-inset-bottom))] lg:pb-0">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:fixed focus:start-4 focus:top-4 focus:z-50 focus:rounded-control focus:bg-surface focus:px-4 focus:py-2 focus:text-sm focus:font-medium focus:shadow-popover focus:outline-2 focus:outline-ring"
      >
        {t('nav.skipToContent')}
      </a>
      <SiteHeader />
      {/* tabIndex -1：路由切换后把焦点移到主内容，屏幕阅读器能读到新页面 */}
      <main id="main" tabIndex={-1} className="flex-1 outline-none">
        {children}
      </main>
      <SiteFooter />
      <BottomNav />
    </div>
  )
}
