'use server'

import { redirect } from 'next/navigation'
import { callService, ServiceError } from '@/lib/server/api'
import { clearSessionCookie, getSessionToken, setSessionCookie } from '@/lib/server/session'
import { errorText } from '@/lib/i18n'
import type { FbAuthResult } from '@/lib/api/types'
import { fieldErrorsOf, loginSchema, registerSchema, type AuthState } from './schema'

/**
 * 账号相关的写操作。
 *
 * 都是 Server Action：表单在没有 JS 的情况下也能提交，而且会话令牌全程留在服务端，
 * 浏览器只拿到 HttpOnly Cookie（adr/0008）。
 */

/** 只允许跳回站内路径，挡掉 ?next=//evil.com 这类开放重定向。 */
function safeNext(raw: FormDataEntryValue | null): string {
  const value = typeof raw === 'string' ? raw : ''
  if (!value.startsWith('/') || value.startsWith('//')) return '/'
  return value
}

function failed(error: unknown): AuthState {
  if (error instanceof ServiceError) {
    return { error: errorText(error.code, error.message) }
  }
  throw error
}

export async function login(_prev: AuthState, form: FormData): Promise<AuthState> {
  const parsed = loginSchema.safeParse(Object.fromEntries(form))
  if (!parsed.success) return { fieldErrors: fieldErrorsOf(parsed.error) }

  let result: FbAuthResult
  try {
    result = await callService<FbAuthResult>('/api/v1/feedback/auth/login', {
      method: 'POST',
      body: parsed.data,
    })
  } catch (error) {
    return failed(error)
  }
  await setSessionCookie(result.token ?? '', result.expires_in ?? 0)
  redirect(safeNext(form.get('next')))
}

export async function register(_prev: AuthState, form: FormData): Promise<AuthState> {
  const parsed = registerSchema.safeParse(Object.fromEntries(form))
  if (!parsed.success) return { fieldErrors: fieldErrorsOf(parsed.error) }

  let result: FbAuthResult
  try {
    result = await callService<FbAuthResult>('/api/v1/feedback/auth/register', {
      method: 'POST',
      body: {
        handle: parsed.data.handle,
        password: parsed.data.password,
        nickname: parsed.data.nickname,
        invite_code: parsed.data.invite_code,
      },
    })
  } catch (error) {
    return failed(error)
  }
  await setSessionCookie(result.token ?? '', result.expires_in ?? 0)
  // 注册成功直接进站。以前这里要停下来让用户抄恢复码，现在没有这一步了（adr/0011）。
  redirect(safeNext(form.get('next')))
}

// 没有 recover action：恢复码与「忘记密码」已删除（adr/0011）。
// 忘记密码的账号无法恢复，运营也没有代为重置的接口。

/**
 * 退出登录。
 *
 * 先让后端作废令牌，再清本地 Cookie。后端失败也要清 Cookie——
 * 用户点了退出就必须在这台设备上退出，不能因为后端抖动把人留在登录态。
 */
export async function logout() {
  const token = await getSessionToken()
  if (token) {
    try {
      await callService('/api/v1/feedback/auth/logout', { method: 'POST', token })
    } catch (error) {
      if (!(error instanceof ServiceError)) throw error
    }
  }
  await clearSessionCookie()
  redirect('/')
}
