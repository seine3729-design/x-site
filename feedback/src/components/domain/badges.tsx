import {
  Archive,
  Bug,
  CalendarClock,
  CircleCheck,
  CircleSlash,
  CircleX,
  Copy,
  Eye,
  Globe,
  Hammer,
  Inbox,
  Lightbulb,
  Lock,
  Rocket,
  Users,
  WifiOff,
  type LucideIcon,
} from 'lucide-react'
import { Badge, type BadgeProps } from '@/components/ui/badge'
import { t } from '@/lib/i18n'
import type {
  FbKind,
  FbPostKind,
  FbProjectStatus,
  FbStatus,
  FbTopicStatus,
  FbVisibility,
} from '@/lib/api/types'

/**
 * 业务徽标。
 *
 * 每个徽标都是「图标 + 文字」：状态不能只靠颜色区分（WCAG 1.4.1），
 * 而且用户不该需要记住紫色代表什么。图标是装饰性的，对无障碍树隐藏。
 */

type Tone = NonNullable<BadgeProps['tone']>

const STATUS_STYLE: Record<FbStatus, { tone: Tone; icon: LucideIcon }> = {
  submitted: { tone: 'info', icon: Inbox },
  triaged: { tone: 'brand', icon: CircleCheck },
  planned: { tone: 'brand', icon: CalendarClock },
  in_progress: { tone: 'warning', icon: Hammer },
  verifying: { tone: 'warning', icon: Eye },
  shipped: { tone: 'success', icon: Rocket },
  closed: { tone: 'neutral', icon: CircleSlash },
  duplicate: { tone: 'neutral', icon: Copy },
  declined: { tone: 'danger', icon: CircleX },
}

export function StatusBadge({ status, size }: { status: FbStatus; size?: BadgeProps['size'] }) {
  const style = STATUS_STYLE[status] ?? STATUS_STYLE.submitted
  const Icon = style.icon
  return (
    <Badge tone={style.tone} size={size}>
      <Icon aria-hidden />
      {t(`status.${status}` as 'status.submitted')}
    </Badge>
  )
}

const KIND_ICON: Record<FbKind, LucideIcon> = {
  bug: Bug,
  experience: Eye,
  feature: Lightbulb,
  content: Inbox,
  access: WifiOff,
  competitor: Rocket,
  other: Inbox,
}

export function KindBadge({ kind, size }: { kind: FbKind; size?: BadgeProps['size'] }) {
  const Icon = KIND_ICON[kind] ?? Inbox
  return (
    <Badge tone="outline" size={size}>
      <Icon aria-hidden />
      {t(`kind.${kind}` as 'kind.bug')}
    </Badge>
  )
}

/**
 * 可见性。
 * 公开内容不加徽标——绝大多数内容都是公开的，给每条都挂一个只会增加噪音；
 * 只在「仅成员」「私密」时提示，这才是用户需要注意的信息。
 */
export function VisibilityBadge({
  visibility,
  size,
}: {
  visibility: FbVisibility
  size?: BadgeProps['size']
}) {
  if (visibility === 'public') return null
  const Icon = visibility === 'private' ? Lock : Users
  return (
    <Badge tone={visibility === 'private' ? 'danger' : 'neutral'} size={size}>
      <Icon aria-hidden />
      {t(visibility === 'private' ? 'visibility.privateShort' : 'visibility.membersShort')}
    </Badge>
  )
}

const PROJECT_STATUS_STYLE: Record<FbProjectStatus, { tone: Tone; icon: LucideIcon }> = {
  draft: { tone: 'neutral', icon: CalendarClock },
  recruiting: { tone: 'success', icon: Users },
  active: { tone: 'brand', icon: Rocket },
  paused: { tone: 'warning', icon: CircleSlash },
  archived: { tone: 'neutral', icon: Archive },
}

export function ProjectStatusBadge({
  status,
  size,
}: {
  status: FbProjectStatus
  size?: BadgeProps['size']
}) {
  const style = PROJECT_STATUS_STYLE[status] ?? PROJECT_STATUS_STYLE.active
  const Icon = style.icon
  return (
    <Badge tone={style.tone} size={size}>
      <Icon aria-hidden />
      {t(`projectStatus.${status}` as 'projectStatus.active')}
    </Badge>
  )
}

export function TopicStatusBadge({
  status,
  size,
}: {
  status: FbTopicStatus
  size?: BadgeProps['size']
}) {
  const open = status === 'open'
  return (
    <Badge tone={open ? 'success' : 'neutral'} size={size}>
      {open ? <CircleDotPulse /> : <Archive aria-hidden />}
      {t(`topicStatus.${status}` as 'topicStatus.open')}
    </Badge>
  )
}

/**
 * 「征集中」的小圆点。
 * 做成静态圆点而不是闪烁动画：这个状态会一直停在页面上，
 * 持续动画既分散注意也违反 prefers-reduced-motion 的意图。
 */
function CircleDotPulse() {
  return <span className="size-1.5 shrink-0 rounded-pill bg-success" aria-hidden />
}

const POST_KIND_STYLE: Record<FbPostKind, { tone: Tone; icon: LucideIcon }> = {
  announcement: { tone: 'info', icon: Globe },
  test_task: { tone: 'brand', icon: Hammer },
  changelog: { tone: 'success', icon: Rocket },
}

export function PostKindBadge({
  kind,
  size,
}: {
  kind: FbPostKind
  size?: BadgeProps['size']
}) {
  const style = POST_KIND_STYLE[kind] ?? POST_KIND_STYLE.announcement
  const Icon = style.icon
  return (
    <Badge tone={style.tone} size={size}>
      <Icon aria-hidden />
      {t(`postKind.${kind}` as 'postKind.announcement')}
    </Badge>
  )
}
