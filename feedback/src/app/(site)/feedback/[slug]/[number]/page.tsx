import type { Metadata } from 'next'
import Link from 'next/link'
import { notFound } from 'next/navigation'
import { ChevronLeft, Lock, Users } from 'lucide-react'
import { KindBadge, StatusBadge } from '@/components/domain/badges'
import { Alert } from '@/components/ui/alert'
import { Card } from '@/components/ui/card'
import { Container } from '@/components/ui/section'
import { Timeline } from '@/features/feedback/components/timeline'
import { VoteButton } from '@/features/feedback/components/vote-button'
import { getFeedback } from '@/lib/api/queries'
import { formatDateTime, formatRelative } from '@/lib/format'
import { t } from '@/lib/i18n'
import { requestNow } from '@/lib/server/now'
import { getCurrentUser } from '@/lib/server/session'

type Params = { params: Promise<{ slug: string; number: string }> }

// 这一页依赖会话或查询参数，按访问者实时渲染，不预渲染（feedback/docs/06 第 11.5 节）
export const instant = false

export async function generateMetadata({ params }: Params): Promise<Metadata> {
  const { slug, number } = await params
  const detail = await getFeedback(slug, Number(number))
  if (!detail?.feedback) return { title: t('state.notFoundTitle'), robots: { index: false } }
  const fb = detail.feedback
  return {
    title: `${fb.key} ${fb.title}`,
    // 非公开反馈不进搜索引擎，也不把摘要写进分享卡（feedback/docs/02 第 11 节）
    description: fb.visibility === 'public' ? fb.title : undefined,
    robots: fb.visibility === 'public' ? undefined : { index: false, follow: false },
  }
}

export default async function FeedbackDetailPage({ params }: Params) {
  const { slug, number } = await params
  const n = Number(number)
  if (!Number.isInteger(n) || n <= 0) notFound()

  const [detail, user, now] = await Promise.all([
    getFeedback(slug, n),
    getCurrentUser(),
    requestNow(),
  ])
  // 查不到与无权查看都是 404：两者必须不可区分，否则能探测出私密反馈的存在
  if (!detail?.feedback) notFound()

  const fb = detail.feedback
  const events = detail.events ?? []
  const author = fb.author_nickname || fb.author_handle || t('detail.anonymousAuthor')

  return (
    <Container className="py-6 sm:py-8">
      <Link
        href={`/feedback?project=${fb.project_slug ?? ''}`}
        className="inline-flex items-center gap-1 rounded-control py-1 text-sm text-fg-muted hover:text-fg hover:underline"
      >
        <ChevronLeft className="size-4" aria-hidden />
        {t('detail.backToList')}
      </Link>

      <div className="mt-4 grid gap-8 lg:grid-cols-[minmax(0,1fr)_17rem] lg:gap-10">
        <article className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-sm text-fg-subtle tabular">{fb.key}</span>
            {fb.status && <StatusBadge status={fb.status} size="md" />}
            {fb.kind && <KindBadge kind={fb.kind} size="md" />}
          </div>

          <h1 className="mt-3 text-xl leading-snug font-semibold tracking-tight text-fg sm:text-2xl">
            {fb.title}
          </h1>

          <p className="mt-3 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-fg-muted">
            <Link href={`/projects/${fb.project_slug ?? ''}`} className="hover:underline">
              {fb.project_name}
            </Link>
            <span aria-hidden>·</span>
            <span>{t('detail.submittedBy', { who: author })}</span>
            <span aria-hidden>·</span>
            <time dateTime={fb.created_at} title={formatDateTime(fb.created_at)}>
              {formatRelative(fb.created_at, now)}
            </time>
          </p>

          <div className="mt-5 flex flex-col gap-3">
            {fb.visibility === 'private' && (
              <Alert tone="info">
                <span className="inline-flex items-center gap-1.5">
                  <Lock className="size-3.5" aria-hidden />
                  {t('detail.privateHint')}
                </span>
              </Alert>
            )}
            {fb.visibility === 'members' && (
              <Alert tone="info">
                <span className="inline-flex items-center gap-1.5">
                  <Users className="size-3.5" aria-hidden />
                  {t('detail.membersHint')}
                </span>
              </Alert>
            )}
            {fb.status === 'duplicate' && <Alert tone="warning">{t('detail.duplicateOf')}</Alert>}
            {fb.status === 'planned' && <Alert tone="info">{t('status.plannedNote')}</Alert>}
          </div>

          <section className="mt-6" aria-labelledby="body-heading">
            <h2 id="body-heading" className="text-sm font-semibold text-fg">
              {t('detail.bodyTitle')}
            </h2>
            <div className="mt-2 max-w-prose text-sm leading-relaxed text-fg user-text">
              {fb.body}
            </div>
          </section>

          {(fb.env || fb.page_url) && (
            <Card padding="sm" className="mt-5 bg-surface-raised">
              <dl className="flex flex-col gap-2 text-xs">
                {fb.env && (
                  <div className="flex gap-2">
                    <dt className="shrink-0 text-fg-subtle">{t('detail.envTitle')}</dt>
                    <dd className="min-w-0 text-fg-muted">{fb.env}</dd>
                  </div>
                )}
                {fb.page_url && (
                  <div className="flex gap-2">
                    <dt className="shrink-0 text-fg-subtle">{t('detail.pageTitle')}</dt>
                    <dd className="min-w-0 text-fg-muted break-token">{fb.page_url}</dd>
                  </div>
                )}
              </dl>
            </Card>
          )}

          <section className="mt-8" aria-labelledby="timeline-heading">
            <h2 id="timeline-heading" className="mb-4 text-sm font-semibold text-fg">
              {t('detail.timelineTitle')}
            </h2>
            <Timeline events={events} now={now} />
          </section>
        </article>

        {/* 辅助栏：桌面在右侧，移动端落到正文下面，但始终不遮挡内容 */}
        <aside className="lg:sticky lg:top-20 lg:self-start">
          <VoteButton
            slug={fb.project_slug ?? ''}
            number={fb.number ?? 0}
            count={fb.vote_count ?? 0}
            voted={!!fb.voted}
            signedIn={!!user}
          />
        </aside>
      </div>
    </Container>
  )
}
