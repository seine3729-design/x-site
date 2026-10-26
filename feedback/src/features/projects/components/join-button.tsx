'use client'

import { useActionState, useEffect } from 'react'
import { Check, Clock, Plus } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { t } from '@/lib/i18n'
import { joinProject, type JoinState } from '../actions'
import type { FbJoinMode } from '@/lib/api/types'

const EMPTY: JoinState = {}

/**
 * 加入项目按钮。
 *
 * 不能自助加入的项目（仅邀请 / 需审核 / 不收新成员）不渲染一个点不动的按钮，
 * 而是直接说明怎么才能进去——给用户一个「看起来能点其实没用」的按钮比不给更糟。
 */
export function JoinButton({
  slug,
  name,
  joined,
  joinMode,
  memberStatus,
  acceptsFeedback,
  size = 'lg',
}: {
  slug: string
  name: string
  joined: boolean
  joinMode: FbJoinMode
  /** 访问者在该项目的成员状态；pending 表示申请已提交、等运营确认。 */
  memberStatus?: string
  acceptsFeedback: boolean
  size?: 'md' | 'lg'
}) {
  const [state, action, pending] = useActionState(joinProject, EMPTY)
  const applying = joinMode === 'application'

  useEffect(() => {
    if (state.joined) {
      toast.success(
        applying ? t('project.appliedToast', { name }) : t('project.joinedToast', { name }),
      )
    }
    if (state.error) toast.error(state.error)
  }, [state, name, applying])

  if (joined) {
    return (
      <Button variant="outline" size={size} disabled>
        <Check aria-hidden />
        {t('action.joined')}
      </Button>
    )
  }

  // 申请已提交：说明现在在等什么，而不是再给一个点了没反应的按钮
  if (memberStatus === 'pending' || (state.joined && applying)) {
    return (
      <div className="flex flex-col gap-1">
        <Button variant="outline" size={size} disabled>
          <Clock aria-hidden />
          {t('project.pending')}
        </Button>
        <p className="max-w-sm text-xs text-fg-muted">{t('project.pendingHint')}</p>
      </div>
    )
  }

  // 只能靠邀请码或运营添加的项目，不渲染一个点不动的按钮，直接说清怎么进
  if (!acceptsFeedback || joinMode === 'invite_only' || joinMode === 'closed') {
    const hint =
      joinMode === 'invite_only' ? t('project.inviteOnlyHint') : t('project.closedHint')
    return <p className="max-w-sm text-sm text-fg-muted">{hint}</p>
  }

  return (
    <form action={action}>
      <input type="hidden" name="slug" value={slug} />
      <Button
        type="submit"
        size={size}
        loading={pending}
        loadingLabel={applying ? t('project.applying') : t('action.joining')}
      >
        <Plus aria-hidden />
        {applying ? t('project.apply') : t('action.join')}
      </Button>
    </form>
  )
}
