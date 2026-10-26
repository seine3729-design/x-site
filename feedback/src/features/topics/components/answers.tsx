'use client'

import { useActionState, useEffect, useOptimistic, useRef, useState } from 'react'
import { MessageSquare, Sparkles, ThumbsUp } from 'lucide-react'
import { toast } from 'sonner'
import { Alert } from '@/components/ui/alert'
import { Avatar } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/field'
import { formatCount, formatDateTime, formatRelative } from '@/lib/format'
import { cn } from '@/lib/utils'
import { t } from '@/lib/i18n'
import type { FbAnswer } from '@/lib/api/types'
import { postAnswer, setHelpful, type AnswerState, type HelpfulState } from '../actions'

const EMPTY_ANSWER: AnswerState = {}

/** 回答作者的展示名：昵称优先，其次用户名。永远不暴露用户 ID。 */
function nameOf(answer: FbAnswer): string {
  return answer.nickname || answer.handle || '内测用户'
}

/**
 * 回答输入框。
 *
 * 不能回答时不渲染一个点不动的输入框，而是说明原因（未登录 / 已结束 / 不是成员）——
 * 让人把字打完才发现发不出去是最糟的。
 */
export function AnswerComposer({
  topicId,
  canAnswer,
  signedIn,
  closed,
}: {
  topicId: number
  canAnswer: boolean
  signedIn: boolean
  closed: boolean
}) {
  const [state, action, pending] = useActionState(postAnswer, EMPTY_ANSWER)
  const formRef = useRef<HTMLFormElement>(null)

  useEffect(() => {
    if (state.ok) {
      toast.success(t('topic.answerPosted'))
      formRef.current?.reset()
    }
  }, [state.ok])

  if (closed) return <Alert tone="info">{t('topic.composerClosed')}</Alert>
  if (!signedIn) return <Alert tone="info">{t('topic.composerLogin')}</Alert>
  if (!canAnswer) return <Alert tone="info">{t('topic.composerNotMember')}</Alert>

  return (
    <form ref={formRef} action={action} className="flex flex-col gap-3">
      <input type="hidden" name="topic_id" value={topicId} />
      <input type="hidden" name="parent_id" value={0} />
      <label htmlFor="answer-body" className="text-sm font-medium text-fg">
        {t('topic.composerTitle')}
      </label>
      <Textarea
        id="answer-body"
        name="body"
        rows={5}
        required
        placeholder={t('topic.composerPlaceholder')}
        aria-describedby="answer-hint"
      />
      <p id="answer-hint" className="text-xs text-fg-muted">
        {t('topic.composerHint')}
      </p>
      {state.error && <Alert tone="danger">{state.error}</Alert>}
      <div>
        <Button type="submit" loading={pending} loadingLabel={t('action.submitting')}>
          {t('topic.composerSubmit')}
        </Button>
      </div>
    </form>
  )
}

/** 「有道理」按钮：乐观更新，失败回滚并说明。 */
function HelpfulButton({
  answer,
  topicId,
  disabled,
}: {
  answer: FbAnswer
  topicId: number
  disabled: boolean
}) {
  const [state, action, pending] = useActionState(setHelpful, {} as HelpfulState)
  const server = {
    count: state.count ?? answer.helpful_count ?? 0,
    active: state.active ?? !!answer.helpful_by_me,
  }
  const [view, addOptimistic] = useOptimistic(server, (_p, next: typeof server) => next)

  useEffect(() => {
    if (state.error) toast.error(state.error)
  }, [state.error])

  const next = !view.active
  return (
    <form
      action={action}
      onSubmit={() => addOptimistic({ count: view.count + (next ? 1 : -1), active: next })}
    >
      <input type="hidden" name="answer_id" value={answer.id} />
      <input type="hidden" name="topic_id" value={topicId} />
      <input type="hidden" name="active" value={next ? '1' : '0'} />
      <Button
        type="submit"
        variant={view.active ? 'subtle' : 'ghost'}
        size="sm"
        aria-pressed={view.active}
        disabled={disabled}
        loading={pending}
      >
        <ThumbsUp aria-hidden />
        {t('action.helpful')}
        {view.count > 0 && <span className="tabular">{formatCount(view.count)}</span>}
      </Button>
    </form>
  )
}

/** 回复输入框：点「回复」才出现，取消后焦点回到按钮。 */
function ReplyBox({
  topicId,
  parent,
  onClose,
}: {
  topicId: number
  parent: FbAnswer
  onClose: () => void
}) {
  const [state, action, pending] = useActionState(postAnswer, EMPTY_ANSWER)

  useEffect(() => {
    if (state.ok) {
      toast.success(t('topic.answerPosted'))
      onClose()
    }
  }, [state.ok, onClose])

  return (
    <form action={action} className="mt-3 flex flex-col gap-2">
      <input type="hidden" name="topic_id" value={topicId} />
      <input type="hidden" name="parent_id" value={parent.id} />
      <Textarea
        name="body"
        rows={3}
        required
        autoFocus
        aria-label={t('topic.replyPlaceholder', { who: nameOf(parent) })}
        placeholder={t('topic.replyPlaceholder', { who: nameOf(parent) })}
      />
      {state.error && <Alert tone="danger">{state.error}</Alert>}
      <div className="flex gap-2">
        <Button type="submit" size="sm" loading={pending}>
          {t('topic.replySubmit')}
        </Button>
        <Button type="button" variant="ghost" size="sm" onClick={onClose}>
          {t('topic.replyCancel')}
        </Button>
      </div>
    </form>
  )
}

function AnswerItem({
  answer,
  replies,
  topicId,
  now,
  canAnswer,
  signedIn,
}: {
  answer: FbAnswer
  replies: FbAnswer[]
  topicId: number
  now: number
  canAnswer: boolean
  signedIn: boolean
}) {
  const [replying, setReplying] = useState(false)
  const name = nameOf(answer)

  return (
    <li
      className={cn(
        'rounded-surface border p-4 sm:p-5',
        answer.featured ? 'border-primary-border bg-primary-surface/40' : 'border-border bg-surface',
      )}
    >
      <div className="flex flex-wrap items-center gap-2">
        <Avatar name={name} seed={answer.tester_no ?? 0} size="sm" />
        {answer.tester_no ? (
          <Badge tone="outline" className="tabular">
            {t('topic.testerNo', { no: answer.tester_no })}
          </Badge>
        ) : null}
        <span className="text-sm font-medium text-fg">{name}</span>
        <time
          dateTime={answer.created_at}
          title={formatDateTime(answer.created_at)}
          className="text-xs text-fg-muted"
        >
          {formatRelative(answer.created_at, now)}
        </time>
        {answer.featured && (
          <Badge tone="brand">
            <Sparkles aria-hidden />
            {t('topic.featured')}
          </Badge>
        )}
      </div>

      <p className="mt-3 text-sm leading-relaxed text-fg user-text">{answer.body}</p>

      <div className="mt-3 flex flex-wrap items-center gap-1">
        <HelpfulButton answer={answer} topicId={topicId} disabled={!signedIn} />
        {canAnswer && (
          <Button variant="ghost" size="sm" onClick={() => setReplying((v) => !v)}>
            <MessageSquare aria-hidden />
            {t('action.reply')}
          </Button>
        )}
      </div>

      {replying && (
        <ReplyBox topicId={topicId} parent={answer} onClose={() => setReplying(false)} />
      )}

      {replies.length > 0 && (
        <ul className="mt-4 flex flex-col gap-3 border-s border-border ps-4">
          {replies.map((reply) => (
            <li key={reply.id}>
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-sm font-medium text-fg">{nameOf(reply)}</span>
                {reply.tester_no ? (
                  <span className="text-2xs text-fg-subtle tabular">
                    {t('topic.testerNo', { no: reply.tester_no })}
                  </span>
                ) : null}
                <time
                  dateTime={reply.created_at}
                  title={formatDateTime(reply.created_at)}
                  className="text-xs text-fg-muted"
                >
                  {formatRelative(reply.created_at, now)}
                </time>
              </div>
              <p className="mt-1.5 text-sm leading-relaxed text-fg user-text">{reply.body}</p>
              <div className="mt-1.5">
                <HelpfulButton answer={reply} topicId={topicId} disabled={!signedIn} />
              </div>
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}

/**
 * 回答列表。
 *
 * 后端返回的是平铺列表（带 parent_id），这里按顶层分组再渲染。
 * 只有一层回复——回复「回复」时服务端会把它挂到同一个顶层回答下，
 * 所以这里不需要递归。
 */
export function AnswerList({
  answers,
  topicId,
  now,
  canAnswer,
  signedIn,
}: {
  answers: FbAnswer[]
  topicId: number
  now: number
  canAnswer: boolean
  signedIn: boolean
}) {
  const tops = answers.filter((a) => !a.parent_id)
  const repliesOf = new Map<number, FbAnswer[]>()
  for (const answer of answers) {
    if (!answer.parent_id) continue
    const list = repliesOf.get(answer.parent_id) ?? []
    list.push(answer)
    repliesOf.set(answer.parent_id, list)
  }

  return (
    <ul className="flex flex-col gap-4">
      {tops.map((answer) => (
        <AnswerItem
          key={answer.id}
          answer={answer}
          replies={repliesOf.get(answer.id ?? 0) ?? []}
          topicId={topicId}
          now={now}
          canAnswer={canAnswer}
          signedIn={signedIn}
        />
      ))}
    </ul>
  )
}
