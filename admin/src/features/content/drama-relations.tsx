import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useBlocker } from '@tanstack/react-router'
import { Save, Undo2, X } from 'lucide-react'
import { toast } from 'sonner'
import { Badge, Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Input, Label } from '@/components/ui'
import { LoadError, WithReason } from '@/features/access/shared'
import { useCan } from '@/stores/auth-store'
import { contentApi, contentKeys, taxonomiesQuery, type DramaDetail, type DramaTerm } from './api'
import { SearchPicker, type PickerOption } from './components/search-picker'
import { TermsSelector } from './components/terms-selector'
import { searchTags } from './tags-page'
import { lookupReason, sameMembers } from './utils'

/** 分区底部的保存栏；有未保存的修改时离开或切换分区前提醒。 */
function SaveBar({
  what,
  dirty,
  editable,
  saving,
  onSave,
  onReset,
}: {
  what: string
  dirty: boolean
  editable: boolean
  saving: boolean
  onSave: () => void
  onReset: () => void
}) {
  useBlocker({
    shouldBlockFn: () => dirty && !window.confirm(`${what}的修改尚未保存，确定离开吗？`),
    enableBeforeUnload: () => dirty,
  })
  return (
    <div className='sticky bottom-4 z-10 flex flex-wrap items-center justify-end gap-2 rounded-lg border bg-background/95 p-2 shadow-sm backdrop-blur'>
      <span className='me-auto ps-2 text-sm text-muted-foreground'>{dirty ? '有未保存的修改' : null}</span>
      <Button type='button' variant='ghost' disabled={!dirty || saving} onClick={onReset}>
        <Undo2 className='size-4' /> 撤销修改
      </Button>
      <WithReason reason={editable ? null : '当前角色只能查看'}>
        <Button type='button' disabled={!editable || !dirty || saving} onClick={onSave}>
          <Save className='size-4' /> {saving ? '保存中…' : `保存${what}`}
        </Button>
      </WithReason>
    </div>
  )
}

function useUpdateDetail(id: number) {
  const qc = useQueryClient()
  return (patch: Partial<DramaDetail>) => {
    qc.setQueryData<DramaDetail>(contentKeys.drama(id), (old) => (old ? { ...old, ...patch } : old))
    void qc.invalidateQueries({ queryKey: [...contentKeys.dramas, 'list'] })
  }
}

// ---------- 标签 ----------

/** 标签（docs/06 第 5 节）：搜标签多选；已并入其他标签的不能选。 */
export function TagsSection({ drama, editable }: { drama: DramaDetail; editable: boolean }) {
  const tagsReason = lookupReason(useCan(), 'tags')
  const initial = drama.tags.map((t) => ({ value: t.id, label: t.name }))
  const [tags, setTags] = useState<PickerOption[]>(initial)
  const update = useUpdateDetail(drama.id)
  const dirty = !sameMembers(
    tags.map((t) => t.value),
    initial.map((t) => t.value)
  )
  const toggle = (o: PickerOption) =>
    setTags((cur) => (cur.some((t) => t.value === o.value) ? cur.filter((t) => t.value !== o.value) : [...cur, o]))
  const save = useMutation({
    mutationFn: () => contentApi.setDramaTags(drama.id, tags.map((t) => t.value)),
    onSuccess: (res) => {
      toast.success(`已保存标签（${res.tags.length} 个）`)
      update({ tags: res.tags })
    },
  })

  return (
    <div className='flex flex-col gap-4'>
      <Card>
        <CardHeader className='flex flex-row flex-wrap items-start justify-between gap-2'>
          <div className='flex flex-col gap-1.5'>
            <CardTitle className='text-base'>标签（已选 {tags.length} 个）</CardTitle>
            <CardDescription>敏感标签过年龄门才显示。找不到合适的标签时，先到「标签」中新建。</CardDescription>
          </div>
          {editable && (
            <WithReason reason={tagsReason}>
              <SearchPicker
                queryKey={contentKeys.tags}
                search={(q) => searchTags(q)}
                selected={tags.map((t) => t.value)}
                onSelect={toggle}
                multiple
                trigger='搜索并勾选标签'
                placeholder='搜名称或 slug'
                ariaLabel='选择标签'
                disabled={!!tagsReason}
                className='w-48'
              />
            </WithReason>
          )}
        </CardHeader>
        <CardContent>
          {tags.length === 0 ? (
            <p className='rounded-md border border-dashed py-8 text-center text-sm text-muted-foreground'>还没有标签。</p>
          ) : (
            <ul className='flex flex-wrap gap-2' aria-label='已选标签'>
              {tags.map((t) => (
                <li key={t.value}>
                  <Badge variant='secondary' className='gap-1 py-1 text-sm'>
                    {t.label}
                    {editable && (
                      <button type='button' disabled={save.isPending} aria-label={`移除标签 ${t.label}`} onClick={() => toggle(t)}>
                        <X className='size-3.5' />
                      </button>
                    )}
                  </Badge>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
      <SaveBar what='标签' dirty={dirty} editable={editable} saving={save.isPending} onSave={() => save.mutate()} onReset={() => setTags(initial)} />
    </div>
  )
}

// ---------- 维度取值 ----------

/** 维度取值（docs/06 第 5 节）：按维度单选或多选，可以搜索取值；停用的取值有标注。 */
export function TermsSection({ drama, editable }: { drama: DramaDetail; editable: boolean }) {
  // 列出全部维度要用「筛选维度」页面的读接口；没有查看权限时只显示本剧已选的取值
  const termsReason = lookupReason(useCan(), 'taxonomies')
  const taxonomies = useQuery({ ...taxonomiesQuery, enabled: !termsReason })
  const initial = drama.terms.map((t) => t.id)
  const [terms, setTerms] = useState<number[]>(initial)
  const [query, setQuery] = useState('')
  const update = useUpdateDetail(drama.id)
  const dirty = !sameMembers(terms, initial)
  const save = useMutation({
    mutationFn: () => contentApi.setDramaTerms(drama.id, terms),
    onSuccess: (res) => {
      toast.success(`已保存维度取值（${res.terms.length} 个）`)
      update({ terms: res.terms })
    },
  })
  const list = taxonomies.data ?? []

  return (
    <div className='flex flex-col gap-4'>
      <Card>
        <CardHeader className='flex flex-row flex-wrap items-start justify-between gap-2'>
          <div className='flex flex-col gap-1.5'>
            <CardTitle className='text-base'>维度取值（已选 {terms.length} 个）</CardTitle>
            <CardDescription>单选维度最多选一个，选「不限」表示不选；停用的取值前台不显示。</CardDescription>
          </div>
          {!termsReason && (
            <div className='flex flex-col gap-1'>
              <Label htmlFor='terms-query' className='sr-only'>
                搜取值
              </Label>
              <Input id='terms-query' className='h-9 w-48' placeholder='搜取值名称' value={query} onChange={(e) => setQuery(e.target.value)} />
            </div>
          )}
        </CardHeader>
        <CardContent>
          {termsReason ? (
            <ChosenTerms terms={drama.terms} reason={termsReason} />
          ) : taxonomies.isError ? (
            <LoadError onRetry={() => void taxonomies.refetch()} />
          ) : taxonomies.isPending ? (
            <p className='py-6 text-center text-sm text-muted-foreground'>加载中…</p>
          ) : list.length === 0 ? (
            <p className='rounded-md border border-dashed py-8 text-center text-sm text-muted-foreground'>还没有筛选维度，先到「筛选维度」中新建。</p>
          ) : (
            <TermsSelector taxonomies={list} value={terms} onChange={setTerms} query={query} disabled={!editable || save.isPending} />
          )}
        </CardContent>
      </Card>
      {!termsReason && (
        <SaveBar what='维度取值' dirty={dirty} editable={editable} saving={save.isPending} onSave={() => save.mutate()} onReset={() => setTerms(initial)} />
      )}
    </div>
  )
}

/** 没有「筛选维度」查看权限时，按维度列出本剧已选的取值（只读）。 */
function ChosenTerms({ terms, reason }: { terms: DramaTerm[]; reason: string }) {
  const groups = new Map<string, DramaTerm[]>()
  for (const t of terms) groups.set(t.taxonomy_name, [...(groups.get(t.taxonomy_name) ?? []), t])
  return (
    <div className='flex flex-col gap-3'>
      <p className='text-sm text-muted-foreground'>{reason}才能看到全部取值并修改，下面是本剧已选的取值。</p>
      {terms.length === 0 ? (
        <p className='rounded-md border border-dashed py-8 text-center text-sm text-muted-foreground'>还没有选择取值。</p>
      ) : (
        <dl className='flex flex-col gap-2'>
          {[...groups].map(([name, list]) => (
            <div key={name} className='flex flex-wrap items-center gap-2'>
              <dt className='w-28 shrink-0 text-sm font-medium'>{name}</dt>
              <dd className='flex flex-wrap gap-1'>
                {list.map((t) => (
                  <Badge key={t.id} variant='secondary'>
                    {t.name}
                    {t.status === 'inactive' ? '（已停用）' : ''}
                  </Badge>
                ))}
              </dd>
            </div>
          ))}
        </dl>
      )}
    </div>
  )
}
