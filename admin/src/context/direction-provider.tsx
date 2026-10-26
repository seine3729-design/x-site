import { useEffect } from 'react'
import { Direction } from 'radix-ui'

const RdxDirProvider = Direction.DirectionProvider

export type Direction = 'ltr'

const FIXED_DIRECTION: Direction = 'ltr'

export function DirectionProvider({ children }: { children: React.ReactNode }) {
  useEffect(() => {
    const htmlElement = document.documentElement
    htmlElement.setAttribute('dir', FIXED_DIRECTION)
  }, [])

  return <RdxDirProvider dir={FIXED_DIRECTION}>{children}</RdxDirProvider>
}
