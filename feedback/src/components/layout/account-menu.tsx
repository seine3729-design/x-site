'use client'

import Link from 'next/link'
import dynamic from 'next/dynamic'
import { Avatar } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { t } from '@/lib/i18n'
import type { FbUser } from '@/lib/api/types'

/**
 * 已登录的菜单懒加载。
 *
 * Radix DropdownMenu 会把 floating-ui 一起带进来（实测 28.5 KB br），
 * 而这个菜单首屏不可见、通常也不是用户进站的第一个动作。
 * dynamic 在挂载时就开始取它，等用户点到头像时早已就位。
 *
 * ssr: false：菜单内容不需要出现在 HTML 里（它默认是关的），
 * 让它参与 SSR 只会把那 28.5 KB 重新拽回关键路径。
 */
const AccountMenuContent = dynamic(
  () => import('./account-menu-content').then((m) => m.AccountMenuContent),
  {
    ssr: false,
    // 占位必须和真实触发器等尺寸，否则菜单就位时顶栏会横向跳一下。
    // 做成指向个人中心的链接而不是死按钮：这几百毫秒里点它仍然有用，
    // JS 加载失败时它也还是一个能用的账号入口。
    loading: () => <AccountLink />,
  },
)

function AccountLink() {
  return (
    <Link
      href="/me"
      className="flex items-center gap-2 rounded-pill p-0.5 pe-2 transition-colors duration-(--duration-hover) hover:bg-surface-raised"
      aria-label={t('nav.me')}
    >
      {/* 占位阶段拿不到昵称（它在父组件里），用通用底色的空头像保持尺寸一致 */}
      <Avatar name="" size="sm" />
      <span className="hidden max-w-24 truncate text-sm text-fg-muted sm:inline">
        {t('nav.me')}
      </span>
    </Link>
  )
}

/**
 * 账号入口。
 *
 * 未登录时直接给「登录」，不做成需要点开才知道里面是登录的头像——
 * 未登录用户的下一步只有一个，没必要藏在菜单里。
 * 这条分支也因此完全不引用 DropdownMenu，访客不会下载那 28.5 KB。
 */
export function AccountMenu({ user }: { user: FbUser | null }) {
  if (!user) {
    return (
      <div className="flex items-center gap-2">
        <Button asChild variant="ghost" size="sm" className="hidden sm:inline-flex">
          <Link href="/login">{t('nav.login')}</Link>
        </Button>
        <Button asChild variant="outline" size="sm">
          <Link href="/register">{t('nav.register')}</Link>
        </Button>
      </div>
    )
  }

  return <AccountMenuContent user={user} />
}
