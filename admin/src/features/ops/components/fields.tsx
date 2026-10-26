import { useId, type ReactNode } from 'react'
import type { Control, FieldPath, FieldValues } from 'react-hook-form'
import { Button, Checkbox, Input, Label } from '@/components/ui'
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'

type BaseProps<T extends FieldValues> = {
  control: Control<T>
  name: FieldPath<T>
  label: string
  description?: ReactNode
  disabled?: boolean
}

/**
 * 多选项（复选框组）。值为字符串数组；「不选」通常表示全部，由 description 说明。
 * 用 fieldset + legend 让读屏软件读出分组名称。
 */
export function CheckboxGroupField<T extends FieldValues, V extends string>({
  control,
  name,
  label,
  description,
  disabled,
  options,
}: BaseProps<T> & { options: readonly { value: V; label: string }[] }) {
  const id = useId()
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => {
        const value = (field.value ?? []) as V[]
        const toggle = (v: V, on: boolean) => field.onChange(on ? [...value, v] : value.filter((x) => x !== v))
        return (
          <FormItem>
            <fieldset className='flex flex-col gap-2' disabled={disabled}>
              <legend className='mb-2 text-sm font-medium'>{label}</legend>
              <div className='grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-3'>
                {options.map((o) => (
                  <div key={o.value} className='flex items-center gap-2'>
                    <Checkbox
                      id={`${id}-${o.value}`}
                      checked={value.includes(o.value)}
                      onCheckedChange={(c) => toggle(o.value, c === true)}
                      disabled={disabled}
                    />
                    <Label htmlFor={`${id}-${o.value}`} className='font-normal'>
                      {o.label}
                    </Label>
                  </div>
                ))}
              </div>
            </fieldset>
            {description && <FormDescription>{description}</FormDescription>}
            <FormMessage />
          </FormItem>
        )
      }}
    />
  )
}

/** 本地时间（datetime-local）+「清除」按钮。值为 datetime-local 字符串，空表示不填。 */
export function DateTimeField<T extends FieldValues>({ control, name, label, description, disabled }: BaseProps<T>) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <div className='flex gap-2'>
            <FormControl>
              <Input type='datetime-local' placeholder='选填，年-月-日 时:分' disabled={disabled} {...field} />
            </FormControl>
            <Button type='button' variant='outline' disabled={disabled || !field.value} onClick={() => field.onChange('')}>
              清除
            </Button>
          </div>
          {description && <FormDescription>{description}</FormDescription>}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
