import type { ReactNode } from 'react'
import type { Control, FieldPath, FieldValues } from 'react-hook-form'
import { DatePicker, type DatePickerMode } from '@/components/date-picker'
import { SelectDropdown } from '@/components/select-dropdown'
import { Input, Switch, Textarea } from '@/components/ui'
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'

type BaseProps<T extends FieldValues> = {
  control: Control<T>
  name: FieldPath<T>
  label: string
  description?: ReactNode
  disabled?: boolean
  required?: boolean
}

function Label({ label, required }: { label: string; required?: boolean }) {
  return (
    <FormLabel>
      {label}
      {required && (
        <span className='text-destructive' aria-hidden>
          {' '}
          *
        </span>
      )}
    </FormLabel>
  )
}

/** 文本输入框（multiline 时为多行文本）。值为字符串。 */
export function TextField<T extends FieldValues>({
  control,
  name,
  label,
  description,
  disabled,
  required,
  placeholder,
  multiline,
  rows = 3,
  inputMode,
}: BaseProps<T> & {
  placeholder: string
  multiline?: boolean
  rows?: number
  inputMode?: 'numeric' | 'decimal' | 'text'
}) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <Label label={label} required={required} />
          <FormControl>
            {multiline ? (
              <Textarea rows={rows} placeholder={placeholder} disabled={disabled} {...field} />
            ) : (
              <Input
                type='text'
                inputMode={inputMode}
                autoComplete='off'
                placeholder={placeholder}
                disabled={disabled}
                {...field}
              />
            )}
          </FormControl>
          {description && <FormDescription>{description}</FormDescription>}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

/**
 * 时间选择（日期 / 日期时间 / 月份），统一走 `components/date-picker`，不用原生 input。
 * 值为字符串，格式与它替代的原生 input 一致（见 DatePicker 注释），空串表示未填。
 */
export function DateField<T extends FieldValues>({
  control,
  name,
  label,
  description,
  disabled,
  required,
  mode = 'datetime',
  placeholder,
  clearable,
  min,
  max,
}: BaseProps<T> & {
  mode?: DatePickerMode
  placeholder?: string
  clearable?: boolean
  min?: string
  max?: string
}) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <Label label={label} required={required} />
          <FormControl>
            <DatePicker
              mode={mode}
              value={field.value ?? ''}
              onChange={field.onChange}
              placeholder={placeholder}
              disabled={disabled}
              clearable={clearable ?? !required}
              min={min}
              max={max}
            />
          </FormControl>
          {description && <FormDescription>{description}</FormDescription>}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

/** 下拉选择。值为字符串。 */
export function SelectField<T extends FieldValues>({
  control,
  name,
  label,
  description,
  disabled,
  required,
  placeholder,
  items,
}: BaseProps<T> & { placeholder: string; items: { label: string; value: string }[] }) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <Label label={label} required={required} />
          <SelectDropdown
            isControlled
            defaultValue={field.value}
            onValueChange={field.onChange}
            placeholder={placeholder}
            disabled={disabled}
            items={items}
          />
          {description && <FormDescription>{description}</FormDescription>}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

/** 开关（布尔值），说明文字在右侧。 */
export function SwitchField<T extends FieldValues>({ control, name, label, description, disabled }: BaseProps<T>) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem className='flex flex-row items-center justify-between gap-4 rounded-md border px-3 py-2'>
          <div className='flex flex-col gap-1'>
            <FormLabel>{label}</FormLabel>
            {description && <FormDescription>{description}</FormDescription>}
          </div>
          <FormControl>
            <Switch checked={field.value} onCheckedChange={field.onChange} disabled={disabled} />
          </FormControl>
        </FormItem>
      )}
    />
  )
}
