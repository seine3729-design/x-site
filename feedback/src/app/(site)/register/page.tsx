import type { Metadata } from 'next'
import { redirect } from 'next/navigation'
import { AuthShell } from '@/features/auth/components/auth-shell'
import { RegisterForm } from '@/features/auth/components/forms'
import { t } from '@/lib/i18n'
import { getCurrentUser } from '@/lib/server/session'

// 这一页依赖会话或查询参数，按访问者实时渲染，不预渲染（feedback/docs/06 第 11.5 节）
export const instant = false

export const metadata: Metadata = {
  title: t('auth.registerTitle'),
  robots: { index: false, follow: false },
}

function safeNext(raw: string | string[] | undefined): string {
  const value = Array.isArray(raw) ? raw[0] : raw
  if (!value || !value.startsWith('/') || value.startsWith('//')) return '/'
  return value
}

function first(raw: string | string[] | undefined): string {
  const value = Array.isArray(raw) ? raw[0] : raw
  return value ?? ''
}

export default async function RegisterPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>
}) {
  const params = await searchParams
  if (await getCurrentUser()) redirect('/')

  return (
    <AuthShell title={t('auth.registerTitle')} subtitle={t('auth.registerSubtitle')}>
      {/* 邀请链接可以带上 ?invite=xxx，省得用户手抄 */}
      <RegisterForm next={safeNext(params.next)} inviteCode={first(params.invite)} />
    </AuthShell>
  )
}
