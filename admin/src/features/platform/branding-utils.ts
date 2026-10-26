import type { BrandingOption, Theme } from './branding-api'

/** 预设的色板：用于预览与「用预设色」时的取色。与前台的预设保持同名，值只给预览用。 */
const PRESET_PALETTES: Record<string, Record<string, string>> = {
  classic: { primary: '#e11d48', accent: '#f59e0b', bg: '#0b0b0f', surface: '#17171f', text: '#f5f5f7', muted: '#9ca3af' },
  midnight: { primary: '#3b82f6', accent: '#22d3ee', bg: '#0a1020', surface: '#141c2f', text: '#f1f5f9', muted: '#94a3b8' },
  forest: { primary: '#10b981', accent: '#84cc16', bg: '#071410', surface: '#11211b', text: '#ecfdf5', muted: '#9ca3af' },
  // 主色与前台的 #lib/theme/tokens.ts 保持一致：violet-500 做按钮底色时白字只有 4.44:1，够不到 WCAG AA
  violet: { primary: '#7c3aed', accent: '#ec4899', bg: '#120b1f', surface: '#1e1430', text: '#f5f3ff', muted: '#a1a1aa' },
  neutral: { primary: '#525252', accent: '#737373', bg: '#0c0c0c', surface: '#1a1a1a', text: '#fafafa', muted: '#a3a3a3' },
}

/** 浅色模式的预览底色：深色色板直接用在浅色预览上会看不清。 */
const LIGHT_OVERRIDES: Record<string, string> = { bg: '#f6f6f8', surface: '#ffffff', text: '#18181b', muted: '#6b7280' }

/**
 * 预览用的实际色值：预设色板 + 运营填的覆盖色。
 * scheme 为 light 时把背景与文字换成浅色底，否则深色色板在浅色预览里是黑底。
 */
export function previewColors(theme: Pick<Theme, 'preset' | 'color_scheme' | 'colors'>, scheme: 'dark' | 'light'): Record<string, string> {
  const base = { ...(PRESET_PALETTES[theme.preset] ?? PRESET_PALETTES.classic) }
  if (scheme === 'light') Object.assign(base, LIGHT_OVERRIDES)
  for (const [key, value] of Object.entries(theme.colors ?? {})) {
    if (value) base[key] = value
  }
  return base
}

/** 预览里用哪种深浅：跟随系统时按深色画（前台的默认也是深色）。 */
export function previewScheme(colorScheme: string): 'dark' | 'light' {
  return colorScheme === 'light' ? 'light' : 'dark'
}

/** 封面比例转 CSS aspect-ratio。 */
export function coverAspect(ratio: string): string {
  const [w, h] = ratio.split(':')
  return Number(w) > 0 && Number(h) > 0 ? `${w} / ${h}` : '2 / 3'
}

/** 下拉里某个取值的中文名；找不到时退回原值（后端新增取值而前端还没刷新时不至于显示空白）。 */
export function optionLabel(options: BrandingOption[] | undefined, value: string): string {
  return options?.find((o) => o.value === value)?.label ?? value
}

/** 图片地址：相对路径（如 img/logo.webp）或完整 http(s) 地址；留空也可以。 */
export function isImageKeyOrEmpty(value: string): boolean {
  const s = value.trim()
  if (s === '') return true
  if (s.length > 300) return false
  if (/^https?:\/\//.test(s)) {
    try {
      return new URL(s).hostname !== ''
    } catch {
      return false
    }
  }
  return /^[A-Za-z0-9][A-Za-z0-9/._-]*$/.test(s)
}

export const IMAGE_KEY_HINT = '填上传后的图片路径（如 img/logo.webp）或完整的 http(s) 地址'

/** http(s) 完整地址；留空也可以。 */
export function isHttpURLOrEmpty(value: string): boolean {
  const s = value.trim()
  if (s === '') return true
  if (/[\s\p{Cc}]/u.test(s)) return false
  try {
    const u = new URL(s)
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.hostname !== ''
  } catch {
    return false
  }
}

/** #RGB / #RRGGBB；留空表示用预设色。 */
export function isHexColorOrEmpty(value: string): boolean {
  const s = value.trim()
  return s === '' || /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/.test(s)
}

/** 表单里的颜色是「键 → 色值」，提交前去掉空值并转小写（与后端保存结果一致）。 */
export function cleanColors(colors: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [key, value] of Object.entries(colors)) {
    const v = value.trim().toLowerCase()
    if (v !== '') out[key] = v
  }
  return out
}
