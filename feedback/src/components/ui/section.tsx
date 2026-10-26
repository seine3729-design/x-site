import Link from 'next/link'
import { ArrowRight } from 'lucide-react'
import { cn } from '@/lib/utils'

/**
 * 页面壳：桌面最大宽度 1200px，两侧留白随断点变宽
 * （feedback/docs/06 第 3.1 节、`ui-ux-pro-max` 的 adaptive gutters）。
 */
export function Container({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      className={cn('mx-auto w-full max-w-shell px-4 sm:px-6 lg:px-8', className)}
      {...props}
    />
  )
}

/** 区块标题 + 可选的「查看更多」。标题用 h2，保持页面标题层级连续。 */
export function SectionHeading({
  title,
  hint,
  moreHref,
  moreLabel = '查看更多',
  className,
}: {
  title: string
  hint?: string
  moreHref?: string
  moreLabel?: string
  className?: string
}) {
  return (
    <div className={cn('mb-4', className)}>
      {/*
        标题与「查看更多」始终同一行、基线对齐：
        把它们放进同一个 flex 再让说明文字独占一行，窄屏下说明换行时
        「查看更多」不会被挤到说明文字的右下角。
      */}
      <div className="flex items-baseline justify-between gap-4">
        <h2 className="min-w-0 text-lg font-semibold tracking-tight text-fg sm:text-xl">
          {title}
        </h2>
        {moreHref && (
          <Link
            href={moreHref}
            className="group inline-flex shrink-0 items-center gap-1 rounded-control text-sm font-medium text-primary-fg hover:underline"
          >
            {moreLabel}
            <ArrowRight
              className="size-4 transition-transform duration-(--duration-hover) group-hover:translate-x-0.5"
              aria-hidden
            />
          </Link>
        )}
      </div>
      {hint && <p className="mt-1 max-w-prose text-sm text-fg-muted">{hint}</p>}
    </div>
  )
}
