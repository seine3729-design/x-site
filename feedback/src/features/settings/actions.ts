'use server'

import { revalidatePath } from 'next/cache'
import { redirect } from 'next/navigation'
import { callService, ServiceError } from '@/lib/server/api'
import { getSessionToken, setSessionCookie } from '@/lib/server/session'
import { errorText } from '@/lib/i18n'
import type { FbAuthResult, FbUser } from '@/lib/api/types'

export type ProfileState = { error?: string; ok?: boolean }
export type PasswordState = { error?: string; ok?: boolean }

async function tokenOrLogin(): Promise<string> {
  const token = await getSessionToken()
  if (!token) redirect(`/login?next=${encodeURIComponent('/settings')}`)
  return token
}

/** 保存公开资料（昵称、简介、是否公开贡献等级）。 */
export async function updateProfile(
  _prev: ProfileState,
  form: FormData,
): Promise<ProfileState> {
  const token = await tokenOrLogin()
  try {
    await callService<FbUser>('/api/v1/feedback/me', {
      method: 'PATCH',
      token,
      body: {
        nickname: String(form.get('nickname') ?? '').trim(),
        bio: String(form.get('bio') ?? '').trim(),
        show_level: form.get('show_level') === 'on',
      },
    })
  } catch (error) {
    if (error instanceof ServiceError) {
      if (error.needLogin) redirect(`/login?next=${encodeURIComponent('/settings')}`)
      return { error: errorText(error.code, error.message) }
    }
    throw error
  }
  revalidatePath('/settings')
  revalidatePath('/me')
  return { ok: true }
}

/**
 * 修改密码。
 *
 * 后端会作废其他设备的会话并换发令牌，所以这里必须用新令牌覆盖本机 Cookie，
 * 否则改完密码连自己也被登出。
 *
 * 这是整个站唯一的改密入口，而且必须提供当前密码 —— 没有「忘记密码」通道
 * （adr/0011），也不再有改密后发新恢复码这一步。
 */
export async function changePassword(
  _prev: PasswordState,
  form: FormData,
): Promise<PasswordState> {
  const oldPassword = String(form.get('old_password') ?? '')
  const newPassword = String(form.get('new_password') ?? '')
  if (!oldPassword) return { error: '请填写当前密码' }
  if (newPassword.length < 12) return { error: '新密码至少 12 位' }

  const token = await tokenOrLogin()
  let result: FbAuthResult
  try {
    result = await callService<FbAuthResult>('/api/v1/feedback/me/password', {
      method: 'POST',
      token,
      body: { old_password: oldPassword, new_password: newPassword },
    })
  } catch (error) {
    if (error instanceof ServiceError) {
      if (error.needLogin) redirect(`/login?next=${encodeURIComponent('/settings')}`)
      return { error: errorText(error.code, error.message) }
    }
    throw error
  }
  await setSessionCookie(result.token ?? '', result.expires_in ?? 0)
  return { ok: true }
}
