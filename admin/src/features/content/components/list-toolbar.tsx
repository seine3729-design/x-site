import { useState } from 'react'
import { RotateCcw, Search } from 'lucide-react'
import { Button, Input, Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui'

/** 搜索框：回车或点「搜索」后才提交（写入地址栏）；地址栏里的关键词变化时（如点了重置）同步回来。 */
export function SearchBox({
  value,
  placeholder,
  onSubmit,
  label = '搜索',
}: {
  value: string | undefined
  placeholder: string
  onSubmit: (q: string | undefined) => void
  label?: string
}) {
  return <SearchBoxInner key={value ?? ''} value={value ?? ''} placeholder={placeholder} onSubmit={onSubmit} label={label} />
}

function SearchBoxInner({
  value,
  placeholder,
  onSubmit,
  label,
}: {
  value: string
  placeholder: string
  onSubmit: (q: string | undefined) => void
  label: string
}) {
  const [draft, setDraft] = useState(value)
  return (
    <form
      className='flex items-center gap-2'
      onSubmit={(e) => {
        e.preventDefault()
        onSubmit(draft.trim() || undefined)
      }}
    >
      <Input
        aria-label={label}
        className='h-9 w-56'
        placeholder={placeholder}
        maxLength={50}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
      />
      <Button type='submit' variant='secondary' className='h-9'>
        <Search className='size-4' aria-hidden /> 搜索
      </Button>
    </form>
  )
}

const ALL = '__all__'

/** 筛选下拉框：第一项为「全部」。 */
export function FilterSelect({
  label,
  value,
  options,
  onChange,
  className = 'w-36',
}: {
  label: string
  value: string | undefined
  options: { label: string; value: string }[]
  onChange: (value: string | undefined) => void
  className?: string
}) {
  return (
    <Select value={value ?? ALL} onValueChange={(v) => onChange(v === ALL ? undefined : v)}>
      <SelectTrigger className={`h-9 ${className}`} aria-label={label}>
        <SelectValue placeholder={label} />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ALL}>{label}：全部</SelectItem>
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

/** 清除全部筛选条件。 */
export function ResetButton({ disabled, onClick }: { disabled: boolean; onClick: () => void }) {
  return (
    <Button type='button' variant='ghost' className='h-9' disabled={disabled} onClick={onClick}>
      <RotateCcw className='size-4' aria-hidden /> 重置
    </Button>
  )
}
