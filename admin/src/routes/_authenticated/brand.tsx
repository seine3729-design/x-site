import { createFileRoute } from '@tanstack/react-router'
import { BrandingPage } from '@/features/platform/branding-page'

// 三段配置一次加载，没有筛选参数
export const Route = createFileRoute('/_authenticated/brand')({
  component: BrandingPage,
})
