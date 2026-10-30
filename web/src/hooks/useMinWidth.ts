import { useCallback, useMemo, useSyncExternalStore } from 'react'

function mediaQuery(query: string): MediaQueryList | null {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return null
  try {
    return window.matchMedia(query)
  } catch {
    return null
  }
}

/**
 * Whether the viewport is at least `pixels` wide, as the Tailwind
 * `min-[Npx]:` variants decide it, kept current as the window is resized. For a
 * part that must move to another place in the tree at a width, where CSS alone
 * would have to render it twice. False where the browser has no matchMedia.
 */
export function useMinWidth(pixels: number): boolean {
  // One list per query: every render and every subscription read the same one.
  const list = useMemo(() => mediaQuery(`(min-width: ${pixels}px)`), [pixels])
  const subscribe = useCallback(
    (onChange: () => void) => {
      if (list === null) return () => undefined
      list.addEventListener('change', onChange)
      return () => list.removeEventListener('change', onChange)
    },
    [list],
  )
  return useSyncExternalStore(
    subscribe,
    () => list?.matches ?? false,
    () => false,
  )
}
