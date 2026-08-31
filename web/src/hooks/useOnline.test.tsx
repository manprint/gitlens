import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useOnline } from './useOnline'

describe('useOnline', () => {
  it('UI-SHELL-021 tracks browser offline and online events', async () => {
    const { result } = renderHook(() => useOnline())

    expect(result.current).toBe(navigator.onLine)

    await act(async () => {
      window.dispatchEvent(new Event('offline'))
      await Promise.resolve()
    })
    expect(result.current).toBe(false)

    await act(async () => {
      window.dispatchEvent(new Event('online'))
      await Promise.resolve()
    })
    expect(result.current).toBe(true)
  })
})
