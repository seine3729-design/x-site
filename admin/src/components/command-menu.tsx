import React from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { ArrowRight, ExternalLink, Laptop, Moon, Sun } from 'lucide-react'
import { useSearch } from '@/context/search-provider'
import { useTheme } from '@/context/theme-provider'
import { myMenusQuery, type NavNode } from '@/features/auth/api'
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from '@/components/ui/command'
import { ScrollArea } from './ui/scroll-area'

export function CommandMenu() {
  const navigate = useNavigate()
  const { setTheme } = useTheme()
  const { open, setOpen } = useSearch()
  const { data: nodes = [] } = useQuery(myMenusQuery)
  // 一级页面与外链放在「常用」，目录下的二级菜单按目录分组
  const topLevel = nodes.filter((n) => n.kind !== 'group')
  const directories = nodes.filter((n) => n.kind === 'group')

  const runCommand = React.useCallback(
    (command: () => unknown) => {
      setOpen(false)
      command()
    },
    [setOpen]
  )

  const item = (n: NavNode, groupTitle: string) => (
    <CommandItem
      key={n.code}
      value={`${groupTitle} ${n.title}`}
      onSelect={() => {
        runCommand(() =>
          n.kind === 'link' ? window.open(n.path, '_blank', 'noopener,noreferrer') : navigate({ to: n.path })
        )
      }}
    >
      <div className='flex size-4 items-center justify-center'>
        {n.kind === 'link' ? (
          <ExternalLink className='size-3 text-muted-foreground/80' />
        ) : (
          <ArrowRight className='size-2 text-muted-foreground/80' />
        )}
      </div>
      {n.title}
    </CommandItem>
  )

  return (
    <CommandDialog modal open={open} onOpenChange={setOpen}>
      <CommandInput placeholder='输入页面名称跳转，或切换主题…' />
      <CommandList>
        <ScrollArea type='hover' className='h-72 pe-1'>
          <CommandEmpty>未找到结果</CommandEmpty>
          {topLevel.length > 0 && <CommandGroup heading='常用'>{topLevel.map((n) => item(n, '常用'))}</CommandGroup>}
          {directories.map((dir) => (
            <CommandGroup key={dir.code} heading={dir.title}>
              {dir.children.map((n) => item(n, dir.title))}
            </CommandGroup>
          ))}
          <CommandSeparator />
          <CommandGroup heading='主题'>
            <CommandItem onSelect={() => runCommand(() => setTheme('light'))}>
              <Sun /> <span>浅色</span>
            </CommandItem>
            <CommandItem onSelect={() => runCommand(() => setTheme('dark'))}>
              <Moon className='scale-90' />
              <span>深色</span>
            </CommandItem>
            <CommandItem onSelect={() => runCommand(() => setTheme('system'))}>
              <Laptop />
              <span>系统</span>
            </CommandItem>
          </CommandGroup>
        </ScrollArea>
      </CommandList>
    </CommandDialog>
  )
}
