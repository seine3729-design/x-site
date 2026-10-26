import { cn } from '@/lib/utils'

/**
 * 头像。
 *
 * 社区账号只有用户名，不收集头像（feedback/docs/adr/0007），所以这里不处理图片源，
 * 直接用昵称首字 + 由内测编号决定的底色：同一个人每次看到的颜色一致，
 * 在列表里能当作辅助识别，但不作为唯一信息（旁边一定有名字）。
 */
const TONES = [
  'bg-primary-surface text-primary-fg',
  'bg-info-surface text-info-fg',
  'bg-success-surface text-success-fg',
  'bg-warning-surface text-warning-fg',
  'bg-danger-surface text-danger-fg',
] as const

/** 取用于显示的首字：中文取第一个字，拉丁取首字母大写。 */
function initial(name: string): string {
  const first = [...name.trim()][0]
  if (!first) return '?'
  return /[a-z]/i.test(first) ? first.toUpperCase() : first
}

export function Avatar({
  name,
  seed = 0,
  size = 'md',
  className,
}: {
  name: string
  /** 用来挑底色，传内测编号或用户 ID。 */
  seed?: number
  size?: 'sm' | 'md' | 'lg'
  className?: string
}) {
  const tone = TONES[Math.abs(seed) % TONES.length]
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center justify-center rounded-pill font-medium select-none',
        size === 'sm' && 'size-7 text-xs',
        size === 'md' && 'size-9 text-sm',
        size === 'lg' && 'size-12 text-lg',
        tone,
        className,
      )}
      aria-hidden
    >
      {initial(name)}
    </span>
  )
}
