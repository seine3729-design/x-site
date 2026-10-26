import { CircleUser, House, Inbox, LayoutGrid, MessagesSquare, type LucideIcon } from 'lucide-react'
import { t } from '@/lib/i18n'

/**
 * 一级导航，桌面放顶栏、移动端放底栏，两处用同一份定义
 * （feedback/docs/02 第 1 节：一级导航不超过 5 项，移动端保留文字标签）。
 *
 * 「提交反馈」不占一级导航，它是页面内的主要动作。
 */
export type NavItem = {
  href: string
  label: string
  icon: LucideIcon
  /** 需要登录才有意义的入口：未登录时点进去会落到登录页。 */
  auth?: boolean
}

export const NAV_ITEMS: NavItem[] = [
  { href: '/', label: t('nav.home'), icon: House },
  { href: '/projects', label: t('nav.projects'), icon: LayoutGrid },
  { href: '/feedback', label: t('nav.feedback'), icon: Inbox },
  { href: '/topics', label: t('nav.topics'), icon: MessagesSquare },
  { href: '/me', label: t('nav.me'), icon: CircleUser, auth: true },
]

/** 当前路径是否落在某个导航项下。首页只在完全相等时才算，避免它永远高亮。 */
export function isActive(pathname: string, href: string): boolean {
  if (href === '/') return pathname === '/'
  return pathname === href || pathname.startsWith(`${href}/`)
}
