'use server'

import { revalidatePath } from 'next/cache'
import { redirect } from 'next/navigation'
import { callService, ServiceError } from '@/lib/server/api'
import { getSessionToken } from '@/lib/server/session'
import { errorText, t } from '@/lib/i18n'
import type { FbAnswer } from '@/lib/api/types'

export type AnswerState = { error?: string; ok?: boolean }
export type HelpfulState = { error?: string; count?: number; active?: boolean }

/** 发布回答或回复。parent_id 为 0 表示顶层回答。 */
export async function postAnswer(_prev: AnswerState, form: FormData): Promise<AnswerState> {
  const topicId = Number(form.get('topic_id') ?? 0)
  const parentId = Number(form.get('parent_id') ?? 0)
  const body = String(form.get('body') ?? '').trim()
  if (!topicId) return { error: errorText('BAD_REQUEST') }
  if (body.length < 2) return { error: t('topic.composerHint') }

  const path = `/topics/${topicId}`
  const token = await getSessionToken()
  if (!token) redirect(`/login?next=${encodeURIComponent(path)}`)

  try {
    await callService<FbAnswer>(`/api/v1/feedback/topics/${topicId}/answers`, {
      method: 'POST',
      token,
      body: { parent_id: parentId, body },
    })
  } catch (error) {
    if (error instanceof ServiceError) {
      if (error.needLogin) redirect(`/login?next=${encodeURIComponent(path)}`)
      return { error: errorText(error.code, error.message) }
    }
    throw error
  }

  revalidatePath(path)
  revalidatePath('/topics')
  return { ok: true }
}

/**
 * 设置或取消「有道理」。
 * 接口传目标状态而不是切换，所以连点和重试都不会把计数弄乱。
 */
export async function setHelpful(_prev: HelpfulState, form: FormData): Promise<HelpfulState> {
  const answerId = Number(form.get('answer_id') ?? 0)
  const topicId = Number(form.get('topic_id') ?? 0)
  const active = form.get('active') === '1'
  if (!answerId) return { error: errorText('BAD_REQUEST') }

  const path = `/topics/${topicId}`
  const token = await getSessionToken()
  if (!token) redirect(`/login?next=${encodeURIComponent(path)}`)

  try {
    const updated = await callService<FbAnswer>(`/api/v1/feedback/answers/${answerId}/helpful`, {
      method: 'PUT',
      token,
      body: { active },
    })
    revalidatePath(path)
    return { count: updated.helpful_count ?? 0, active: !!updated.helpful_by_me }
  } catch (error) {
    if (error instanceof ServiceError) {
      if (error.needLogin) redirect(`/login?next=${encodeURIComponent(path)}`)
      return { error: errorText(error.code, error.message) }
    }
    throw error
  }
}
