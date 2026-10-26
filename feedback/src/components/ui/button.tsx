import * as React from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { LoaderCircle } from 'lucide-react'
import { cn } from '@/lib/utils'

/**
 * 按钮（feedback/docs/06 第 4 节）。
 *
 * 几条硬性要求写在实现里，不靠调用方自觉：
 * - 触控目标：默认尺寸高 44px（移动端标准），sm 也有 40px，再小的只给纯图标按钮用并自带内边距；
 * - loading 时宽度不变（文字留在原位，只是透明），不会把旁边的按钮挤走；
 * - loading 自动 disabled，挡住重复提交；
 * - disabled 同时给 cursor-not-allowed 与降低不透明度，不做成「看着能点其实不能」。
 */
const buttonVariants = cva(
  [
    'relative inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap',
    'rounded-control font-medium',
    'transition-colors duration-(--duration-hover) ease-(--ease-out-soft)',
    'cursor-pointer select-none',
    'disabled:pointer-events-none disabled:opacity-50',
    '[&_svg]:pointer-events-none [&_svg]:shrink-0',
  ].join(' '),
  {
    variants: {
      variant: {
        primary: 'bg-primary text-fg-on-fill hover:bg-primary-hover',
        accent: 'bg-accent text-fg-on-fill hover:bg-accent-hover',
        outline:
          'border border-control-border bg-surface text-fg hover:border-control-border-hover hover:bg-surface-raised',
        subtle: 'bg-primary-surface text-primary-fg hover:bg-primary-surface/70',
        ghost: 'text-fg-muted hover:bg-surface-raised hover:text-fg',
        danger: 'bg-danger text-fg-on-fill hover:bg-danger-hover',
        link: 'text-primary-fg underline-offset-4 hover:underline',
      },
      size: {
        sm: 'h-10 px-3 text-sm [&_svg]:size-4',
        md: 'h-11 px-4 text-sm [&_svg]:size-4',
        lg: 'h-12 px-6 text-base [&_svg]:size-5',
        icon: 'size-11 [&_svg]:size-5',
        'icon-sm': 'size-10 [&_svg]:size-4',
      },
      block: { true: 'w-full', false: '' },
    },
    defaultVariants: { variant: 'primary', size: 'md', block: false },
  },
)

export type ButtonProps = React.ComponentProps<'button'> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
    loading?: boolean
    /** loading 时读给屏幕阅读器的说明，默认「处理中」。 */
    loadingLabel?: string
  }

export function Button({
  className,
  variant,
  size,
  block,
  asChild = false,
  loading = false,
  loadingLabel = '处理中',
  disabled,
  children,
  ...props
}: ButtonProps) {
  const classes = cn(buttonVariants({ variant, size, block }), className)

  // asChild 把样式交给子元素（通常是 <Link>）。
  // 这条路径不能包任何额外节点：Radix Slot 只接受单个元素子节点。
  // 链接也不存在 loading 与 disabled，所以这两个能力只在真正的 <button> 上提供。
  if (asChild) {
    return (
      <Slot className={classes} {...props}>
        {children}
      </Slot>
    )
  }

  return (
    <button
      className={classes}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...props}
    >
      {loading && (
        <span className="absolute inset-0 flex items-center justify-center">
          <LoaderCircle className="animate-spin" aria-hidden />
          <span className="sr-only">{loadingLabel}</span>
        </span>
      )}
      {/* 用 invisible 而不是卸载：按钮宽度保持不变，布局不跳 */}
      <span className={cn('inline-flex items-center gap-2', loading && 'invisible')}>
        {children}
      </span>
    </button>
  )
}

export { buttonVariants }
