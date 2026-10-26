import * as React from 'react'
import { Eye, EyeOff } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from './ui/button'
import { Input } from './ui/input'

type PasswordInputProps = Omit<
  React.InputHTMLAttributes<HTMLInputElement>,
  'type'
> & {
  ref?: React.Ref<HTMLInputElement>
}

/** 密码输入框：外观与 Input 一致（高度、边框、焦点环、错误态），右侧按钮切换明文显示。 */
export function PasswordInput({
  className,
  disabled,
  ref,
  ...props
}: PasswordInputProps) {
  const [showPassword, setShowPassword] = React.useState(false)

  return (
    <div className={cn('relative rounded-md', className)}>
      <Input
        type={showPassword ? 'text' : 'password'}
        className='pe-10'
        ref={ref}
        disabled={disabled}
        {...props}
      />
      <Button
        type='button'
        size='icon'
        variant='ghost'
        disabled={disabled}
        className='absolute end-1.5 top-1/2 size-7 -translate-y-1/2 rounded-md text-muted-foreground'
        aria-label='显示密码'
        aria-pressed={showPassword}
        onClick={() => setShowPassword((prev) => !prev)}
      >
        {showPassword ? <Eye size={18} /> : <EyeOff size={18} />}
      </Button>
    </div>
  )
}
