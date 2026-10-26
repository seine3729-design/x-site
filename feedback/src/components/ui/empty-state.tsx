import type { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

/**
 * 空状态（feedback/docs/06 第 4 节）。
 *
 * 要求：解释为什么空，并且只给一个主要的下一步。
 * 不伪造社交证明，不显示虚构数量。图标是装饰性的，对无障碍树隐藏。
 */
export function EmptyState({
  icon: Icon,
  title,
  hint,
  action,
  className,
}: {
  icon?: LucideIcon
  title: string
  hint?: string
  action?: React.ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        'flex flex-col items-center gap-3 rounded-surface border border-dashed border-border px-6 py-12 text-center',
        className,
      )}
    >
      {Icon && (
        <span className="flex size-11 items-center justify-center rounded-pill bg-surface-raised text-fg-subtle">
          <Icon className="size-5" aria-hidden />
        </span>
      )}
      <p className="text-sm font-medium text-fg">{title}</p>
      {hint && <p className="max-w-sm text-sm text-fg-muted">{hint}</p>}
      {action && <div className="mt-1">{action}</div>}
    </div>
  )
}
