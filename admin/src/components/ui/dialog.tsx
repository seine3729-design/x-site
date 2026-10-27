'use client'

import * as React from 'react'
import { Dialog as DialogPrimitive } from 'radix-ui'
import { XIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

const DialogContext = React.createContext<HTMLDivElement | null>(null)

export const useDialogContainer = () => React.useContext(DialogContext)

function Dialog({
  modal = true,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Root>) {
  return <DialogPrimitive.Root data-slot='dialog' modal={modal} {...props} />
}

function DialogTrigger({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Trigger>) {
  return <DialogPrimitive.Trigger data-slot='dialog-trigger' {...props} />
}

function DialogPortal({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Portal>) {
  return <DialogPrimitive.Portal data-slot='dialog-portal' {...props} />
}

function DialogClose({
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Close>) {
  return <DialogPrimitive.Close data-slot='dialog-close' {...props} />
}

function DialogOverlay({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Overlay>) {
  return (
    <DialogPrimitive.Overlay
      data-slot='dialog-overlay'
      className={cn(
        'fixed inset-0 z-50 bg-black/50 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:animate-in data-[state=open]:fade-in-0',
        className
      )}
      {...props}
    />
  )
}

function DialogContent({
  className,
  bodyClassName,
  children,
  showCloseButton = true,
  onOpenAutoFocus,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Content> & {
  showCloseButton?: boolean
  /** 滚动区（dialog-body）的样式覆盖，例如命令面板需要 `p-0`。 */
  bodyClassName?: string
}) {
  const containerRef = React.useRef<HTMLDivElement>(null)

  const handleOpenAutoFocus = React.useCallback(
    (event: Event) => {
      if (onOpenAutoFocus) {
        onOpenAutoFocus(event)
      } else {
        // 移除任何可能在 aria-hidden 元素中的焦点
        const activeElement = document.activeElement as HTMLElement
        if (activeElement && activeElement !== document.body) {
          const isInAriaHidden = activeElement.closest('[aria-hidden="true"]')
          if (isInAriaHidden) {
            activeElement.blur()
          }
        }
      }
    },
    [onOpenAutoFocus]
  )

  return (
    <DialogPortal data-slot='dialog-portal'>
      <DialogOverlay />
      <DialogPrimitive.Content
        ref={containerRef}
        data-slot='dialog-content'
        // 外层只负责定位与最大高度（不滚动），内容超出时由内部 dialog-body 滚动，
        // 这样右上角关闭按钮始终停在原位，不会随内容滚走。
        // 外层刻意不加 overflow-hidden：下拉框（SelectContent）会 portal 到这个元素里，
        // 而它用的是 position: fixed，被 translate 变换当成包含块，一裁剪就展开不出弹窗范围。
        className={cn(
          'fixed top-[50%] left-[50%] z-50 flex max-h-[calc(100dvh-4rem)] w-full max-w-[calc(100%-2rem)] translate-x-[-50%] translate-y-[-50%] flex-col rounded-md border bg-background shadow-lg duration-200 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95 sm:max-w-lg',
          className
        )}
        onOpenAutoFocus={handleOpenAutoFocus}
        onPointerDownOutside={(e) => {
          const target = e.target as HTMLElement
          if (target.closest('[data-radix-popper-content-wrapper]')) {
            e.preventDefault()
          }
        }}
        aria-describedby={undefined}
        {...props}
      >
        <DialogContext.Provider value={containerRef.current}>
          <div
            data-slot='dialog-body'
            className={cn(
              'grid min-h-0 gap-4 overflow-y-auto overscroll-contain rounded-[inherit] p-6',
              bodyClassName
            )}
          >
            {children}
          </div>
        </DialogContext.Provider>
        {showCloseButton && (
          <DialogPrimitive.Close
            data-slot='dialog-close'
            aria-label='关闭'
            // 内容会从按钮下方滚过，所以要有不透明底色；颜色走主题变量，暗色模式下同样可读。
            className="absolute inset-e-3 top-3 z-10 grid size-7 place-items-center rounded-md bg-muted text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none disabled:pointer-events-none [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4"
          >
            <XIcon />
            <span className='sr-only'>关闭</span>
          </DialogPrimitive.Close>
        )}
      </DialogPrimitive.Content>
    </DialogPortal>
  )
}

function DialogHeader({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot='dialog-header'
      className={cn('flex flex-col gap-2 text-center sm:text-start', className)}
      {...props}
    />
  )
}

function DialogFooter({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot='dialog-footer'
      className={cn(
        'flex flex-col-reverse gap-2 sm:flex-row sm:justify-end',
        className
      )}
      {...props}
    />
  )
}

function DialogTitle({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title
      data-slot='dialog-title'
      className={cn('text-lg leading-none font-semibold', className)}
      {...props}
    />
  )
}

function DialogDescription({
  className,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Description>) {
  return (
    <DialogPrimitive.Description
      data-slot='dialog-description'
      className={cn('text-sm text-muted-foreground', className)}
      {...props}
    />
  )
}

export {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogOverlay,
  DialogPortal,
  DialogTitle,
  DialogTrigger,
}
