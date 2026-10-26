import { useState } from 'react'
import { Crown, Flame, Sparkles, type LucideIcon } from 'lucide-react'
import type { Control, FieldPath, FieldValues } from 'react-hook-form'
import { Input, Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui'
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'

/**
 * 角标预设（docs/06 第 5 节「剧资料」）。选预设时 drama.badge 存的是这里的 value（如 hot），
 * 前台按它选样式、文案按用户的界面语言渲染（9 种语言）。
 *
 * 和前台两处一一对应，改这里要同步改：
 * - apps/src/lib/drama/badge.ts（预设表与文案键）
 * - apps/src/app.css 的 .badge-*（配色）
 */
type BadgePreset = {
  value: string
  label: string
  /** 预览用，和前台 .badge-* 的渐变保持一致 */
  background: string
  /** 预览用的文字色，默认白色 */
  color?: string
  icon?: LucideIcon
  /** 早期手填的写法（前台也认这些，会按本预设渲染），保存时统一成 value */
  aliases: string[]
}

export const BADGE_PRESETS: BadgePreset[] = [
  { value: 'hot', label: '热播', background: 'linear-gradient(135deg,#ff8f3d,#ef2d56 72%)', icon: Flame, aliases: ['热播', '热门', '最热', '热'] },
  { value: 'new', label: '新剧', background: 'linear-gradient(135deg,#37dfc6,#0b86c4 76%)', icon: Sparkles, aliases: ['新剧', '最新', '上新', '新'] },
  { value: 'exclusive', label: '独家', background: 'linear-gradient(135deg,#e8536f,#bd183c)', aliases: ['独家', '独播', '独家首发'] },
  { value: 'trending', label: '爆款', background: 'linear-gradient(135deg,#ff79c6,#9d20b4 76%)', aliases: ['爆款', '爆火', '霸榜'] },
  { value: 'preview', label: '预告', background: 'linear-gradient(135deg,#8b9cff,#3f36c9 76%)', aliases: ['soon', '预告', '预上线', '即将上线'] },
  { value: 'free', label: '限免', background: 'linear-gradient(135deg,#58e08a,#0f7d3c 76%)', aliases: ['限免', '免费', '免费看'] },
  { value: 'finished', label: '完结', background: 'rgba(17,17,24,0.72)', aliases: ['end', 'ended', '完结', '已完结', '全集'] },
  {
    value: 'vip',
    label: '会员',
    background: 'linear-gradient(135deg,#f9c45d,#f59e0b)',
    color: '#111118',
    icon: Crown,
    aliases: ['svip', '会员', '会员专享'],
  },
]

/** 预设 id 或老数据的写法都归到对应预设；认不出来才算自定义文案 */
function findPreset(value: string): BadgePreset | undefined {
  const v = value.trim().toLowerCase()
  if (v === '') return undefined
  return BADGE_PRESETS.find((p) => p.value === v || p.aliases.includes(v))
}

const NONE = '__none'
const CUSTOM = '__custom'

/** 下拉框与输入框旁边的预览角标，配色、圆角、字号对齐前台 */
function BadgeChip({ preset, text }: { preset?: BadgePreset; text?: string }) {
  const Icon = preset?.icon
  return (
    <span
      className='inline-flex max-w-40 items-center gap-0.5 truncate rounded-md px-1.5 py-px text-[11px] leading-4 font-bold text-white'
      style={{ background: preset?.background ?? 'var(--primary)', color: preset?.color }}
    >
      {Icon && <Icon className='size-3 text-current' aria-hidden />}
      {preset ? preset.label : text}
    </span>
  )
}

function BadgeControl({
  value,
  onChange,
  disabled,
}: {
  value: string
  onChange: (value: string) => void
  disabled?: boolean
}) {
  const preset = findPreset(value)
  // 认不出的文案就是自定义；选了「自定义」但还没填时也要把输入框留着，所以单独记一个状态
  const [custom, setCustom] = useState(() => value !== '' && !preset)
  const mode = custom ? CUSTOM : (preset?.value ?? NONE)

  return (
    <div className='flex flex-col gap-2'>
      <Select
        value={mode}
        onValueChange={(next) => {
          if (next === CUSTOM) {
            setCustom(true)
            onChange('')
            return
          }
          setCustom(false)
          onChange(next === NONE ? '' : next)
        }}
      >
        <FormControl>
          <SelectTrigger className='w-full' disabled={disabled}>
            <SelectValue placeholder='请选择角标' />
          </SelectTrigger>
        </FormControl>
        <SelectContent>
          <SelectItem value={NONE}>不显示角标</SelectItem>
          {BADGE_PRESETS.map((p) => (
            <SelectItem key={p.value} value={p.value}>
              <BadgeChip preset={p} />
            </SelectItem>
          ))}
          <SelectItem value={CUSTOM}>自定义文案</SelectItem>
        </SelectContent>
      </Select>
      {custom && (
        <div className='flex items-center gap-2'>
          <Input
            autoComplete='off'
            placeholder='请输入角标文案，最多 16 个字'
            value={value}
            onChange={(e) => onChange(e.target.value)}
            disabled={disabled}
            className='h-10'
          />
          {value.trim() !== '' && <BadgeChip text={value.trim()} />}
        </div>
      )}
    </div>
  )
}

/** 角标：预设（前台有配套样式、文案自动翻译）或自定义文案。值存在一个字符串字段里。 */
export function BadgeField<T extends FieldValues>({
  control,
  name,
  disabled,
}: {
  control: Control<T>
  name: FieldPath<T>
  disabled?: boolean
}) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>角标</FormLabel>
          <BadgeControl value={field.value ?? ''} onChange={field.onChange} disabled={disabled} />
          <FormDescription>
            预设角标按用户的界面语言显示（9 种语言）；「独家」「会员」的颜色跟随品牌与主题里配的品牌色、会员色。自定义文案只显示填写的这一种语言。
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
