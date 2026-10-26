import { useId } from 'react'
import { Badge, Checkbox, Label, RadioGroup, RadioGroupItem } from '@/components/ui'
import type { Taxonomy } from '../api'
import { toggleTerm } from '../utils'

const NONE = 'none'

/**
 * 按维度选择取值：单选维度为单选按钮（含「不限」），多选维度为复选框；停用的取值标注「已停用」（仍可选，前台不显示）。
 * query 不为空时只显示名称包含它的取值。
 */
export function TermsSelector({
  taxonomies,
  value,
  onChange,
  query = '',
  disabled,
  noneLabel = '不限',
}: {
  taxonomies: Taxonomy[]
  value: readonly number[]
  onChange: (next: number[]) => void
  query?: string
  disabled?: boolean
  noneLabel?: string
}) {
  const baseId = useId()
  const q = query.trim()
  return (
    <div className='grid gap-3 md:grid-cols-2'>
      {taxonomies.map((x) => {
        const terms = q ? x.terms.filter((t) => t.name.includes(q)) : x.terms
        if (q && terms.length === 0) return null
        const chosen = x.terms.filter((t) => value.includes(t.id))
        return (
          <fieldset key={x.id} className='flex flex-col gap-2 rounded-md border p-3' disabled={disabled}>
            <legend className='flex items-center gap-2 px-1 text-sm font-medium'>
              {x.name}
              <Badge variant='outline'>{x.multi_select ? '多选' : '单选'}</Badge>
              {x.status === 'inactive' && <Badge variant='outline'>维度已停用</Badge>}
            </legend>
            {x.terms.length === 0 ? (
              <p className='text-xs text-muted-foreground'>这个维度还没有取值。</p>
            ) : x.multi_select ? (
              <div className='flex flex-wrap gap-x-4 gap-y-2'>
                {terms.map((t) => {
                  const id = `${baseId}-${t.id}`
                  return (
                    <div key={t.id} className='flex items-center gap-2'>
                      <Checkbox id={id} checked={value.includes(t.id)} onCheckedChange={() => onChange(toggleTerm(value, x, t))} />
                      <Label htmlFor={id} className='font-normal'>
                        {t.name}
                        {t.status === 'inactive' && <span className='text-xs text-muted-foreground'>（已停用）</span>}
                      </Label>
                    </div>
                  )
                })}
              </div>
            ) : (
              <RadioGroup
                className='flex flex-wrap gap-x-4 gap-y-2'
                value={chosen[0] ? String(chosen[0].id) : NONE}
                onValueChange={(v) => {
                  const others = value.filter((id) => !x.terms.some((t) => t.id === id))
                  onChange(v === NONE ? others : [...others, Number(v)])
                }}
              >
                {!q && (
                  <div className='flex items-center gap-2'>
                    <RadioGroupItem id={`${baseId}-${x.id}-none`} value={NONE} />
                    <Label htmlFor={`${baseId}-${x.id}-none`} className='font-normal text-muted-foreground'>
                      {noneLabel}
                    </Label>
                  </div>
                )}
                {terms.map((t) => {
                  const id = `${baseId}-${t.id}`
                  return (
                    <div key={t.id} className='flex items-center gap-2'>
                      <RadioGroupItem id={id} value={String(t.id)} />
                      <Label htmlFor={id} className='font-normal'>
                        {t.name}
                        {t.status === 'inactive' && <span className='text-xs text-muted-foreground'>（已停用）</span>}
                      </Label>
                    </div>
                  )
                })}
              </RadioGroup>
            )}
          </fieldset>
        )
      })}
    </div>
  )
}
