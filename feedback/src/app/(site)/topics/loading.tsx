import { Container } from '@/components/ui/section'
import {
  Skeleton,
  SkeletonCardGrid,
  SkeletonPageHeader,
  SkeletonRegion,
} from '@/components/ui/skeleton'

/** 议题区的加载态。真实页面在页头下方有三个状态标签页，所以骨架也留出那一行。 */
export default function Loading() {
  return (
    <Container className="flex flex-col gap-6 py-8 sm:py-10">
      <SkeletonRegion className="flex flex-col gap-6" label="议题列表正在加载">
        <SkeletonPageHeader />
        {/* 三个标签页（全部 / 征集中 / 已结束） */}
        <div className="flex gap-2">
          <Skeleton className="h-9 w-20 rounded-pill" />
          <Skeleton className="h-9 w-20 rounded-pill" />
          <Skeleton className="h-9 w-20 rounded-pill" />
        </div>
        <SkeletonCardGrid count={4} className="grid gap-4 sm:grid-cols-2" />
      </SkeletonRegion>
    </Container>
  )
}
