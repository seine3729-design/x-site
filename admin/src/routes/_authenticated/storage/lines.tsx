import { createFileRoute } from '@tanstack/react-router'
import { StorageLinesPage } from '@/features/storage/lines-page'

export const Route = createFileRoute('/_authenticated/storage/lines')({
  component: StorageLinesPage,
})
