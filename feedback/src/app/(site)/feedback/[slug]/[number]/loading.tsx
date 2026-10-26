import { Container } from '@/components/ui/section'
import { Skeleton, SkeletonArticle, SkeletonRegion } from '@/components/ui/skeleton'

/**
 * 反馈详情的加载态。
 *
 * 真实页面是「正文主栏 + 右侧状态时间线」的两列布局（桌面），
 * 骨架照搬同一套栅格，否则内容到位时左栏宽度会突然变化。
 */
export default function Loading() {
  return (
    <Container className="flex flex-col gap-8 py-8 sm:py-10">
      <SkeletonRegion className="flex flex-col gap-8" label="反馈详情正在加载">
        {/* 面包屑 */}
        <Skeleton className="h-4 w-56" />

        <div className="grid gap-10 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
          <SkeletonArticle />
          <div className="flex flex-col gap-4">
            <Skeleton className="h-5 w-24" />
            {/* 状态时间线：每条是一个圆点 + 两行文字 */}
            {Array.from({ length: 4 }, (_, i) => (
              <div key={i} className="flex gap-3">
                <Skeleton className="mt-1 size-3 shrink-0 rounded-pill" />
                <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                  <Skeleton className="h-4 w-28" />
                  <Skeleton className="h-3.5 w-20" />
                </div>
              </div>
            ))}
          </div>
        </div>
      </SkeletonRegion>
    </Container>
  )
}
