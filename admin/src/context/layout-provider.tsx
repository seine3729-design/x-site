import { createContext, useContext } from 'react'
import { useLayoutStorage } from '@/hooks/use-layout-storage'

export type Collapsible = 'offcanvas' | 'icon' | 'none'
export type Variant = 'inset' | 'sidebar' | 'floating'

// Default values
const DEFAULT_VARIANT = 'floating'
const DEFAULT_COLLAPSIBLE = 'icon'

type LayoutContextType = {
  resetLayout: () => void

  defaultCollapsible: Collapsible
  collapsible: Collapsible
  setCollapsible: (collapsible: Collapsible) => void

  defaultVariant: Variant
  variant: Variant
  setVariant: (variant: Variant) => void
}

const LayoutContext = createContext<LayoutContextType | null>(null)

type LayoutProviderProps = {
  children: React.ReactNode
}

export function LayoutProvider({ children }: LayoutProviderProps) {
  const { settings, updateSettings } = useLayoutStorage()

  const setCollapsible = (newCollapsible: Collapsible) => {
    updateSettings({ collapsible: newCollapsible })
  }

  const setVariant = (newVariant: Variant) => {
    updateSettings({ variant: newVariant })
  }

  const resetLayout = () => {
    updateSettings({ 
      collapsible: DEFAULT_COLLAPSIBLE,
      variant: DEFAULT_VARIANT 
    })
  }

  const contextValue: LayoutContextType = {
    resetLayout,
    defaultCollapsible: DEFAULT_COLLAPSIBLE,
    collapsible: settings.collapsible,
    setCollapsible,
    defaultVariant: DEFAULT_VARIANT,
    variant: settings.variant,
    setVariant,
  }

  return <LayoutContext value={contextValue}>{children}</LayoutContext>
}

// Define the hook for the provider
// eslint-disable-next-line react-refresh/only-export-components
export function useLayout() {
  const context = useContext(LayoutContext)
  if (!context) {
    throw new Error('useLayout must be used within a LayoutProvider')
  }
  return context
}
