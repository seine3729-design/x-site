import Link from 'next/link'
import { MessageSquare, ThumbsUp } from 'lucide-react'
import { KindBadge, StatusBadge, VisibilityBadge } from '@/components/domain/badges'
import { Card } from '@/components/ui/card'
import { formatCount, formatDateTime, formatRelative } from '@/lib/format'
import type { FbFeedback } from '@/lib/api/types'

/** 反馈详情页的地址。编号对用户是 KEY-数字，路由用「项目标识 / 编号」两段，可深链可分享。 */
export function feedbackHref(item: FbFeedback): string {
  return `/feedback/${item.project_slug ?? ''}/${item.number ?? 0}`
}

/**
 * 反馈卡片（feedback/docs/06 第 3.4 节）。
 *
 * 几个刻意的选择：
 * - 标题换行而不是截断：移动端截成半句话就看不懂是什么问题了；
 * - 整卡可点击，但内部不再放第二个链接或按钮，避免嵌套交互；
 * - 时间同时给相对值（可读）和绝对值（title/dateTime，供屏幕阅读器与精确查看）；
 * - 编号用等宽 + tabular，一列对齐时不会抖。
 */
export function FeedbackCard({ item, now }: { item: FbFeedback; now: number }) {
  const absolute = formatDateTime(item.last_activity_at)
  return (
    <Card interactive padding="none" className="group">
      <Link href={feedbackHref(item)} className="block rounded-surface p-4 sm:p-5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-2xs text-fg-subtle tabular">{item.key}</span>
          {item.status && <StatusBadge status={item.status} />}
          {item.kind && <KindBadge kind={item.kind} />}
          {item.visibility && <VisibilityBadge visibility={item.visibility} />}
        </div>

        <h3 className="mt-2.5 text-base leading-snug font-medium text-fg group-hover:text-primary-fg">
          {item.title}
        </h3>

        <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-fg-muted">
          <span className="truncate">{item.project_name}</span>
          <span aria-hidden>·</span>
          <time dateTime={item.last_activity_at} title={absolute}>
            {formatRelative(item.last_activity_at, now)}更新
          </time>
          {(item.vote_count ?? 0) > 0 && (
            <>
              <span aria-hidden>·</span>
              <span className="inline-flex items-center gap-1">
                <ThumbsUp className="size-3.5" aria-hidden />
                <span className="tabular">{formatCount(item.vote_count ?? 0)}</span>
                <span className="sr-only">人也遇到</span>
              </span>
            </>
          )}
        </div>
      </Link>
    </Card>
  )
}

/** 紧凑版：首页、项目页的侧边摘要用，只保留状态与标题。 */
export function FeedbackRow({ item }: { item: FbFeedback }) {
  return (
    <Link
      href={feedbackHref(item)}
      className="flex items-start gap-3 rounded-control px-2 py-2.5 transition-colors duration-(--duration-hover) hover:bg-surface-raised"
    >
      <MessageSquare className="mt-0.5 size-4 shrink-0 text-fg-subtle" aria-hidden />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm text-fg">{item.title}</span>
        <span className="mt-1 flex items-center gap-2">
          {item.status && <StatusBadge status={item.status} />}
        </span>
      </span>
    </Link>
  )
}
