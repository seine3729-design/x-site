import { StatusBadge } from '@/components/domain/badges'
import { formatDateTime, formatRelative } from '@/lib/format'
import { t } from '@/lib/i18n'
import type { FbFeedbackEvent, FbStatus } from '@/lib/api/types'

/**
 * 状态时间线（feedback/docs/06 第 3.5 节）。
 *
 * 用 ol 而不是一堆 div：屏幕阅读器会读出「列表，共 N 项」，顺序和进展一目了然。
 * 竖线是装饰性的，对无障碍树隐藏。
 * 内部备注不会出现在这里——前台接口本身就不下发（service 侧已剥掉）。
 */
export function Timeline({ events, now }: { events: FbFeedbackEvent[]; now: number }) {
  if (events.length === 0) {
    return <p className="text-sm text-fg-muted">{t('detail.timelineEmpty')}</p>
  }

  return (
    <ol className="flex flex-col">
      {events.map((event, index) => {
        const last = index === events.length - 1
        const from = event.from_status as FbStatus | ''
        return (
          <li key={event.id} className="flex gap-3">
            <div className="flex flex-col items-center" aria-hidden>
              <span className="mt-1.5 size-2 shrink-0 rounded-pill bg-primary" />
              {!last && <span className="w-px flex-1 bg-border" />}
            </div>
            <div className={last ? 'pb-0' : 'pb-5'}>
              <div className="flex flex-wrap items-center gap-2">
                {event.to_status && <StatusBadge status={event.to_status} />}
                {from && (
                  <span className="text-xs text-fg-subtle">
                    {t('detail.timelineFrom', {
                      from: t(`status.${from}` as 'status.submitted'),
                      to: t(`status.${event.to_status ?? 'submitted'}` as 'status.submitted'),
                    })}
                  </span>
                )}
                <time
                  dateTime={event.created_at}
                  title={formatDateTime(event.created_at)}
                  className="text-xs text-fg-muted"
                >
                  {formatRelative(event.created_at, now)}
                </time>
              </div>
              {event.note && (
                <p className="mt-1.5 text-sm text-fg user-text">{event.note}</p>
              )}
              {/*
                后端的 actor_name 是后台账号的用户名（例如 ops_zhang）。
                前台不显示它：用户不需要知道是哪个运营处理的，而内部账号名
                暴露在公开页面上既没有价值也不该出现。只标明这是官方动作。
              */}
              {event.actor_name && <p className="mt-1 text-xs text-fg-subtle">项目团队</p>}
            </div>
          </li>
        )
      })}
    </ol>
  )
}
