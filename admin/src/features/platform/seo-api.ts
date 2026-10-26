import { queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'
import type { BrandingOption } from './branding-api'

/**
 * 后台「SEO 与统计」的接口（docs/06 第 17 节，S3-3 / A3-3，docs/specs/A3-3.md；
 * 契约 service/api/openapi.yaml 的 AdminSEO / AdminAnalyticsCode）。
 *
 * 统计代码走单独的接口与单独的权限点 seo.analytics_code（默认只有超管）：
 * 那段代码会被原样注入每个页面，没有权限的人连现在埋了什么都不该看到。
 */

export type SEOVerification = { name: string; content: string }

export type SEO = {
  /** 「页面编码 → 模板」，模板里可带 {变量}；缺哪页就用前台默认。 */
  title_templates: Record<string, string>
  desc_templates: Record<string, string>
  og_image: string
  robots_allow: boolean
  robots_extra: string
  sitemap: boolean
  structured_data: boolean
  verifications: SEOVerification[]
}

export type SEOConfig = {
  seo: SEO
  /** 可配置模板的页面（含中文名与可用变量提示），由后端给。 */
  pages: BrandingOption[]
  /** 模板里可插入的变量，「点击插入」照此渲染。 */
  variables: BrandingOption[]
  max_template: number
}

export type AnalyticsCode = {
  head_snippet: string
  /** 注入 </body> 前。 */
  body_snippet: string
}

export const seoKeys = {
  config: ['platform', 'seo'] as const,
  analytics: ['platform', 'analytics-code'] as const,
}

export const seoConfigQuery = queryOptions({
  queryKey: seoKeys.config,
  queryFn: () => http.get<SEOConfig>('/seo'),
})

export const analyticsQuery = queryOptions({
  queryKey: seoKeys.analytics,
  queryFn: () => http.get<AnalyticsCode>('/analytics-code'),
})

export const seoApi = {
  setSEO: (body: SEO) => http.put<SEOConfig>('/seo', body),
  setAnalytics: (body: AnalyticsCode) => http.put<AnalyticsCode>('/analytics-code', body),
}

/** 与后端一致的上限。 */
export const MAX_OG_IMAGE = 300
export const MAX_ROBOTS_EXTRA = 2000
export const MAX_VERIFICATIONS = 10
export const MAX_VERIFY_NAME = 60
export const MAX_VERIFY_CONTENT = 200
export const MAX_ANALYTICS = 20_000
/** 统计代码的权限点（默认只有超管）。 */
export const PERM_ANALYTICS_CODE = 'seo.analytics_code'
