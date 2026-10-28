import { useEffect, useState } from 'react'

import {
  readPreferences,
  writePreferences,
  type Preferences,
  type ThemePreference,
} from '@/lib/preferences'

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
  const [preferences, setPreferences] = useState<Preferences>(() => readPreferences())
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

  function updatePreferences(patch: Partial<Preferences>) {
    const next = writePreferences({ ...preferences, ...patch })
    setPreferences(next)
    setResolvedTheme(applyTheme(next.theme))
  }

  return {
    theme: preferences.theme,
    resolvedTheme,
    setTheme: (theme) => updatePreferences({ theme }),
  }
}
