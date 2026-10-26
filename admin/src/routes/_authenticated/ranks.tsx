import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { RANK_BOARDS } from '@/features/ops/api'
import { RanksPage } from '@/features/ops/ranks-page'

// 默认周榜（不写进地址栏）；非法取值回到默认
const searchSchema = z.object({
  board: z.enum(RANK_BOARDS).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/ranks')({
  component: RanksPage,
  validateSearch: searchSchema,
})
