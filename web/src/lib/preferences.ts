import type { RangePreset } from './timerange'

export const PREFERENCES_STORAGE_KEY = 'pglens.preferences'

export type ThemePreference = 'dark' | 'light' | 'system'

/** The default time ranges offered: `15m` is deliberately not one (R-11.0 P2). */
export const DEFAULT_TIME_RANGES = [
  '1h',
  '6h',
  '24h',
  '7d',
] as const satisfies readonly RangePreset[]
export type DefaultTimeRange = (typeof DEFAULT_TIME_RANGES)[number]

/** `auto` keeps every surface's own policy (R-11.0 P1). */
export const REFRESH_INTERVALS = ['auto', 'off', '15s', '30s', '60s'] as const
export type RefreshIntervalPreference = (typeof REFRESH_INTERVALS)[number]

/** The shared tables' page sizes (D23). */
export const TABLE_PAGE_SIZES = [20, 50, 100] as const
export type TablePageSize = (typeof TABLE_PAGE_SIZES)[number]

export interface Preferences {
  theme: ThemePreference
  defaultTimeRange: DefaultTimeRange
  refreshInterval: RefreshIntervalPreference
  tablePageSize: TablePageSize
}

// index.html repeats the theme default for the first paint; keep the two in step.
export const DEFAULT_PREFERENCES: Preferences = {
  theme: 'light',
  defaultTimeRange: '1h',
  refreshInterval: 'auto',
  tablePageSize: 20,
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

function oneOf<T>(allowed: readonly T[], value: unknown, fallback: T): T {
  return allowed.includes(value as T) ? (value as T) : fallback
}

/** Each field is checked on its own: an invalid value falls back to its default only. */
function normalizePreferences(value: unknown): Preferences {
  if (typeof value !== 'object' || value === null) return { ...DEFAULT_PREFERENCES }

  const candidate = value as Partial<Record<keyof Preferences, unknown>>
  return {
    theme: isThemePreference(candidate.theme) ? candidate.theme : DEFAULT_PREFERENCES.theme,
    defaultTimeRange: oneOf(
      DEFAULT_TIME_RANGES,
      candidate.defaultTimeRange,
      DEFAULT_PREFERENCES.defaultTimeRange,
    ),
    refreshInterval: oneOf(
      REFRESH_INTERVALS,
      candidate.refreshInterval,
      DEFAULT_PREFERENCES.refreshInterval,
    ),
    tablePageSize: oneOf(
      TABLE_PAGE_SIZES,
      candidate.tablePageSize,
      DEFAULT_PREFERENCES.tablePageSize,
    ),
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

/** Store `preferences`; whether the storage kept them. Never throws. */
function persist(next: Preferences, storage: PreferencesStorage | null): boolean {
  if (!storage) return false

  try {
    storage.setItem(PREFERENCES_STORAGE_KEY, JSON.stringify(next))
    return true
  } catch {
    // Preference persistence is best effort; a full or unavailable store must not break the UI.
    return false
  }
}

export function writePreferences(
  preferences: Partial<Preferences>,
  storage: PreferencesStorage | null = getStorage(),
): Preferences {
  const next = normalizePreferences({ ...readPreferences(storage), ...preferences })
  persist(next, storage)
  return next
}

/*
 * The live store: every reader of the preferences (theme toggle, Preferences
 * tab, time range, refresh interval, tables) subscribes here, so a change
 * applies at once in the running app without a reload.
 */

type Listener = () => void

const listeners = new Set<Listener>()

/** The last snapshot, keyed by the stored text it was read from (a stable object per value). */
let cached: { raw: string | null; preferences: Preferences } | null = null
/**
 * A change the storage could not keep (full, blocked): it holds for this page
 * until the stored text changes, so the control still answers.
 */
let unsaved: { raw: string | null; preferences: Preferences } | null = null

function readRaw(): string | null {
  try {
    return getStorage()?.getItem(PREFERENCES_STORAGE_KEY) ?? null
  } catch {
    return null
  }
}

/** The current preferences; the same object until they change (`useSyncExternalStore`). */
export function getPreferencesSnapshot(): Preferences {
  const raw = readRaw()
  if (unsaved !== null) {
    if (unsaved.raw === raw) return unsaved.preferences
    unsaved = null
  }
  if (cached?.raw !== raw) {
    cached = { raw, preferences: readPreferences() }
  }
  return cached.preferences
}

function notify() {
  for (const listener of listeners) listener()
}

function onStorage(event: StorageEvent) {
  // Another tab of the console changed them.
  if (event.key === null || event.key === PREFERENCES_STORAGE_KEY) notify()
}

export function subscribePreferences(listener: Listener): () => void {
  listeners.add(listener)
  if (listeners.size === 1 && typeof window !== 'undefined') {
    window.addEventListener('storage', onStorage)
  }
  return () => {
    listeners.delete(listener)
    if (listeners.size === 0 && typeof window !== 'undefined') {
      window.removeEventListener('storage', onStorage)
    }
  }
}

/** Apply `patch` now and store it (best effort); returns the new preferences. Never throws. */
export function updatePreferences(patch: Partial<Preferences>): Preferences {
  const next = normalizePreferences({ ...getPreferencesSnapshot(), ...patch })
  unsaved = persist(next, getStorage()) ? null : { raw: readRaw(), preferences: next }
  notify()
  return next
}

/** Every preference back to its default. */
export function resetPreferences(): Preferences {
  return updatePreferences(DEFAULT_PREFERENCES)
}
