'use client'

import { useState } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'
import { Search, SlidersHorizontal, X } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field, Input } from '@/components/ui/field'
import { SelectField } from '@/components/ui/select'
import { Sheet } from '@/components/ui/sheet'
import { t } from '@/lib/i18n'

/*
 * 这里的筛选抽屉**刻意不做懒加载**。
 *
 * 试过：它只在移动端出现，看着像是桌面端白拿的减重。实测关键路径只少 1.1 KB
 * （169.2 → 168.1 KB br）—— 因为 Radix Dialog 的大头是 dismissable-layer
 * 与 focus-scope，而同一页的 Radix Select 也用它们，拆不出去。
 * 1 KB 换一层 dynamic 间接不划算，所以维持直接 import。
 */
import type { FbKind, FbProjectView, FbStatus } from '@/lib/api/types'

/**
 * 「不限」选项的哨兵值。
 *
 * Radix Select 的 Item 不接受空字符串（空串是它内部用来表示「未选」的），
 * 所以「全部项目」不能直接用 value=""。这里用一个不会和真实 slug / 状态值
 * 冲突的哨兵，写进 URL 之前再换回空串，URL 的样子不受影响。
 */
const ANY = '~any'

const STATUSES: FbStatus[] = [
  'submitted',
  'triaged',
  'planned',
  'in_progress',
  'verifying',
  'shipped',
  'closed',
  'duplicate',
  'declined',
]

const KINDS: FbKind[] = [
  'bug',
  'experience',
  'feature',
  'content',
  'access',
  'competitor',
  'other',
]

const SORTS = ['latest', 'newest', 'votes'] as const

/**
 * 反馈列表的搜索与筛选（feedback/docs/06 第 3.4 节）。
 *
 * 所有条件都写进 URL：刷新、分享、浏览器返回都能回到同一个列表。
 * 桌面常驻一行；移动端收进底部抽屉，按钮上显示已选数量，
 * 这样小屏不会被一排下拉占掉半屏。
 */
export function FeedbackFilters({ projects }: { projects: FbProjectView[] }) {
  const router = useRouter()
  const params = useSearchParams()
  const [open, setOpen] = useState(false)

  const current = {
    q: params.get('q') ?? '',
    project: params.get('project') ?? '',
    status: params.get('status') ?? '',
    kind: params.get('kind') ?? '',
    sort: params.get('sort') ?? 'latest',
  }

  /** 改任一条件都回到第 1 页：留在第 5 页看一个只有 2 页的结果没有意义。 */
  const apply = (patch: Partial<typeof current>) => {
    const next = new URLSearchParams()
    const merged = { ...current, ...patch }
    for (const [key, value] of Object.entries(merged)) {
      if (!value) continue
      if (key === 'sort' && value === 'latest') continue
      next.set(key, value)
    }
    const query = next.toString()
    router.push(query ? `/feedback?${query}` : '/feedback')
  }

  const activeCount = [current.project, current.status, current.kind].filter(Boolean).length
  const hasAny = activeCount > 0 || !!current.q || current.sort !== 'latest'

  const controls = (prefix: string) => (
    <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:gap-3">
      <Field id={`${prefix}-project`} label={t('list.project')} className="sm:w-44">
        {(props) => (
          <SelectField
            {...props}
            value={current.project || ANY}
            onValueChange={(v) => apply({ project: v === ANY ? '' : v })}
            options={[
              { value: ANY, label: t('list.allProjects') },
              // 契约里 slug 是可选字段。没有 slug 的项目拼不出有效筛选值，
              // 与其塞一个空串选项（选了等于清空筛选），不如不出现在列表里。
              ...projects.flatMap((p) =>
                p.slug ? [{ value: p.slug, label: p.name ?? p.slug }] : [],
              ),
            ]}
          />
        )}
      </Field>
      <Field id={`${prefix}-status`} label={t('list.status')} className="sm:w-36">
        {(props) => (
          <SelectField
            {...props}
            value={current.status || ANY}
            onValueChange={(v) => apply({ status: v === ANY ? '' : v })}
            options={[
              { value: ANY, label: t('list.allStatus') },
              ...STATUSES.map((s) => ({
                value: s,
                label: t(`status.${s}` as 'status.submitted'),
              })),
            ]}
          />
        )}
      </Field>
      <Field id={`${prefix}-kind`} label={t('list.kind')} className="sm:w-36">
        {(props) => (
          <SelectField
            {...props}
            value={current.kind || ANY}
            onValueChange={(v) => apply({ kind: v === ANY ? '' : v })}
            options={[
              { value: ANY, label: t('list.allKinds') },
              ...KINDS.map((k) => ({ value: k, label: t(`kind.${k}` as 'kind.bug') })),
            ]}
          />
        )}
      </Field>
      <Field id={`${prefix}-sort`} label={t('list.sort')} className="sm:w-40">
        {(props) => (
          <SelectField
            {...props}
            value={current.sort}
            onValueChange={(v) => apply({ sort: v })}
            options={SORTS.map((s) => ({
              value: s,
              label: t(
                s === 'latest'
                  ? 'list.sortLatest'
                  : s === 'newest'
                    ? 'list.sortNewest'
                    : 'list.sortVotes',
              ),
            }))}
          />
        )}
      </Field>
    </div>
  )

  return (
    <div className="flex flex-col gap-3">
      <div className="flex gap-2">
        <form
          className="flex min-w-0 flex-1 gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            const value = new FormData(e.currentTarget).get('q')
            apply({ q: typeof value === 'string' ? value.trim() : '' })
          }}
        >
          <Input
            name="q"
            type="search"
            defaultValue={current.q}
            placeholder={t('list.searchPlaceholder')}
            aria-label={t('list.searchPlaceholder')}
            className="min-w-0 flex-1"
          />
          <Button type="submit" variant="outline" size="icon" aria-label={t('action.search')}>
            <Search aria-hidden />
          </Button>
        </form>

        {/* 移动端：筛选收进抽屉，按钮上标出已选数量 */}
        <Button
          variant="outline"
          size="icon"
          className="sm:hidden"
          onClick={() => setOpen(true)}
          aria-label={
            activeCount > 0
              ? `${t('list.filterTitle')}，${t('list.filterSelected', { count: activeCount })}`
              : t('list.filterTitle')
          }
        >
          <SlidersHorizontal aria-hidden />
          {activeCount > 0 && (
            <span className="absolute -inset-e-1 -top-1 flex size-5 items-center justify-center rounded-pill bg-primary text-2xs font-medium text-fg-on-fill tabular">
              {activeCount}
            </span>
          )}
        </Button>
      </div>

      <div className="hidden sm:block">{controls('desktop')}</div>

      {hasAny && (
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={() => router.push('/feedback')}>
            <X aria-hidden />
            {t('action.reset')}
          </Button>
          {current.q && <Badge tone="brand">{current.q}</Badge>}
        </div>
      )}

      <Sheet
        open={open}
        onOpenChange={setOpen}
        title={t('list.filterTitle')}
        description={t('list.filterDescription')}
        footer={
          <Button block onClick={() => setOpen(false)}>
            {t('list.filterApply')}
          </Button>
        }
      >
        <div className="flex flex-col gap-4">{controls('sheet')}</div>
      </Sheet>
    </div>
  )
}
