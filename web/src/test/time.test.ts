import { describe, expect, it, vi } from 'vitest'

import { NOW } from './time'

describe('deterministic test time', () => {
  it('freezes Date.now at NOW', () => {
    expect(Date.now()).toBe(NOW.getTime())
  })

  it('advances only when the test advances timers', () => {
    const initial = Date.now()

    expect(Date.now()).toBe(initial)
    vi.advanceTimersByTime(5 * 60 * 1000)
    expect(Date.now()).toBe(initial + 5 * 60 * 1000)
  })

  it('resolves the UTC timezone', () => {
    expect(Intl.DateTimeFormat().resolvedOptions().timeZone).toBe('UTC')
  })

  it('formats a timestamp identically on repeated runs', () => {
    const format = new Intl.DateTimeFormat('en-US', {
      dateStyle: 'medium',
      timeStyle: 'short',
      timeZone: 'UTC',
    })

    expect(format.format(NOW)).toBe(format.format(NOW))
  })
})
