import Link from 'next/link'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { cn } from '@/lib/utils'
import { t } from '@/lib/i18n'

/**
 * 分页。
 *
 * 用链接而不是按钮：页码写在 URL 里，刷新、分享、浏览器前后退都能回到同一页
 * （feedback/docs/06 第 10 节：筛选与页码是页面状态的权威源）。
 * 不可用的方向渲染成 span，而不是 disabled 的链接——键盘不会停在一个点不动的东西上。
 */
export function Pagination({
  page,
  pageSize,
  total,
  hrefFor,
  className,
}: {
  page: number
  pageSize: number
  total: number
  /** 给定页码返回地址，调用方负责保留其他筛选参数。 */
  hrefFor: (page: number) => string
  className?: string
}) {
  const pages = Math.max(1, Math.ceil(total / Math.max(pageSize, 1)))
  if (pages <= 1) return null

  const base =
    'inline-flex h-10 items-center gap-1 rounded-control border border-border px-3 text-sm'
  const enabled = 'cursor-pointer text-fg hover:border-control-border hover:bg-surface-raised'
  const disabled = 'text-fg-subtle'

  return (
    <nav className={cn('flex items-center justify-between gap-4', className)} aria-label="分页">
      {page > 1 ? (
        <Link href={hrefFor(page - 1)} className={cn(base, enabled)} rel="prev">
          <ChevronLeft className="size-4" aria-hidden />
          {t('pager.prev')}
        </Link>
      ) : (
        <span className={cn(base, disabled)} aria-hidden>
          <ChevronLeft className="size-4" />
          {t('pager.prev')}
        </span>
      )}

      <p className="text-sm text-fg-muted tabular" aria-live="polite">
        {t('pager.status', { page, total: pages })}
        <span className="ms-2 text-fg-subtle">{t('pager.total', { count: total })}</span>
      </p>

      {page < pages ? (
        <Link href={hrefFor(page + 1)} className={cn(base, enabled)} rel="next">
          {t('pager.next')}
          <ChevronRight className="size-4" aria-hidden />
        </Link>
      ) : (
        <span className={cn(base, disabled)} aria-hidden>
          {t('pager.next')}
          <ChevronRight className="size-4" />
        </span>
      )}
    </nav>
  )
}
