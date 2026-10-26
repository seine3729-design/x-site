import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { ArrowLeft, BarChart3, PlayCircle, Trash2 } from 'lucide-react'
import { HttpError } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import { NoPermission } from '@/components/module-placeholder'
import { Badge, Button, Skeleton, Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui'
import { LoadError, WithReason } from '@/features/access/shared'
import { dramaQuery, episodesQuery, type DramaDetail } from './api'
import { DramaStatusBadge } from './components/badges'
import { EpisodesSection } from './drama-episodes'
import { ProfileSection } from './drama-profile'
import { TagsSection, TermsSection } from './drama-relations'
import { DeleteDramaDialog } from './dramas-page'
import { episodeSummary } from './utils'

/** 地址栏里的分区（剧资料为默认，不写入地址栏）。 */
type SectionTab = 'tags' | 'terms' | 'episodes'

/** 标签、取值的内容变化后（保存成功），用新数据重新初始化分区的草稿。 */
const relationKey = (d: DramaDetail) => ({
  tags: d.tags.map((t) => t.id).join(','),
  terms: d.terms.map((t) => t.id).join(','),
})

/** 剧集编辑（docs/06 第 5 节）：剧资料、标签、维度取值、分集分别保存。 */
export function DramaEditPage() {
  const can = useCan()
  const visible = can('dramas.view')
  const { id: idParam } = useParams({ from: '/_authenticated/dramas/$id' })
  const { tab = 'profile' } = useSearch({ from: '/_authenticated/dramas/$id' })
  const navigate = useNavigate({ from: '/dramas/$id' })
  const id = Number(idParam)
  const valid = Number.isInteger(id) && id > 0
  const detail = useQuery({ ...dramaQuery(id), enabled: visible && valid })
  const episodes = useQuery({ ...episodesQuery(id), enabled: visible && valid })
  const [deleting, setDeleting] = useState(false)

  if (!visible) return <NoPermission title='剧集' />
  if (!valid || (detail.error instanceof HttpError && detail.error.status === 404)) {
    return (
      <div className='flex flex-col items-start gap-3'>
        <h1 className='text-2xl font-semibold'>剧不存在</h1>
        <p className='text-muted-foreground'>编号 {idParam} 的剧不存在，或已放进回收站。</p>
        <Button asChild variant='outline'>
          <Link to='/dramas'>
            <ArrowLeft /> 回到剧集列表
          </Link>
        </Button>
      </div>
    )
  }
  if (detail.isError) return <LoadError onRetry={() => void detail.refetch()} />
  if (detail.isPending) return <Skeleton className='h-96 w-full' />

  const d = detail.data
  const editable = can('dramas.edit')
  const keys = relationKey(d)

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div className='flex flex-col gap-1'>
          <Link to='/dramas' className='flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground'>
            <ArrowLeft className='size-4' aria-hidden /> 剧集列表
          </Link>
          <div className='flex flex-wrap items-center gap-2'>
            <h1 className='text-2xl font-semibold tracking-tight'>
              <span className='me-2 text-muted-foreground tabular-nums'>#{d.id}</span>
              {d.title}
            </h1>
            <DramaStatusBadge status={d.display_status} publishAt={d.publish_at} />
            {!editable && <Badge variant='secondary'>只读</Badge>}
          </div>
          <p className='text-sm text-muted-foreground'>{episodeSummary(d)}</p>
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          <WithReason reason='试播随 S1-8（签名播放清单）上线后提供'>
            <Button variant='outline' disabled>
              <PlayCircle /> 试播
            </Button>
          </WithReason>
          <WithReason reason='单剧分析随 A4-1 提供'>
            <Button variant='outline' disabled>
              <BarChart3 /> 看数据
            </Button>
          </WithReason>
          {editable && (
            <Button variant='destructive' onClick={() => setDeleting(true)}>
              <Trash2 /> 放进回收站
            </Button>
          )}
        </div>
      </div>

      <Tabs value={tab} onValueChange={(v) => void navigate({ search: { tab: v === 'profile' ? undefined : (v as SectionTab) } })}>
        <TabsList className='flex-wrap'>
          <TabsTrigger value='profile'>剧资料</TabsTrigger>
          <TabsTrigger value='tags'>标签（{d.tags.length}）</TabsTrigger>
          <TabsTrigger value='terms'>维度取值（{d.terms.length}）</TabsTrigger>
          <TabsTrigger value='episodes'>分集（{episodes.data?.length ?? '…'}）</TabsTrigger>
        </TabsList>
        <TabsContent value='profile' className='mt-4'>
          <ProfileSection drama={d} editable={editable} />
        </TabsContent>
        <TabsContent value='tags' className='mt-4'>
          <TagsSection key={keys.tags} drama={d} editable={editable} />
        </TabsContent>
        <TabsContent value='terms' className='mt-4'>
          <TermsSection key={keys.terms} drama={d} editable={editable} />
        </TabsContent>
        <TabsContent value='episodes' className='mt-4'>
          <EpisodesSection drama={d} editable={editable} />
        </TabsContent>
      </Tabs>

      {deleting && (
        <DeleteDramaDialog drama={d} onClose={() => setDeleting(false)} onDeleted={() => void navigate({ to: '/dramas' })} />
      )}
    </div>
  )
}
