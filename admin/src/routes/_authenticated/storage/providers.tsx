import { createFileRoute } from '@tanstack/react-router'
import { StorageProvidersPage } from '@/features/storage/providers-page'

export const Route = createFileRoute('/_authenticated/storage/providers')({
  component: StorageProvidersPage,
})
