'use client'

import Link from 'next/link'
import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { CircleUser, LogOut, Settings } from 'lucide-react'
import { Avatar } from '@/components/ui/avatar'
import { logout } from '@/features/auth/actions'
import { cn } from '@/lib/utils'
import { t } from '@/lib/i18n'
import type { FbUser } from '@/lib/api/types'

/** min-h-10：菜单项也是触控目标，纯 padding 撑出来的高度在移动端不够点。 */
const ITEM =
  'flex min-h-10 cursor-pointer items-center gap-2 rounded-control px-2.5 py-2 text-sm outline-none data-highlighted:bg-surface-raised'

/**
 * 已登录用户的账号菜单。
 *
 * 单独成文件是为了让它能被 account-menu.tsx 懒加载：
 * Radix DropdownMenu 会连带 floating-ui 一起进包（实测 28.5 KB br），
 * 而这个菜单在首屏完全不可见，没有理由挡在 hydration 前面。
 *
 * 「退出登录」与其他菜单项之间有分隔线，避免误点（destructive-nav-separation）。
 */
export function AccountMenuContent({ user }: { user: FbUser }) {
  const name = user.nickname || user.handle || ''
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild>
        <button
          type="button"
          className="flex cursor-pointer items-center gap-2 rounded-pill p-0.5 pe-2 transition-colors duration-(--duration-hover) hover:bg-surface-raised"
          aria-label={`${t('nav.account')}：${name}`}
        >
          <Avatar name={name} seed={user.tester_no ?? 0} size="sm" />
          <span className="hidden max-w-24 truncate text-sm text-fg-muted sm:inline">{name}</span>
        </button>
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content
          align="end"
          sideOffset={8}
          className={cn(
            'z-50 min-w-52 rounded-surface border border-border bg-surface-overlay p-1 shadow-popover',
            'origin-(--radix-dropdown-menu-content-transform-origin)',
            'data-[state=open]:animate-[fb-scale-in_var(--duration-hover)_var(--ease-out-soft)]',
            'data-[state=closed]:animate-[fb-fade-out_120ms_var(--ease-out-soft)]',
          )}
        >
          <div className="px-2.5 py-2">
            <p className="truncate text-sm font-medium text-fg">{name}</p>
            {user.tester_no ? (
              <p className="mt-0.5 text-2xs text-fg-muted tabular">内测 {user.tester_no}</p>
            ) : null}
          </div>
          <DropdownMenu.Separator className="my-1 h-px bg-border" />
          {/*
            这里原来还有一项「我的反馈」指向 /me/feedback —— 那个路由不存在，是死链。
            /me 页面本身就有「我的反馈」区块，所以不补路由，直接去掉这一项。
          */}
          <DropdownMenu.Item asChild className={ITEM}>
            <Link href="/me">
              <CircleUser className="size-4" aria-hidden />
              {t('nav.me')}
            </Link>
          </DropdownMenu.Item>
          <DropdownMenu.Item asChild className={ITEM}>
            <Link href="/settings">
              <Settings className="size-4" aria-hidden />
              设置
            </Link>
          </DropdownMenu.Item>
          <DropdownMenu.Separator className="my-1 h-px bg-border" />
          <DropdownMenu.Item asChild className={ITEM}>
            <form action={logout}>
              <button
                type="submit"
                className="flex w-full cursor-pointer items-center gap-2 text-danger-fg"
              >
                <LogOut className="size-4" aria-hidden />
                {t('nav.logout')}
              </button>
            </form>
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  )
}
