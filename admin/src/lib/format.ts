/**
 * 格式化时间
 * @param dateString ISO 8601 格式的时间字符串 (e.g., "2026-01-21T06:47:31+08:00")
 * @param format 格式化类型，默认为 'datetime'
 * @returns 格式化后的时间字符串
 */
export function formatDateTime(
  dateString: string,
  format: 'datetime' | 'date' | 'time' = 'datetime'
): string {
  if (!dateString) return '-'

  try {
    const date = new Date(dateString)
    
    if (isNaN(date.getTime())) {
      return dateString
    }

    const year = date.getFullYear()
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')
    const hours = String(date.getHours()).padStart(2, '0')
    const minutes = String(date.getMinutes()).padStart(2, '0')
    const seconds = String(date.getSeconds()).padStart(2, '0')

    switch (format) {
      case 'date':
        return `${year}-${month}-${day}`
      case 'time':
        return `${hours}:${minutes}:${seconds}`
      case 'datetime':
      default:
        return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`
    }
  } catch (_error) {
    return dateString
  }
}
