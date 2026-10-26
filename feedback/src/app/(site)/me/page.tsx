import type { Metadata } from 'next'
import Link from 'next/link'
import { Inbox, LayoutGrid, Plus, Sparkles, Trophy } from 'lucide-react'
import { Avatar } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { EmptyState } from '@/components/ui/empty-state'
import { Container, SectionHeading } from '@/components/ui/section'
import { FeedbackCard } from '@/features/feedback/components/feedback-card'
import { ProjectCard } from '@/features/projects/components/project-card'
import { listLeaderboard, listMyFeedback, listMyProjects } from '@/lib/api/queries'
import { formatCount, formatDate } from '@/lib/format'
import { cn } from '@/lib/utils'
import { t } from '@/lib/i18n'
import { requestNow } from '@/lib/server/now'
import { getCurrentUser } from '@/lib/server/session'

// 这一页依赖会话或查询参数，按访问者实时渲染，不预渲染（feedback/docs/06 第 11.5 节）
export const instant = false

export const metadata: Metadata = {
  title: t('me.title'),
  robots: { index: false, follow: false },
}

export default async function MePage() {
  const user = await getCurrentUser()

  // 不 redirect 到登录：这是底部导航的一个入口，直接解释要登录比把人弹走更友好
  if (!user) {
    return (
      <Container className="py-12">
        <EmptyState
          icon={Inbox}
          title={t('me.needLogin')}
          hint={t('me.needLoginHint')}
          action={
            <div className="flex gap-3">
              <Button asChild>
                <Link href="/login?next=/me">{t('nav.login')}</Link>
              </Button>
              <Button asChild variant="outline">
                <Link href="/register">{t('nav.register')}</Link>
              </Button>
            </div>
          }
        />
      </Container>
    )
  }

  const now = await requestNow()
  const [projects, feedback, leaderboard] = await Promise.all([
    listMyProjects(),
    listMyFeedback({ page_size: 10 }),
    listLeaderboard(10),
  ])

  const name = user.nickname || user.handle || ''

  return (
    <Container className="flex flex-col gap-10 py-8 sm:py-10">
      <header className="flex flex-wrap items-center gap-4">
        <Avatar name={name} seed={user.tester_no ?? 0} size="lg" />
        <div className="min-w-0">
          <h1 className="text-xl font-semibold tracking-tight text-fg sm:text-2xl">{name}</h1>
          <p className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-fg-muted">
            {user.tester_no ? (
              <span className="tabular">
                {t('me.testerNo')} {user.tester_no}
              </span>
            ) : null}
            {user.created_at && <span>{t('me.joinedAt', { when: formatDate(user.created_at) })}</span>}
          </p>
        </div>
        <div className="ms-auto flex items-center gap-3">
          <Card padding="sm" className="min-w-28 text-center">
            <p className="text-xl font-semibold text-fg tabular">{formatCount(user.points ?? 0)}</p>
            <p className="mt-0.5 text-2xs text-fg-muted">{t('me.points')}</p>
          </Card>
          <Button asChild variant="outline" size="sm">
            <Link href="/feedback/new">
              <Plus aria-hidden />
              {t('action.newFeedback')}
            </Link>
          </Button>
        </div>
      </header>

      <p className="-mt-6 text-xs text-fg-subtle">{t('me.pointsHint')}</p>

      <section aria-labelledby="mp-heading">
        <SectionHeading title={t('me.myProjects')} moreHref="/projects" />
        <div id="mp-heading" className="sr-only">
          {t('me.myProjects')}
        </div>
        {projects.length > 0 ? (
          <div className="grid auto-rows-fr gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {projects.map((project) => (
              <ProjectCard key={project.id} project={project} />
            ))}
          </div>
        ) : (
          <EmptyState
            icon={LayoutGrid}
            title={t('me.myProjectsEmpty')}
            action={
              <Button asChild size="sm">
                <Link href="/projects">{t('action.browseProjects')}</Link>
              </Button>
            }
          />
        )}
      </section>

      <div className="grid gap-10 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <section aria-labelledby="mf-heading">
          <SectionHeading title={t('me.myFeedback')} />
          <div id="mf-heading" className="sr-only">
            {t('me.myFeedback')}
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
              title={t('me.myFeedbackEmpty')}
              hint={t('me.myFeedbackEmptyHint')}
              action={
                <Button asChild size="sm">
                  <Link href="/feedback/new">{t('action.newFeedback')}</Link>
                </Button>
              }
            />
          )}
        </section>

        <section aria-labelledby="lb-heading">
          <SectionHeading title={t('me.leaderboard')} hint={t('me.leaderboardHint')} />
          <div id="lb-heading" className="sr-only">
            {t('me.leaderboard')}
          </div>
          {leaderboard.length > 0 ? (
            <ol className="flex flex-col gap-1">
              {leaderboard.map((entry, index) => {
                const self = entry.user_id === user.id
                const who = entry.nickname || entry.handle || ''
                return (
                  <li
                    key={entry.user_id}
                    className={cn(
                      'flex items-center gap-3 rounded-control px-3 py-2.5',
                      self ? 'bg-primary-surface' : 'bg-surface',
                    )}
                  >
                    <span
                      className="w-6 shrink-0 text-center text-sm text-fg-subtle tabular"
                      aria-label={t('me.rank', { n: index + 1 })}
                    >
                      {index + 1}
                    </span>
                    <Avatar name={who} seed={entry.user_id ?? 0} size="sm" />
                    <span className="min-w-0 flex-1 truncate text-sm text-fg">
                      {who}
                      {self && <span className="ms-1.5 text-xs text-primary-fg">{t('me.you')}</span>}
                    </span>
                    <span className="inline-flex shrink-0 items-center gap-1 text-sm text-fg-muted tabular">
                      <Sparkles className="size-3.5" aria-hidden />
                      {formatCount(entry.points ?? 0)}
                    </span>
                  </li>
                )
              })}
            </ol>
          ) : (
            <EmptyState icon={Trophy} title={t('me.leaderboardEmpty')} />
          )}
        </section>
      </div>
    </Container>
  )
}
