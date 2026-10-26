import { Container } from '@/components/ui/section'
import { Skeleton, SkeletonArticle, SkeletonRegion } from '@/components/ui/skeleton'

/** 动态详情的加载态。正文宽度跟真实页面一样收在 prose 栏里。 */
export default function Loading() {
  return (
    <Container className="flex flex-col gap-8 py-8 sm:py-10">
      <SkeletonRegion className="flex max-w-prose flex-col gap-8" label="动态正在加载">
        <Skeleton className="h-4 w-40" />
        <SkeletonArticle />
      </SkeletonRegion>
    </Container>
  )
}
