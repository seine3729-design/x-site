import Link from 'next/link'
import { ArrowRight, MessageCircle, Pin } from 'lucide-react'
import { TopicStatusBadge } from '@/components/domain/badges'
import { Badge } from '@/components/ui/badge'
import { Card, CardTitle } from '@/components/ui/card'
import { formatDate, formatRemaining } from '@/lib/format'
import { t } from '@/lib/i18n'
import type { FbTopic } from '@/lib/api/types'

/**
 * 议题卡片（feedback/docs/08 第 8.5 节）。
 *
 * 议题是运营抛出的一个具体问题，所以卡片的重心是问题本身：
 * 标题给到接近小标题的字号，状态与项目放在上方做上下文，
 * 回答数与截止时间放在底部——它们影响「要不要现在参与」。
 */
export function TopicCard({ topic, now }: { topic: FbTopic; now: number }) {
  const remaining = topic.status === 'open' ? formatRemaining(topic.ends_at, now) : ''
  const answers = topic.answer_count ?? 0
  return (
    <Card interactive padding="none" className="group flex h-full flex-col">
      <Link href={`/topics/${topic.id}`} className="flex flex-1 flex-col rounded-surface p-5">
        <div className="flex flex-wrap items-center gap-2">
          {topic.project_name && <Badge tone="outline">{topic.project_name}</Badge>}
          {topic.status && <TopicStatusBadge status={topic.status} />}
          {topic.pinned && (
            <span className="inline-flex items-center gap-1 text-2xs text-fg-subtle">
              <Pin className="size-3" aria-hidden />
              置顶
            </span>
          )}
        </div>

        <CardTitle className="mt-3 text-lg leading-snug group-hover:text-primary-fg">
          {topic.title}
        </CardTitle>

        {topic.body && <p className="mt-2 line-clamp-2 text-sm text-fg-muted">{topic.body}</p>}

        <div className="mt-4 flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-border pt-4 text-xs text-fg-muted">
          <span className="inline-flex items-center gap-1.5">
            <MessageCircle className="size-3.5" aria-hidden />
            <span className="tabular">{answers}</span>
            <span>个回答</span>
          </span>
          {remaining && (
            <>
              <span aria-hidden>·</span>
              <span className="text-warning-fg">{remaining}</span>
            </>
          )}
          {!remaining && topic.published_at && (
            <>
              <span aria-hidden>·</span>
              <time dateTime={topic.published_at}>{formatDate(topic.published_at)}</time>
            </>
          )}
          <span className="ms-auto inline-flex items-center gap-1 font-medium text-primary-fg">
            {topic.status === 'open' ? t('action.answer') : t('action.viewDetail')}
            <ArrowRight
              className="size-3.5 transition-transform duration-(--duration-hover) group-hover:translate-x-0.5"
              aria-hidden
            />
          </span>
        </div>
      </Link>
    </Card>
  )
}
