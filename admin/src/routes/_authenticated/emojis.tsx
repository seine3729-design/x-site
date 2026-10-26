import { createFileRoute } from '@tanstack/react-router'
import { EmojisPage } from '@/features/community/emojis-page'

export const Route = createFileRoute('/_authenticated/emojis')({
  component: EmojisPage,
})
