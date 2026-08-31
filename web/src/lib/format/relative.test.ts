import { describe, expect, it } from 'vitest'

import { formatRelative } from './relative'

const NOW = new Date('2026-01-02T03:04:05.000Z')

describe('formatRelative', () => {
  it('returns an unknown sentinel for an invalid timestamp', () => {
    expect(formatRelative('not-a-date', NOW)).toBe('—')
  })

  it.each([
    ['zero distance', '2026-01-02T03:04:05.000Z', 'less than a minute ago'],
    ['negative direction', '2026-01-02T02:59:05.000Z', '5 minutes ago'],
    ['future direction', '2026-01-02T03:09:05.000Z', 'in 5 minutes'],
  ])('%s', (_name, iso, expected) => {
    expect(formatRelative(iso, NOW)).toBe(expected)
  })

  it('uses the requested date-fns locale', () => {
    expect(formatRelative('2026-01-02T02:59:05.000Z', NOW, 'it-IT')).toContain('fa')
  })
})
