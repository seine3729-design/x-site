'use client'

import * as Dialog from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { t } from '@/lib/i18n'

/**
 * 底部抽屉（移动端）/ 居中弹窗（桌面）。
 *
 * Radix Dialog 负责焦点锁定、Esc 关闭、关闭后焦点回到触发按钮——
 * 这几件事手写很容易漏，而漏掉任何一件键盘用户就会被困在里面。
 *
 * 动效只用 transform/opacity，退出比进入短（docs/06 第 8 节），
 * 并且在 prefers-reduced-motion 下由 globals.css 统一关掉。
 */
export function Sheet({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: string
  children: React.ReactNode
  footer?: React.ReactNode
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay
          className={cn(
            'fixed inset-0 z-50 bg-scrim',
            'data-[state=open]:animate-[fb-fade-in_var(--duration-overlay)_var(--ease-out-soft)]',
            'data-[state=closed]:animate-[fb-fade-out_150ms_var(--ease-out-soft)]',
          )}
        />
        <Dialog.Content
          className={cn(
            'fixed z-50 flex flex-col bg-surface-overlay shadow-popover',
            // 移动端贴底，桌面居中
            'inset-x-0 bottom-0 max-h-[85dvh] rounded-t-surface-lg',
            'sm:inset-x-auto sm:bottom-auto sm:top-1/2 sm:left-1/2 sm:w-full sm:max-w-lg',
            'sm:-translate-x-1/2 sm:-translate-y-1/2 sm:rounded-surface-lg',
            // 移动端从底部滑入，桌面轻微放大淡入：动效方向和它出现的位置一致
            'data-[state=open]:animate-[fb-slide-up-in_var(--duration-overlay)_var(--ease-out-soft)]',
            'data-[state=closed]:animate-[fb-slide-up-out_150ms_var(--ease-out-soft)]',
            'sm:data-[state=open]:animate-[fb-pop-in_var(--duration-overlay)_var(--ease-out-soft)]',
            'sm:data-[state=closed]:animate-[fb-fade-out_150ms_var(--ease-out-soft)]',
          )}
          style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
        >
          <div className="flex items-start justify-between gap-4 border-b border-border px-5 py-4">
            <div className="min-w-0">
              <Dialog.Title className="text-base font-semibold text-fg">{title}</Dialog.Title>
              {description && (
                <Dialog.Description className="mt-1 text-sm text-fg-muted">
                  {description}
                </Dialog.Description>
              )}
            </div>
            <Dialog.Close
              className="-me-1 -mt-1 flex size-10 shrink-0 cursor-pointer items-center justify-center rounded-control text-fg-muted hover:bg-surface-raised hover:text-fg"
              aria-label={t('action.close')}
            >
              <X className="size-5" aria-hidden />
            </Dialog.Close>
          </div>
          <div className="flex-1 overflow-y-auto px-5 py-4">{children}</div>
          {footer && <div className="border-t border-border px-5 py-4">{footer}</div>}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
