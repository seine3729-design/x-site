import type { Metadata } from 'next'
import Link from 'next/link'
import { notFound } from 'next/navigation'
import { ExternalLink, Inbox, Megaphone, MessagesSquare, Plus, Users } from 'lucide-react'
import { ProjectStatusBadge } from '@/components/domain/badges'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { Container, SectionHeading } from '@/components/ui/section'
import { FeedbackCard } from '@/features/feedback/components/feedback-card'
import { PostCard } from '@/features/posts/components/post-card'
import { JoinButton } from '@/features/projects/components/join-button'
import { TopicCard } from '@/features/topics/components/topic-card'
import { getProject, listFeedback, listPosts, listTopics } from '@/lib/api/queries'
import { formatCount } from '@/lib/format'
import { t } from '@/lib/i18n'
import { requestNow } from '@/lib/server/now'

type Params = { params: Promise<{ slug: string }> }

// 这一页依赖会话或查询参数，按访问者实时渲染，不预渲染（feedback/docs/06 第 11.5 节）
export const instant = false

export async function generateMetadata({ params }: Params): Promise<Metadata> {
  const { slug } = await params
  const project = await getProject(slug)
  if (!project) return { title: t('project.notFound'), robots: { index: false } }
  return {
    title: project.name,
    description: project.summary,
    // 仅成员项目不进搜索引擎（feedback/docs/02 第 11 节）
    robots: project.visibility === 'public' ? undefined : { index: false, follow: false },
  }
}

export default async function ProjectPage({ params }: Params) {
  const { slug } = await params
  const project = await getProject(slug)
  // 不存在与无权查看都走 404，不暴露仅成员项目是否存在
  if (!project) notFound()

  const now = await requestNow()
  const projectId = project.id ?? 0
  const [posts, feedback, topics] = await Promise.all([
    listPosts({ project_id: projectId, page_size: 4 }),
    listFeedback({ project_id: projectId, page_size: 5, sort: 'latest' }),
    listTopics({ project_id: projectId, status: 'open', page_size: 2 }),
  ])

  const acceptsFeedback = project.status === 'recruiting' || project.status === 'active'
  const focus = posts.list.filter((p) => p.kind === 'test_task').slice(0, 2)
  const otherPosts = posts.list.filter((p) => !focus.includes(p)).slice(0, 3)

  return (
    <>
      <header className="border-b border-border bg-surface">
        <Container className="py-8 sm:py-10">
          <div className="flex flex-wrap items-center gap-2">
            {project.status && <ProjectStatusBadge status={project.status} size="md" />}
            {project.visibility === 'members' && (
              <Badge tone="neutral" size="md">
                <Users aria-hidden />
                {t('visibility.membersShort')}
              </Badge>
            )}
          </div>
          <h1 className="mt-3 text-2xl font-semibold tracking-tight text-fg sm:text-3xl">
            {project.name}
          </h1>
          {project.summary && (
            <p className="mt-2 max-w-prose text-sm text-fg-muted sm:text-base">
              {project.summary}
            </p>
          )}

          <dl className="mt-5 flex flex-wrap items-center gap-x-6 gap-y-2 text-sm text-fg-muted">
            <div className="flex items-center gap-1.5">
              <Users className="size-4" aria-hidden />
              <dd className="tabular">{formatCount(project.member_count ?? 0)}</dd>
              <dt>{t('project.members')}</dt>
            </div>
            <div className="flex items-center gap-1.5">
              <Inbox className="size-4" aria-hidden />
              <dd className="tabular">{formatCount(project.feedback_count ?? 0)}</dd>
              <dt>{t('project.feedbackCount')}</dt>
            </div>
          </dl>

          <div className="mt-6 flex flex-wrap items-center gap-3">
            {project.joined && acceptsFeedback ? (
              <Button asChild size="lg">
                <Link href={`/feedback/new?project=${project.slug}`}>
                  <Plus aria-hidden />
                  {t('action.newFeedback')}
                </Link>
              </Button>
            ) : null}
            <JoinButton
              slug={project.slug ?? ''}
              name={project.name ?? ''}
              joined={!!project.joined}
              joinMode={project.join_mode ?? 'closed'}
              memberStatus={project.member_status}
              acceptsFeedback={acceptsFeedback}
            />
            {project.try_url && (
              <Button asChild variant="outline" size="lg">
                <a href={project.try_url} target="_blank" rel="noopener noreferrer">
                  {t('action.tryIt')}
                  <ExternalLink aria-hidden />
                  <span className="sr-only">{t('a11y.openInNew')}</span>
                </a>
              </Button>
            )}
          </div>
        </Container>
      </header>

      <Container className="flex flex-col gap-12 py-10">
        {/* 暂停与归档给常驻提示，不是只把按钮变灰（feedback/docs/06 第 3.3 节） */}
        {project.status === 'paused' && <Alert tone="warning">{t('projectStatus.pausedNote')}</Alert>}
        {project.status === 'archived' && <Alert tone="info">{t('projectStatus.archivedNote')}</Alert>}

        <section aria-labelledby="focus-heading">
          <SectionHeading title={t('project.focusTitle')} />
          <div id="focus-heading" className="sr-only">
            {t('project.focusTitle')}
          </div>
          {focus.length > 0 ? (
            // 只有一条时占满整行：这是本页最想让人读到的内容，
            // 放在两列网格里只占一半会显得像没加载完
            <div className={focus.length > 1 ? 'grid auto-rows-fr gap-4 md:grid-cols-2' : ''}>
              {focus.map((post) => (
                <PostCard key={post.id} post={post} now={now} />
              ))}
            </div>
          ) : (
            <EmptyState icon={Megaphone} title={t('project.focusEmpty')} />
          )}
        </section>

        <div className="grid gap-12 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)] lg:gap-10">
          <section aria-labelledby="pf-heading">
            <SectionHeading
              title={t('project.feedbackTitle')}
              moreHref={`/feedback?project=${project.slug}`}
            />
            <div id="pf-heading" className="sr-only">
              {t('project.feedbackTitle')}
            </div>
            {feedback.list.length > 0 ? (
              <div className="flex flex-col gap-3">
                {feedback.list.map((item) => (
                  <FeedbackCard key={item.id} item={item} now={now} />
                ))}
              </div>
            ) : (
              <EmptyState
                icon={Inbox}
                title={t('project.feedbackEmpty')}
                hint={project.joined ? undefined : t('project.joinToSubmit')}
                action={
                  project.joined && acceptsFeedback ? (
                    <Button asChild size="sm">
                      <Link href={`/feedback/new?project=${project.slug}`}>
                        {t('action.newFeedback')}
                      </Link>
                    </Button>
                  ) : undefined
                }
              />
            )}
          </section>

          <div className="flex flex-col gap-12">
            <section aria-labelledby="pt-heading">
              <SectionHeading
                title={t('project.topicsTitle')}
                moreHref={`/topics?project=${project.slug}`}
              />
              <div id="pt-heading" className="sr-only">
                {t('project.topicsTitle')}
              </div>
              {topics.list.length > 0 ? (
                <div className="flex flex-col gap-4">
                  {topics.list.map((topic) => (
                    <TopicCard key={topic.id} topic={topic} now={now} />
                  ))}
                </div>
              ) : (
                <EmptyState icon={MessagesSquare} title={t('project.topicsEmpty')} />
              )}
            </section>

            {otherPosts.length > 0 && (
              <section aria-labelledby="pu-heading">
                <SectionHeading title={t('project.updatesTitle')} />
                <div id="pu-heading" className="sr-only">
                  {t('project.updatesTitle')}
                </div>
                <div className="flex flex-col gap-4">
                  {otherPosts.map((post) => (
                    <PostCard key={post.id} post={post} now={now} />
                  ))}
                </div>
              </section>
            )}
          </div>
        </div>

        {project.description && (
          <section aria-labelledby="about-heading">
            <h2
              id="about-heading"
              className="text-lg font-semibold tracking-tight text-fg sm:text-xl"
            >
              {t('project.aboutTitle')}
            </h2>
            <div className="mt-3 max-w-prose text-sm leading-relaxed text-fg-muted user-text">
              {project.description}
            </div>
          </section>
        )}
      </Container>
    </>
  )
}
