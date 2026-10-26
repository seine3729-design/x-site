import type { Metadata } from 'next'
import { redirect } from 'next/navigation'
import { AuthShell } from '@/features/auth/components/auth-shell'
import { LoginForm } from '@/features/auth/components/forms'
import { t } from '@/lib/i18n'
import { getCurrentUser } from '@/lib/server/session'

// 这一页依赖会话或查询参数，按访问者实时渲染，不预渲染（feedback/docs/06 第 11.5 节）
export const instant = false

export const metadata: Metadata = {
  title: t('auth.loginTitle'),
  // 登录页不该进搜索结果
  robots: { index: false, follow: false },
}

/** 只接受站内路径，挡掉 ?next=//evil.com 这类开放重定向。 */
function safeNext(raw: string | string[] | undefined): string {
  const value = Array.isArray(raw) ? raw[0] : raw
  if (!value || !value.startsWith('/') || value.startsWith('//')) return '/'
  return value
}

export default async function LoginPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>
}) {
  const params = await searchParams
  const next = safeNext(params.next)
  // 已登录的人进登录页没有意义，直接送回去
  if (await getCurrentUser()) redirect(next)

  return (
    <AuthShell title={t('auth.loginTitle')} subtitle={t('auth.loginSubtitle')}>
      <LoginForm next={next} />
    </AuthShell>
  )
}
