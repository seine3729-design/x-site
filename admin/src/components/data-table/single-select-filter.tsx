import * as React from 'react'
import { Check, CirclePlus } from 'lucide-react'
import { type Column } from '@tanstack/react-table'
import { Badge, Button, Separator, Popover, PopoverContent, PopoverTrigger } from '@/components/ui'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from '@/components/ui/command'

type DataTableSingleSelectFilterProps<TData, TValue> = {
  column?: Column<TData, TValue>
  title?: string
  className?: string
  options: {
    label: string
    value: string
    icon?: React.ComponentType<{ className?: string }>
  }[]
}

export function DataTableSingleSelectFilter<TData, TValue>({
  column,
  title,
  className,
  options,
}: DataTableSingleSelectFilterProps<TData, TValue>) {
  const selectedValue = column?.getFilterValue() as string | undefined

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant='outline' size='sm' className={className || 'h-9 border-dashed'}>
          <CirclePlus className='size-4' />
          {title}
          {selectedValue && (
            <>
              <Separator orientation='vertical' className='mx-2 h-4' />
              <Badge
                variant='secondary'
                className='rounded-sm px-1 font-normal'
              >
                {options.find((opt) => opt.value === selectedValue)?.label}
              </Badge>
            </>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent className='w-50 p-0' align='start'>
        <Command>
          <CommandInput placeholder={`搜索${title ?? ''}`} />
          <CommandList>
            <CommandEmpty>暂无数据</CommandEmpty>
            <CommandGroup>
              {options.map((option) => {
                const isSelected = selectedValue === option.value
                return (
                  <CommandItem
                    key={option.value}
                    className='cursor-pointer'
                    onSelect={() => {
                      column?.setFilterValue(
                        isSelected ? undefined : option.value
                      )
                    }}
                  >
                    {option.icon && (
                      <option.icon className='size-4 text-muted-foreground' />
                    )}
                    <span>{option.label}</span>
                    {isSelected && (
                      <Check className='ms-auto h-4 w-4' />
                    )}
                  </CommandItem>
                )
              })}
            </CommandGroup>
            {selectedValue && (
              <>
                <CommandSeparator />
                <CommandGroup>
                  <CommandItem
                    onSelect={() => column?.setFilterValue(undefined)}
                    className='justify-center text-center'
                  >
                    清除筛选
                  </CommandItem>
                </CommandGroup>
              </>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
