import { describe, expect, it, vi } from 'vitest'

import { TIMESTAMP_COLUMN_WIDTH, formatRecentTimestamp, formatTimestamp } from './timestamp'

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

  // The suite pins TZ=UTC, where a Date read back in local time looks correct.
  // The browser zone must really change, or the case proves nothing.
  it.each([
    ['UTC', 'Europe/Rome', '1970-01-01 00:00:00 UTC'],
    ['Europe/Rome', 'Europe/Rome', '1970-01-01 01:00:00 UTC+1'],
    ['UTC', 'America/New_York', '1970-01-01 00:00:00 UTC'],
    ['America/New_York', 'America/New_York', '1969-12-31 19:00:00 UTC-5'],
  ])(
    'WD-02 renders display zone %s when the browser runs in %s',
    (displayZone, browserZone, expected) => {
      vi.stubEnv('TZ', browserZone)
      expect(new Date(0).getTimezoneOffset()).not.toBe(0)

      expect(formatTimestamp('1970-01-01T00:00:00.000Z', displayZone)).toBe(expected)
    },
  )

  it('accepts a locale for date-fns formatting', () => {
    expect(formatTimestamp('2026-01-02T03:04:05.000Z', 'UTC', 'it-IT')).toBe(
      '2026-01-02 03:04:05 UTC',
    )
  })
})

describe('formatRecentTimestamp', () => {
  const now = new Date('2026-11-20T22:30:00.000Z')
  it('UI-TZ-020 drops the date only when it is today in the console zone', () => {
    expect(formatRecentTimestamp('2026-11-20T20:32:59Z', now, 'Europe/Rome')).toBe('21:32:59 UTC+1')
    expect(formatRecentTimestamp('2026-11-20T22:59:00Z', now, 'Europe/Rome')).toBe('23:59:00 UTC+1')
    expect(formatRecentTimestamp('2026-11-19T20:00:00Z', now, 'Europe/Rome')).toBe(
      '2026-11-19 21:00:00 UTC+1',
    )
    // Already the 21st in Rome while now is the 20th there.
    expect(formatRecentTimestamp('2026-11-20T23:30:00Z', now, 'Europe/Rome')).toBe(
      '2026-11-21 00:30:00 UTC+1',
    )
    expect(formatRecentTimestamp('bad', now, 'UTC')).toBe('—')
  })

  it('UI-TZ-021 sizes timestamp columns in rem', () => {
    expect(TIMESTAMP_COLUMN_WIDTH).toMatch(/^\d+(\.\d+)?rem$/)
  })
})
