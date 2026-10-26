import { useState } from 'react'
import {
  type ColumnDef,
  type ColumnFiltersState,
  type PaginationState,
  type SortingState,
  type VisibilityState,
  flexRender,
  getCoreRowModel,
  getFacetedRowModel,
  getFacetedUniqueValues,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { cn } from '@/lib/utils'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { DataTablePagination, DataTableToolbar } from '@/components/data-table'

export type FilterOption = {
  label: string
  value: string
  icon?: React.ComponentType<{ className?: string }>
}

export type FilterConfig = {
  columnId: string
  title: string
  type?: 'multi' | 'single'
  options: FilterOption[]
}

export type ColumnFilterConfig = {
  columnId: string
  searchKey: string
  type: 'string' | 'array'
}

type DataTableProps<TData> = {
  data: TData[]
  columns: ColumnDef<TData, unknown>[]
  searchPlaceholder?: string
  searchKey?: string
  searchOptions?: { label: string; value: string }[]
  filters?: FilterConfig[]
  enableRowSelection?: boolean
  defaultPageSize?: number
  defaultPage?: number
  renderBulkActions?: (
    table: ReturnType<typeof useReactTable<TData>>
  ) => React.ReactNode
  className?: string
  tableClassName?: string
  onRefresh?: () => void
  meta?: Record<string, unknown>
  serverSide?: boolean
  totalCount?: number
  loading?: boolean
  hidePagination?: boolean
  onPaginationChange?: (pageNum: number, pageSize: number) => void
  onFilterChange?: (filters: ColumnFiltersState) => void
  onSearchChange?: (searchKey: string, searchValue: string) => void
}

export function DataTable<TData>({
  data,
  columns,
  searchPlaceholder = '搜索...',
  searchKey,
  searchOptions,
  filters = [],
  enableRowSelection = true,
  defaultPageSize = 10,
  defaultPage = 1,
  renderBulkActions,
  className,
  tableClassName,
  onRefresh,
  meta,
  serverSide = false,
  totalCount,
  loading = false,
  hidePagination = false,
  onPaginationChange,
  onFilterChange,
  onSearchChange,
}: DataTableProps<TData>) {
  const [rowSelection, setRowSelection] = useState({})
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})
  const [sorting, setSorting] = useState<SortingState>([])
  const [localColumnFilters, setLocalColumnFilters] =
    useState<ColumnFiltersState>([])
  const [localPagination, setLocalPagination] = useState<PaginationState>({
    pageIndex: defaultPage - 1,
    pageSize: defaultPageSize,
  })

  // 使用本地状态进行筛选和分页
  const columnFiltersState = localColumnFilters
  const onColumnFiltersChange = (updater: any) => {
    setLocalColumnFilters(updater)
    if (serverSide && onFilterChange) {
      const newFilters =
        typeof updater === 'function' ? updater(localColumnFilters) : updater
      onFilterChange(newFilters)
    }
  }
  const paginationState = localPagination
  const onPaginationChangeHandler = (updater: any) => {
    const newPagination =
      typeof updater === 'function' ? updater(localPagination) : updater
    setLocalPagination(newPagination)
    if (serverSide && onPaginationChange) {
      onPaginationChange(newPagination.pageIndex + 1, newPagination.pageSize)
    }
  }

  const table = useReactTable({
    data,
    columns,
    state: {
      sorting,
      columnVisibility,
      rowSelection,
      columnFilters: columnFiltersState,
      pagination: paginationState,
    },
    enableRowSelection,
    onRowSelectionChange: setRowSelection,
    onSortingChange: setSorting,
    onColumnFiltersChange: onColumnFiltersChange,
    onColumnVisibilityChange: setColumnVisibility,
    onPaginationChange: onPaginationChangeHandler,
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: serverSide ? undefined : getFilteredRowModel(),
    getPaginationRowModel: serverSide ? undefined : getPaginationRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFacetedRowModel: getFacetedRowModel(),
    getFacetedUniqueValues: getFacetedUniqueValues(),
    manualPagination: serverSide,
    manualFiltering: serverSide,
    pageCount:
      serverSide && totalCount
        ? Math.ceil(totalCount / paginationState.pageSize)
        : undefined,
    meta,
  })

  return (
    <div
      className={cn(
        'flex flex-col gap-4',
        'max-sm:has-[div[role="toolbar"]]:mb-16',
        className
      )}
    >
      {searchKey || searchOptions || filters.length > 0 ? (
        <DataTableToolbar
          table={table}
          searchPlaceholder={searchPlaceholder}
          searchKey={searchKey}
          searchOptions={searchOptions}
          filters={filters}
          onRefresh={onRefresh}
          serverSide={serverSide}
          onSearchChange={onSearchChange}
        />
      ) : null}
      <div className='overflow-hidden rounded-md border'>
        <Table className={tableClassName}>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id} className='group/row'>
                {headerGroup.headers.map((header) => (
                  <TableHead
                    key={header.id}
                    colSpan={header.colSpan}
                    className={cn(
                      'bg-muted',
                      header.index === 0 && 'pl-6',
                      header.index === headerGroup.headers.length - 1 && 'pr-6',
                      header.column.columnDef.meta?.className,
                      header.column.columnDef.meta?.thClassName
                    )}
                  >
                    {header.isPlaceholder
                      ? null
                      : flexRender(
                          header.column.columnDef.header,
                          header.getContext()
                        )}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell
                  colSpan={columns.length}
                  className='h-48 text-center'
                >
                  <div className="loader h-48 text-center"></div>
                </TableCell>
              </TableRow>
            ) : table.getRowModel().rows.length ? (
              table.getRowModel().rows.map((row) => (
                <TableRow
                  key={row.id}
                  data-state={row.getIsSelected() && 'selected'}
                  className='group/row'
                >
                  {row.getVisibleCells().map((cell, cellIndex) => (
                    <TableCell
                      key={cell.id}
                      className={cn(
                        'bg-background group-hover/row:bg-muted group-data-[state=selected]/row:bg-muted',
                        cellIndex === 0 && 'pl-6',
                        cellIndex === row.getVisibleCells().length - 1 &&
                          'pr-6',
                        cell.column.columnDef.meta?.className,
                        cell.column.columnDef.meta?.tdClassName
                      )}
                    >
                      {flexRender(
                        cell.column.columnDef.cell,
                        cell.getContext()
                      )}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell
                  colSpan={columns.length}
                  className='h-24 text-center'
                >
                  暂无数据
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      {!hidePagination && <DataTablePagination table={table} />}
      {renderBulkActions && renderBulkActions(table)}
    </div>
  )
}
