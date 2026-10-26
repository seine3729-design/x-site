import { cn } from '@/lib/utils'

/**
 * 骨架屏（feedback/docs/06 第 5 节）。
 *
 * 原则只有一条：**骨架的尺寸要和最终内容一致**。
 * 骨架比真实内容矮，内容到位时页面会往下跳一截，那是实打实的 CLS，
 * 比不放骨架更糟。所以下面每个组件的高度都是按对应的真实组件量出来的，
 * 改动真实组件时要一起改。
 *
 * 动效：`animate-pulse` 在 `prefers-reduced-motion` 下由 globals.css 统一关掉，
 * 骨架会变成静止的灰块 —— 这是预期行为，不需要在这里再判断一次。
 *
 * 无障碍：单个色块一律 `aria-hidden`，由最外层容器统一挂一个 `role="status"`，
 * 否则屏幕阅读器会把十几个空 div 逐个读出来。
 */
export function Skeleton({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      className={cn('animate-pulse rounded-control bg-surface-sunken', className)}
      aria-hidden
      {...props}
    />
  )
}

/**
 * 加载区域的外壳：把一组骨架标成一个「正在加载」的状态区。
 *
 * aria-busy 让辅助技术知道这块内容还在变化中；label 只读一次，
 * 不会把里面每个色块都念出来。
 */
export function SkeletonRegion({
  className,
  children,
  label = '内容正在加载',
}: {
  className?: string
  children: React.ReactNode
  label?: string
}) {
  return (
    <div className={className} role="status" aria-busy="true" aria-label={label}>
      {children}
    </div>
  )
}

/** 一段文本的骨架。行高 h-4 对应 text-sm/text-base 的实际行高。 */
export function SkeletonText({
  lines = 1,
  className,
  lastWidth = '60%',
}: {
  lines?: number
  className?: string
  /** 最后一行的宽度。真实段落的末行通常不满宽，骨架照抄这个观感。 */
  lastWidth?: string
}) {
  return (
    <div className={cn('flex flex-col gap-2', className)}>
      {Array.from({ length: lines }, (_, i) => (
        <Skeleton
          key={i}
          className="h-4"
          style={i === lines - 1 && lines > 1 ? { width: lastWidth } : undefined}
        />
      ))}
    </div>
  )
}

/** 页头骨架：对应各列表页的「大标题 + 一行说明」。 */
export function SkeletonPageHeader({ withAction = false }: { withAction?: boolean }) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="flex min-w-0 flex-1 flex-col gap-3">
        {/* h-8 对应 text-2xl 标题 */}
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-4 w-full max-w-md" />
      </div>
      {/* 对应页头右侧的主操作按钮（h-10 = size default） */}
      {withAction && <Skeleton className="h-10 w-28 shrink-0 rounded-control" />}
    </div>
  )
}

/**
 * 筛选条骨架：一个搜索框 + 若干下拉。
 *
 * 移动端只显示搜索框与一个筛选按钮（真实组件里下拉收进抽屉），
 * 所以这里的下拉也跟着 `hidden sm:flex`，不然小屏会多出几个凭空的灰块。
 */
export function SkeletonFilters({ selects = 4 }: { selects?: number }) {
  return (
    <div className="flex flex-col gap-3">
      <div className="flex gap-2">
        <Skeleton className="h-11 min-w-0 flex-1" />
        <Skeleton className="size-11 shrink-0" />
      </div>
      <div className="hidden gap-3 sm:flex">
        {Array.from({ length: selects }, (_, i) => (
          <div key={i} className="flex flex-col gap-1.5">
            <Skeleton className="h-4 w-12" />
            <Skeleton className="h-11 w-36" />
          </div>
        ))}
      </div>
    </div>
  )
}

/** 列表骨架：对应 FeedbackCard 那种一行一条的紧凑列表。 */
export function SkeletonList({ rows = 5, className }: { rows?: number; className?: string }) {
  return (
    <div className={cn('flex flex-col gap-3', className)}>
      {Array.from({ length: rows }, (_, i) => (
        <div
          key={i}
          className="flex items-start gap-3 rounded-surface border border-border bg-surface p-4"
        >
          <Skeleton className="mt-0.5 size-4 shrink-0 rounded-sm" />
          <div className="flex min-w-0 flex-1 flex-col gap-2.5">
            <div className="flex items-center gap-2">
              <Skeleton className="h-5 w-16 rounded-pill" />
              <Skeleton className="h-5 w-20 rounded-pill" />
            </div>
            <Skeleton className="h-5 w-full max-w-sm" />
            <Skeleton className="h-3.5 w-40" />
          </div>
        </div>
      ))}
    </div>
  )
}

/** 卡片网格骨架：对应项目卡 / 动态卡那种多列网格。 */
export function SkeletonCardGrid({
  count = 6,
  className = 'grid gap-4 sm:grid-cols-2 lg:grid-cols-3',
}: {
  count?: number
  className?: string
}) {
  return (
    <div className={className}>
      {Array.from({ length: count }, (_, i) => (
        <div
          key={i}
          className="flex flex-col gap-3 rounded-surface border border-border bg-surface p-5"
        >
          <Skeleton className="h-5 w-24 rounded-pill" />
          <Skeleton className="h-6 w-32" />
          <SkeletonText lines={2} />
          <Skeleton className="h-3.5 w-28" />
        </div>
      ))}
    </div>
  )
}

/** 正文骨架：对应反馈详情、议题详情的长文区。 */
export function SkeletonArticle() {
  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center gap-2">
        <Skeleton className="h-5 w-16 rounded-pill" />
        <Skeleton className="h-5 w-20 rounded-pill" />
      </div>
      <Skeleton className="h-9 w-full max-w-lg" />
      <Skeleton className="h-4 w-48" />
      <SkeletonText lines={5} lastWidth="40%" />
    </div>
  )
}
