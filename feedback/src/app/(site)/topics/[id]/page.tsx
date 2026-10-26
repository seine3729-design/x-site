import type { Metadata } from 'next'
import Link from 'next/link'
import { notFound } from 'next/navigation'
import { ChevronLeft, MessageCircle } from 'lucide-react'
import { TopicStatusBadge } from '@/components/domain/badges'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Container } from '@/components/ui/section'
import { AnswerComposer, AnswerList } from '@/features/topics/components/answers'
import { getTopic, listAnswers } from '@/lib/api/queries'
import { formatDateTime, formatRemaining } from '@/lib/format'
import { t } from '@/lib/i18n'
import { requestNow } from '@/lib/server/now'
import { getCurrentUser } from '@/lib/server/session'

type Params = { params: Promise<{ id: string }> }

// 这一页依赖会话或查询参数，按访问者实时渲染，不预渲染（feedback/docs/06 第 11.5 节）
export const instant = false

export async function generateMetadata({ params }: Params): Promise<Metadata> {
  const { id } = await params
  const topic = await getTopic(Number(id))
  if (!topic) return { title: t('state.notFoundTitle'), robots: { index: false } }
  return {
    title: topic.title,
    description: topic.visibility === 'public' ? topic.title : undefined,
    robots: topic.visibility === 'public' ? undefined : { index: false, follow: false },
  }
}

export default async function TopicDetailPage({ params }: Params) {
  const { id } = await params
  const topicId = Number(id)
  if (!Number.isInteger(topicId) || topicId <= 0) notFound()

  const [topic, user, now] = await Promise.all([getTopic(topicId), getCurrentUser(), requestNow()])
  // 查不到与无权查看都是 404，不暴露仅成员议题是否存在
  if (!topic) notFound()

  const answers = await listAnswers(topicId, { page_size: 100 })
  const remaining = topic.status === 'open' ? formatRemaining(topic.ends_at, now) : ''
  const closed = topic.status !== 'open' || (!!topic.ends_at && !remaining)

  return (
    <Container className="py-6 sm:py-8">
      {/*
        面包屑放在读栏内部，和正文左边缘对齐。
        放在 Container 直接子级会贴着页面边缘，而正文是居中的窄栏，
        两个不同的左基准看起来像是排版错了。
      */}
      <div className="mx-auto max-w-3xl">
        <Link
          href="/topics"
          className="inline-flex items-center gap-1 rounded-control py-1 text-sm text-fg-muted hover:text-fg hover:underline"
        >
          <ChevronLeft className="size-4" aria-hidden />
          {t('topic.breadcrumb')}
        </Link>
      </div>

      <div className="mx-auto mt-4 max-w-3xl">
        <div className="flex flex-wrap items-center gap-2">
          {topic.project_name && (
            <Link href={`/projects/${topic.project_slug ?? ''}`}>
              <Badge tone="outline">{topic.project_name}</Badge>
            </Link>
          )}
          {topic.status && <TopicStatusBadge status={topic.status} size="md" />}
          <span className="inline-flex items-center gap-1.5 text-xs text-fg-muted">
            <MessageCircle className="size-3.5" aria-hidden />
            <span className="tabular">{topic.answer_count ?? 0}</span>
            <span>{t('topic.answerCount', { count: topic.answer_count ?? 0 }).slice(-3)}</span>
          </span>
          {remaining && <span className="text-xs text-warning-fg">{remaining}</span>}
        </div>

        <h1 className="mt-3 text-xl leading-snug font-semibold tracking-tight text-fg sm:text-2xl">
          {topic.title}
        </h1>

        {topic.body && (
          <div className="mt-4 text-sm leading-relaxed text-fg-muted user-text">{topic.body}</div>
        )}

        {topic.ends_at && !closed && (
          <p className="mt-4 text-xs text-fg-subtle">
            {t('topic.endsAt', { when: formatDateTime(topic.ends_at) })}
          </p>
        )}

        {closed && (
          <Alert tone="info" className="mt-5">
            {t('topic.closedNote')}
          </Alert>
        )}

        {/* 回答输入框放在正文之后、列表之前：读完问题就能直接写，不用先翻过别人的回答 */}
        <Card className="mt-6" padding="md">
          <AnswerComposer
            topicId={topicId}
            canAnswer={!!topic.can_answer}
            signedIn={!!user}
            closed={closed}
          />
          {topic.answer_hint && !closed && (
            <p className="mt-2 text-xs text-fg-subtle">{topic.answer_hint}</p>
          )}
        </Card>

        <section className="mt-8" aria-labelledby="answers-heading">
          <h2 id="answers-heading" className="mb-4 text-base font-semibold text-fg">
            {t('topic.answersTitle')}
            <span className="ms-2 text-sm font-normal text-fg-muted tabular">
              {answers.total}
            </span>
          </h2>
          {answers.list.length > 0 ? (
            <>
              <p className="mb-4 text-xs text-fg-subtle">{t('topic.helpfulHint')}</p>
              <AnswerList
                answers={answers.list}
                topicId={topicId}
                now={now}
                canAnswer={!!topic.can_answer && !closed}
                signedIn={!!user}
              />
            </>
          ) : (
            <p className="text-sm text-fg-muted">
              {t('topic.answersEmpty')}
              <span className="ms-1 text-fg-subtle">{t('topic.answersEmptyHint')}</span>
            </p>
          )}
        </section>
      </div>
    </Container>
  )
}
