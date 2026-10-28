import { localizedNumber, UNKNOWN } from './shared'

/**
 * Formats an execution time given in milliseconds with the unit a reader
 * expects at that size: microseconds under a millisecond is too fine for
 * pg_stat_statements, so sub-millisecond values keep two decimals in ms, and
 * longer values move to seconds, minutes and hours.
 */
export function formatMilliseconds(ms: number | null | undefined, locale = 'en-US'): string {
  if (ms === null || ms === undefined || !Number.isFinite(ms)) return UNKNOWN

  const sign = ms < 0 ? '-' : ''
  const value = Math.abs(ms)

  if (value < 1) return `${sign}${localizedNumber(value, locale, { maximumFractionDigits: 3 })} ms`
  if (value < 1_000) {
    return `${sign}${localizedNumber(value, locale, { maximumFractionDigits: value < 10 ? 2 : 1 })} ms`
  }
  if (value < 60_000) {
    return `${sign}${localizedNumber(value / 1_000, locale, { maximumFractionDigits: 2 })} s`
  }
  if (value < 3_600_000) {
    return `${sign}${localizedNumber(value / 60_000, locale, { maximumFractionDigits: 1 })} min`
  }
  return `${sign}${localizedNumber(value / 3_600_000, locale, { maximumFractionDigits: 1 })} h`
}
