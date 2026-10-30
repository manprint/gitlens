import { useEffect, useState } from 'react'

import { updatePreferences, type ThemePreference } from '@/lib/preferences'

import { usePreferences } from './usePreferences'

export type ResolvedTheme = 'dark' | 'light'

function systemTheme(): ResolvedTheme {
  if (typeof window === 'undefined') return 'light'

  try {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  } catch {
    return 'light'
  }
}

export function applyTheme(theme: ThemePreference): ResolvedTheme {
  const resolved = theme === 'system' ? systemTheme() : theme
  if (typeof document !== 'undefined') {
    document.documentElement.classList.toggle('dark', resolved === 'dark')
    document.documentElement.style.colorScheme = resolved
  }
  return resolved
}

export interface UseThemeResult {
  theme: ThemePreference
  resolvedTheme: ResolvedTheme
  setTheme: (theme: ThemePreference) => void
}

export function useTheme(): UseThemeResult {
  // The shared store: the header toggle and the Preferences tab show the same theme.
  const preferences = usePreferences()
  const [resolvedTheme, setResolvedTheme] = useState<ResolvedTheme>(() =>
    applyTheme(preferences.theme),
  )

  useEffect(() => {
    const updateTheme = () => setResolvedTheme(applyTheme(preferences.theme))
    updateTheme()

    if (preferences.theme !== 'system' || typeof window === 'undefined') return

    let mediaQuery: MediaQueryList
    try {
      mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    } catch {
      return
    }

    const handleChange = () => updateTheme()
    mediaQuery.addEventListener?.('change', handleChange)
    mediaQuery.addListener?.(handleChange)
    return () => {
      mediaQuery.removeEventListener?.('change', handleChange)
      mediaQuery.removeListener?.(handleChange)
    }
  }, [preferences.theme])

  function setTheme(theme: ThemePreference) {
    const next = updatePreferences({ theme })
    setResolvedTheme(applyTheme(next.theme))
  }

  return {
    theme: preferences.theme,
    resolvedTheme,
    setTheme,
  }
}
