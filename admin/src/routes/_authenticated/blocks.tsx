import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { BlocksPage } from '@/features/site/blocks-page'

// 编辑的页面写在地址栏，便于刷新与分享；默认首页（不写进地址栏），非法取值回到默认
const searchSchema = z.object({
  page: z
    .string()
    .refine((v) => v === 'home' || /^channel:[a-z0-9-]+$/.test(v))
    .optional()
    .catch(undefined),
})

export const Route = createFileRoute('/_authenticated/blocks')({
  component: BlocksPage,
  validateSearch: searchSchema,
})
