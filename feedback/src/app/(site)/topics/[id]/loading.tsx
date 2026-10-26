import { Container } from '@/components/ui/section'
import { Skeleton, SkeletonArticle, SkeletonRegion } from '@/components/ui/skeleton'

/** 议题详情的加载态：正文 + 回答列表。 */
export default function Loading() {
  return (
    <Container className="flex flex-col gap-8 py-8 sm:py-10">
      <SkeletonRegion className="flex flex-col gap-8" label="议题正在加载">
        <Skeleton className="h-4 w-48" />
        <SkeletonArticle />
        <div className="flex flex-col gap-5">
          <Skeleton className="h-5 w-28" />
          {Array.from({ length: 3 }, (_, i) => (
            <div key={i} className="flex gap-3">
              <Skeleton className="size-7 shrink-0 rounded-pill" />
              <div className="flex min-w-0 flex-1 flex-col gap-2">
                <Skeleton className="h-4 w-32" />
                <Skeleton className="h-4 w-full max-w-md" />
                <Skeleton className="h-3.5 w-24" />
              </div>
            </div>
          ))}
        </div>
      </SkeletonRegion>
    </Container>
  )
}
