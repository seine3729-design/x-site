import { Badge } from '@/components/ui/badge'
import {
  KIND_LABELS,
  POST_STATUS_LABELS,
  PRIORITY_LABELS,
  PROJECT_STATUS_LABELS,
  SOURCE_LABELS,
  STATUS_LABELS,
  TOPIC_STATUS_LABELS,
  USER_STATUS_LABELS,
  VISIBILITY_LABELS,
  type FbKind,
  type FbPostStatus,
  type FbPriority,
  type FbProjectStatus,
  type FbSource,
  type FbStatus,
  type FbTopicStatus,
  type FbUserStatus,
  type FbVisibility,
} from '../api'

type Variant = 'default' | 'secondary' | 'outline' | 'destructive'

/** 状态徽标：文字始终可读，颜色只是辅助（不单独用颜色表达含义）。 */
export function StatusBadge({ status }: { status: FbStatus }) {
  const variant: Record<FbStatus, Variant> = {
    submitted: 'default',
    triaged: 'secondary',
    planned: 'secondary',
    in_progress: 'secondary',
    verifying: 'secondary',
    shipped: 'outline',
    closed: 'outline',
    duplicate: 'outline',
    declined: 'destructive',
  }
  return <Badge variant={variant[status]}>{STATUS_LABELS[status]}</Badge>
}

export function KindBadge({ kind }: { kind: FbKind }) {
  return (
    <Badge variant='outline' className='font-normal'>
      {KIND_LABELS[kind]}
    </Badge>
  )
}

export function PriorityBadge({ priority }: { priority: FbPriority }) {
  if (priority === 'unset') return <span className='text-muted-foreground'>—</span>
  const variant: Record<Exclude<FbPriority, 'unset'>, Variant> = {
    critical: 'destructive',
    high: 'default',
    medium: 'secondary',
    low: 'outline',
  }
  return <Badge variant={variant[priority]}>{PRIORITY_LABELS[priority]}</Badge>
}

/** 可见性：私密与仅成员要显眼，避免误把内部内容当公开内容处理。 */
export function VisibilityBadge({ visibility }: { visibility: FbVisibility }) {
  if (visibility === 'public') {
    return <span className='text-muted-foreground'>{VISIBILITY_LABELS.public}</span>
  }
  return <Badge variant={visibility === 'private' ? 'destructive' : 'secondary'}>{VISIBILITY_LABELS[visibility]}</Badge>
}

export function ProjectStatusBadge({ status }: { status: FbProjectStatus }) {
  const variant: Record<FbProjectStatus, Variant> = {
    draft: 'outline',
    recruiting: 'default',
    active: 'default',
    paused: 'secondary',
    archived: 'outline',
  }
  return <Badge variant={variant[status]}>{PROJECT_STATUS_LABELS[status]}</Badge>
}

export function PostStatusBadge({ status }: { status: FbPostStatus }) {
  const variant: Record<FbPostStatus, Variant> = { draft: 'outline', published: 'default', hidden: 'secondary' }
  return <Badge variant={variant[status]}>{POST_STATUS_LABELS[status]}</Badge>
}

export function UserStatusBadge({ status }: { status: FbUserStatus }) {
  const variant: Record<FbUserStatus, Variant> = { active: 'default', restricted: 'secondary', suspended: 'destructive' }
  return <Badge variant={variant[status]}>{USER_STATUS_LABELS[status]}</Badge>
}

export function TopicStatusBadge({ status }: { status: FbTopicStatus }) {
  const variant: Record<FbTopicStatus, Variant> = {
    draft: 'outline',
    open: 'default',
    closed: 'secondary',
    archived: 'outline',
  }
  return <Badge variant={variant[status]}>{TOPIC_STATUS_LABELS[status]}</Badge>
}

/** 来源：外部网站送进来的要显眼，运营分诊时需要知道这条不是社区成员写的。 */
export function SourceBadge({ source }: { source: FbSource }) {
  if (source === 'community') return <span className='text-muted-foreground'>{SOURCE_LABELS.community}</span>
  return <Badge variant='secondary'>{SOURCE_LABELS.open_api}</Badge>
}
