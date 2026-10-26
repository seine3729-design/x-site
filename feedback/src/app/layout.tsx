import type { Metadata, Viewport } from 'next'
import { ServiceWorkerScript } from '@/components/pwa/service-worker-script'
import { ThemeScript } from '@/components/theme/theme-script'
import { ThemeSync } from '@/components/theme/theme-sync'
import { Toaster } from '@/components/ui/toaster'
import { t } from '@/lib/i18n'
import './globals.css'

export const metadata: Metadata = {
  title: {
    default: `${t('brand.name')} · ${t('brand.tagline')}`,
    template: `%s · ${t('brand.name')}`,
  },
  description: t('brand.description'),
  applicationName: t('brand.name'),
  formatDetection: { telephone: false, email: false, address: false },
}

export const viewport: Viewport = {
  width: 'device-width',
  initialScale: 1,
  // 不设 maximumScale / userScalable：禁用缩放会违反 WCAG 1.4.4
  // 跟着顶栏底色走（globals.css 的 --surface），移动端地址栏才不会和页面断开。
  // 这里只能写字面值：themeColor 要在 CSS 加载前就生效，用不了 CSS 变量。
  themeColor: [
    { media: '(prefers-color-scheme: light)', color: '#ffffff' },
    { media: '(prefers-color-scheme: dark)', color: '#16161d' },
  ],
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN" suppressHydrationWarning>
      <head>
        {/*
          静态资源 CDN。两件东西都从这里取：带内容哈希的产物（next.config.ts 的 assetPrefix）
          与品牌 Logo（components/brand/logo.tsx，顶栏里 priority 加载）。

          提前把连接建好，省掉首屏上 DNS + TCP + TLS 的一整个来回。

          写两条不是笔误：浏览器的连接池按「是否带 CORS」分开。
          模块脚本一律按 CORS 模式取（规范行为），而 <img> 取 Logo 是 no-cors，
          只写一条的话另一类请求仍要自己重新建连接。
        */}
        <link rel="preconnect" href="https://cdn.duanju66.cc" />
        <link rel="preconnect" href="https://cdn.duanju66.cc" crossOrigin="anonymous" />
        {/* 必须在样式之前同步执行，否则深色模式会闪一下浅色 */}
        <ThemeScript />
      </head>
      <body>
        {/*
          客户端导航时 React 会把上面那段内联脚本加的 .dark / data-theme 抹掉，
          这个组件负责补回去。不放在 SiteShell 里是因为 /offline 不在 (site) 分组下。
        */}
        <ThemeSync />
        {children}
        {/*
          Toaster：不抢焦点，普通提示 4 秒自动消失，错误提示由调用方设为常驻。
          richColors 会用自己的配色，这里关掉，统一走我们的语义 token。
          sonner 本体懒加载，见 components/ui/toaster.tsx。
        */}
        <Toaster />
        {/*
          Service Worker：缓存带哈希的静态产物 + 断网时回退 /offline。
          刻意没有 web app manifest —— 没有它就不满足浏览器的可安装条件，
          地址栏不会出现安装图标、也不会弹安装横幅。
        */}
        <ServiceWorkerScript />
      </body>
    </html>
  )
}
