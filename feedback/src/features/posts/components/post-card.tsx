import Link from 'next/link'
import { Pin } from 'lucide-react'
import { PostKindBadge } from '@/components/domain/badges'
import { Card, CardTitle } from '@/components/ui/card'
import { formatDateTime, formatRelative } from '@/lib/format'
import type { FbPost } from '@/lib/api/types'

/**
 * 项目动态卡片：公告、测试任务、上线小结。
 * 置顶用图标 + 可访问名标出，不只靠排序位置暗示。
 */
export function PostCard({ post, now }: { post: FbPost; now: number }) {
  const published = post.published_at ?? post.created_at
  return (
    <Card interactive padding="none" className="group">
      <Link href={`/updates/${post.id}`} className="block rounded-surface p-5">
        <div className="flex flex-wrap items-center gap-2">
          {post.kind && <PostKindBadge kind={post.kind} />}
          {post.pinned && (
            <span className="inline-flex items-center gap-1 text-2xs text-fg-subtle">
              <Pin className="size-3" aria-hidden />
              置顶
            </span>
          )}
        </div>
        <CardTitle className="mt-2.5 group-hover:text-primary-fg">{post.title}</CardTitle>
        {post.body && <p className="mt-2 line-clamp-2 text-sm text-fg-muted">{post.body}</p>}
        <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-fg-muted">
          <span className="truncate">{post.project_name}</span>
          <span aria-hidden>·</span>
          <time dateTime={published} title={formatDateTime(published)}>
            {formatRelative(published, now)}
          </time>
        </div>
      </Link>
    </Card>
  )
}
