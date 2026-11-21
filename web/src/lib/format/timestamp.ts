import { displayTimeZone } from '../timezone'

import { UNKNOWN } from './shared'

/** `YYYY-MM-DD HH:mm:ss UTC+1`, by default in the console's zone (lib/timezone). */
export function formatTimestamp(
  iso: string,
  tz: string = displayTimeZone(),
  locale = 'en-US',
): string {
  const date = new Date(iso)
  if (!Number.isFinite(date.getTime())) return UNKNOWN

  const parts = new Intl.DateTimeFormat(locale, {
    timeZone: tz,
    numberingSystem: 'latn',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
    timeZoneName: 'shortOffset',
  })
    .formatToParts(date)
    .reduce<Record<string, string>>((result, part) => {
      result[part.type] = part.value
      return result
    }, {})

  const timeZoneName = parts.timeZoneName ?? 'GMT'
  const offset =
    timeZoneName === 'GMT' || timeZoneName === 'GMT+0' || timeZoneName === 'GMT-0'
      ? 'UTC'
      : timeZoneName.replace('GMT', 'UTC')
  // The parts already are the wall clock in tz. Rebuilding a Date from them
  // would read the clock back in the browser's own zone and shift it.
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second} ${offset}`
}

/**
 * Width of a table column that shows one formatTimestamp value on one line:
 * `2026-11-20 21:32:59 UTC+10` at text-sm plus the cell padding.
 */
export const TIMESTAMP_COLUMN_WIDTH = '14.5rem'

/** formatTimestamp without the date when `iso` falls on the same day as `now` in tz. */
export function formatRecentTimestamp(
  iso: string,
  now: Date = new Date(),
  tz: string = displayTimeZone(),
  locale = 'en-US',
): string {
  const full = formatTimestamp(iso, tz, locale)
  if (full === UNKNOWN) return full
  return full.slice(0, 10) === formatTimestamp(now.toISOString(), tz, locale).slice(0, 10)
    ? full.slice(11)
    : full
}
