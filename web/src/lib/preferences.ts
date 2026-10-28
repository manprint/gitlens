export const PREFERENCES_STORAGE_KEY = 'pglens.preferences'

export type ThemePreference = 'dark' | 'light' | 'system'

export interface Preferences {
  theme: ThemePreference
}

// index.html repeats this default for the first paint; keep the two in step.
export const DEFAULT_PREFERENCES: Preferences = {
  theme: 'light',
}

type PreferencesStorage = Pick<Storage, 'getItem' | 'setItem'>

function getStorage(): PreferencesStorage | null {
  try {
    return typeof window === 'undefined' ? null : window.localStorage
  } catch {
    return null
  }
}

function isThemePreference(value: unknown): value is ThemePreference {
  return value === 'dark' || value === 'light' || value === 'system'
}

function normalizePreferences(value: unknown): Preferences {
  if (typeof value !== 'object' || value === null) return { ...DEFAULT_PREFERENCES }

  const candidate = value as Partial<Preferences>
  return {
    theme: isThemePreference(candidate.theme) ? candidate.theme : DEFAULT_PREFERENCES.theme,
  }
}

export function readPreferences(storage: PreferencesStorage | null = getStorage()): Preferences {
  if (!storage) return { ...DEFAULT_PREFERENCES }

  try {
    const raw = storage.getItem(PREFERENCES_STORAGE_KEY)
    return raw === null ? { ...DEFAULT_PREFERENCES } : normalizePreferences(JSON.parse(raw))
  } catch {
    return { ...DEFAULT_PREFERENCES }
  }
}

export function writePreferences(
  preferences: Partial<Preferences>,
  storage: PreferencesStorage | null = getStorage(),
): Preferences {
  const next = normalizePreferences({ ...readPreferences(storage), ...preferences })
  if (!storage) return next

  try {
    storage.setItem(PREFERENCES_STORAGE_KEY, JSON.stringify(next))
  } catch {
    // Preference persistence is best effort; a full or unavailable store must not break the UI.
  }
  return next
}
