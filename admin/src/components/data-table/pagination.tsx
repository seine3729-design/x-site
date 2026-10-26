import { ChevronLeft, ChevronRight } from 'lucide-react'
import { type Table } from '@tanstack/react-table'
import { cn, getPageNumbers } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

type DataTablePaginationProps<TData> = {
  table: Table<TData>
  className?: string
}

export function DataTablePagination<TData>({
  table,
  className,
}: DataTablePaginationProps<TData>) {
  const currentPage = table.getState().pagination.pageIndex + 1
  const totalPages = table.getPageCount()
  const pageNumbers = getPageNumbers(currentPage, totalPages)

  return (
    <div
      className={cn(
        'flex items-center justify-between overflow-clip',
        '@max-2xl/content:flex-col-reverse @max-2xl/content:gap-4',
        className
      )}
      style={{ overflowClipMargin: 1 }}
    >
      <div className='flex w-full items-center justify-between'>
        <div className='flex items-center justify-center text-sm font-medium'>
          共 {totalPages} 页
        </div>
      </div>

      <div className='flex items-center space-x-2'>
        <div className='flex items-center space-x-2'>
          <Button
            variant='outline'
            className='h-9 w-9 p-0'
            onClick={() => table.previousPage()}
            disabled={!table.getCanPreviousPage()}
          >
            <span className='sr-only'>上一页</span>
            <ChevronLeft className='h-4 w-4' />
          </Button>

          {/* 分页按钮 */}
          {pageNumbers.map((pageNumber, index) => (
            <div key={`${pageNumber}-${index}`} className='flex items-center'>
              {pageNumber === '...' ? (
                <span className='px-1 text-sm text-muted-foreground'>...</span>
              ) : (
                <Button
                  variant={currentPage === pageNumber ? 'default' : 'outline'}
                  className='h-9 min-w-9 px-2'
                  onClick={() => table.setPageIndex((pageNumber as number) - 1)}
                >
                  <span className='sr-only'>前往第{pageNumber}页</span>
                  {pageNumber}
                </Button>
              )}
            </div>
          ))}

          <Button
            variant='outline'
            className='h-9 w-9 p-0'
            onClick={() => table.nextPage()}
            disabled={!table.getCanNextPage()}
          >
            <span className='sr-only'>下一页</span>
            <ChevronRight className='h-4 w-4' />
          </Button>
        </div>
        <div className='flex items-center gap-2'>
          <Select
          
            value={`${table.getState().pagination.pageSize}`}
            onValueChange={(value) => {
              table.setPageSize(Number(value))
            }}
          >
            <SelectTrigger className='h-9 w-[110px] cursor-pointer'>
              <SelectValue placeholder={table.getState().pagination.pageSize} />
            </SelectTrigger>
            <SelectContent side='top'>
              {[10, 20, 50, 100, 500].map((pageSize) => (
                <SelectItem key={pageSize} value={`${pageSize}`} className='cursor-pointer'>
                  {pageSize} 条/页
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
    </div>
  )
}
