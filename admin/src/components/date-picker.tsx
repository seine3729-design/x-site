import * as React from 'react'
import { zhCN } from 'date-fns/locale'
import { CalendarIcon, ChevronLeft, ChevronRight, Clock } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'

/**
 * 后台统一的时间选择器（docs/06 开头的布局说明）。
 *
 * 替代所有 `<input type="date|datetime-local|month">`：原生控件在各浏览器里长相、
 * 交互、语言都不一致（Safari 甚至没有日历面板），后台只用中文，所以统一走这里。
 *
 * **值是字符串，格式与它替代的原生 input 完全一致**，调用点不需要做任何转换：
 * - `mode='date'`     → `yyyy-MM-dd`
 * - `mode='datetime'` → `yyyy-MM-ddTHH:mm`（与 datetime-local 一致，本地时间）
 * - `mode='month'`    → `yyyy-MM`
 *
 * 空字符串表示未选择。
 */
export type DatePickerMode = 'date' | 'datetime' | 'month'

type DatePickerProps = {
  mode?: DatePickerMode
  value: string
  onChange: (value: string) => void
  /** 未选择时触发器上显示的文字。 */
  placeholder?: string
  id?: string
  disabled?: boolean
  /** 是否提供「清除」。默认提供；必填项传 false。 */
  clearable?: boolean
  /** 可选下限 / 上限，格式与 value 相同。 */
  min?: string
  max?: string
  /** 触发器样式。默认 `h-10 w-full`，工具栏里按需传 `h-9 w-36`。 */
  className?: string
  'aria-label'?: string
  'aria-invalid'?: boolean
  'aria-describedby'?: string
}

const pad = (n: number) => String(n).padStart(2, '0')
const dayStr = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`

type Parsed = { day?: Date; hour: number; minute: number; year?: number; month?: number }

/** 宽松解析：允许秒、允许用空格代替 T，解析不出来就当未选择。 */
function parseValue(mode: DatePickerMode, value: string): Parsed {
  const v = (value ?? '').trim()
  if (mode === 'month') {
    const m = /^(\d{4})-(\d{1,2})$/.exec(v)
    return m ? { year: Number(m[1]), month: Number(m[2]) - 1, hour: 0, minute: 0 } : { hour: 0, minute: 0 }
  }
  const m = /^(\d{4})-(\d{1,2})-(\d{1,2})(?:[T ](\d{1,2}):(\d{1,2}))?/.exec(v)
  if (!m) return { hour: 0, minute: 0 }
  return {
    day: new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])),
    hour: m[4] ? Number(m[4]) : 0,
    minute: m[5] ? Number(m[5]) : 0,
  }
}

function toDate(mode: DatePickerMode, value?: string): Date | undefined {
  if (!value) return undefined
  const p = parseValue(mode, value)
  if (mode === 'month') return p.year === undefined ? undefined : new Date(p.year, p.month ?? 0, 1)
  return p.day
}

/** 触发器上显示的文字；与表格里的时间列对齐，用 `yyyy-MM-dd HH:mm`。 */
function displayText(mode: DatePickerMode, p: Parsed): string {
  if (mode === 'month') return p.year === undefined ? '' : `${p.year}-${pad((p.month ?? 0) + 1)}`
  if (!p.day) return ''
  const d = dayStr(p.day)
  return mode === 'datetime' ? `${d} ${pad(p.hour)}:${pad(p.minute)}` : d
}

/** 时 / 分滚动列。刻意不用 Radix Select：它会 portal 到弹层外面，点一下就把 Popover 关掉。 */
function TimeColumn({
  label,
  count,
  value,
  onSelect,
  disabled,
}: {
  label: string
  count: number
  value: number
  onSelect: (v: number) => void
  disabled?: boolean
}) {
  const activeRef = React.useRef<HTMLButtonElement>(null)
  React.useEffect(() => {
    activeRef.current?.scrollIntoView({ block: 'center' })
  }, [])
  return (
    <div className='flex min-w-0 flex-col'>
      <span className='px-1 pb-1 text-center text-xs text-muted-foreground'>{label}</span>
      <div className='flex max-h-56 flex-col gap-0.5 overflow-y-auto overscroll-contain pe-1' role='listbox' aria-label={label}>
        {Array.from({ length: count }, (_, i) => (
          <Button
            key={i}
            ref={i === value ? activeRef : undefined}
            type='button'
            size='sm'
            variant={i === value ? 'default' : 'ghost'}
            role='option'
            aria-selected={i === value}
            disabled={disabled}
            className='h-8 w-11 shrink-0 font-normal tabular-nums'
            onClick={() => onSelect(i)}
          >
            {pad(i)}
          </Button>
        ))}
      </div>
    </div>
  )
}

export function DatePicker({
  mode = 'date',
  value,
  onChange,
  placeholder,
  id,
  disabled,
  clearable = true,
  min,
  max,
  className,
  'aria-label': ariaLabel,
  'aria-invalid': ariaInvalid,
  'aria-describedby': ariaDescribedBy,
}: DatePickerProps) {
  const [open, setOpen] = React.useState(false)
  const parsed = parseValue(mode, value)
  const text = displayText(mode, parsed)
  const hint = placeholder ?? (mode === 'month' ? '请选择月份' : mode === 'datetime' ? '请选择日期与时间' : '请选择日期')

  const minDate = toDate(mode, min)
  const maxDate = toDate(mode, max)

  // 月份模式里翻年用的本地视图状态，每次打开时回到当前值所在年份
  const [viewYear, setViewYear] = React.useState(parsed.year ?? new Date().getFullYear())
  React.useEffect(() => {
    if (open) setViewYear(parseValue(mode, value).year ?? new Date().getFullYear())
  }, [open, mode, value])

  const emitDay = (day: Date | undefined) => {
    if (!day) return
    onChange(mode === 'datetime' ? `${dayStr(day)}T${pad(parsed.hour)}:${pad(parsed.minute)}` : dayStr(day))
    if (mode === 'date') setOpen(false)
  }

  const emitTime = (hour: number, minute: number) => {
    const day = parsed.day ?? new Date()
    onChange(`${dayStr(day)}T${pad(hour)}:${pad(minute)}`)
  }

  const emitMonth = (year: number, month: number) => {
    onChange(`${year}-${pad(month + 1)}`)
    setOpen(false)
  }

  const now = new Date()
  const monthOutOfRange = (year: number, month: number) => {
    const first = new Date(year, month, 1)
    if (minDate && first < new Date(minDate.getFullYear(), minDate.getMonth(), 1)) return true
    if (maxDate && first > new Date(maxDate.getFullYear(), maxDate.getMonth(), 1)) return true
    return false
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          id={id}
          type='button'
          variant='outline'
          disabled={disabled}
          data-empty={!text}
          data-slot='date-picker-trigger'
          aria-label={ariaLabel}
          aria-invalid={ariaInvalid}
          aria-describedby={ariaDescribedBy}
          className={cn(
            'h-10 w-full justify-start gap-2 px-3 font-normal tabular-nums data-[empty=true]:font-sans data-[empty=true]:text-muted-foreground',
            className
          )}
        >
          {mode === 'datetime' ? <Clock className='opacity-50' /> : <CalendarIcon className='opacity-50' />}
          <span className='truncate'>{text || hint}</span>
        </Button>
      </PopoverTrigger>

      <PopoverContent className='w-auto p-0' align='start'>
        {mode === 'month' ? (
          <div className='p-3'>
            <div className='mb-2 flex items-center justify-between'>
              <Button
                type='button'
                variant='ghost'
                size='icon'
                className='size-8'
                aria-label='上一年'
                onClick={() => setViewYear((y) => y - 1)}
              >
                <ChevronLeft />
              </Button>
              <span className='text-sm font-medium tabular-nums'>{viewYear} 年</span>
              <Button
                type='button'
                variant='ghost'
                size='icon'
                className='size-8'
                aria-label='下一年'
                onClick={() => setViewYear((y) => y + 1)}
              >
                <ChevronRight />
              </Button>
            </div>
            <div className='grid grid-cols-3 gap-1'>
              {Array.from({ length: 12 }, (_, i) => (
                <Button
                  key={i}
                  type='button'
                  variant={parsed.year === viewYear && parsed.month === i ? 'default' : 'ghost'}
                  className='h-9 w-16 font-normal'
                  disabled={monthOutOfRange(viewYear, i)}
                  onClick={() => emitMonth(viewYear, i)}
                >
                  {i + 1} 月
                </Button>
              ))}
            </div>
          </div>
        ) : (
          <div className='flex items-start divide-x'>
            <Calendar
              mode='single'
              locale={zhCN}
              captionLayout='dropdown'
              startMonth={minDate ?? new Date(now.getFullYear() - 10, 0)}
              endMonth={maxDate ?? new Date(now.getFullYear() + 10, 11)}
              defaultMonth={parsed.day}
              selected={parsed.day}
              onSelect={emitDay}
              disabled={[...(minDate ? [{ before: minDate }] : []), ...(maxDate ? [{ after: maxDate }] : [])]}
              formatters={{ formatMonthDropdown: (date) => `${date.getMonth() + 1} 月` }}
            />
            {mode === 'datetime' && (
              <div className='flex gap-1 p-3 ps-2'>
                <TimeColumn label='时' count={24} value={parsed.hour} onSelect={(h) => emitTime(h, parsed.minute)} />
                <TimeColumn label='分' count={60} value={parsed.minute} onSelect={(m) => emitTime(parsed.hour, m)} />
              </div>
            )}
          </div>
        )}

        <div className='flex items-center justify-between gap-2 border-t p-2'>
          {clearable ? (
            <Button
              type='button'
              variant='ghost'
              size='sm'
              disabled={!value}
              onClick={() => {
                onChange('')
                setOpen(false)
              }}
            >
              清除
            </Button>
          ) : (
            <span />
          )}
          <div className='flex items-center gap-1'>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              onClick={() => {
                if (mode === 'month') {
                  emitMonth(now.getFullYear(), now.getMonth())
                } else if (mode === 'datetime') {
                  onChange(`${dayStr(now)}T${pad(now.getHours())}:${pad(now.getMinutes())}`)
                } else {
                  onChange(dayStr(now))
                  setOpen(false)
                }
              }}
            >
              {mode === 'month' ? '本月' : mode === 'datetime' ? '现在' : '今天'}
            </Button>
            {mode === 'datetime' && (
              <Button type='button' size='sm' onClick={() => setOpen(false)}>
                完成
              </Button>
            )}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}
