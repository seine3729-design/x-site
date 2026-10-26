import { ArrowDown, ArrowUp, ChevronsUpDown, RotateCcw } from 'lucide-react'
import { type Column } from '@tanstack/react-table'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

type DataTableColumnHeaderProps<TData, TValue> =
  React.HTMLAttributes<HTMLDivElement> & {
    column: Column<TData, TValue>
    title: string
  }

export function DataTableColumnHeader<TData, TValue>({
  column,
  title,
  className,
}: DataTableColumnHeaderProps<TData, TValue>) {
  if (!column.getCanSort()) {
    return <div className={cn(className)}>{title}</div>
  }

  return (
    <div className={cn('flex items-center space-x-2', className)}>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant='ghost'
            size='sm'
            className='h-8 p-0! focus-visible:ring-0 data-[state=open]:bg-accent'
          >
            <span>{title}</span>
            {column.getIsSorted() === 'desc' ? (
              <ArrowDown className='ms-2 h-4 w-4' />
            ) : column.getIsSorted() === 'asc' ? (
              <ArrowUp className='ms-2 h-4 w-4' />
            ) : (
              <ChevronsUpDown className='ms-2 h-4 w-4' />
            )}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align='start'>
          <DropdownMenuItem onClick={() => column.toggleSorting(false)}>
            <ArrowUp className='size-3.5 text-muted-foreground/70' />
            升序
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => column.toggleSorting(true)}>
            <ArrowDown className='size-3.5 text-muted-foreground/70' />
            降序
          </DropdownMenuItem>
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => column.clearSorting()}>
              <RotateCcw className='size-3.5 text-muted-foreground/70' />
              重置
            </DropdownMenuItem>
          </>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
