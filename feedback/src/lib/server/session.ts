import 'server-only'
import { cache } from 'react'
import { cookies } from 'next/headers'
import { env } from '@/env'
import { callService, ServiceError } from './api'
import type { FbUser } from '@/lib/api/types'

/**
 * 社区会话（feedback/docs/adr/0008）。
 *
 * Go 在登录响应体里返回不透明令牌，由这里写成本服务的 HttpOnly Cookie；
 * 后续请求从 Cookie 取出，以 `Authorization: Bearer` 转发给 Go。
 * 浏览器拿不到令牌本身，JS 也读不到这个 Cookie。
 *
 * CSRF：Go 侧不读 Cookie，所以跨站请求即使带上 Cookie 也不会被 Go 当成身份；
 * 对本服务的写入口（Server Action）由 Next.js 的 Origin 校验 + SameSite=Lax 兜住。
 */

const COOKIE = env.isProduction ? '__Host-fb_session' : 'fb_session'

/** 写入会话。maxAgeSeconds 用后端返回的 expires_in，保持两边过期时间一致。 */
export async function setSessionCookie(token: string, maxAgeSeconds: number) {
  const store = await cookies()
  store.set(COOKIE, token, {
    httpOnly: true,
    secure: env.isProduction,
    // Lax 而不是 Strict：用户经常从邀请链接、通知邮件之外的站点跳进来，
    // Strict 会让第一次导航不带 Cookie，表现为「明明登录了却是未登录」。
    sameSite: 'lax',
    path: '/',
    maxAge: maxAgeSeconds > 0 ? maxAgeSeconds : undefined,
  })
}

export async function clearSessionCookie() {
  const store = await cookies()
  store.set(COOKIE, '', {
    httpOnly: true,
    secure: env.isProduction,
    sameSite: 'lax',
    path: '/',
    maxAge: 0,
  })
}

export async function getSessionToken(): Promise<string | undefined> {
  const store = await cookies()
  return store.get(COOKIE)?.value || undefined
}

/**
 * 当前登录用户；未登录或令牌已失效返回 null。
 *
 * 用 React cache 包一层：一次渲染里顶栏、页面、子组件都会问「当前是谁」，
 * 不包的话每个调用点都会打一次后端。
 */
export const getCurrentUser = cache(async (): Promise<FbUser | null> => {
  const token = await getSessionToken()
  if (!token) return null
  try {
    return await callService<FbUser>('/api/v1/feedback/me', { token })
  } catch (error) {
    // 令牌过期/被改密作废：按未登录处理。这里不能清 Cookie——
    // Server Component 的渲染过程中不允许写 Cookie，清理交给下一次写操作或登出。
    if (error instanceof ServiceError && error.needLogin) return null
    throw error
  }
})
