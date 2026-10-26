import type { Metadata } from 'next'
import Link from 'next/link'
import { redirect } from 'next/navigation'
import { LayoutGrid } from 'lucide-react'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { Container } from '@/components/ui/section'
import { SubmitForm } from '@/features/feedback/components/submit-form'
import { listMyProjects } from '@/lib/api/queries'
import { t } from '@/lib/i18n'
import { getCurrentUser } from '@/lib/server/session'

// 这一页依赖会话或查询参数，按访问者实时渲染，不预渲染（feedback/docs/06 第 11.5 节）
export const instant = false

export const metadata: Metadata = {
  title: t('submit.title'),
  robots: { index: false, follow: false },
}

export default async function NewFeedbackPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>
}) {
  const params = await searchParams
  const user = await getCurrentUser()
  // 未登录就带着回跳地址去登录，登录完回到这个表单继续
  if (!user) redirect(`/login?next=${encodeURIComponent('/feedback/new')}`)

  // 只能往已加入的项目提交，所以下拉里只放这些——
  // 让用户选一个会被后端拒绝的项目是在浪费他填表的时间
  const mine = await listMyProjects()
  const joinable = mine.filter((p) => p.status === 'recruiting' || p.status === 'active')
  const preset = Array.isArray(params.project) ? params.project[0] : params.project

  return (
    <Container className="py-8 sm:py-10">
      <div className="mx-auto max-w-prose">
        <h1 className="text-2xl font-semibold tracking-tight text-fg sm:text-3xl">
          {t('submit.title')}
        </h1>
        <p className="mt-2 text-sm text-fg-muted sm:text-base">{t('submit.subtitle')}</p>

        <Alert tone="info" className="mt-5">
          {t('submit.searchFirst')}
        </Alert>

        <div className="mt-7">
          {joinable.length > 0 ? (
            <SubmitForm
              projects={joinable}
              defaultProject={joinable.some((p) => p.slug === preset) ? preset : undefined}
            />
          ) : (
            <EmptyState
              icon={LayoutGrid}
              title={t('submit.noProject')}
              hint={t('submit.noProjectHint')}
              action={
                <Button asChild size="sm">
                  <Link href="/projects">{t('action.browseProjects')}</Link>
                </Button>
              }
            />
          )}
        </div>
      </div>
    </Container>
  )
}
