import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { PREFERENCES_STORAGE_KEY } from '@/lib/preferences'

import { useTheme } from './useTheme'

describe('useTheme', () => {
  it('UI-PREF-004 persists a theme change and applies its class', () => {
    localStorage.clear()
    document.documentElement.classList.remove('dark')

    const { result } = renderHook(() => useTheme())

    expect(result.current.theme).toBe('light')
    expect(document.documentElement).not.toHaveClass('dark')

    act(() => result.current.setTheme('dark'))

    expect(result.current.theme).toBe('dark')
    expect(document.documentElement).toHaveClass('dark')
    expect(JSON.parse(localStorage.getItem(PREFERENCES_STORAGE_KEY) ?? '{}')).toMatchObject({
      theme: 'dark',
    })
  })
})
