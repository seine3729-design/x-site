import { useState, useEffect } from 'react'
import { getCookie, setCookie } from '@/lib/cookies'

export interface LayoutSettings {
  theme: 'light' | 'dark' | 'system'
  variant: 'inset' | 'floating' | 'sidebar'
  collapsible: 'icon' | 'offcanvas' | 'none'
  sidebarState: boolean
  avatar: string
}

const DEFAULT_SETTINGS: LayoutSettings = {
  theme: 'system',
  variant: 'floating',
  collapsible: 'icon',
  sidebarState: true,
  avatar: 'horse',
}

const COOKIE_NAME = 'JT_LAYOUT'
const COOKIE_MAX_AGE = 60 * 60 * 24 * 365 // 1 year

export function useLayoutStorage() {
  const [settings, setSettings] = useState<LayoutSettings>(() => {
    const stored = getCookie(COOKIE_NAME)
    if (stored) {
      try {
        return { ...DEFAULT_SETTINGS, ...JSON.parse(stored) }
      } catch {
        return DEFAULT_SETTINGS
      }
    }
    return DEFAULT_SETTINGS
  })

  useEffect(() => {
    setCookie(COOKIE_NAME, JSON.stringify(settings), COOKIE_MAX_AGE)
  }, [settings])

  const updateSettings = (updates: Partial<LayoutSettings>) => {
    setSettings((prev) => ({ ...prev, ...updates }))
  }

  const resetSettings = () => {
    setSettings(DEFAULT_SETTINGS)
  }

  return {
    settings,
    updateSettings,
    resetSettings,
    defaultSettings: DEFAULT_SETTINGS,
  }
}
