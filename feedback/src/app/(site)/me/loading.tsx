import { Container } from '@/components/ui/section'
import {
  Skeleton,
  SkeletonCardGrid,
  SkeletonList,
  SkeletonRegion,
} from '@/components/ui/skeleton'

/**
 * 「我的」页加载态。
 *
 * 页头是头像 + 名字 + 贡献值卡片，和列表页的页头不一样，所以这里单独拼，
 * 不复用 SkeletonPageHeader。
 */
export default function Loading() {
  return (
    <Container className="flex flex-col gap-10 py-8 sm:py-10">
      <SkeletonRegion className="flex flex-col gap-10" label="个人页正在加载">
        <div className="flex flex-wrap items-center gap-4">
          {/* size-12 对应 Avatar size="lg" */}
          <Skeleton className="size-12 rounded-pill" />
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <Skeleton className="h-7 w-32" />
            <Skeleton className="h-4 w-48" />
          </div>
          <Skeleton className="h-20 w-28 rounded-surface" />
        </div>

        <SkeletonCardGrid count={3} />

        <div className="grid gap-10 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
          <SkeletonList rows={4} />
          <div className="flex flex-col gap-3">
            {Array.from({ length: 5 }, (_, i) => (
              <div key={i} className="flex items-center gap-3">
                <Skeleton className="size-7 rounded-pill" />
                <Skeleton className="h-4 min-w-0 flex-1" />
                <Skeleton className="h-4 w-10" />
              </div>
            ))}
          </div>
        </div>
      </SkeletonRegion>
    </Container>
  )
}
