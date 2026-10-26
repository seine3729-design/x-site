'use client'

import { useCallback, useSyncExternalStore } from 'react'
import { Monitor, Moon, Sun } from 'lucide-react'
import { cn } from '@/lib/utils'
import { t } from '@/lib/i18n'
import { applyTheme, readTheme, THEME_STORAGE_KEY, type Theme } from './apply'

const OPTIONS: { value: Theme; label: string; icon: typeof Sun }[] = [
  { value: 'system', label: t('nav.themeSystem'), icon: Monitor },
  { value: 'light', label: t('nav.themeLight'), icon: Sun },
  { value: 'dark', label: t('nav.themeDark'), icon: Moon },
]

/** localStorage 不会自己通知同一个标签页，所以改值时派发这个事件。 */
const CHANGE_EVENT = 'fb-theme-change'

/**
 * 当前主题选择。
 *
 * 用 useSyncExternalStore 而不是 useState + useEffect：
 * localStorage 是外部状态，effect 里同步 setState 会触发级联渲染（React 19 的 lint 会报错），
 * 而且在首帧之后才生效。这里还顺便处理了另一个标签页改主题（storage 事件）和系统主题变化。
 */
function useTheme(): Theme {
  const subscribe = useCallback((onChange: () => void) => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const onSystemChange = () => {
      if (readTheme() === 'system') applyTheme('system')
      onChange()
    }
    window.addEventListener('storage', onChange)
    window.addEventListener(CHANGE_EVENT, onChange)
    mq.addEventListener('change', onSystemChange)
    return () => {
      window.removeEventListener('storage', onChange)
      window.removeEventListener(CHANGE_EVENT, onChange)
      mq.removeEventListener('change', onSystemChange)
    }
  }, [])

  // 服务端没有 localStorage，先按「跟随系统」渲染。视觉上的选中态由
  // html[data-theme] + CSS 决定，首帧就是对的，所以这里的初值只影响 aria 状态。
  return useSyncExternalStore(subscribe, readTheme, () => 'system' as Theme)
}

/**
 * 外观切换：三段式分段控件（跟随系统 / 浅色 / 深色）。
 *
 * 为什么不是下拉菜单：下拉的触发器只能显示一个图标，用户看不出这是明暗开关
 * —— 选「跟随系统」时它显示的是一个显示器图标，和主题毫无关联。
 * 三段并排把所有选项和当前状态一次摊开，不用点开就知道这里能切主题。
 *
 * 这里不再用 Radix DropdownMenu，但首屏体积几乎没变（实测 164.1 → 163.8 KB br）：
 * floating-ui 那条依赖是账号菜单拉进来的，少一个使用方并不会让它消失。
 * 这个改动的理由是「看得见」，不是省体积。
 */
export function ThemeToggle() {
  const theme = useTheme()

  const choose = (next: Theme) => {
    localStorage.setItem(THEME_STORAGE_KEY, next)
    applyTheme(next)
    window.dispatchEvent(new Event(CHANGE_EVENT))
  }

  return (
    <div
      // radiogroup 而不是一组独立按钮：三个选项互斥，屏幕阅读器要读出「3 之 2」
      role="radiogroup"
      aria-label={t('nav.theme')}
      className="flex items-center gap-0.5 rounded-pill bg-surface-sunken p-0.5"
    >
      {OPTIONS.map((option) => {
        const Icon = option.icon
        return (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={theme === option.value}
            // 图标按钮没有可见文字，可访问名靠这个
            aria-label={option.label}
            // 这两个 data 属性给 globals.css 的高亮规则用
            className={cn(
              'theme-seg flex size-8 cursor-pointer items-center justify-center rounded-pill',
              'text-fg-subtle transition-colors duration-(--duration-hover)',
              'hover:text-fg',
            )}
            data-mode={option.value}
            onClick={() => choose(option.value)}
          >
            <Icon className="size-4" aria-hidden />
          </button>
        )
      })}
    </div>
  )
}
