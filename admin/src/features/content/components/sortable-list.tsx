import { useRef, useState, type ReactNode } from 'react'
import { ArrowDown, ArrowUp, GripVertical } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui'

/**
 * 可排序列表（docs/06「后台通用组件」：手柄拖动 + 键盘上下）：
 * 拖动手柄调整顺序；手柄获得焦点时按 ↑ / ↓ 移动；也可以点上移 / 下移按钮（不用拖动也能完成）。
 * 只负责交互，顺序由调用方保存。
 */
export function SortableList<T>({
  items,
  getKey,
  getLabel,
  onMove,
  renderItem,
  disabled,
  className,
}: {
  items: readonly T[]
  getKey: (item: T) => string | number
  getLabel: (item: T) => string
  onMove: (from: number, to: number) => void
  renderItem: (item: T, index: number) => ReactNode
  disabled?: boolean
  className?: string
}) {
  const [dragging, setDragging] = useState<number | null>(null)
  const [over, setOver] = useState<number | null>(null)
  const [announcement, setAnnouncement] = useState('')
  const handles = useRef(new Map<string | number, HTMLButtonElement>())
  const armed = useRef(false) // 只有按住手柄时才允许拖动整行

  const move = (from: number, to: number, refocus: boolean) => {
    if (to < 0 || to >= items.length || from === to) return
    const key = getKey(items[from])
    onMove(from, to)
    setAnnouncement(`「${getLabel(items[from])}」已移到第 ${to + 1} 位`)
    // 移动后焦点跟着原来的项走，便于连续按方向键
    if (refocus) requestAnimationFrame(() => handles.current.get(key)?.focus())
  }

  return (
    <>
      <ol className={cn('flex flex-col gap-1', className)}>
        {items.map((item, i) => {
          const key = getKey(item)
          const label = getLabel(item)
          return (
            // 拖放只是鼠标的便捷操作；键盘与单击的替代方式在手柄和上移 / 下移按钮上
            // oxlint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
            <li
              key={key}
              draggable={!disabled}
              onDragStart={(e) => {
                if (!armed.current) {
                  e.preventDefault()
                  return
                }
                setDragging(i)
                e.dataTransfer.effectAllowed = 'move'
                e.dataTransfer.setData('text/plain', String(key))
              }}
              onDragOver={(e) => {
                if (dragging === null) return
                e.preventDefault()
                setOver(i)
              }}
              onDrop={(e) => {
                e.preventDefault()
                if (dragging !== null) move(dragging, i, false)
                setDragging(null)
                setOver(null)
              }}
              onDragEnd={() => {
                armed.current = false
                setDragging(null)
                setOver(null)
              }}
              className={cn(
                'flex items-center gap-2 rounded-md border bg-background px-2 py-1.5',
                dragging === i && 'opacity-50',
                over === i && dragging !== null && dragging !== i && 'border-primary ring-1 ring-primary'
              )}
            >
              <button
                type='button'
                ref={(el) => {
                  if (el) handles.current.set(key, el)
                  else handles.current.delete(key)
                }}
                disabled={disabled}
                aria-label={`调整「${label}」的顺序：拖动，或按上下方向键`}
                className='cursor-grab rounded p-1 text-muted-foreground hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50 active:cursor-grabbing'
                onPointerDown={() => {
                  armed.current = true
                }}
                onPointerUp={() => {
                  armed.current = false
                }}
                onKeyDown={(e) => {
                  if (e.key === 'ArrowUp') {
                    e.preventDefault()
                    move(i, i - 1, true)
                  } else if (e.key === 'ArrowDown') {
                    e.preventDefault()
                    move(i, i + 1, true)
                  }
                }}
              >
                <GripVertical className='size-4' aria-hidden />
              </button>
              <div className='min-w-0 flex-1'>{renderItem(item, i)}</div>
              <div className='flex shrink-0 items-center'>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  className='size-8'
                  disabled={disabled || i === 0}
                  aria-label={`上移「${label}」`}
                  onClick={() => move(i, i - 1, false)}
                >
                  <ArrowUp className='size-4' />
                </Button>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  className='size-8'
                  disabled={disabled || i === items.length - 1}
                  aria-label={`下移「${label}」`}
                  onClick={() => move(i, i + 1, false)}
                >
                  <ArrowDown className='size-4' />
                </Button>
              </div>
            </li>
          )
        })}
      </ol>
      <output aria-live='polite' className='sr-only'>
        {announcement}
      </output>
    </>
  )
}
