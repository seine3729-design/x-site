import { useQuery } from '@tanstack/react-query'
import type { PageInfo } from '@/config/navigation'
import { resolveIcon } from '@/config/icons'
import { myMenusQuery, type NavNode } from '@/features/auth/api'
import { ModulePlaceholder } from '@/components/module-placeholder'

/** 在两级菜单中查找某个页面，返回菜单项与所属目录（一级页面没有目录）。 */
function findPage(nodes: NavNode[], code: string): { item: NavNode; dir?: NavNode } | null {
  for (const n of nodes) {
    if (n.kind === 'page' && n.code === code) return { item: n }
    const child = n.children.find((c) => c.kind === 'page' && c.code === code)
    if (child) return { item: child, dir: n }
  }
  return null
}

/** 用后台菜单中配置的名称与图标渲染某个内置页面的占位页；菜单被隐藏时用默认名称。 */
export function PlaceholderFor({ page }: { page: PageInfo }) {
  const { data: nodes = [] } = useQuery(myMenusQuery)
  const found = findPage(nodes, page.code)
  if (found) {
    return (
      <ModulePlaceholder page={page} title={found.item.title} icon={resolveIcon(found.item.icon)} groupTitle={found.dir?.title} />
    )
  }
  return <ModulePlaceholder page={page} title={page.title} icon={resolveIcon('')} />
}
