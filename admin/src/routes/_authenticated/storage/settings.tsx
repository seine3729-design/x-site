import { createFileRoute } from '@tanstack/react-router'
import { StorageSettingsPage } from '@/features/storage/settings-page'

export const Route = createFileRoute('/_authenticated/storage/settings')({
  component: StorageSettingsPage,
})
