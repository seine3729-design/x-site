import Link from 'next/link'
import { Compass } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Container } from '@/components/ui/section'
import { t } from '@/lib/i18n'

/**
 * 404。
 *
 * 文案刻意把「不存在」和「只对成员可见」说成同一句：私密资源不能通过
 * 404 与 403 的差别被探测出来（feedback/docs/02 第 6 节）。
 */
export default function NotFound() {
  return (
    <Container className="flex min-h-[60vh] flex-col items-center justify-center gap-4 py-16 text-center">
      <span className="flex size-12 items-center justify-center rounded-pill bg-surface-raised text-fg-subtle">
        <Compass className="size-6" aria-hidden />
      </span>
      <h1 className="text-xl font-semibold text-fg">{t('state.notFoundTitle')}</h1>
      <p className="max-w-md text-sm text-fg-muted">{t('state.notFoundHint')}</p>
      <div className="mt-2 flex gap-3">
        <Button asChild>
          <Link href="/">{t('nav.home')}</Link>
        </Button>
        <Button asChild variant="outline">
          <Link href="/projects">{t('action.browseProjects')}</Link>
        </Button>
      </div>
    </Container>
  )
}
