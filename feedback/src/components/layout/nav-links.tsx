'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { cn } from '@/lib/utils'
import { isActive, NAV_ITEMS } from './nav-items'

/**
 * 桌面顶栏导航。
 *
 * 当前位置用「文字加粗 + 底部指示条」表示，不只靠颜色（WCAG 1.4.1），
 * 并且带 aria-current 让屏幕阅读器也知道在哪一页。
 */
export function NavLinks() {
  const pathname = usePathname()
  return (
    <nav aria-label="主导航" className="hidden items-center gap-1 lg:flex">
      {NAV_ITEMS.map((item) => {
        const active = isActive(pathname, item.href)
        return (
          <Link
            key={item.href}
            href={item.href}
            aria-current={active ? 'page' : undefined}
            className={cn(
              'relative rounded-control px-3 py-2 text-sm transition-colors duration-(--duration-hover)',
              active
                ? 'font-semibold text-fg'
                : 'text-fg-muted hover:bg-surface-raised hover:text-fg',
            )}
          >
            {item.label}
            {active && (
              <span
                className="absolute inset-x-3 -bottom-px h-0.5 rounded-pill bg-primary"
                aria-hidden
              />
            )}
          </Link>
        )
      })}
    </nav>
  )
}
