import Link from 'next/link'
import { LogoMark } from '@/components/brand/logo'
import { Container } from '@/components/ui/section'
import { t } from '@/lib/i18n'

/**
 * 登录、注册、重置共用的壳。
 *
 * 窄栏居中：这三个页面都只有一件事要做，不需要侧栏也不需要导航干扰。
 * 页面标题用 h1，和站内其他页面保持标题层级一致。
 */
export function AuthShell({
  title,
  subtitle,
  children,
  footer,
}: {
  title: string
  subtitle: string
  children: React.ReactNode
  footer?: React.ReactNode
}) {
  return (
    <Container className="flex justify-center py-10 sm:py-16">
      <div className="w-full max-w-md">
        <div className="flex flex-col items-center text-center">
          <Link href="/" aria-label={`${t('brand.name')}首页`} className="rounded-control">
            <LogoMark className="size-9 text-primary" />
          </Link>
          <h1 className="mt-4 text-2xl font-semibold tracking-tight text-fg">{title}</h1>
          <p className="mt-2 text-sm text-fg-muted">{subtitle}</p>
        </div>
        <div className="mt-8 rounded-surface border border-border bg-surface p-5 sm:p-6">
          {children}
        </div>
        {footer && <div className="mt-5 text-center text-sm text-fg-muted">{footer}</div>}
      </div>
    </Container>
  )
}
