import { useState } from 'react'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { getPageNumbers } from '@/lib/utils'
import { Button, Input, Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui'

export const PAGE_SIZES = [10, 20, 50, 100] as const

/** 服务端页码分页：总条数、总页数、页码按钮、跳页与每页条数。 */
export function Pager({
  page,
  pageSize,
  total,
  onChange,
}: {
  page: number
  pageSize: number
  total: number
  onChange: (page: number, pageSize: number) => void
}) {
  const pages = Math.max(1, Math.ceil(total / pageSize))
  const [jump, setJump] = useState('')
  const go = (p: number) => onChange(Math.min(Math.max(1, p), pages), pageSize)

  return (
    <nav aria-label='分页' className='flex flex-wrap items-center justify-between gap-3'>
      <span className='text-sm text-muted-foreground tabular-nums'>
        共 {total} 条，第 {Math.min(page, pages)} / {pages} 页
      </span>
      <div className='flex flex-wrap items-center gap-2'>
        <Button variant='outline' size='icon' disabled={page <= 1} onClick={() => go(page - 1)} aria-label='上一页'>
          <ChevronLeft className='size-4' />
        </Button>
        {getPageNumbers(page, pages).map((n, i) =>
          n === '...' ? (
            <span key={`gap-${i}`} className='px-1 text-sm text-muted-foreground' aria-hidden>
              …
            </span>
          ) : (
            <Button
              key={n}
              variant={n === page ? 'default' : 'outline'}
              className='min-w-9 px-2 tabular-nums'
              aria-current={n === page ? 'page' : undefined}
              aria-label={`第 ${n} 页`}
              onClick={() => go(n as number)}
            >
              {n}
            </Button>
          )
        )}
        <Button variant='outline' size='icon' disabled={page >= pages} onClick={() => go(page + 1)} aria-label='下一页'>
          <ChevronRight className='size-4' />
        </Button>
        <form
          className='flex items-center gap-1 text-sm'
          onSubmit={(e) => {
            e.preventDefault()
            const n = Number(jump)
            if (Number.isInteger(n) && n > 0) go(n)
            setJump('')
          }}
        >
          <label htmlFor='pager-jump' className='text-muted-foreground'>
            跳至
          </label>
          <Input
            id='pager-jump'
            inputMode='numeric'
            className='h-9 w-16 tabular-nums'
            placeholder='页码'
            value={jump}
            onChange={(e) => setJump(e.target.value.replace(/\D/g, ''))}
          />
          <span className='text-muted-foreground'>页</span>
        </form>
        <Select value={String(pageSize)} onValueChange={(v) => onChange(1, Number(v))}>
          <SelectTrigger className='h-9 w-27.5' aria-label='每页条数'>
            <SelectValue placeholder='每页条数' />
          </SelectTrigger>
          <SelectContent>
            {PAGE_SIZES.map((s) => (
              <SelectItem key={s} value={String(s)}>
                {s} 条/页
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </nav>
  )
}
