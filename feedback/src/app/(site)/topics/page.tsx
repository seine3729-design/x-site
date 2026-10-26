import type { Metadata } from 'next'
import Link from 'next/link'
import { MessagesSquare } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { Pagination } from '@/components/ui/pagination'
import { Container } from '@/components/ui/section'
import { TopicCard } from '@/features/topics/components/topic-card'
import { listProjects, listTopics } from '@/lib/api/queries'
import { cn } from '@/lib/utils'
import { t } from '@/lib/i18n'
import { requestNow } from '@/lib/server/now'

// 这一页依赖会话或查询参数，按访问者实时渲染，不预渲染（feedback/docs/06 第 11.5 节）
export const instant = false

export const metadata: Metadata = {
  title: t('list.topicsTitle'),
  description: t('list.topicsSubtitle'),
}

const PAGE_SIZE = 12
const TABS = [
  { value: '', label: t('list.allTopicStatus') },
  { value: 'open', label: t('list.topicOpen') },
  { value: 'closed', label: t('list.topicClosed') },
] as const

type Search = Record<string, string | string[] | undefined>

function one(params: Search, key: string): string {
  const value = params[key]
  return (Array.isArray(value) ? value[0] : value) ?? ''
}

export default async function TopicsPage({ searchParams }: { searchParams: Promise<Search> }) {
  const params = await searchParams
  const page = Math.max(1, Number(one(params, 'page')) || 1)
  const statusParam = one(params, 'status')
  const status = statusParam === 'open' || statusParam === 'closed' ? statusParam : undefined
  const projectSlug = one(params, 'project')

  const now = await requestNow()
  const projects = await listProjects({ page_size: 100 })
  const project = projects.list.find((p) => p.slug === projectSlug)

  const result = await listTopics({
    project_id: project?.id,
    status,
    page,
    page_size: PAGE_SIZE,
  })

  const hrefFor = (next: number, nextStatus = statusParam) => {
    const sp = new URLSearchParams()
    if (projectSlug) sp.set('project', projectSlug)
    if (nextStatus) sp.set('status', nextStatus)
    if (next > 1) sp.set('page', String(next))
    const query = sp.toString()
    return query ? `/topics?${query}` : '/topics'
  }

  return (
    <Container className="flex flex-col gap-6 py-8 sm:py-10">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight text-fg sm:text-3xl">
          {t('list.topicsTitle')}
        </h1>
        <p className="mt-2 max-w-prose text-sm text-fg-muted sm:text-base">
          {t('list.topicsSubtitle')}
        </p>
      </header>

      {/* 状态只有三个取值，用标签页比下拉更直接；当前项用 aria-current 标出 */}
      <nav aria-label={t('list.status')} className="flex gap-1 border-b border-border">
        {TABS.map((tab) => {
          const active = statusParam === tab.value
          return (
            <Link
              key={tab.value || 'all'}
              href={hrefFor(1, tab.value)}
              aria-current={active ? 'page' : undefined}
              className={cn(
                '-mb-px border-b-2 px-3 py-2.5 text-sm transition-colors duration-(--duration-hover)',
                active
                  ? 'border-primary font-semibold text-fg'
                  : 'border-transparent text-fg-muted hover:text-fg',
              )}
            >
              {tab.label}
            </Link>
          )
        })}
      </nav>

      {result.list.length > 0 ? (
        <>
          <div className="grid auto-rows-fr gap-4 md:grid-cols-2">
            {result.list.map((topic) => (
              <TopicCard key={topic.id} topic={topic} now={now} />
            ))}
          </div>
          <Pagination
            page={result.page}
            pageSize={result.page_size}
            total={result.total}
            hrefFor={(p) => hrefFor(p)}
          />
        </>
      ) : (
        <EmptyState
          icon={MessagesSquare}
          title={t('list.topicsEmpty')}
          hint={t('list.topicsEmptyHint')}
          action={
            statusParam ? (
              <Button asChild variant="outline" size="sm">
                <Link href="/topics">{t('action.reset')}</Link>
              </Button>
            ) : undefined
          }
        />
      )}
    </Container>
  )
}
