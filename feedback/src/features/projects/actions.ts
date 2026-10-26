'use server'

import { revalidatePath } from 'next/cache'
import { redirect } from 'next/navigation'
import { callService, ServiceError } from '@/lib/server/api'
import { getSessionToken } from '@/lib/server/session'
import { errorText } from '@/lib/i18n'

export type JoinState = { error?: string; joined?: boolean }

/**
 * 加入项目。
 *
 * 未登录时不报错，而是带着回跳地址送去登录页——用户的意图是加入这个项目，
 * 登录完应该回到这里继续，而不是回到首页重新找一遍。
 */
export async function joinProject(_prev: JoinState, form: FormData): Promise<JoinState> {
  const slug = String(form.get('slug') ?? '')
  if (!slug) return { error: errorText('BAD_REQUEST') }

  const token = await getSessionToken()
  if (!token) redirect(`/login?next=${encodeURIComponent(`/projects/${slug}`)}`)

  try {
    await callService(`/api/v1/feedback/projects/${encodeURIComponent(slug)}/join`, {
      method: 'POST',
      token,
    })
  } catch (error) {
    if (error instanceof ServiceError) {
      if (error.needLogin) redirect(`/login?next=${encodeURIComponent(`/projects/${slug}`)}`)
      return { error: errorText(error.code, error.message) }
    }
    throw error
  }

  revalidatePath(`/projects/${slug}`)
  revalidatePath('/projects')
  return { joined: true }
}
