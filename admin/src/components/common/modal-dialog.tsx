import { type ReactNode } from 'react'
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui'

export type ModalDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: string
  children: ReactNode
  footer?: ReactNode
  showCloseButton?: boolean
  size?: 'sm' | 'md' | 'lg' | 'xl' | 'full'
  onConfirm?: () => void
  onCancel?: () => void
  confirmText?: string
  cancelText?: string
  confirmVariant?: 'default' | 'destructive' | 'outline' | 'secondary' | 'ghost' | 'link'
  isLoading?: boolean
  className?: string
}

const sizeClasses = {
  sm: 'sm:max-w-sm',
  md: 'sm:max-w-md',
  lg: 'sm:max-w-lg',
  xl: 'sm:max-w-xl',
  full: 'sm:max-w-[90vw]',
}

export function ModalDialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  showCloseButton = true,
  size = 'md',
  onConfirm,
  onCancel,
  confirmText = 'Confirm',
  cancelText = 'Cancel',
  confirmVariant = 'default',
  isLoading = false,
  className,
}: ModalDialogProps) {
  const handleCancel = () => {
    if (onCancel) {
      onCancel()
    } else {
      onOpenChange(false)
    }
  }

  const handleConfirm = () => {
    if (onConfirm) {
      onConfirm()
    }
  }

  const defaultFooter = (onConfirm || onCancel) && (
    <DialogFooter>
      {onCancel && (
        <Button
          type='button'
          variant='outline'
          onClick={handleCancel}
          disabled={isLoading}
        >
          {cancelText}
        </Button>
      )}
      {onConfirm && (
        <Button
          type='button'
          variant={confirmVariant}
          onClick={handleConfirm}
          disabled={isLoading}
        >
          {isLoading ? 'Loading...' : confirmText}
        </Button>
      )}
    </DialogFooter>
  )

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className={`${sizeClasses[size]} ${className || ''}`}
        showCloseButton={showCloseButton}
      >
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>
        <div className='py-4'>{children}</div>
        {footer || defaultFooter}
      </DialogContent>
    </Dialog>
  )
}
