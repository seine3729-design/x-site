import Image from 'next/image'
import { t } from '@/lib/i18n'
import { cn } from '@/lib/utils'

const LOGO_SRC = 'https://cdn.duanju66.cc/static/logo.webp'
const LOGO_WIDTH = 1602
const LOGO_HEIGHT = 728

/** 远程品牌 Logo。顶栏、页脚共用，尺寸由原图比例和响应式样式共同约束。 */
export function Logo({
  className,
}: {
  className?: string
  textClassName?: string
}) {
  return (
    <span className={cn('inline-flex items-center', className)}>
      <Image
        src={LOGO_SRC}
        alt={t('brand.name')}
        width={LOGO_WIDTH}
        height={LOGO_HEIGHT}
        sizes="100px"
        className="h-8 w-auto object-contain sm:h-9"
        priority
        unoptimized
      />
    </span>
  )
}

/** 兼容登录壳的小尺寸品牌标记。 */
export function LogoMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      className={cn('size-7', className)}
      role="img"
      aria-hidden
      focusable="false"
    >
      <path
        d="M4 7a5 5 0 0 1 5-5h6a5 5 0 0 1 5 5v6a5 5 0 0 1-5 5H9.8L4.8 21.6A.5.5 0 0 1 4 21.2V7Z"
        fill="currentColor"
      />
      <path
        d="m8.4 10.4 2.5 2.5 4.7-4.9"
        fill="none"
        stroke="var(--surface)"
        strokeWidth="2.1"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}
