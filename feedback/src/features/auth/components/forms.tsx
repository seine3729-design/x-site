'use client'

import { useActionState, useEffect, useRef } from 'react'
import Link from 'next/link'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, Input } from '@/components/ui/field'
import { t } from '@/lib/i18n'
import { login, register } from '../actions'
import type { AuthState } from '../schema'

const EMPTY: AuthState = {}

/**
 * 提交失败后的错误摘要（feedback/docs/06 第 3.6 节）。
 *
 * 多字段出错时在顶部给一个可聚焦的摘要并把焦点移上去：
 * 只在字段下方标错误的话，屏幕阅读器用户和把页面放大的用户都可能看不到。
 */
function ErrorSummary({ state }: { state: AuthState }) {
  const ref = useRef<HTMLDivElement>(null)
  const count = Object.keys(state.fieldErrors ?? {}).length

  useEffect(() => {
    if (state.error || count > 0) ref.current?.focus()
  }, [state.error, count])

  if (!state.error && count === 0) return null
  return (
    <Alert ref={ref} tone="danger" tabIndex={-1} className="outline-none">
      {state.error ?? t('auth.errorSummary', { count })}
    </Alert>
  )
}

function SubmitButton({ label, pending }: { label: string; pending: boolean }) {
  return (
    <Button type="submit" size="lg" block loading={pending} loadingLabel={t('action.submitting')}>
      {label}
    </Button>
  )
}

// ---------- 登录 ----------

export function LoginForm({ next }: { next: string }) {
  const [state, action, pending] = useActionState(login, EMPTY)
  const errors = state.fieldErrors ?? {}

  return (
    <form action={action} className="flex flex-col gap-4" noValidate>
      <ErrorSummary state={state} />
      <input type="hidden" name="next" value={next} />
      <Field id="handle" label={t('auth.handle')} required error={errors.handle}>
        {(props) => (
          <Input
            {...props}
            name="handle"
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
            placeholder={t('auth.handlePlaceholder')}
          />
        )}
      </Field>
      <Field id="password" label={t('auth.password')} required error={errors.password}>
        {(props) => (
          <Input
            {...props}
            name="password"
            type="password"
            autoComplete="current-password"
            placeholder={t('auth.passwordPlaceholder')}
          />
        )}
      </Field>
      <SubmitButton label={t('auth.submitLogin')} pending={pending} />
      {/*
        这里没有「忘记密码」入口，是因为确实没有这个功能（adr/0011）：
        不收邮箱和手机号，也没有恢复码，忘记密码即永久失去账号。
        与其放一个点进去只会说「无法找回」的链接，不如不放。
      */}
      <div className="text-center text-sm">
        <Link href="/register" className="text-fg-muted hover:text-fg hover:underline">
          {t('auth.toRegister')}
        </Link>
      </div>
    </form>
  )
}

// ---------- 注册 ----------

/**
 * 注册表单。
 *
 * 字段顺序：用户名 → 密码 → 昵称 → 邀请码。
 * 邀请码放最后是因为它通常是从别处复制粘贴来的，前三项要用户现想；
 * 而且带邀请链接进来时它已经预填好了（见 inviteCode），放在开头会让人
 * 以为还要再填一次。
 */
export function RegisterForm({ next, inviteCode }: { next: string; inviteCode: string }) {
  const [state, action, pending] = useActionState(register, EMPTY)
  const errors = state.fieldErrors ?? {}

  return (
    <form action={action} className="flex flex-col gap-4" noValidate>
      <ErrorSummary state={state} />
      <input type="hidden" name="next" value={next} />
      <Field
        id="handle"
        label={t('auth.handle')}
        required
        hint={t('auth.handleHint')}
        error={errors.handle}
      >
        {(props) => (
          <Input
            {...props}
            name="handle"
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
            placeholder={t('auth.handlePlaceholder')}
          />
        )}
      </Field>
      <Field
        id="password"
        label={t('auth.password')}
        required
        hint={t('auth.passwordHint')}
        error={errors.password}
      >
        {(props) => (
          <Input
            {...props}
            name="password"
            type="password"
            autoComplete="new-password"
            placeholder={t('auth.passwordNewPlaceholder')}
          />
        )}
      </Field>
      <Field
        id="nickname"
        label={t('auth.nickname')}
        hint={t('auth.nicknameHint')}
        error={errors.nickname}
      >
        {(props) => (
          <Input {...props} name="nickname" placeholder={t('auth.nicknamePlaceholder')} />
        )}
      </Field>
      <Field
        id="invite_code"
        label={t('auth.inviteCode')}
        required
        hint={t('auth.inviteCodeHint')}
        error={errors.invite_code}
      >
        {(props) => (
          <Input
            {...props}
            name="invite_code"
            defaultValue={inviteCode}
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
            placeholder={t('auth.inviteCodePlaceholder')}
          />
        )}
      </Field>

      {/*
        不是勾选框，只是告知。
        恢复码与「忘记密码」已经删掉了（adr/0011），但「密码丢了就找不回」这件事
        仍然成立，而且后果不可逆——注册前让人看到它是必要的。
        做成静态提示而不是必须勾选的确认：不额外拦一道手，但也不藏起来。
      */}
      {/* <Alert tone="warning">{t('auth.noRecoveryNotice')}</Alert> */}

      <SubmitButton label={t('auth.submitRegister')} pending={pending} />
      <p className="text-xs text-fg-muted">{t('auth.noInvite')}</p>
      <div className="text-center text-sm">
        <Link href="/login" className="text-fg-muted hover:text-fg hover:underline">
          {t('auth.toLogin')}
        </Link>
      </div>
    </form>
  )
}

// 没有 RecoverForm：恢复码与「忘记密码」已删除（adr/0011）。
