import { type ReactNode, useEffect, useState } from 'react'
import { ChevronRight, ExternalLink } from 'lucide-react'
import { Link, useLocation } from '@tanstack/react-router'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Badge,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  useSidebar,
} from '@/components/ui'
import {
  type NavCollapsible,
  type NavLink,
  type NavGroup as NavGroupProps,
} from './types'

/** 侧栏菜单：一级页面与外链直接跳转，一级目录可展开显示二级菜单；侧栏收起时目录改为弹出菜单。 */
export function NavGroup({ title, items }: NavGroupProps) {
  const { state, isMobile } = useSidebar()
  const pathname = useLocation({ select: (location) => location.pathname })
  return (
    <SidebarGroup>
      {title && <SidebarGroupLabel>{title}</SidebarGroupLabel>}
      <SidebarMenu>
        {items.map((item) => {
          const key = `${item.title}-${item.url ?? 'dir'}`

          if (!item.items)
            return <SidebarMenuLink key={key} item={item} pathname={pathname} />

          if (state === 'collapsed' && !isMobile)
            return (
              <SidebarMenuCollapsedDropdown key={key} item={item} pathname={pathname} />
            )

          return <SidebarMenuCollapsible key={key} item={item} pathname={pathname} />
        })}
      </SidebarMenu>
    </SidebarGroup>
  )
}

function NavBadge({ children }: { children: ReactNode }) {
  return <Badge className='rounded-full px-1 py-0 text-xs'>{children}</Badge>
}

/** 外链在新窗口打开，并标出外链图标（读屏读作「在新窗口打开」）。 */
function ExternalMark() {
  return <ExternalLink className='ms-auto size-3.5 text-muted-foreground' aria-label='在新窗口打开' />
}

function SidebarMenuLink({ item, pathname }: { item: NavLink; pathname: string }) {
  const { setOpenMobile } = useSidebar()
  if (item.external) {
    return (
      <SidebarMenuItem>
        <SidebarMenuButton asChild tooltip={item.title}>
          <a href={String(item.url)} target='_blank' rel='noopener noreferrer'>
            {item.icon && <item.icon />}
            {/* 名称后面还有外链图标，组件自带的省略样式只作用于最后一个 span，这里单独加 */}
            <span className='truncate'>{item.title}</span>
            <ExternalMark />
          </a>
        </SidebarMenuButton>
      </SidebarMenuItem>
    )
  }
  return (
    <SidebarMenuItem>
      <SidebarMenuButton asChild isActive={isActive(pathname, item.url)} tooltip={item.title}>
        <Link to={item.url} onClick={() => setOpenMobile(false)}>
          {item.icon && <item.icon />}
          <span>{item.title}</span>
          {item.badge && <NavBadge>{item.badge}</NavBadge>}
        </Link>
      </SidebarMenuButton>
    </SidebarMenuItem>
  )
}

/**
 * 用户展开着的目录（按标题记忆）。首次进入按需加载的页面时侧栏会重新挂载，
 * 记在模块里才能保持展开状态；刷新页面后只展开当前页面所在的目录。
 */
const openDirs = new Set<string>()

function SidebarMenuCollapsible({ item, pathname }: { item: NavCollapsible; pathname: string }) {
  const { setOpenMobile } = useSidebar()
  const active = containsActive(pathname, item)
  const [open, setOpen] = useState(() => active || openDirs.has(item.title))
  // 跳转到这个目录下的页面时（如从命令面板跳转）自动展开；在渲染期调整状态，不需要 effect
  const [wasActive, setWasActive] = useState(active)
  if (active !== wasActive) {
    setWasActive(active)
    if (active) setOpen(true)
  }
  useEffect(() => {
    if (open) openDirs.add(item.title)
    else openDirs.delete(item.title)
  }, [open, item.title])
  return (
    <Collapsible
      asChild
      open={open}
      onOpenChange={setOpen}
      className='group/collapsible'
    >
      <SidebarMenuItem>
        <CollapsibleTrigger asChild>
          <SidebarMenuButton tooltip={item.title}>
            {item.icon && <item.icon />}
            <span>{item.title}</span>
            {item.badge && <NavBadge>{item.badge}</NavBadge>}
            <ChevronRight className='ms-auto transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90 rtl:rotate-180' />
          </SidebarMenuButton>
        </CollapsibleTrigger>
        <CollapsibleContent className='CollapsibleContent'>
          <SidebarMenuSub>
            {item.items.map((sub) => (
              <SidebarMenuSubItem key={`${sub.title}-${sub.url}`}>
                {/* 名称超出侧栏宽度时省略显示，悬停可看全名 */}
                {sub.external ? (
                  <SidebarMenuSubButton asChild>
                    <a href={String(sub.url)} target='_blank' rel='noopener noreferrer' title={sub.title}>
                      {sub.icon && <sub.icon />}
                      <span className='truncate'>{sub.title}</span>
                      <ExternalMark />
                    </a>
                  </SidebarMenuSubButton>
                ) : (
                  <SidebarMenuSubButton asChild isActive={isActive(pathname, sub.url)}>
                    <Link to={sub.url} title={sub.title} onClick={() => setOpenMobile(false)}>
                      {sub.icon && <sub.icon />}
                      <span>{sub.title}</span>
                      {sub.badge && <NavBadge>{sub.badge}</NavBadge>}
                    </Link>
                  </SidebarMenuSubButton>
                )}
              </SidebarMenuSubItem>
            ))}
          </SidebarMenuSub>
        </CollapsibleContent>
      </SidebarMenuItem>
    </Collapsible>
  )
}

function SidebarMenuCollapsedDropdown({ item, pathname }: { item: NavCollapsible; pathname: string }) {
  return (
    <SidebarMenuItem>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <SidebarMenuButton tooltip={item.title} isActive={containsActive(pathname, item)}>
            {item.icon && <item.icon />}
            <span>{item.title}</span>
            {item.badge && <NavBadge>{item.badge}</NavBadge>}
            <ChevronRight className='ms-auto' />
          </SidebarMenuButton>
        </DropdownMenuTrigger>
        <DropdownMenuContent side='right' align='start' sideOffset={4}>
          <DropdownMenuLabel>
            {item.title} {item.badge ? `(${item.badge})` : ''}
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          {item.items.map((sub) => (
            <DropdownMenuItem key={`${sub.title}-${sub.url}`} asChild>
              {sub.external ? (
                <a href={String(sub.url)} target='_blank' rel='noopener noreferrer'>
                  {sub.icon && <sub.icon />}
                  <span className='max-w-52 text-wrap'>{sub.title}</span>
                  <ExternalMark />
                </a>
              ) : (
                <Link to={sub.url} className={isActive(pathname, sub.url) ? 'bg-secondary' : ''}>
                  {sub.icon && <sub.icon />}
                  <span className='max-w-52 text-wrap'>{sub.title}</span>
                  {sub.badge && <span className='ms-auto text-xs'>{sub.badge}</span>}
                </Link>
              )}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </SidebarMenuItem>
  )
}

const normalizePath = (p: string) => p.replace(/\/+$/, '') || '/'

/** 当前路径是否就是该菜单的地址（忽略末尾斜杠）。 */
function isActive(pathname: string, url: unknown) {
  return typeof url === 'string' && normalizePath(pathname) === normalizePath(url)
}

/** 当前页面是否在该目录下（用于默认展开所在目录）。 */
function containsActive(pathname: string, item: NavCollapsible) {
  return item.items.some((sub) => !sub.external && isActive(pathname, sub.url))
}
