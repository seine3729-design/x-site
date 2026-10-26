import { createFileRoute } from '@tanstack/react-router'
import { SEOPage } from '@/features/platform/seo-page'

// 一次加载 SEO 段；统计代码按权限另取，没有筛选参数
export const Route = createFileRoute('/_authenticated/seo')({
  component: SEOPage,
})
