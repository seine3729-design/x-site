import Link from 'next/link'
import { Logo } from '@/components/brand/logo'
import { Container } from '@/components/ui/section'
import { t } from '@/lib/i18n'

const LINKS: { href: string; label: string }[] = [
  { href: '/guide', label: t('footer.guide') },
  { href: '/projects', label: t('nav.projects') },
  { href: '/feedback', label: t('nav.feedback') },
  { href: '/topics', label: t('nav.topics') },
]

const POLICY: { href: string; label: string }[] = [
  { href: '/privacy', label: t('footer.privacy') },
  { href: '/terms', label: t('footer.terms') },
]

/**
 * 页脚。
 *
 * 刻意不加上外边距：页脚直接贴着内容区底边，
 * 否则首页底部的 CTA 色块与页脚之间会留出一条无意义的空白带。
 */
export function SiteFooter() {
  return (
    <footer className="border-t border-border bg-surface">
      <Container className="grid gap-8 py-10 sm:grid-cols-2 lg:grid-cols-4">
        <div className="lg:col-span-2">
          <Logo />
          <p className="mt-3 max-w-sm text-sm text-fg-muted">{t('brand.shortDescription')}</p>
        </div>
        <nav aria-label={t('footer.about')}>
          <h2 className="text-xs font-semibold text-fg">{t('footer.about')}</h2>
          <ul className="mt-3 flex flex-col gap-2">
            {LINKS.map((link) => (
              <li key={link.href}>
                <Link href={link.href} className="text-sm text-fg-muted hover:text-fg hover:underline">
                  {link.label}
                </Link>
              </li>
            ))}
          </ul>
        </nav>
        <nav aria-label={t('footer.terms')}>
          <h2 className="text-xs font-semibold text-fg">{t('footer.terms')}</h2>
          <ul className="mt-3 flex flex-col gap-2">
            {POLICY.map((link) => (
              <li key={link.href}>
                <Link href={link.href} className="text-sm text-fg-muted hover:text-fg hover:underline">
                  {link.label}
                </Link>
              </li>
            ))}
          </ul>
        </nav>
      </Container>
      <Container className="border-t border-border py-5">
        <p className="text-xs text-fg-subtle">{t('footer.copyright')}</p>
      </Container>
    </footer>
  )
}
