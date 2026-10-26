import { type LinkProps } from '@tanstack/react-router'

type BaseNavItem = {
  title: string
  badge?: string
  icon?: React.ElementType
  /** 外部链接：新窗口打开，不走前端路由 */
  external?: boolean
}

type NavUrl = LinkProps['to'] | (string & {})

/** 一级页面或外链：直接跳转。 */
type NavLink = BaseNavItem & {
  url: NavUrl
  items?: never
}

/** 一级目录：可展开，items 为二级菜单（页面或外链）。 */
type NavCollapsible = BaseNavItem & {
  items: (BaseNavItem & { url: NavUrl })[]
  url?: never
}

type NavItem = NavCollapsible | NavLink

type NavGroup = {
  /** 侧栏分组标题；为空时不显示标题行 */
  title?: string
  items: NavItem[]
}

export type { NavGroup, NavItem, NavCollapsible, NavLink }
