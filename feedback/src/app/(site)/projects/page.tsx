import type { Metadata } from 'next'
import { LayoutGrid } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { Pagination } from '@/components/ui/pagination'
import { Container } from '@/components/ui/section'
import { ProjectCard } from '@/features/projects/components/project-card'
import { listMyProjects, listProjects } from '@/lib/api/queries'
import { t } from '@/lib/i18n'
import { getCurrentUser } from '@/lib/server/session'

// 这一页依赖会话或查询参数，按访问者实时渲染，不预渲染（feedback/docs/06 第 11.5 节）
export const instant = false

export const metadata: Metadata = {
  title: t('project.listTitle'),
  description: t('project.listSubtitle'),
}

const PAGE_SIZE = 12

export default async function ProjectsPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>
}) {
  const params = await searchParams
  const page = Math.max(1, Number(Array.isArray(params.page) ? params.page[0] : params.page) || 1)

  const user = await getCurrentUser()
  const [all, mine] = await Promise.all([
    listProjects({ page, page_size: PAGE_SIZE }),
    // 「我加入的」只在第一页显示：翻页的人是在浏览全部目录，不需要每页都重复一遍
    user && page === 1 ? listMyProjects() : Promise.resolve([]),
  ])

  return (
    <Container className="flex flex-col gap-10 py-8 sm:py-10">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight text-fg sm:text-3xl">
          {t('project.listTitle')}
        </h1>
        <p className="mt-2 max-w-prose text-sm text-fg-muted sm:text-base">
          {t('project.listSubtitle')}
        </p>
      </header>

      {mine.length > 0 && (
        <section aria-labelledby="mine-heading">
          <h2 id="mine-heading" className="mb-4 text-lg font-semibold tracking-tight text-fg">
            {t('project.mineTitle')}
          </h2>
          <div className="grid auto-rows-fr gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {mine.map((project) => (
              <ProjectCard key={project.id} project={project} />
            ))}
          </div>
        </section>
      )}

      <section aria-labelledby="all-heading">
        {mine.length > 0 && (
          <h2 id="all-heading" className="mb-4 text-lg font-semibold tracking-tight text-fg">
            {t('project.allTitle')}
          </h2>
        )}
        {all.list.length > 0 ? (
          <>
            <div className="grid auto-rows-fr gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {all.list.map((project) => (
                <ProjectCard key={project.id} project={project} />
              ))}
            </div>
            <Pagination
              className="mt-8"
              page={all.page}
              pageSize={all.page_size}
              total={all.total}
              hrefFor={(p) => (p === 1 ? '/projects' : `/projects?page=${p}`)}
            />
          </>
        ) : (
          <EmptyState
            icon={LayoutGrid}
            title={t('project.empty')}
            hint={t('project.emptyHint')}
          />
        )}
      </section>
    </Container>
  )
}
