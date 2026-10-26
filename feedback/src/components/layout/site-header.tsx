import { Suspense } from 'react'
import Link from 'next/link'
import { Plus } from 'lucide-react'
import { Logo } from '@/components/brand/logo'
import { ThemeToggle } from '@/components/theme/theme-toggle'
import { Button } from '@/components/ui/button'
import { Container } from '@/components/ui/section'
import { getCurrentUser } from '@/lib/server/session'
import { t } from '@/lib/i18n'
import { AccountMenu } from './account-menu'
import { NavLinks } from './nav-links'

/**
 * 顶栏（feedback/docs/06 第 3.1 节）。
 *
 * 桌面：Logo + 一级导航 + 提交反馈 + 外观 + 账号。
 * 移动：顶栏只留上下文与必要动作，一级导航交给底部导航栏。
 *
 * 吸顶是半透明毛玻璃，但背景色不透明度留得够高（/85），
 * 滚动时下面的文字不会透上来影响对比度。
 *
 * 顶栏本身不读会话 —— 这是首页能做成静态页的关键。
 * 读 Cookie 的部分收在 `AccountSlot` 里，用 Suspense 隔开：
 * Next.js 就能把顶栏连同页面的公开内容一起预渲染成静态 HTML，
 * 只把账号这一小块留作运行时填充（feedback/docs/06 第 11.5 节）。
 */
export function SiteHeader() {
  return (
    <header className="sticky top-0 z-40 border-b border-border bg-surface/85 backdrop-blur-md">
      <Container className="flex h-16 items-center gap-3 sm:gap-6">
        <Link
          href="/"
          className="shrink-0 rounded-control"
          aria-label={`${t('brand.name')}首页`}
        >
          <Logo />
        </Link>

        <NavLinks />

        <div className="ms-auto flex items-center gap-1.5 sm:gap-2">
          <Button asChild size="sm" className="max-sm:hidden">
            <Link href="/feedback/new">
              <Plus aria-hidden />
              {t('action.newFeedback')}
            </Link>
          </Button>
          {/*
            移动端只给图标，节省横向空间；aria-label 保证可访问名不丢。
            尺寸用 44px（icon 档）而不是 40px：这是本页的主操作，
            docs/06 第 6 节要求主操作触控目标至少 44×44。
          */}
          <Button asChild size="icon" className="sm:hidden" aria-label={t('action.newFeedback')}>
            <Link href="/feedback/new">
              <Plus aria-hidden />
            </Link>
          </Button>
          <ThemeToggle />
          <Suspense fallback={<AccountSlotFallback />}>
            <AccountSlot />
          </Suspense>
        </div>
      </Container>
    </header>
  )
}

/**
 * 槽位的最小宽度。
 *
 * 三种状态会先后出现在这里：占位 → 未登录（登录 + 注册两个按钮）
 * 或已登录（头像 + 昵称）。宽度不一致的话，流式替换的那一刻
 * 左边的「提交反馈」按钮会被推一下，就是一次实打实的 CLS。
 * 所以给一个能装下最宽那种状态的下限，三者都右对齐。
 */
const SLOT = 'flex min-w-16 justify-end sm:min-w-32'

async function AccountSlot() {
  // 这里是整个顶栏唯一读 Cookie 的地方
  const user = await getCurrentUser().catch(() => null)
  return (
    <div className={SLOT}>
      <AccountMenu user={user} />
    </div>
  )
}

function AccountSlotFallback() {
  return (
    <div className={SLOT} aria-hidden>
      {/* 和头像同尺寸的占位。不用 animate-pulse：它只闪一瞬就被替换掉，
          闪一下反而比静默占位更显眼。 */}
      <span className="size-7 rounded-pill bg-surface-raised" />
    </div>
  )
}
