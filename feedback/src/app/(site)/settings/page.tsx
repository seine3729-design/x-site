import type { Metadata } from 'next'
import { redirect } from 'next/navigation'
import { Alert } from '@/components/ui/alert'
import { Card } from '@/components/ui/card'
import { Container } from '@/components/ui/section'
import { PasswordForm, ProfileForm } from '@/features/settings/components/forms'
import { t } from '@/lib/i18n'
import { getCurrentUser } from '@/lib/server/session'

// 这一页依赖会话或查询参数，按访问者实时渲染，不预渲染（feedback/docs/06 第 11.5 节）
export const instant = false

export const metadata: Metadata = {
  title: t('settings.title'),
  robots: { index: false, follow: false },
}

export default async function SettingsPage() {
  const user = await getCurrentUser()
  if (!user) redirect(`/login?next=${encodeURIComponent('/settings')}`)

  return (
    <Container className="py-8 sm:py-10">
      <div className="mx-auto max-w-prose">
        <h1 className="text-2xl font-semibold tracking-tight text-fg sm:text-3xl">
          {t('settings.title')}
        </h1>
        <p className="mt-2 text-sm text-fg-muted">{t('settings.subtitle')}</p>

        <div className="mt-8 flex flex-col gap-8">
          <section aria-labelledby="account-heading">
            <h2 id="account-heading" className="text-base font-semibold text-fg">
              {t('settings.accountTitle')}
            </h2>
            <Card className="mt-3" padding="md">
              <dl className="flex flex-col gap-4 text-sm">
                <div>
                  <dt className="text-fg-muted">{t('settings.handleLabel')}</dt>
                  <dd className="mt-0.5 font-medium text-fg">{user.handle}</dd>
                  <dd className="mt-0.5 text-xs text-fg-subtle">{t('settings.handleHint')}</dd>
                </div>
                {user.tester_no ? (
                  <div>
                    <dt className="text-fg-muted">{t('settings.testerNoLabel')}</dt>
                    <dd className="mt-0.5 font-medium text-fg tabular">{user.tester_no}</dd>
                    <dd className="mt-0.5 text-xs text-fg-subtle">{t('settings.testerNoHint')}</dd>
                  </div>
                ) : null}
              </dl>
            </Card>
          </section>

          <section aria-labelledby="profile-heading">
            <h2 id="profile-heading" className="text-base font-semibold text-fg">
              {t('settings.profileTitle')}
            </h2>
            <p className="mt-1 text-sm text-fg-muted">{t('settings.profileHint')}</p>
            <Card className="mt-3" padding="md">
              <ProfileForm user={user} />
            </Card>
          </section>

          <section aria-labelledby="password-heading">
            <h2 id="password-heading" className="text-base font-semibold text-fg">
              {t('settings.passwordTitle')}
            </h2>
            <Card className="mt-3" padding="md">
              <PasswordForm />
            </Card>
          </section>

          {/* 把「没有邮箱意味着什么」说在设置页上，而不是只在注册时提一句 */}
          <Alert tone="info" title={t('settings.noEmailTitle')}>
            {t('settings.noEmailBody')}
          </Alert>
        </div>
      </div>
    </Container>
  )
}
