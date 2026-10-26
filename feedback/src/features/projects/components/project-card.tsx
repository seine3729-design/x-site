import Link from 'next/link'
import { ArrowRight, Check, Inbox, Users } from 'lucide-react'
import { ProjectStatusBadge } from '@/components/domain/badges'
import { Badge } from '@/components/ui/badge'
import { Card, CardTitle } from '@/components/ui/card'
import { formatCount } from '@/lib/format'
import { t } from '@/lib/i18n'
import type { FbProjectView } from '@/lib/api/types'

/**
 * 项目卡片（feedback/docs/06 第 3.2 节）。
 *
 * 一眼要能看出三件事：这是什么项目、现在什么状态、我能不能进去。
 * 「已加入」用图标 + 文字标明，不只用颜色；加入方式（仅邀请/需审核）
 * 在这里就说清楚，避免用户点进去才发现进不去。
 */
export function ProjectCard({ project }: { project: FbProjectView }) {
  const joinable = project.status === 'recruiting' || project.status === 'active'
  return (
    <Card interactive padding="none" className="group flex h-full flex-col">
      <Link
        href={`/projects/${project.slug}`}
        className="flex flex-1 flex-col rounded-surface p-5"
      >
        <div className="flex items-start justify-between gap-3">
          <CardTitle className="group-hover:text-primary-fg">{project.name}</CardTitle>
          {project.joined ? (
            <Badge tone="success">
              <Check aria-hidden />
              {t('action.joined')}
            </Badge>
          ) : null}
        </div>

        {project.summary && (
          <p className="mt-2 line-clamp-2 text-sm text-fg-muted">{project.summary}</p>
        )}

        <div className="mt-4 flex flex-wrap items-center gap-2">
          {project.status && <ProjectStatusBadge status={project.status} />}
          {!project.joined && joinable && project.join_mode !== 'open' && (
            <Badge tone="outline">{t(`joinMode.${project.join_mode}` as 'joinMode.open')}</Badge>
          )}
        </div>

        <div className="mt-4 flex items-center gap-4 border-t border-border pt-4 text-xs text-fg-muted">
          <span className="inline-flex items-center gap-1.5">
            <Users className="size-3.5" aria-hidden />
            <span className="tabular">{formatCount(project.member_count ?? 0)}</span>
            <span>成员</span>
          </span>
          <span className="inline-flex items-center gap-1.5">
            <Inbox className="size-3.5" aria-hidden />
            <span className="tabular">{formatCount(project.feedback_count ?? 0)}</span>
            <span>反馈</span>
          </span>
          <ArrowRight
            className="ms-auto size-4 text-fg-subtle transition-transform duration-(--duration-hover) group-hover:translate-x-0.5 group-hover:text-primary-fg"
            aria-hidden
          />
        </div>
      </Link>
    </Card>
  )
}
