import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

/** 合并 class：后出现的 Tailwind 工具类覆盖先出现的同类。 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
