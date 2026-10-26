import { useState } from 'react'

/**
 * 用于确认对话框的自定义钩子
 * @param initialState string | null
 * @returns 一个有状态的值，以及一个用于更新该值的函数。
 * @example const [open, setOpen] = useDialogState<"approve" | "reject">()
 */
export default function useDialogState<T extends string | boolean>(
  initialState: T | null = null
) {
  const [open, _setOpen] = useState<T | null>(initialState)

  const setOpen = (str: T | null) =>
    _setOpen((prev) => (prev === str ? null : str))

  return [open, setOpen] as const
}
