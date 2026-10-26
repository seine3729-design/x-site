import { createFileRoute } from '@tanstack/react-router'
import { StoragePreviewPage } from '@/features/storage/preview-page'

export const Route = createFileRoute('/_authenticated/storage/preview')({
  component: StoragePreviewPage,
})
