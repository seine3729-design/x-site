import { CircleCheck, Info, TriangleAlert } from 'lucide-react'
import { cn } from '@/lib/utils'

const TONES = {
  info: { box: 'border-transparent bg-info-surface text-info-fg', icon: Info },
  success: { box: 'border-transparent bg-success-surface text-success-fg', icon: CircleCheck },
  warning: { box: 'border-transparent bg-warning-surface text-warning-fg', icon: TriangleAlert },
  danger: { box: 'border-transparent bg-danger-surface text-danger-fg', icon: TriangleAlert },
} as const

/**
 * 提示块。
 *
 * `role` 默认按语气选：danger 用 alert（会被屏幕阅读器打断播报），
 * 其余用 status（礼貌播报，不打断）。表单失败后的错误摘要要能被聚焦，
 * 所以调用方可以传 tabIndex={-1} 配合 ref 使用。
 */
export function Alert({
  tone = 'info',
  title,
  children,
  className,
  ...props
}: {
  tone?: keyof typeof TONES
  title?: string
  children?: React.ReactNode
} & React.ComponentProps<'div'>) {
  const { box, icon: Icon } = TONES[tone]
  return (
    <div
      role={tone === 'danger' ? 'alert' : 'status'}
      className={cn('flex gap-3 rounded-surface border px-4 py-3 text-sm', box, className)}
      {...props}
    >
      <Icon className="mt-0.5 size-4 shrink-0" aria-hidden />
      <div className="min-w-0 flex-1">
        {title && <p className="font-medium">{title}</p>}
        {children && <div className={cn(title && 'mt-1')}>{children}</div>}
      </div>
    </div>
  )
}
