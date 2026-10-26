import { createFileRoute } from '@tanstack/react-router'
import { MenusPage } from '@/features/site/menus-page'

// 菜单与防丢地址都一次加载全部，没有筛选参数
export const Route = createFileRoute('/_authenticated/menus')({
  component: MenusPage,
})
