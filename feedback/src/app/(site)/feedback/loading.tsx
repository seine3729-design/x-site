import { Container } from '@/components/ui/section'
import {
  SkeletonFilters,
  SkeletonList,
  SkeletonPageHeader,
  SkeletonRegion,
} from '@/components/ui/skeleton'

/**
 * 反馈列表的加载态。
 *
 * 容器的 gap 与 py 必须和 page.tsx 的 Container 完全一致
 * （`gap-6 py-8 sm:py-10`），否则内容到位时整页会上下跳一次。
 *
 * 行数取 5 而不是一页的 20：首屏看得见的大约就是 5 条，
 * 铺满 20 个灰块会让人以为内容已经加载完了。
 */
export default function Loading() {
  return (
    <Container className="flex flex-col gap-6 py-8 sm:py-10">
      <SkeletonRegion className="flex flex-col gap-6" label="反馈列表正在加载">
        <SkeletonPageHeader withAction />
        <SkeletonFilters selects={4} />
        <SkeletonList rows={5} />
      </SkeletonRegion>
    </Container>
  )
}
