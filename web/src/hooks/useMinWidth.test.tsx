import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useMinWidth } from './useMinWidth'

/** A matchMedia that answers `(min-width: Npx)` from a width the test changes. */
function mockViewport(initial: number) {
  let width = initial
  const listeners = new Set<() => void>()
  const queries: string[] = []
  vi.stubGlobal('matchMedia', (query: string) => {
    queries.push(query)
    const min = Number(/min-width:\s*(\d+)px/.exec(query)?.[1] ?? NaN)
    return {
      get matches() {
        return width >= min
      },
      media: query,
      addEventListener: (_: string, listener: () => void) => listeners.add(listener),
      removeEventListener: (_: string, listener: () => void) => listeners.delete(listener),
    }
  })
  return {
    resize(next: number) {
      width = next
      for (const listener of listeners) listener()
    },
    listeners,
    queries,
  }
}

describe('useMinWidth', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('UI-SHELL-030 follows the viewport across the width', () => {
    const viewport = mockViewport(1920)
    const { result, rerender, unmount } = renderHook(() => useMinWidth(1600))
    expect(result.current).toBe(true)
    // One list per query, however often the hook renders.
    rerender()
    rerender()
    expect(viewport.queries).toEqual(['(min-width: 1600px)'])

    act(() => viewport.resize(1599))
    expect(result.current).toBe(false)
    act(() => viewport.resize(1600))
    expect(result.current).toBe(true)

    unmount()
    expect(viewport.listeners.size).toBe(0)
  })

  it('UI-SHELL-031 is false where the browser has no matchMedia', () => {
    vi.stubGlobal('matchMedia', undefined)
    const { result } = renderHook(() => useMinWidth(1600))
    expect(result.current).toBe(false)
  })
})
