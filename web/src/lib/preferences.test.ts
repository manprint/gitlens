import { describe, expect, it, vi } from 'vitest'

import {
  DEFAULT_PREFERENCES,
  getPreferencesSnapshot,
  PREFERENCES_STORAGE_KEY,
  readPreferences,
  resetPreferences,
  subscribePreferences,
  updatePreferences,
  writePreferences,
} from './preferences'

describe('preferences', () => {
  it('UI-PREF-001 defaults when storage is empty', () => {
    localStorage.clear()

    expect(readPreferences()).toEqual(DEFAULT_PREFERENCES)
  })

  it('UI-PREF-002 defaults when storage holds invalid JSON', () => {
    localStorage.setItem(PREFERENCES_STORAGE_KEY, '{not-json')

    expect(readPreferences()).toEqual(DEFAULT_PREFERENCES)
  })

  it('UI-PREF-003 defaults when localStorage throws', () => {
    const brokenStorage = {
      getItem: vi.fn(() => {
        throw new Error('storage unavailable')
      }),
      setItem: vi.fn(() => {
        throw new Error('storage unavailable')
      }),
    }

    expect(readPreferences(brokenStorage)).toEqual(DEFAULT_PREFERENCES)
    expect(writePreferences({ theme: 'dark' }, brokenStorage)).toEqual({
      ...DEFAULT_PREFERENCES,
      theme: 'dark',
    })
  })

  it('normalizes valid and invalid stored preference fields', () => {
    localStorage.setItem(PREFERENCES_STORAGE_KEY, JSON.stringify({ theme: 'system' }))
    expect(readPreferences()).toEqual({ ...DEFAULT_PREFERENCES, theme: 'system' })

    localStorage.setItem(PREFERENCES_STORAGE_KEY, JSON.stringify({ theme: 'sepia' }))
    expect(readPreferences()).toEqual(DEFAULT_PREFERENCES)
  })

  it('returns defaults when an explicit storage is unavailable', () => {
    expect(readPreferences(null)).toEqual(DEFAULT_PREFERENCES)
    expect(writePreferences({ theme: 'light' }, null)).toEqual({
      ...DEFAULT_PREFERENCES,
      theme: 'light',
    })
  })

  it('PREF-001 defaults: light theme, 1h range, automatic refresh, 20 rows', () => {
    localStorage.clear()

    expect(DEFAULT_PREFERENCES).toEqual({
      theme: 'light',
      defaultTimeRange: '1h',
      refreshInterval: 'auto',
      tablePageSize: 20,
    })
    expect(readPreferences()).toEqual(DEFAULT_PREFERENCES)
    expect(getPreferencesSnapshot()).toEqual(DEFAULT_PREFERENCES)
  })

  it('PREF-002 a change is stored, read back after a reload and announced to subscribers', () => {
    localStorage.clear()
    const listener = vi.fn()
    const unsubscribe = subscribePreferences(listener)

    const next = updatePreferences({ defaultTimeRange: '6h', tablePageSize: 50 })
    expect(listener).toHaveBeenCalledTimes(1)
    expect(next).toEqual({ ...DEFAULT_PREFERENCES, defaultTimeRange: '6h', tablePageSize: 50 })
    // A reload reads the stored text again.
    expect(readPreferences()).toEqual(next)
    expect(getPreferencesSnapshot()).toEqual(next)
    // The snapshot is the same object until the preferences change.
    expect(getPreferencesSnapshot()).toBe(getPreferencesSnapshot())

    updatePreferences({ refreshInterval: '30s', theme: 'dark' })
    expect(readPreferences()).toEqual({
      theme: 'dark',
      defaultTimeRange: '6h',
      refreshInterval: '30s',
      tablePageSize: 50,
    })

    expect(resetPreferences()).toEqual(DEFAULT_PREFERENCES)
    expect(readPreferences()).toEqual(DEFAULT_PREFERENCES)
    expect(listener).toHaveBeenCalledTimes(3)
    unsubscribe()
    updatePreferences({ tablePageSize: 100 })
    expect(listener).toHaveBeenCalledTimes(3)
    localStorage.clear()
  })

  it('PREF-003 an invalid stored value falls back to its own default only', () => {
    localStorage.setItem(
      PREFERENCES_STORAGE_KEY,
      JSON.stringify({
        theme: 'dark',
        defaultTimeRange: '15m',
        refreshInterval: '5s',
        tablePageSize: '50',
      }),
    )
    expect(readPreferences()).toEqual({ ...DEFAULT_PREFERENCES, theme: 'dark' })
    expect(getPreferencesSnapshot()).toEqual({ ...DEFAULT_PREFERENCES, theme: 'dark' })

    localStorage.setItem(
      PREFERENCES_STORAGE_KEY,
      JSON.stringify({
        theme: 7,
        defaultTimeRange: '24h',
        refreshInterval: 'off',
        tablePageSize: 30,
      }),
    )
    expect(readPreferences()).toEqual({
      ...DEFAULT_PREFERENCES,
      defaultTimeRange: '24h',
      refreshInterval: 'off',
    })

    localStorage.setItem(PREFERENCES_STORAGE_KEY, JSON.stringify(['6h']))
    expect(readPreferences()).toEqual(DEFAULT_PREFERENCES)
    localStorage.clear()
  })

  it('PREF-004 an unavailable storage never throws and the change still applies to this page', () => {
    localStorage.clear()
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('storage unavailable')
    })
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota exceeded')
    })
    try {
      expect(() => getPreferencesSnapshot()).not.toThrow()
      expect(getPreferencesSnapshot()).toEqual(DEFAULT_PREFERENCES)
      expect(() => updatePreferences({ tablePageSize: 100 })).not.toThrow()
      // Not stored, but this page keeps the change.
      expect(getPreferencesSnapshot().tablePageSize).toBe(100)
      expect(() => resetPreferences()).not.toThrow()
      expect(getPreferencesSnapshot()).toEqual(DEFAULT_PREFERENCES)
    } finally {
      getItem.mockRestore()
      setItem.mockRestore()
    }
    expect(localStorage.getItem(PREFERENCES_STORAGE_KEY)).toBeNull()
    expect(getPreferencesSnapshot()).toEqual(DEFAULT_PREFERENCES)
  })
})
