import { Container } from '@/components/ui/section'
import { SkeletonCardGrid, SkeletonPageHeader, SkeletonRegion } from '@/components/ui/skeleton'

/** 项目目录的加载态。容器 gap/py 对齐 page.tsx 的 `gap-10 py-8 sm:py-10`。 */
export default function Loading() {
  return (
    <Container className="flex flex-col gap-10 py-8 sm:py-10">
      <SkeletonRegion className="flex flex-col gap-10" label="项目列表正在加载">
        <SkeletonPageHeader />
        <SkeletonCardGrid count={6} />
      </SkeletonRegion>
    </Container>
  )
}
