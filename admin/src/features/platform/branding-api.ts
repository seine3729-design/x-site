import { queryOptions } from '@tanstack/react-query'
import { http } from '@/lib/http'

/**
 * 后台「品牌与主题」的接口（docs/06 第 16 节，S3-3 / A3-3，docs/specs/A3-3.md；
 * 契约 service/api/openapi.yaml 的 AdminBrand / AdminTheme / AdminLayout）。
 *
 * 一次 GET 拿三段 + 各下拉的可选值（一个页面三张卡，不发三次请求），保存分三个 PUT：
 * 改配色不该把别人正在编辑的布局一起写回去。保存后站点版本 +1，各端几秒内生效。
 */

export type Brand = {
  site_name: string
  slogan: string
  copyright: string
  support_url: string
  /** 头部 Logo：上传后的图片相对路径或完整地址；留空用站名文字。 */
  logo_light: string
  favicon: string
}

export type Theme = {
  preset: string
  color_scheme: string
  allow_scheme_toggle: boolean
  /** 覆盖预设里的颜色，键见 options.colors；留空表示用预设色。 */
  colors: Record<string, string>
  radius: number
  cover_ratio: string
  font: string
}

export type Layout = {
  header_style: string
  card_style: string
  play_mode: string
  mobile_tabbar: boolean
}

/** 下拉里的一项，编码与中文名都由后端给（前端不抄一份枚举）。 */
export type BrandingOption = { value: string; label: string; hint?: string }

export type BrandingOptions = {
  presets: BrandingOption[]
  color_schemes: BrandingOption[]
  colors: BrandingOption[]
  cover_ratios: BrandingOption[]
  fonts: BrandingOption[]
  header_styles: BrandingOption[]
  card_styles: BrandingOption[]
  play_modes: BrandingOption[]
  max_radius: number
}

export type BrandingConfig = {
  brand: Brand
  theme: Theme
  layout: Layout
  options: BrandingOptions
}

export const brandingKeys = {
  config: ['platform', 'branding'] as const,
}

export const brandingConfigQuery = queryOptions({
  queryKey: brandingKeys.config,
  queryFn: () => http.get<BrandingConfig>('/brand'),
})

export const brandingApi = {
  setBrand: (body: Brand) => http.put<BrandingConfig>('/brand', body),
  setTheme: (body: Theme) => http.put<BrandingConfig>('/theme', body),
  setLayout: (body: Layout) => http.put<BrandingConfig>('/layout', body),
}

/** 与后端一致的上限。 */
export const MAX_SITE_NAME = 30
export const MAX_SLOGAN = 60
export const MAX_COPYRIGHT = 100
export const MAX_IMAGE_KEY = 300
export const MAX_SUPPORT_URL = 500
