import { afterEach, describe, expect, it, vi } from 'vitest'

import { formatAxisTime, formatRangeLabel, formatTooltipTime } from './chartTime'
import { formatTimestamp } from './format'
import { resetPreferences, updatePreferences } from './preferences'
import {
  browserTimeZone,
  displayTimeZone,
  fromZonedInput,
  isTimeZone,
  resolveTimeZone,
  serverTimeZone,
  setServerTimeZone,
  subscribeTimeZone,
  toZonedInput,
  wallClock,
  zoneLabel,
} from './timezone'

const WINTER = Date.parse('2026-11-20T17:05:09Z')
const SUMMER = Date.parse('2026-07-01T10:00:00Z')

afterEach(() => {
  setServerTimeZone('UTC')
  resetPreferences()
})

describe('console time zone', () => {
  it('TZ-UI-001 follows the server default until the operator picks another zone', () => {
    expect(displayTimeZone()).toBe('UTC')
    const listener = vi.fn()
    const unsubscribe = subscribeTimeZone(listener)
    setServerTimeZone('Europe/Rome')
    setServerTimeZone('Europe/Rome')
    expect(listener).toHaveBeenCalledTimes(1)
    expect(serverTimeZone()).toBe('Europe/Rome')
    expect(displayTimeZone()).toBe('Europe/Rome')

    updatePreferences({ timeZone: 'UTC' })
    expect(listener).toHaveBeenCalledTimes(2)
    expect(displayTimeZone()).toBe('UTC')
    updatePreferences({ timeZone: 'browser' })
    expect(displayTimeZone()).toBe(browserTimeZone())
    unsubscribe()

    setServerTimeZone('Mars/Base')
    expect(serverTimeZone()).toBe('UTC')
    setServerTimeZone(undefined)
    expect(serverTimeZone()).toBe('UTC')
    expect(isTimeZone('')).toBe(false)
    expect(resolveTimeZone('server', 'Asia/Tokyo', 'UTC')).toBe('Asia/Tokyo')
    expect(resolveTimeZone('browser', 'Asia/Tokyo', 'America/New_York')).toBe('America/New_York')
    expect(resolveTimeZone('UTC', 'Asia/Tokyo', 'America/New_York')).toBe('UTC')
  })

  it('TZ-UI-002 reads the wall clock and the offset of a zone, DST included', () => {
    expect(wallClock(WINTER, 'Europe/Rome')).toEqual({
      year: 2026,
      month: 11,
      day: 20,
      hour: 18,
      minute: 5,
      second: 9,
    })
    expect(wallClock(WINTER, 'UTC').hour).toBe(17)
    expect(zoneLabel('Europe/Rome', WINTER)).toBe('UTC+1')
    expect(zoneLabel('Europe/Rome', SUMMER)).toBe('UTC+2')
    expect(zoneLabel('Asia/Kolkata', WINTER)).toBe('UTC+5:30')
    expect(zoneLabel('Etc/GMT', WINTER)).toBe('UTC')
    expect(zoneLabel('UTC')).toBe('UTC')
  })

  it('TZ-UI-003 converts datetime-local text both ways in the zone', () => {
    expect(toZonedInput(WINTER, 'Europe/Rome')).toBe('2026-11-20T18:05')
    expect(toZonedInput(SUMMER, 'Europe/Rome')).toBe('2026-07-01T12:00')
    expect(fromZonedInput('2026-11-20T18:05', 'Europe/Rome')?.toISOString()).toBe(
      '2026-11-20T17:05:00.000Z',
    )
    expect(fromZonedInput('2026-07-01T12:00', 'Europe/Rome')?.toISOString()).toBe(
      '2026-07-01T10:00:00.000Z',
    )
    // The night DST ends 02:30 comes twice: the later (winter) one is read.
    expect(fromZonedInput('2026-10-25T02:30', 'Europe/Rome')?.toISOString()).toBe(
      '2026-10-25T01:30:00.000Z',
    )
    // The night it starts 02:30 does not exist: it reads as 03:30 summer time.
    expect(fromZonedInput('2026-03-29T02:30', 'Europe/Rome')?.toISOString()).toBe(
      '2026-03-29T01:30:00.000Z',
    )
    expect(fromZonedInput('2026-11-20T18:05', 'UTC')?.toISOString()).toBe(
      '2026-11-20T18:05:00.000Z',
    )
    expect(fromZonedInput('2026-11-20 18:05', 'UTC')).toBeNull()
    expect(fromZonedInput('', 'UTC')).toBeNull()
  })

  it('TZ-UI-004 every formatter writes the console zone', () => {
    setServerTimeZone('Europe/Rome')
    expect(formatTimestamp('2026-11-20T17:05:09Z')).toBe('2026-11-20 18:05:09 UTC+1')
    expect(formatTooltipTime(WINTER)).toBe('2026-11-20 18:05:09 UTC+1')
    expect(formatAxisTime(WINTER, 60 * 60 * 1_000)).toBe('18:05')
    expect(formatAxisTime(WINTER, 7 * 24 * 60 * 60 * 1_000)).toBe('Nov 20 18:05')
    expect(
      formatRangeLabel(new Date('2026-11-20T22:30:00Z'), new Date('2026-11-20T23:30:00Z'), 60),
    ).toBe('Nov 20 23:30 → Nov 21 00:30 UTC+1 · step 1m')

    updatePreferences({ timeZone: 'UTC' })
    expect(formatTimestamp('2026-11-20T17:05:09Z')).toBe('2026-11-20 17:05:09 UTC')
    expect(formatTooltipTime(WINTER)).toBe('2026-11-20 17:05:09 UTC')
    expect(
      formatRangeLabel(new Date('2026-11-20T22:30:00Z'), new Date('2026-11-20T23:30:00Z'), 60),
    ).toBe('Nov 20 22:30 → 23:30 UTC · step 1m')
  })
})
