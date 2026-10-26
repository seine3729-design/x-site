import type { Metadata } from 'next'
import Link from 'next/link'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { AuthShell } from '@/features/auth/components/auth-shell'
import { t } from '@/lib/i18n'
import { getCurrentUser } from '@/lib/server/session'

// 这一页依赖会话或查询参数，按访问者实时渲染，不预渲染（feedback/docs/06 第 11.5 节）
export const instant = false

export const metadata: Metadata = {
  title: t('invite.title'),
  // 邀请链接不该被搜索引擎收录
  robots: { index: false, follow: false },
}

type Params = { params: Promise<{ token: string }> }

/**
 * 邀请落地页。
 *
 * 这一页不校验邀请码——校验在注册时由后端做（并且带防枚举：无效、过期、用完
 * 都返回同一个提示）。在这里先调一次接口验证会把「某个邀请码是否有效」
 * 变成一个任何人都能探测的接口。
 */
export default async function InvitePage({ params }: Params) {
  const { token } = await params
  const code = decodeURIComponent(token).trim().slice(0, 64)
  const user = await getCurrentUser()

  // 已登录的人点邀请链接很常见。诚实说明现状：邀请码只在注册时核销，
  // 已有账号进「仅邀请」项目要靠运营添加（feedback/docs/11 第 13.3 节的缺口）。
  if (user) {
    return (
      <AuthShell title={t('invite.signedInTitle')} subtitle={t('invite.signedInBody')}>
        <div className="flex flex-col gap-3">
          <Button asChild size="lg" block>
            <Link href="/projects">{t('invite.signedInAction')}</Link>
          </Button>
        </div>
      </AuthShell>
    )
  }

  return (
    <AuthShell
      title={t('invite.title')}
      subtitle={t('invite.subtitle')}
      footer={
        <>
          {t('invite.haveAccount')}{' '}
          <Link href="/login" className="text-primary-fg hover:underline">
            {t('nav.login')}
          </Link>
        </>
      }
    >
      <div className="flex flex-col gap-5">
        <div>
          <p className="text-sm font-medium text-fg">{t('invite.codeLabel')}</p>
          <div className="mt-2 rounded-control border border-border bg-surface-raised px-3 py-3">
            <code className="font-mono text-base font-semibold break-token select-all">
              {code}
            </code>
          </div>
          <p className="mt-2 text-xs text-fg-muted">{t('invite.codeHint')}</p>
        </div>

        <Alert tone="warning">{t('invite.noRecoveryWarning')}</Alert>

        <Button asChild size="lg" block>
          <Link href={`/register?invite=${encodeURIComponent(code)}`}>
            {t('invite.register')}
          </Link>
        </Button>
      </div>
    </AuthShell>
  )
}
