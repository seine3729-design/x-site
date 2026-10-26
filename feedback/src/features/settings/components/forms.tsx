'use client'

import { useActionState, useEffect } from 'react'
import { toast } from 'sonner'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, Input, Textarea } from '@/components/ui/field'
import { t } from '@/lib/i18n'
import type { FbUser } from '@/lib/api/types'
import { changePassword, updateProfile, type PasswordState, type ProfileState } from '../actions'

export function ProfileForm({ user }: { user: FbUser }) {
  const [state, action, pending] = useActionState(updateProfile, {} as ProfileState)

  useEffect(() => {
    if (state.ok) toast.success(t('settings.profileSaved'))
  }, [state.ok])

  return (
    <form action={action} className="flex flex-col gap-4">
      {state.error && <Alert tone="danger">{state.error}</Alert>}
      <Field id="nickname" label={t('settings.nickname')}>
        {(props) => (
          <Input
            {...props}
            name="nickname"
            defaultValue={user.nickname ?? ''}
            placeholder={t('settings.nicknamePlaceholder')}
          />
        )}
      </Field>
      <Field id="bio" label={t('settings.bio')}>
        {(props) => (
          <Textarea
            {...props}
            name="bio"
            rows={3}
            defaultValue={user.bio ?? ''}
            placeholder={t('settings.bioPlaceholder')}
          />
        )}
      </Field>
      <div className="flex flex-col gap-1.5">
        <label className="flex cursor-pointer items-start gap-2.5 text-sm text-fg">
          <input
            type="checkbox"
            name="show_level"
            defaultChecked={user.show_level ?? true}
            className="mt-0.5 size-4 shrink-0 cursor-pointer rounded-sm border-control-border accent-primary"
            aria-describedby="show-level-hint"
          />
          <span>{t('settings.showLevel')}</span>
        </label>
        <p id="show-level-hint" className="text-xs text-fg-muted">
          {t('settings.showLevelHint')}
        </p>
      </div>
      <div>
        <Button type="submit" loading={pending} loadingLabel={t('action.saving')}>
          {t('settings.saveProfile')}
        </Button>
      </div>
    </form>
  )
}

export function PasswordForm() {
  const [state, action, pending] = useActionState(changePassword, {} as PasswordState)

  return (
    <form action={action} className="flex flex-col gap-4">
      {state.error && <Alert tone="danger">{state.error}</Alert>}
      {/* 改完不再有恢复码要保存（adr/0011），直接给一条成功提示就够 */}
      {state.ok && <Alert tone="success">{t('settings.passwordSaved')}</Alert>}
      <Alert tone="warning">{t('settings.passwordHint')}</Alert>
      <Field id="old_password" label={t('settings.oldPassword')} required>
        {(props) => (
          <Input
            {...props}
            name="old_password"
            type="password"
            autoComplete="current-password"
            placeholder={t('settings.oldPasswordPlaceholder')}
          />
        )}
      </Field>
      <Field
        id="new_password"
        label={t('settings.newPassword')}
        required
        hint={t('auth.passwordHint')}
      >
        {(props) => (
          <Input
            {...props}
            name="new_password"
            type="password"
            autoComplete="new-password"
            placeholder={t('settings.newPasswordPlaceholder')}
          />
        )}
      </Field>
      <div>
        <Button type="submit" variant="outline" loading={pending} loadingLabel={t('action.saving')}>
          {t('settings.savePassword')}
        </Button>
      </div>
    </form>
  )
}
