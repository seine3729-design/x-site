import { useEffect, useState, type ReactNode } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Check, ChevronsUpDown, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button, Popover, PopoverContent, PopoverTrigger } from '@/components/ui'
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command'

export type PickerOption<T = unknown> = {
  value: number
  label: string
  /** 第二行的补充信息，如 slug、编号、状态。 */
  hint?: string
  disabled?: boolean
  /** 原始记录，供选择后使用。 */
  data?: T
}

/** 输入停止 delay 毫秒后才更新，避免每个按键都请求接口。 */
export function useDebounced<T>(value: T, delay = 250): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay)
    return () => clearTimeout(timer)
  }, [value, delay])
  return debounced
}

/**
 * 按关键词搜索并选择记录（标签、专题、剧集）的下拉框。选项来自后端搜索接口。
 * - selected：已选中的值（显示对勾）；multiple 为 true 时选择后不关闭，便于连续添加。
 * - trigger：按钮上的文字（如当前选中项的名称或「搜索并勾选标签」）。
 */
export function SearchPicker<T = unknown>({
  queryKey,
  search,
  selected = [],
  onSelect,
  trigger,
  placeholder,
  emptyText = '没有找到',
  multiple = false,
  disabled,
  className,
  ariaLabel,
}: {
  queryKey: readonly unknown[]
  search: (q: string) => Promise<PickerOption<T>[]>
  selected?: readonly number[]
  onSelect: (option: PickerOption<T>) => void
  trigger: ReactNode
  placeholder: string
  emptyText?: string
  multiple?: boolean
  disabled?: boolean
  className?: string
  ariaLabel?: string
}) {
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const term = useDebounced(q.trim())
  const options = useQuery({
    queryKey: [...queryKey, 'picker', term],
    queryFn: () => search(term),
    enabled: open,
    staleTime: 30 * 1000,
    // 换关键词时先保留上一次的结果（变淡），避免列表闪成「搜索中…」
    placeholderData: keepPreviousData,
  })

  return (
    <Popover
      // 在弹窗内使用时，modal 让下拉列表可以滚动、点选（不被外层弹窗的滚动锁拦截）
      modal
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (!o) setQ('')
      }}
    >
      <PopoverTrigger asChild>
        <Button
          type='button'
          variant='outline'
          aria-haspopup='listbox'
          aria-expanded={open}
          aria-label={ariaLabel}
          disabled={disabled}
          className={cn('justify-between font-normal', className)}
        >
          <span className='truncate'>{trigger}</span>
          <ChevronsUpDown className='size-4 shrink-0 opacity-50' aria-hidden />
        </Button>
      </PopoverTrigger>
      <PopoverContent className='w-(--radix-popover-trigger-width) min-w-72 p-0' align='start'>
        <Command shouldFilter={false}>
          <CommandInput value={q} onValueChange={setQ} placeholder={placeholder} />
          <CommandList aria-busy={options.isFetching} className={cn(options.isPlaceholderData && 'opacity-60')}>
            {options.isFetching && !options.data ? (
              <div className='flex items-center justify-center gap-2 py-6 text-sm text-muted-foreground'>
                <Loader2 className='size-4 animate-spin' aria-hidden /> 搜索中…
              </div>
            ) : options.isError ? (
              <div className='py-6 text-center text-sm text-destructive'>加载失败，请重试</div>
            ) : (
              <>
                <CommandEmpty>{emptyText}</CommandEmpty>
                <CommandGroup>
                  {(options.data ?? []).map((o) => {
                    const checked = selected.includes(o.value)
                    return (
                      <CommandItem
                        key={o.value}
                        value={String(o.value)}
                        disabled={o.disabled}
                        // 多选时点选项不让搜索框失去焦点，可以接着输入下一个关键词
                        onMouseDown={multiple ? (e) => e.preventDefault() : undefined}
                        onSelect={() => {
                          onSelect(o)
                          if (!multiple) setOpen(false)
                        }}
                      >
                        <Check className={cn('size-4', checked ? 'opacity-100' : 'opacity-0')} aria-hidden />
                        <div className='flex min-w-0 flex-col'>
                          <span className='truncate'>{o.label}</span>
                          {o.hint && <span className='truncate text-xs text-muted-foreground'>{o.hint}</span>}
                        </div>
                      </CommandItem>
                    )
                  })}
                </CommandGroup>
              </>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
