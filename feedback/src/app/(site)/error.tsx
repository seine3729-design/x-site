'use client'

import { RotateCcw, TriangleAlert } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Container } from '@/components/ui/section'
import { t } from '@/lib/i18n'

/**
 * 页面级错误兜底（feedback/docs/06 第 10 节）。
 *
 * 只展示可理解的原因和「问题编号」，不展示后端堆栈或内部信息。
 * digest 是 Next.js 给这次错误的标识，用户报问题时念给我们能对上服务端日志。
 */
export default function SiteError({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  return (
    <Container className="flex min-h-[60vh] flex-col items-center justify-center gap-4 py-16 text-center">
      <span className="flex size-12 items-center justify-center rounded-pill bg-danger-surface text-danger-fg">
        <TriangleAlert className="size-6" aria-hidden />
      </span>
      <h1 className="text-xl font-semibold text-fg">{t('state.errorTitle')}</h1>
      <p className="max-w-md text-sm text-fg-muted">{t('state.errorHint')}</p>
      {error.digest && (
        <p className="font-mono text-2xs text-fg-subtle break-token">
          {t('state.requestId')}：{error.digest}
        </p>
      )}
      <Button onClick={reset} variant="outline" className="mt-2">
        <RotateCcw aria-hidden />
        {t('action.retry')}
      </Button>
    </Container>
  )
}
