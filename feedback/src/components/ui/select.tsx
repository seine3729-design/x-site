'use client'

import * as React from 'react'
import * as SelectPrimitive from '@radix-ui/react-select'
import { Check, ChevronDown, ChevronUp } from 'lucide-react'
import { cn } from '@/lib/utils'

/**
 * 下拉选择（Radix Select，对齐 admin/src/components/ui/select.tsx 的结构）。
 *
 * 为什么不继续用原生 <select>：
 * - 原生选项列表的样式由系统决定，没法跟着主题走，深色模式下在部分平台仍是白底；
 * - 没法在选项里放勾选标记、分组标题、次要说明；
 * - 触发器的箭头只能用背景图硬编码颜色，主题色一改就对不上（上一版就是这么坏掉的）。
 *
 * Radix 负责键盘导航、输入首字母跳转、焦点返回、滚动边界，这些手写极易漏。
 * 代价是浮层里的列表不再是系统原生控件，所以移动端也要自己保证触控高度够。
 *
 * 表单提交：Select 的 `name` 传给 Root，Radix 会渲染一个隐藏的原生 select，
 * 所以 Server Action 里照常用 formData.get(name) 取值，不需要额外的隐藏字段。
 */

export const Select = SelectPrimitive.Root
export const SelectGroup = SelectPrimitive.Group
export const SelectValue = SelectPrimitive.Value

export function SelectTrigger({
  className,
  children,
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Trigger>) {
  return (
    <SelectPrimitive.Trigger
      className={cn(
        // 和 field.tsx 的 CONTROL 保持同一套尺寸与边框，表单里看起来是一套东西
        'flex h-11 w-full cursor-pointer items-center justify-between gap-2 rounded-control border bg-surface px-3',
        'text-start text-base text-fg',
        'border-control-border hover:border-control-border-hover',
        'transition-colors duration-(--duration-hover)',
        'disabled:cursor-not-allowed disabled:opacity-50',
        'aria-invalid:border-danger aria-invalid:hover:border-danger',
        // 未选中时显示的是 placeholder，颜色要降下来
        'data-placeholder:text-fg-subtle',
        '*:data-[slot=select-value]:truncate',
        className,
      )}
      {...props}
    >
      {children}
      <SelectPrimitive.Icon asChild>
        <ChevronDown className="size-4 shrink-0 text-fg-subtle" aria-hidden />
      </SelectPrimitive.Icon>
    </SelectPrimitive.Trigger>
  )
}

export function SelectContent({
  className,
  children,
  position = 'popper',
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Content>) {
  return (
    <SelectPrimitive.Portal>
      <SelectPrimitive.Content
        position={position}
        sideOffset={4}
        className={cn(
          'relative z-50 overflow-hidden rounded-surface border border-border bg-surface-overlay shadow-popover',
          // 不让浮层超出视口：Radix 把可用高度算好给到这个变量
          'max-h-(--radix-select-content-available-height) min-w-32',
          'origin-(--radix-select-content-transform-origin)',
          'data-[state=open]:animate-[fb-scale-in_var(--duration-hover)_var(--ease-out-soft)]',
          'data-[state=closed]:animate-[fb-fade-out_120ms_var(--ease-out-soft)]',
          className,
        )}
        {...props}
      >
        <SelectScrollUpButton />
        <SelectPrimitive.Viewport
          className={cn(
            'p-1',
            // popper 模式下让浮层至少和触发器一样宽，否则长选项会被挤窄
            position === 'popper' && 'w-full min-w-(--radix-select-trigger-width)',
          )}
        >
          {children}
        </SelectPrimitive.Viewport>
        <SelectScrollDownButton />
      </SelectPrimitive.Content>
    </SelectPrimitive.Portal>
  )
}

export function SelectLabel({
  className,
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Label>) {
  return (
    <SelectPrimitive.Label
      className={cn('px-2.5 py-1.5 text-xs font-medium text-fg-subtle', className)}
      {...props}
    />
  )
}

export function SelectItem({
  className,
  children,
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Item>) {
  return (
    <SelectPrimitive.Item
      className={cn(
        // min-h-10 而不是靠 padding 撑：移动端触控目标不能比这个更小
        'relative flex min-h-10 w-full cursor-pointer items-center rounded-control py-2 ps-2.5 pe-9',
        'text-sm text-fg outline-none select-none',
        // 键盘与鼠标共用同一个高亮态，不另外写 hover
        'data-highlighted:bg-surface-raised data-highlighted:text-fg',
        'data-[state=checked]:font-medium data-[state=checked]:text-primary-fg',
        'data-disabled:pointer-events-none data-disabled:opacity-50',
        className,
      )}
      {...props}
    >
      <SelectPrimitive.ItemText>{children}</SelectPrimitive.ItemText>
      <span className="absolute inset-e-2.5 flex size-4 items-center justify-center">
        <SelectPrimitive.ItemIndicator>
          <Check className="size-4" aria-hidden />
        </SelectPrimitive.ItemIndicator>
      </span>
    </SelectPrimitive.Item>
  )
}

export function SelectSeparator({
  className,
  ...props
}: React.ComponentProps<typeof SelectPrimitive.Separator>) {
  return (
    <SelectPrimitive.Separator
      className={cn('-mx-1 my-1 h-px bg-border', className)}
      {...props}
    />
  )
}

function SelectScrollUpButton() {
  return (
    <SelectPrimitive.ScrollUpButton className="flex h-6 cursor-default items-center justify-center bg-surface-overlay text-fg-subtle">
      <ChevronUp className="size-4" aria-hidden />
    </SelectPrimitive.ScrollUpButton>
  )
}

function SelectScrollDownButton() {
  return (
    <SelectPrimitive.ScrollDownButton className="flex h-6 cursor-default items-center justify-center bg-surface-overlay text-fg-subtle">
      <ChevronDown className="size-4" aria-hidden />
    </SelectPrimitive.ScrollDownButton>
  )
}

/**
 * 对接 `Field` 的便捷封装。
 *
 * `Field` 把 id / aria-describedby / aria-invalid / required 注入进来，但它们的去处不同：
 * 前三个属于触发器（它才是那个可聚焦元素），`required` 属于 Root（隐藏的原生 select 要用）。
 * 不封装的话每个调用点都得手分这一次，六处就错六次。
 */
export function SelectField({
  options,
  placeholder,
  className,
  required,
  id,
  'aria-describedby': describedBy,
  'aria-invalid': invalid,
  ...root
}: Omit<React.ComponentProps<typeof SelectPrimitive.Root>, 'children'> & {
  options: { value: string; label: string; disabled?: boolean }[]
  placeholder?: string
  className?: string
  id?: string
  'aria-describedby'?: string
  'aria-invalid'?: boolean
}) {
  return (
    <Select required={required} {...root}>
      <SelectTrigger
        id={id}
        aria-describedby={describedBy}
        aria-invalid={invalid}
        className={className}
      >
        <SelectValue data-slot="select-value" placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        {options.map((opt) => (
          <SelectItem key={opt.value} value={opt.value} disabled={opt.disabled}>
            {opt.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
