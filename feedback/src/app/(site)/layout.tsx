import { SiteShell } from '@/components/layout/site-shell'

/** 用户端的所有页面共用同一个壳（顶栏 + 底部导航 + 页脚）。 */
export default function SiteLayout({ children }: { children: React.ReactNode }) {
  return <SiteShell>{children}</SiteShell>
}
