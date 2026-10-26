import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function sleep(ms: number = 1000) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

/**
 * 生成分页页码数组，包含省略号
 * @param currentPage - 当前页码（从1开始）
 * @param totalPages - 总页数
 * @returns 包含页码和省略号的数组
 *
 * 示例：
 * - 小数据集 (≤5 页): [1, 2, 3, 4, 5]
 * - 接近开始: [1, 2, 3, 4, '...', 10]
 * - 中间: [1, '...', 4, 5, 6, '...', 10]
 * - 接近结束: [1, '...', 7, 8, 9, 10]
 */
export function getPageNumbers(currentPage: number, totalPages: number) {
  const maxVisiblePages = 5 // 要显示的最大页面按钮数量
  const rangeWithDots = []

  if (totalPages <= maxVisiblePages) {
    // 如果总页数为 5 页或更少，则显示所有页面
    for (let i = 1; i <= totalPages; i++) {
      rangeWithDots.push(i)
    }
  } else {
    // 始终显示第一页
    rangeWithDots.push(1)

    if (currentPage <= 3) {
      // 接近开始: [1] [2] [3] [4] ... [10]
      for (let i = 2; i <= 4; i++) {
        rangeWithDots.push(i)
      }
      rangeWithDots.push('...', totalPages)
    } else if (currentPage >= totalPages - 2) {
      // 接近结束: [1] ... [7] [8] [9] [10]
      rangeWithDots.push('...')
      for (let i = totalPages - 3; i <= totalPages; i++) {
        rangeWithDots.push(i)
      }
    } else {
      // 中间: [1] ... [4] [5] [6] ... [10]
      rangeWithDots.push('...')
      for (let i = currentPage - 1; i <= currentPage + 1; i++) {
        rangeWithDots.push(i)
      }
      rangeWithDots.push('...', totalPages)
    }
  }

  return rangeWithDots
}
