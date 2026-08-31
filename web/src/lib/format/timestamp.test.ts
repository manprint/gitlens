import { describe, expect, it } from 'vitest'

import { formatTimestamp } from './timestamp'

describe('formatTimestamp', () => {
  it.each([
    ['invalid input is unknown', 'not-a-date', 'UTC', '—'],
    ['epoch in UTC', '1970-01-01T00:00:00.000Z', 'UTC', '1970-01-01 00:00:00 UTC'],
    [
      'negative epoch in New York',
      '1969-12-31T23:00:00.000Z',
      'America/New_York',
      '1969-12-31 18:00:00 UTC-5',
    ],
  ])('%s', (_name, iso, tz, expected) => {
    expect(formatTimestamp(iso, tz)).toBe(expected)
  })

  it('accepts a locale for date-fns formatting', () => {
    expect(formatTimestamp('2026-01-02T03:04:05.000Z', 'UTC', 'it-IT')).toBe(
      '2026-01-02 03:04:05 UTC',
    )
  })
})
