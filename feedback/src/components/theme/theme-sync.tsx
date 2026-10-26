'use client'

import { useEffect } from 'react'
import { usePathname } from 'next/navigation'
import { syncThemeIfLost } from './apply'

/**
 * 客户端导航后把主题标记补回 `<html>`。
 *
 * 为什么需要它：`ThemeScript` 只在**新文档**加载时跑一次。客户端路由导航不是新文档，
 * 而 React 在导航时会重新 reconcile root layout 的 `<html>`，把脚本运行时加上的
 * `.dark` 与 `data-theme` 抹掉（服务端渲染的 `<html>` 上没有这两个标记）。
 *
 * 这个 bug 是开启 Cache Components 之后才暴露的：以前未登录访问 /settings 是
 * HTTP 307，浏览器层面跳转 = 新文档，脚本会重跑；现在那些路由是部分预渲染，
 * redirect 走在流式内容里 = 客户端导航，脚本不再执行，页面就变回浅色了。
 *
 * 挂在 root layout 上而不是 SiteShell 里：/offline 这类页面不在 (site) 分组下，
 * 挂在壳里会漏掉它们。
 *
 * 用 useEffect 而不是 useLayoutEffect：后者在 SSR 阶段会报警告，而这里的代价
 * 只是客户端导航时可能有一帧旧配色 —— 导航本身就在替换内容，实测看不出来。
 * 真正要紧的首次加载是由内联脚本在首帧之前处理的，不经过这里。
 */
export function ThemeSync() {
  const pathname = usePathname()

  useEffect(() => {
    syncThemeIfLost()
  }, [pathname])

  return null
}
