import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { optionalId } from '@/features/content/search'
import { TaxonomiesPage } from '@/features/content/taxonomies-page'

// ?t=<维度 ID>：右侧显示该维度的取值
const searchSchema = z.object({ t: optionalId })

export const Route = createFileRoute('/_authenticated/taxonomies')({
  component: TaxonomiesPage,
  validateSearch: searchSchema,
})
