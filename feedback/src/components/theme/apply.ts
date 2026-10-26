export const THEME_STORAGE_KEY = 'fb-theme'

export type Theme = 'system' | 'light' | 'dark'

/** 读用户选的模式。非法值与未设置都按「跟随系统」。 */
export function readTheme(): Theme {
  try {
    const stored = localStorage.getItem(THEME_STORAGE_KEY)
    return stored === 'light' || stored === 'dark' ? stored : 'system'
  } catch {
    // localStorage 被禁用（隐私模式、嵌入场景）时按系统走，不要抛错
    return 'system'
  }
}

/**
 * 把主题写到 `<html>` 上。
 *
 * 写两个东西，各有用途（和 theme-script.tsx 里的内联脚本保持一致）：
 *   .dark 类        → 最终外观，CSS 变量按它切换
 *   data-theme 属性 → 用户选的模式，分段控件的选中态靠它（纯 CSS 驱动）
 *
 * 跟随系统且系统是深色时，最终外观和「选了深色」一样，单靠 .dark 分不出来，
 * 所以两者都要写。
 */
export function applyTheme(theme: Theme): void {
  const dark =
    theme === 'dark' ||
    (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  const root = document.documentElement
  root.classList.toggle('dark', dark)
  root.dataset.theme = theme
  root.style.colorScheme = dark ? 'dark' : 'light'
}

/**
 * 如果 `<html>` 上的主题标记丢了，按 localStorage 补回去。
 *
 * 什么时候会丢：客户端路由导航时 React 会重新 reconcile root layout 的 `<html>`，
 * 把内联脚本运行时加上的 class 与 data-theme 一起抹掉 —— 服务端渲染的 `<html>`
 * 上并没有这两个标记。表现是「跳转之后页面突然变回浅色」。
 *
 * 只在标记确实不一致时才写 DOM：无条件写会在每次导航都碰一次 class，
 * 可能触发不必要的样式重算。
 */
export function syncThemeIfLost(): void {
  const theme = readTheme()
  const root = document.documentElement
  const shouldBeDark =
    theme === 'dark' ||
    (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  if (root.classList.contains('dark') !== shouldBeDark || root.dataset.theme !== theme) {
    applyTheme(theme)
  }
}
