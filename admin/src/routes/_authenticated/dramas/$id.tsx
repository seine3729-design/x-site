import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { DramaEditPage } from '@/features/content/drama-edit-page'

// ?tab=tags|terms|episodes：直接打开某个分区（默认剧资料；其他值按剧资料处理）
const searchSchema = z.object({
  tab: z.enum(['tags', 'terms', 'episodes']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/dramas/$id')({
  component: DramaEditPage,
  validateSearch: searchSchema,
})
