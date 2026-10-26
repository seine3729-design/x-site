import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

/**
 * 徽标（feedback/docs/06 第 4 节）。
 *
 * 规则：文字始终可读，颜色只是辅助——状态、优先级、角色都不能只靠颜色表达
 * （WCAG 1.4.1）。所以这里没有「只有一个色点」的变体，每个徽标都带文字。
 */
const badgeVariants = cva(
  'inline-flex items-center gap-1.5 rounded-pill border px-2.5 py-0.5 text-2xs font-medium whitespace-nowrap [&_svg]:size-3 [&_svg]:shrink-0',
  {
    variants: {
      tone: {
        neutral: 'border-border bg-surface-raised text-fg-muted',
        outline: 'border-border bg-transparent text-fg-muted',
        brand: 'border-primary-border bg-primary-surface text-primary-fg',
        success: 'border-transparent bg-success-surface text-success-fg',
        warning: 'border-transparent bg-warning-surface text-warning-fg',
        danger: 'border-transparent bg-danger-surface text-danger-fg',
        info: 'border-transparent bg-info-surface text-info-fg',
        solid: 'border-transparent bg-primary text-fg-on-fill',
      },
      size: {
        sm: 'px-2 py-0.5 text-2xs',
        md: 'px-2.5 py-1 text-xs',
      },
    },
    defaultVariants: { tone: 'neutral', size: 'sm' },
  },
)

export type BadgeProps = React.ComponentProps<'span'> & VariantProps<typeof badgeVariants>

export function Badge({ className, tone, size, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ tone, size }), className)} {...props} />
}

export { badgeVariants }
