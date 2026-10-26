import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

/**
 * 卡片。
 *
 * 分层靠背景与边框，不靠阴影堆叠（feedback/docs/06 第 2.4 节）。
 * `interactive` 用于整卡可点击的场景：hover 只改边框与背景，不做位移，
 * 避免把旁边内容挤动，也避免在触屏上出现「按下才看到变化」的割裂感。
 */
const cardVariants = cva('rounded-surface border bg-surface', {
  variants: {
    interactive: {
      true: 'transition-colors duration-(--duration-hover) ease-(--ease-out-soft) hover:border-primary-border hover:bg-surface-raised focus-within:border-primary-border',
      false: '',
    },
    padding: {
      none: '',
      sm: 'p-4',
      md: 'p-5',
      lg: 'p-6 sm:p-7',
    },
  },
  defaultVariants: { interactive: false, padding: 'md' },
})

export type CardProps = React.ComponentProps<'div'> & VariantProps<typeof cardVariants>

export function Card({ className, interactive, padding, ...props }: CardProps) {
  return <div className={cn(cardVariants({ interactive, padding }), className)} {...props} />
}

export function CardTitle({ className, ...props }: React.ComponentProps<'h3'>) {
  return <h3 className={cn('text-base leading-snug font-semibold text-fg', className)} {...props} />
}

export function CardMeta({ className, ...props }: React.ComponentProps<'p'>) {
  return <p className={cn('text-xs text-fg-muted', className)} {...props} />
}
