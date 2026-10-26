'use server'

import { revalidatePath } from 'next/cache'
import { redirect } from 'next/navigation'
import { callService, ServiceError } from '@/lib/server/api'
import { getSessionToken } from '@/lib/server/session'
import { errorText } from '@/lib/i18n'
import type { FbFeedback } from '@/lib/api/types'

export type VoteState = { error?: string; count?: number; voted?: boolean }

/**
 * 标记「我也遇到」。
 *
 * 接口是幂等的（传目标状态而不是切换），所以重复提交、网络重试都不会把票数弄乱。
 * 未登录时带回跳地址去登录，不是报一句「请先登录」就把人留在原地。
 */
export async function setVote(_prev: VoteState, form: FormData): Promise<VoteState> {
  const slug = String(form.get('slug') ?? '')
  const number = Number(form.get('number') ?? 0)
  const active = form.get('active') === '1'
  if (!slug || !number) return { error: errorText('BAD_REQUEST') }

  const path = `/feedback/${slug}/${number}`
  const token = await getSessionToken()
  if (!token) redirect(`/login?next=${encodeURIComponent(path)}`)

  try {
    const updated = await callService<FbFeedback>(
      `/api/v1/feedback/issues/${encodeURIComponent(slug)}/${number}/vote`,
      { method: 'PUT', token, body: { active } },
    )
    revalidatePath(path)
    return { count: updated.vote_count ?? 0, voted: !!updated.voted }
  } catch (error) {
    if (error instanceof ServiceError) {
      if (error.needLogin) redirect(`/login?next=${encodeURIComponent(path)}`)
      return { error: errorText(error.code, error.message) }
    }
    throw error
  }
}

export type SubmitState = { error?: string; fieldErrors?: Record<string, string> }

const KINDS = new Set(['bug', 'experience', 'feature', 'content', 'access', 'competitor', 'other'])
const VISIBILITIES = new Set(['public', 'members', 'private'])

/**
 * 提交反馈。
 *
 * 成功后直接跳到详情页（不是弹个 toast 就留在表单上）：用户需要看到编号、
 * 当前状态和接下来会发生什么，这也是 feedback/docs/02 第 5.2 节的要求。
 * 失败时把已填内容交回给表单，不让人重打一遍。
 */
export async function submitFeedback(_prev: SubmitState, form: FormData): Promise<SubmitState> {
  const fieldErrors: Record<string, string> = {}

  const projectSlug = String(form.get('project_slug') ?? '').trim()
  const kind = String(form.get('kind') ?? '').trim()
  const title = String(form.get('title') ?? '').trim()
  const body = String(form.get('body') ?? '').trim()
  const env = String(form.get('env') ?? '').trim()
  const pageUrl = String(form.get('page_url') ?? '').trim()
  const visibility = String(form.get('visibility') ?? 'public').trim()

  if (!projectSlug) fieldErrors.project_slug = '请选择项目'
  if (!KINDS.has(kind)) fieldErrors.kind = '请选择内容类型'
  if (title.length < 4 || title.length > 160) fieldErrors.title = '标题需在 4 到 160 个字之间'
  if (!body) fieldErrors.body = '请填写详细内容'
  if (!VISIBILITIES.has(visibility)) fieldErrors.visibility = '请选择可见性'
  if (Object.keys(fieldErrors).length > 0) return { fieldErrors }

  const token = await getSessionToken()
  if (!token) redirect(`/login?next=${encodeURIComponent('/feedback/new')}`)

  let created: FbFeedback
  try {
    created = await callService<FbFeedback>('/api/v1/feedback/issues', {
      method: 'POST',
      token,
      body: {
        project_slug: projectSlug,
        kind,
        title,
        body,
        env,
        page_url: pageUrl,
        visibility,
      },
    })
  } catch (error) {
    if (error instanceof ServiceError) {
      if (error.needLogin) redirect(`/login?next=${encodeURIComponent('/feedback/new')}`)
      return { error: errorText(error.code, error.message) }
    }
    throw error
  }

  revalidatePath('/feedback')
  revalidatePath(`/projects/${projectSlug}`)
  redirect(`/feedback/${created.project_slug ?? projectSlug}/${created.number ?? 0}?created=1`)
}
