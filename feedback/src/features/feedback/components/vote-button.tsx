'use client'

import { useActionState, useEffect, useOptimistic } from 'react'
import { ThumbsUp } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { formatCount } from '@/lib/format'
import { t } from '@/lib/i18n'
import { setVote, type VoteState } from '../actions'

/**
 * 「我也遇到」按钮。
 *
 * - 用 aria-pressed 表达开关态，不靠颜色（WCAG 1.4.1）；
 * - 乐观更新票数，失败时回滚并 toast 说明——docs/06 第 10 节要求失败必须回滚，
 *   不能让用户以为点成功了；
 * - 接口传目标状态而不是切换，所以连点也不会算错。
 */
export function VoteButton({
  slug,
  number,
  count,
  voted,
  signedIn,
}: {
  slug: string
  number: number
  count: number
  voted: boolean
  signedIn: boolean
}) {
  const [state, action, pending] = useActionState(setVote, {} as VoteState)
  const server = { count: state.count ?? count, voted: state.voted ?? voted }
  const [view, addOptimistic] = useOptimistic(server, (_prev, next: typeof server) => next)

  useEffect(() => {
    if (state.error) toast.error(state.error)
  }, [state.error])

  const nextActive = !view.voted

  return (
    <form
      action={action}
      onSubmit={() =>
        addOptimistic({ count: view.count + (nextActive ? 1 : -1), voted: nextActive })
      }
      className="flex flex-col gap-2"
    >
      <input type="hidden" name="slug" value={slug} />
      <input type="hidden" name="number" value={number} />
      <input type="hidden" name="active" value={nextActive ? '1' : '0'} />
      <Button
        type="submit"
        variant={view.voted ? 'subtle' : 'outline'}
        size="lg"
        aria-pressed={view.voted}
        loading={pending}
        className="justify-start"
      >
        <ThumbsUp aria-hidden />
        <span>{view.voted ? t('action.voted') : t('action.vote')}</span>
        <span className="ms-auto tabular">{formatCount(view.count)}</span>
      </Button>
      <p className="text-xs text-fg-muted">
        {signedIn
          ? view.count > 0
            ? t('detail.voteCount', { count: formatCount(view.count) })
            : t('detail.voteHint')
          : t('detail.voteLoginHint')}
      </p>
    </form>
  )
}
