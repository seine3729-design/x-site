import { cn } from '@/lib/utils'

type MainProps = React.HTMLAttributes<HTMLElement> & {
  fixed?: boolean
  fluid?: boolean
  ref?: React.Ref<HTMLElement>
}

export function Main({ fixed, className, fluid, ...props }: MainProps) {
  return (
    <main
      data-layout={fixed ? 'fixed' : 'auto'}
      className={cn(
        // 如果布局是固定的，请使主容器flex并增长
        fixed && 'flex grow flex-col overflow-hidden',

        // 如果布局不是流体的，请设置最大宽度
        !fluid &&
          '@7xl/content:mx-auto @7xl/content:w-full',
        className
      )}
      {...props}
    />
  )
}
