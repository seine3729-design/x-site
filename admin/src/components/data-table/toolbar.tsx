import { useState } from 'react'
import { type Table } from '@tanstack/react-table'
import { Search, RotateCcw, RefreshCw } from 'lucide-react'
import {
  Button,
  Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui'
import { DataTableFacetedFilter } from './faceted-filter'
import { DataTableSingleSelectFilter } from './single-select-filter'

type DataTableToolbarProps<TData> = {
  table: Table<TData>
  searchPlaceholder?: string
  searchKey?: string
  searchOptions?: { label: string; value: string }[]
  filters?: {
    columnId: string
    title: string
    type?: 'multi' | 'single'
    options: {
      label: string
      value: string
      icon?: React.ComponentType<{ className?: string }>
    }[]
  }[]
  onRefresh?: () => void
  serverSide?: boolean
  onSearchChange?: (searchKey: string, searchValue: string) => void
}

export function DataTableToolbar<TData>({
  table,
  searchPlaceholder = '搜索',
  searchKey,
  searchOptions,
  filters = [],
  onRefresh,
  serverSide = false,
  onSearchChange,
}: DataTableToolbarProps<TData>) {
  const [searchValue, setSearchValue] = useState('')
  const [currentSearchKey, setCurrentSearchKey] = useState(searchKey || (searchOptions?.[0]?.value ?? ''))

  return (
    <div className='flex items-center justify-between'>
      <div className='flex flex-1 flex-col-reverse items-start gap-y-2 sm:flex-row sm:items-center sm:space-x-2'>
        {searchOptions && searchOptions.length > 0 ? (
          <div className='flex items-center gap-2'>
            <Select
              value={currentSearchKey}
              onValueChange={setCurrentSearchKey}
            >
              <SelectTrigger className='h-10 w-[140px]' aria-label='搜索字段'>
                <SelectValue placeholder='选择搜索字段' />
              </SelectTrigger>
              <SelectContent>
                {searchOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              placeholder={searchPlaceholder}
              value={searchValue}
              onChange={(event) => setSearchValue(event.target.value)}
              className='h-10 w-[200px]'
            />
          </div>
        ) : searchKey ? (
          <Input
            placeholder={searchPlaceholder}
            value={
              serverSide
                ? searchValue
                : ((table.getColumn(searchKey)?.getFilterValue() as string) ??
                  '')
            }
            onChange={(event) => {
              if (serverSide) {
                setSearchValue(event.target.value)
              } else {
                table.getColumn(searchKey)?.setFilterValue(event.target.value)
              }
            }}
            className='h-10 w-[200px]'
          />
        ) : (
          <Input
            placeholder={searchPlaceholder}
            value={table.getState().globalFilter ?? ''}
            onChange={(event) => table.setGlobalFilter(event.target.value)}
            className='h-10 w-[200px]'
          />
        )}
        <div className='flex gap-x-2'>
          {filters.map((filter) => {
            const column = table.getColumn(filter.columnId)
            if (!column) return null

            const FilterComponent =
              filter.type === 'single'
                ? DataTableSingleSelectFilter
                : DataTableFacetedFilter

            return (
              <FilterComponent
                key={filter.columnId}
                column={column}
                title={filter.title}
                className='h-10 border-dashed'
                options={filter.options}
              />
            )
          })}
        </div>
        <div className='flex'>
          {serverSide && onSearchChange && (searchKey || searchOptions) && (
            <Button
              variant='default'
              onClick={() => {
                onSearchChange(currentSearchKey, searchValue)
              }}
              className='h-10 mx-2'
            >
              <Search className='h-4' />
              查询
            </Button>
          )}
          <Button
            variant='outline'
            onClick={() => {
              table.resetColumnFilters()
              table.setGlobalFilter('')
              if (serverSide) {
                setSearchValue('')
                if (onSearchChange) {
                  onSearchChange('__RESET__', '')
                }
              }
            }}
            className='h-10'
          >
            <RotateCcw className='h-4 w-4' />
            重置
          </Button>
        </div>
      </div>
      {onRefresh && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant='ghost' onClick={onRefresh} className='h-8'>
              <RefreshCw className='h-4 w-4' />
            </Button>
          </TooltipTrigger>
          <TooltipContent>刷新</TooltipContent>
        </Tooltip>
      )}
    </div>
  )
}
