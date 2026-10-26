import { useQuery } from '@tanstack/react-query'
import { BRAND } from '@/config/navigation'
import { menuIcon } from '@/config/icons'
import { myMenusQuery, type NavNode } from '@/features/auth/api'
import { useLayout } from '@/context/layout-provider'
import { Button } from '@/components/ui/button'
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSkeleton,
  SidebarRail,
} from '@/components/ui/sidebar'
import { NavGroup } from './nav-group'
import type { NavItem } from './types'

/**
 * 把接口返回的两级菜单转换为侧栏组件的数据结构：目录可展开，页面与外链直接跳转。
 * 一级、二级菜单都带图标，未设置图标的按类型显示默认图标。
 */
export function toNavItems(nodes: NavNode[]): NavItem[] {
  return nodes.map((n) =>
    n.kind === 'group'
      ? {
          title: n.title,
          icon: menuIcon(n.kind, n.icon),
          items: n.children.map((c) => ({
            title: c.title,
            url: c.path,
            icon: menuIcon(c.kind, c.icon),
            external: c.kind === 'link',
          })),
        }
      : { title: n.title, url: n.path, icon: menuIcon(n.kind, n.icon), external: n.kind === 'link' }
  )
}

export function AppSidebar() {
  const { collapsible, variant } = useLayout()
  const BrandIcon = BRAND.icon
  const menus = useQuery(myMenusQuery)

  return (
    <Sidebar collapsible={collapsible} variant={variant}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size='lg' className='pointer-events-none'>
              <div className='flex aspect-square size-8 items-center justify-center rounded-md bg-sidebar-primary text-sidebar-primary-foreground'>
                <BrandIcon className='size-4' aria-hidden />
              </div>
              <span className='truncate font-semibold'>{BRAND.name}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        {menus.isPending ? (
          <SidebarGroup aria-busy='true' aria-label='菜单加载中'>
            <SidebarMenu>
              {Array.from({ length: 8 }, (_, i) => (
                <SidebarMenuItem key={i}>
                  <SidebarMenuSkeleton showIcon />
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroup>
        ) : menus.isError ? (
          <SidebarGroup>
            <p className='px-2 text-sm text-muted-foreground'>菜单加载失败</p>
            <Button variant='outline' size='sm' className='m-2' onClick={() => void menus.refetch()}>
              重试
            </Button>
          </SidebarGroup>
        ) : (
          <NavGroup items={toNavItems(menus.data)} />
        )}
      </SidebarContent>
      <SidebarRail />
    </Sidebar>
  )
}
