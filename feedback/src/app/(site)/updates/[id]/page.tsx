import type { Metadata } from 'next'
import Link from 'next/link'
import { notFound } from 'next/navigation'
import { ChevronLeft, Users } from 'lucide-react'
import { PostKindBadge } from '@/components/domain/badges'
import { Alert } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Container } from '@/components/ui/section'
import { getPost } from '@/lib/api/queries'
import { formatDateTime, formatRelative } from '@/lib/format'
import { t } from '@/lib/i18n'
import { requestNow } from '@/lib/server/now'

type Params = { params: Promise<{ id: string }> }

// 这一页依赖会话或查询参数，按访问者实时渲染，不预渲染（feedback/docs/06 第 11.5 节）
export const instant = false

export async function generateMetadata({ params }: Params): Promise<Metadata> {
  const { id } = await params
  const post = await getPost(Number(id))
  if (!post) return { title: t('post.notFound'), robots: { index: false } }
  return {
    title: post.title,
    description: post.visibility === 'public' ? post.title : undefined,
    robots: post.visibility === 'public' ? undefined : { index: false, follow: false },
  }
}

export default async function PostDetailPage({ params }: Params) {
  const { id } = await params
  const postId = Number(id)
  if (!Number.isInteger(postId) || postId <= 0) notFound()

  const [post, now] = await Promise.all([getPost(postId), requestNow()])
  // 不存在与无权查看都是 404，不暴露仅成员动态是否存在
  if (!post) notFound()

  const published = post.published_at ?? post.created_at

  return (
    <Container className="py-6 sm:py-8">
      <div className="mx-auto max-w-prose">
        <Link
          href={`/projects/${post.project_slug ?? ''}`}
          className="inline-flex items-center gap-1 rounded-control py-1 text-sm text-fg-muted hover:text-fg hover:underline"
        >
          <ChevronLeft className="size-4" aria-hidden />
          {post.project_name}
        </Link>

        <div className="mt-4 flex flex-wrap items-center gap-2">
          {post.kind && <PostKindBadge kind={post.kind} size="md" />}
          {post.visibility === 'members' && (
            <Badge tone="neutral" size="md">
              <Users aria-hidden />
              {t('visibility.membersShort')}
            </Badge>
          )}
        </div>

        <h1 className="mt-3 text-xl leading-snug font-semibold tracking-tight text-fg sm:text-2xl">
          {post.title}
        </h1>

        <p className="mt-2 text-xs text-fg-muted">
          <time dateTime={published} title={formatDateTime(published)}>
            {t('post.publishedAt', { when: formatRelative(published, now) })}
          </time>
        </p>

        {post.visibility === 'members' && (
          <Alert tone="info" className="mt-5">
            {t('post.membersHint')}
          </Alert>
        )}

        <div className="mt-6 text-sm leading-relaxed text-fg user-text">{post.body}</div>
      </div>
    </Container>
  )
}
