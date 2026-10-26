import type { Metadata } from 'next'
import Link from 'next/link'
import { Inbox, Plus, SearchX } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { Pagination } from '@/components/ui/pagination'
import { Container } from '@/components/ui/section'
import { FeedbackCard } from '@/features/feedback/components/feedback-card'
import { FeedbackFilters } from '@/features/feedback/components/feedback-filters'
import { listFeedback, listProjects } from '@/lib/api/queries'
import { t } from '@/lib/i18n'
import { requestNow } from '@/lib/server/now'

// 这一页依赖会话或查询参数，按访问者实时渲染，不预渲染（feedback/docs/06 第 11.5 节）
export const instant = false

export const metadata: Metadata = {
  title: t('list.feedbackTitle'),
  description: t('list.feedbackSubtitle'),
}

const PAGE_SIZE = 20

type Search = Record<string, string | string[] | undefined>

function one(params: Search, key: string): string {
  const value = params[key]
  return (Array.isArray(value) ? value[0] : value) ?? ''
}

export default async function FeedbackListPage({
  searchParams,
}: {
  searchParams: Promise<Search>
}) {
  const params = await searchParams
  const page = Math.max(1, Number(one(params, 'page')) || 1)
  const projectSlug = one(params, 'project')
  const q = one(params, 'q')
  const status = one(params, 'status')
  const kind = one(params, 'kind')
  const sort = one(params, 'sort') || 'latest'

  const now = await requestNow()
  // 项目列表用来把地址里的 slug 换成后端要的 project_id，同时给筛选下拉提供选项
  const projects = await listProjects({ page_size: 100 })
  const project = projects.list.find((p) => p.slug === projectSlug)

  const result = await listFeedback({
    project_id: project?.id,
    q,
    status,
    kind,
    sort,
    page,
    page_size: PAGE_SIZE,
  })

  const filtered = !!(projectSlug || q || status || kind)

  /** 翻页时保留当前筛选，只换 page。 */
  const hrefFor = (next: number) => {
    const sp = new URLSearchParams()
    for (const [key, value] of Object.entries({ q, project: projectSlug, status, kind })) {
      if (value) sp.set(key, value)
    }
    if (sort !== 'latest') sp.set('sort', sort)
    if (next > 1) sp.set('page', String(next))
    const query = sp.toString()
    return query ? `/feedback?${query}` : '/feedback'
  }

  return (
    <Container className="flex flex-col gap-6 py-8 sm:py-10">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight text-fg sm:text-3xl">
            {t('list.feedbackTitle')}
          </h1>
          <p className="mt-2 max-w-prose text-sm text-fg-muted sm:text-base">
            {t('list.feedbackSubtitle')}
          </p>
        </div>
        <Button asChild size="lg" className="max-sm:hidden">
          <Link href="/feedback/new">
            <Plus aria-hidden />
            {t('action.newFeedback')}
          </Link>
        </Button>
      </header>

      <FeedbackFilters projects={projects.list} />

      {result.list.length > 0 ? (
        <>
          <div className="flex flex-col gap-3">
            {result.list.map((item) => (
              <FeedbackCard key={item.id} item={item} now={now} />
            ))}
          </div>
          <Pagination
            page={result.page}
            pageSize={result.page_size}
            total={result.total}
            hrefFor={hrefFor}
          />
        </>
      ) : filtered ? (
        // 搜索无结果与「一条都没有」是两种状态：前者要保留筛选并提示怎么调整
        <EmptyState
          icon={SearchX}
          title={t('list.noResult')}
          hint={t('list.noResultHint')}
          action={
            <Button asChild variant="outline" size="sm">
              <Link href="/feedback">{t('action.reset')}</Link>
            </Button>
          }
        />
      ) : (
        <EmptyState
          icon={Inbox}
          title={t('list.empty')}
          hint={t('list.emptyHint')}
          action={
            <Button asChild size="sm">
              <Link href="/feedback/new">{t('action.newFeedback')}</Link>
            </Button>
          }
        />
      )}
    </Container>
  )
}
