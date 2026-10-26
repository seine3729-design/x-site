'use client'

import { useActionState, useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { Bug, Eye, Inbox, Lightbulb, Rocket, WifiOff, type LucideIcon } from 'lucide-react'
import { Alert } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, Input, Textarea } from '@/components/ui/field'
import { SelectField } from '@/components/ui/select'
import { cn } from '@/lib/utils'
import { t } from '@/lib/i18n'
import type { FbKind, FbProjectView } from '@/lib/api/types'
import { submitFeedback, type SubmitState } from '../actions'

/** 六种内容类型，顺序按运营希望用户优先考虑的程度排。 */
const KINDS: { value: FbKind; icon: LucideIcon }[] = [
  { value: 'feature', icon: Lightbulb },
  { value: 'content', icon: Inbox },
  { value: 'competitor', icon: Rocket },
  { value: 'bug', icon: Bug },
  { value: 'experience', icon: Eye },
  { value: 'access', icon: WifiOff },
]

/** 这两类强依赖设备与网络信息，选中后把「设备与网络」展开并标为建议填写。 */
const NEEDS_ENV = new Set<FbKind>(['bug', 'access', 'experience'])

const EMPTY: SubmitState = {}

/**
 * 提交反馈表单（feedback/docs/06 第 3.6 节）。
 *
 * 单页渐进式，不做多步向导：按类型展开额外字段，而不是一次把所有字段铺满。
 * 类型用卡片而不是下拉——它决定后面出现哪些字段，值得占这个空间。
 */
export function SubmitForm({
  projects,
  defaultProject,
}: {
  projects: FbProjectView[]
  defaultProject?: string
}) {
  const [state, action, pending] = useActionState(submitFeedback, EMPTY)
  const [kind, setKind] = useState<FbKind | ''>('')
  const errors = state.fieldErrors ?? {}
  const summaryRef = useRef<HTMLDivElement>(null)

  const errorCount = Object.keys(errors).length
  useEffect(() => {
    if (state.error || errorCount > 0) summaryRef.current?.focus()
  }, [state.error, errorCount])

  return (
    <form action={action} className="flex flex-col gap-7" noValidate>
      {(state.error || errorCount > 0) && (
        <Alert ref={summaryRef} tone="danger" tabIndex={-1} className="outline-none">
          {state.error ?? t('auth.errorSummary', { count: errorCount })}
        </Alert>
      )}

      <Field
        id="project_slug"
        label={t('submit.project')}
        required
        hint={t('submit.projectHint')}
        error={errors.project_slug}
      >
        {(props) => (
          <SelectField
            {...props}
            name="project_slug"
            // 这里必须是 undefined 而不是 ''：空串在 Radix Select 里表示「有一个空值选项」，
            // 会让占位文案不显示。未选状态交给 placeholder 表达。
            defaultValue={defaultProject || undefined}
            placeholder={t('submit.projectPlaceholder')}
            // 同上：没有 slug 的项目提交上去后端也认不出，直接不给选
            options={projects.flatMap((p) =>
              p.slug ? [{ value: p.slug, label: p.name ?? p.slug }] : [],
            )}
          />
        )}
      </Field>

      {/* 类型卡片：用 radiogroup 语义，键盘能用方向键在卡片间移动 */}
      <fieldset>
        <legend className="text-sm font-medium text-fg">
          {t('submit.kind')}
          <span className="ms-0.5 text-danger-fg" aria-hidden>
            *
          </span>
        </legend>
        <p className="mt-1 text-xs text-fg-muted">{t('submit.kindHint')}</p>
        <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {KINDS.map((item) => {
            const Icon = item.icon
            const checked = kind === item.value
            return (
              <label
                key={item.value}
                className={cn(
                  'flex cursor-pointer gap-3 rounded-surface border p-4 transition-colors duration-(--duration-hover)',
                  checked
                    ? 'border-primary bg-primary-surface'
                    : 'border-border bg-surface hover:border-control-border hover:bg-surface-raised',
                  // 焦点环跟着原生 radio 走，所以这里用 has 选择器把它画到卡片上
                  'has-focus-visible:outline-2 has-focus-visible:outline-offset-2 has-focus-visible:outline-ring',
                )}
              >
                <input
                  type="radio"
                  name="kind"
                  value={item.value}
                  checked={checked}
                  onChange={() => setKind(item.value)}
                  className="sr-only"
                />
                <Icon
                  className={cn('mt-0.5 size-5 shrink-0', checked ? 'text-primary-fg' : 'text-fg-subtle')}
                  aria-hidden
                />
                <span className="min-w-0">
                  <span className={cn('block text-sm font-medium', checked ? 'text-primary-fg' : 'text-fg')}>
                    {t(`kind.${item.value}` as 'kind.bug')}
                  </span>
                  <span className="mt-0.5 block text-xs text-fg-muted">
                    {t(`kind.${item.value}Hint` as 'kind.bugHint')}
                  </span>
                </span>
              </label>
            )
          })}
        </div>
        {errors.kind && <p className="mt-2 text-sm text-danger-fg">{errors.kind}</p>}
      </fieldset>

      <Field
        id="title"
        label={t('submit.feedbackTitle')}
        required
        hint={t('submit.titleHint')}
        error={errors.title}
      >
        {(props) => <Input {...props} name="title" placeholder={t('submit.titlePlaceholder')} />}
      </Field>

      <Field
        id="body"
        label={t('submit.body')}
        required
        hint={t('submit.bodyHint')}
        error={errors.body}
      >
        {(props) => (
          <Textarea {...props} name="body" rows={8} placeholder={t('submit.bodyPlaceholder')} />
        )}
      </Field>

      {/* 渐进展开：只有真正需要的类型才出现这两个字段，不一次铺满 */}
      {kind && NEEDS_ENV.has(kind) && (
        <>
          <Field id="env" label={t('submit.env')} hint={t('submit.envHint')} error={errors.env}>
            {(props) => <Input {...props} name="env" placeholder={t('submit.envPlaceholder')} />}
          </Field>
          <Field
            id="page_url"
            label={t('submit.pageUrl')}
            hint={t('submit.pageUrlHint')}
            error={errors.page_url}
          >
            {(props) => (
              <Input {...props} name="page_url" placeholder={t('submit.pageUrlPlaceholder')} />
            )}
          </Field>
        </>
      )}

      <Field
        id="visibility"
        label={t('submit.visibility')}
        hint={t('submit.visibilityHint')}
        error={errors.visibility}
      >
        {(props) => (
          <SelectField
            {...props}
            name="visibility"
            defaultValue="public"
            options={[
              { value: 'public', label: t('submit.visibilityPublicLabel') },
              { value: 'members', label: t('submit.visibilityMembersLabel') },
              { value: 'private', label: t('submit.visibilityPrivateLabel') },
            ]}
          />
        )}
      </Field>

      <Alert tone="warning">{t('submit.privacyNote')}</Alert>

      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <Button type="submit" size="lg" loading={pending} loadingLabel={t('submit.submitting')}>
          {t('submit.action')}
        </Button>
        <Button asChild variant="ghost" size="lg">
          <Link href="/feedback">{t('submit.searchAction')}</Link>
        </Button>
      </div>
    </form>
  )
}
