import type { Metadata } from 'next'
import { CloudOff } from 'lucide-react'
import { t } from '@/lib/i18n'

export const metadata: Metadata = {
  title: t('offlinePage.title'),
  robots: { index: false, follow: false },
}

/**
 * 断网回退页，由 Service Worker 在安装时预缓存（public/sw.js）。
 *
 * 刻意不放在 `(site)` 分组下：那个布局的顶栏要读会话（`getCurrentUser`），
 * 一来断网时取不到，二来预缓存一个带账号信息的 HTML 会在换账号后串号。
 * 这里只用 root layout，整页没有任何用户数据，也不发任何请求。
 *
 * 同理不用 Button 组件里的 asChild + Link：客户端路由在断网时没法导航，
 * 用原生 <a> 触发一次真实的重新加载，网络恢复后才能真的回到站点。
 */
export default function OfflinePage() {
  return (
    <main className="flex min-h-dvh flex-col items-center justify-center gap-5 px-6 text-center">
      <span
        className="flex size-14 items-center justify-center rounded-pill bg-surface-raised text-fg-subtle"
        aria-hidden
      >
        <CloudOff className="size-7" />
      </span>
      <div className="max-w-prose">
        <h1 className="text-xl font-semibold tracking-tight text-fg">{t('offlinePage.title')}</h1>
        <p className="mt-2 text-sm text-fg-muted">{t('offlinePage.body')}</p>
      </div>
      {/*
        eslint-disable-next-line @next/next/no-html-link-for-pages --
        这里必须是原生 <a>。next/link 走客户端路由，而断网状态下客户端路由
        取不到 RSC 载荷，点了只会卡住。原生 <a> 触发一次真实的整页加载，
        网络恢复后才能真的回到站点。
      */}
      <a
        href="/"
        className="inline-flex h-11 items-center rounded-control bg-primary px-5 text-sm font-medium text-fg-on-fill"
      >
        {t('offlinePage.retry')}
      </a>
    </main>
  )
}
