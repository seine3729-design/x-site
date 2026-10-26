'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { cn } from '@/lib/utils'
import { isActive, NAV_ITEMS } from './nav-items'

/**
 * 移动端底部导航（feedback/docs/06 第 6 节）。
 *
 * - 5 项，图标必须配文字（只有图标会伤可发现性）；
 * - 每项高 56px，达到 44pt 触控标准还留出余量；
 * - 底部补 safe-area，iPhone 的手势条不会压住最后一项；
 * - 页面内容由 SiteShell 预留等高的底部内边距，不会被这条栏遮住。
 */
export function BottomNav() {
  const pathname = usePathname()
  return (
    <nav
      aria-label="主导航"
      className="fixed inset-x-0 bottom-0 z-40 border-t border-border bg-surface/95 backdrop-blur-sm lg:hidden"
      style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
    >
      <ul className="grid grid-cols-5">
        {NAV_ITEMS.map((item) => {
          const active = isActive(pathname, item.href)
          const Icon = item.icon
          return (
            <li key={item.href}>
              <Link
                href={item.href}
                aria-current={active ? 'page' : undefined}
                className={cn(
                  'flex h-14 flex-col items-center justify-center gap-1 text-2xs transition-colors duration-(--duration-hover)',
                  active ? 'font-medium text-primary-fg' : 'text-fg-muted',
                )}
              >
                <Icon className="size-5" aria-hidden />
                {item.label}
              </Link>
            </li>
          )
        })}
      </ul>
    </nav>
  )
}
