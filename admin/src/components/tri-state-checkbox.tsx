import * as React from 'react'
import { Checkbox as CheckboxPrimitive } from 'radix-ui'
import { CheckIcon, MinusIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

/**
 * 支持「部分选中」的勾选框：checked 为 'indeterminate' 时显示横线（读屏软件读作「部分选中」）。
 * 用于权限树的目录与页面节点；样式与 ui/checkbox 一致。
 */
export function TriStateCheckbox({
  className,
  checked,
  ...props
}: React.ComponentProps<typeof CheckboxPrimitive.Root>) {
  return (
    <CheckboxPrimitive.Root
      data-slot='checkbox'
      checked={checked}
      className={cn(
        'peer size-4 shrink-0 rounded-md border border-input shadow-xs transition-shadow outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:border-primary data-[state=checked]:bg-primary data-[state=checked]:text-primary-foreground data-[state=indeterminate]:border-primary data-[state=indeterminate]:bg-primary data-[state=indeterminate]:text-primary-foreground dark:bg-input/30 dark:data-[state=checked]:bg-primary dark:data-[state=indeterminate]:bg-primary',
        className
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator className='flex items-center justify-center text-current transition-none'>
        {checked === 'indeterminate' ? <MinusIcon className='size-3.5' /> : <CheckIcon className='size-3.5' />}
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  )
}
