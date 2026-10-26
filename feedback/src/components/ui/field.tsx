import * as React from 'react'
import { cn } from '@/lib/utils'

/**
 * 表单控件与字段容器（feedback/docs/06 第 3.6 节、第 4 节）。
 *
 * 几条要求做进组件里，不靠调用方记得：
 * - 每个输入都有可见 Label，placeholder 只是补充说明，不能替代 Label；
 * - 控件高度 44px（移动端触控），字号 16px（iOS 下更小会触发自动缩放）；
 * - 错误与说明通过 aria-describedby 关联，并用 aria-invalid 标出错误态；
 * - 输入框、下拉、文本域共用同一套边框与焦点样式，表单里看起来是一套东西。
 */

const CONTROL = [
  'w-full rounded-control border bg-surface text-base text-fg',
  'border-control-border hover:border-control-border-hover',
  'placeholder:text-fg-subtle',
  'transition-colors duration-(--duration-hover)',
  'disabled:cursor-not-allowed disabled:opacity-50',
  'aria-invalid:border-danger aria-invalid:hover:border-danger',
].join(' ')

export function Input({ className, ...props }: React.ComponentProps<'input'>) {
  return <input className={cn(CONTROL, 'h-11 px-3', className)} {...props} />
}

export function Textarea({ className, ...props }: React.ComponentProps<'textarea'>) {
  return <textarea className={cn(CONTROL, 'min-h-28 px-3 py-2.5 leading-relaxed', className)} {...props} />
}

/* 下拉选择见 components/ui/select.tsx（Radix Select）。
 * 这里不再提供原生 <select> 包装：它的箭头只能用背景图画，颜色得硬编码，
 * 主题 token 一改就对不上；选项列表也跟不了主题。 */

export type FieldProps = {
  /** 控件的 id，Label 与错误/说明都靠它关联。 */
  id: string
  label: string
  required?: boolean
  /** 常驻说明，写在控件下方；不要把它塞进 placeholder。 */
  hint?: React.ReactNode
  error?: string
  className?: string
  children: (props: {
    id: string
    'aria-describedby': string | undefined
    'aria-invalid': boolean | undefined
    required: boolean | undefined
  }) => React.ReactNode
}

export function Field({ id, label, required, hint, error, className, children }: FieldProps) {
  const hintId = hint ? `${id}-hint` : undefined
  const errorId = error ? `${id}-error` : undefined
  const describedBy = [errorId, hintId].filter(Boolean).join(' ') || undefined

  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      <label htmlFor={id} className="text-sm font-medium text-fg">
        {label}
        {required && (
          <span className="ms-0.5 text-danger-fg" aria-hidden>
            *
          </span>
        )}
        {required && <span className="sr-only">（必填）</span>}
      </label>
      {children({
        id,
        'aria-describedby': describedBy,
        'aria-invalid': error ? true : undefined,
        required: required || undefined,
      })}
      {error && (
        <p id={errorId} className="text-sm text-danger-fg">
          {error}
        </p>
      )}
      {hint && (
        <p id={hintId} className="text-xs text-fg-muted">
          {hint}
        </p>
      )}
    </div>
  )
}
