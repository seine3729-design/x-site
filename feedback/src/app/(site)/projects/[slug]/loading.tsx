import { Container } from '@/components/ui/section'
import {
  Skeleton,
  SkeletonCardGrid,
  SkeletonList,
  SkeletonRegion,
  SkeletonText,
} from '@/components/ui/skeleton'

/** 项目主页的加载态：项目头 + 动态 + 反馈列表。 */
export default function Loading() {
  return (
    <Container className="flex flex-col gap-10 py-8 sm:py-10">
      <SkeletonRegion className="flex flex-col gap-10" label="项目正在加载">
        <div className="flex flex-col gap-4">
          <div className="flex items-center gap-2">
            <Skeleton className="h-5 w-14 rounded-pill" />
            <Skeleton className="h-5 w-20 rounded-pill" />
          </div>
          <Skeleton className="h-8 w-48" />
          <SkeletonText lines={2} />
          {/* 加入按钮 */}
          <Skeleton className="h-11 w-28" />
        </div>
        <SkeletonCardGrid count={3} />
        <SkeletonList rows={4} />
      </SkeletonRegion>
    </Container>
  )
}
