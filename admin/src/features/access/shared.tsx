import { type ReactNode } from 'react'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui'
import { Badge } from '@/components/ui/badge'

/** 页面标题行：标题、说明与右侧操作。 */
export function PageHeading({
  title,
  description,
  readonly,
  actions,
}: {
  title: string
  description?: string
  readonly?: boolean
  actions?: ReactNode
}) {
  return (
    <div className='flex flex-wrap items-start justify-between gap-3'>
      <div className='flex flex-col gap-1'>
        <div className='flex items-center gap-2'>
          <h1 className='text-2xl font-semibold tracking-tight'>{title}</h1>
          {readonly && <Badge variant='secondary'>只读</Badge>}
        </div>
        {description && <p className='text-sm text-muted-foreground'>{description}</p>}
      </div>
      {actions && <div className='flex flex-wrap items-center gap-2'>{actions}</div>}
    </div>
  )
}

/**
 * 给不可用的控件加上原因：鼠标悬停显示提示；读屏软件读到紧随其后的说明文字。
 * 禁用的按钮收不到指针事件，所以由外层 span 承载提示。reason 为空时原样渲染。
 */
export function WithReason({ reason, children }: { reason: string | null; children: ReactNode }) {
  if (!reason) return <>{children}</>
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className='inline-flex'>
          {children}
          <span className='sr-only'>（{reason}）</span>
        </span>
      </TooltipTrigger>
      <TooltipContent>{reason}</TooltipContent>
    </Tooltip>
  )
}

/** 加载失败时的提示与重试。 */
export function LoadError({ onRetry }: { onRetry: () => void }) {
  return (
    <div role='alert' className='rounded-md border border-destructive/40 p-4 text-sm'>
      数据加载失败。
      <button type='button' className='ms-2 font-medium text-primary underline-offset-4 hover:underline' onClick={onRetry}>
        重试
      </button>
    </div>
  )
}
