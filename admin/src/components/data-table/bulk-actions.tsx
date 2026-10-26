import { useState, useEffect, useRef } from 'react'
import { type Table } from '@tanstack/react-table'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

type DataTableBulkActionsProps<TData> = {
  table: Table<TData>
  entityName: string
  children: React.ReactNode
}

/**
 * 选中表格行时，用于显示批量操作的模块化工具栏
 *
 * @template TData 表格中的数据类型
 * @param {object} props 组件属性
 * @param {Table<TData>} props.table react-table 实例
 * @param {string} props.entityName 被操作的实体名称（例如："task", "user"）
 * @param {React.ReactNode} props.children 要在工具栏内渲染的操作按钮
 * @returns {React.ReactNode | null} 渲染的组件或在未选中行时返回 null
 */
export function DataTableBulkActions<TData>({
  table,
  entityName,
  children,
}: DataTableBulkActionsProps<TData>): React.ReactNode | null {
  const selectedRows = table.getFilteredSelectedRowModel().rows
  const selectedCount = selectedRows.length
  const toolbarRef = useRef<HTMLDivElement>(null)
  const [announcement, setAnnouncement] = useState('')

  // 向屏幕阅读器宣布选择更改
  useEffect(() => {
    if (selectedCount > 0) {
      const message = `${selectedCount} ${entityName}${selectedCount > 1 ? 's' : ''} selected. Bulk actions toolbar is available.`

      // 使用 queueMicrotask 来延迟状态更新并避免级联渲染。
      queueMicrotask(() => {
        setAnnouncement(message)
      })

      // 在延迟后清除公告
      const timer = setTimeout(() => setAnnouncement(''), 3000)
      return () => clearTimeout(timer)
    }
  }, [selectedCount, entityName])

  const handleClearSelection = () => {
    table.resetRowSelection()
  }

  const handleKeyDown = (event: React.KeyboardEvent) => {
    const buttons = toolbarRef.current?.querySelectorAll('button')
    if (!buttons) return

    const currentIndex = Array.from(buttons).findIndex(
      (button) => button === document.activeElement
    )

    switch (event.key) {
      case 'ArrowRight': {
        event.preventDefault()
        const nextIndex = (currentIndex + 1) % buttons.length
        buttons[nextIndex]?.focus()
        break
      }
      case 'ArrowLeft': {
        event.preventDefault()
        const prevIndex =
          currentIndex === 0 ? buttons.length - 1 : currentIndex - 1
        buttons[prevIndex]?.focus()
        break
      }
      case 'Home':
        event.preventDefault()
        buttons[0]?.focus()
        break
      case 'End':
        event.preventDefault()
        buttons[buttons.length - 1]?.focus()
        break
      case 'Escape': {
        // 检查 Escape 键是否来自下拉触发器或内容
        // 我们无法检查下拉状态，因为 Radix UI 在我们的处理程序运行前就会关闭它
        const target = event.target as HTMLElement
        const activeElement = document.activeElement as HTMLElement

        // 检查事件目标或当前聚焦元素是否是下拉触发器
        const isFromDropdownTrigger =
          target?.getAttribute('data-slot') === 'dropdown-menu-trigger' ||
          activeElement?.getAttribute('data-slot') ===
            'dropdown-menu-trigger' ||
          target?.closest('[data-slot="dropdown-menu-trigger"]') ||
          activeElement?.closest('[data-slot="dropdown-menu-trigger"]')

        // 检查聚焦元素是否在下拉内容内（这是 portaled 的）
        const isFromDropdownContent =
          activeElement?.closest('[data-slot="dropdown-menu-content"]') ||
          target?.closest('[data-slot="dropdown-menu-content"]')

        if (isFromDropdownTrigger || isFromDropdownContent) {
          // Esc键是用来打开下拉菜单的——不要清除选择。
          return
        }

        // Esc键是用来关闭工具栏的——清除选择。
        event.preventDefault()
        handleClearSelection()
        break
      }
    }
  }

  if (selectedCount === 0) {
    return null
  }

  return (
    <>
      {/* Live region for screen reader announcements */}
      <div
        aria-live='polite'
        aria-atomic='true'
        className='sr-only'
        role='status'
      >
        {announcement}
      </div>

      <div
        ref={toolbarRef}
        role='toolbar'
        aria-label={`Bulk actions for ${selectedCount} selected ${entityName}${selectedCount > 1 ? 's' : ''}`}
        aria-describedby='bulk-actions-description'
        tabIndex={-1}
        onKeyDown={handleKeyDown}
        className={cn(
          'fixed bottom-6 left-1/2 z-50 -translate-x-1/2 rounded-xl',
          'transition-all delay-100 duration-300 ease-out hover:scale-105',
          'focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none'
        )}
      >
        <div
          className={cn(
            'p-2 shadow-xl',
            'rounded-xl border',
            'bg-background/95 backdrop-blur-lg supports-backdrop-filter:bg-background/60',
            'flex items-center gap-x-2'
          )}
        >
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant='outline'
                size='icon'
                onClick={handleClearSelection}
                className='size-6 rounded-full'
                aria-label='Clear selection'
                title='Clear selection (Escape)'
              >
                <X />
                <span className='sr-only'>Clear selection</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent>
              <p>Clear selection (Escape)</p>
            </TooltipContent>
          </Tooltip>

          <Separator
            className='h-5'
            orientation='vertical'
            aria-hidden='true'
          />

          <div
            className='flex items-center gap-x-1 text-sm'
            id='bulk-actions-description'
          >
            <Badge
              variant='default'
              className='min-w-8 rounded-md'
              aria-label={`${selectedCount} selected`}
            >
              {selectedCount}
            </Badge>{' '}
            <span className='hidden sm:inline'>
              {entityName}
              {selectedCount > 1 ? 's' : ''}
            </span>{' '}
            selected
          </div>

          <Separator
            className='h-5'
            orientation='vertical'
            aria-hidden='true'
          />

          {children}
        </div>
      </div>
    </>
  )
}
