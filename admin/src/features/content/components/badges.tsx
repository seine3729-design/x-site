import { CircleCheck, CircleDashed, CircleOff, Clock } from 'lucide-react'
import { formatDateTime } from '@/lib/format'
import { Badge } from '@/components/ui'
import { DISPLAY_STATUS_LABELS, type DisplayStatus } from '../api'

/** 剧的显示状态：图标 + 文字（不只靠颜色区分）；定时未到点时附上架时间。 */
export function DramaStatusBadge({ status, publishAt }: { status: DisplayStatus; publishAt: string | null }) {
  const label = DISPLAY_STATUS_LABELS[status]
  switch (status) {
    case 'published':
      return (
        <Badge variant='default'>
          <CircleCheck aria-hidden /> {label}
        </Badge>
      )
    case 'scheduled':
      return (
        <Badge variant='outline' title={publishAt ? `将于 ${formatDateTime(publishAt)} 上架` : undefined}>
          <Clock aria-hidden /> {label}
          {publishAt && <span className='tabular-nums'>{formatDateTime(publishAt).slice(5, 16)}</span>}
        </Badge>
      )
    case 'offline':
      return (
        <Badge variant='destructive'>
          <CircleOff aria-hidden /> {label}
        </Badge>
      )
    default:
      return (
        <Badge variant='secondary'>
          <CircleDashed aria-hidden /> {label}
        </Badge>
      )
  }
}

/** 启用 / 停用。 */
export function ActiveBadge({ active, on = '启用', off = '停用' }: { active: boolean; on?: string; off?: string }) {
  return active ? <Badge variant='secondary'>{on}</Badge> : <Badge variant='outline' className='text-muted-foreground'>{off}</Badge>
}
