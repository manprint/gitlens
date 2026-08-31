import { describe, expect, it, vi } from 'vitest'

import {
  DEFAULT_PREFERENCES,
  PREFERENCES_STORAGE_KEY,
  readPreferences,
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
    expect(writePreferences({ theme: 'light' }, brokenStorage)).toEqual({
      ...DEFAULT_PREFERENCES,
      theme: 'light',
    })
  })

  it('normalizes valid and invalid stored preference fields', () => {
    localStorage.setItem(
      PREFERENCES_STORAGE_KEY,
      JSON.stringify({ theme: 'system', density: 'compact', sidebarCollapsed: true }),
    )
    expect(readPreferences()).toEqual({
      theme: 'system',
      density: 'compact',
      sidebarCollapsed: true,
    })

    localStorage.setItem(
      PREFERENCES_STORAGE_KEY,
      JSON.stringify({ theme: 'sepia', density: 'dense', sidebarCollapsed: 'yes' }),
    )
    expect(readPreferences()).toEqual(DEFAULT_PREFERENCES)
  })

  it('returns defaults when an explicit storage is unavailable', () => {
    expect(readPreferences(null)).toEqual(DEFAULT_PREFERENCES)
    expect(writePreferences({ theme: 'light' }, null)).toEqual({
      ...DEFAULT_PREFERENCES,
      theme: 'light',
    })
  })
})
