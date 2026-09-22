import { localizedNumber, UNKNOWN } from './shared'

/**
 * Formats a per-second rate. Rates below 10/s keep two decimals so a table
 * scanned every few minutes (0.02/s) does not read as never scanned.
 */
export function formatRate(perSecond: number | null, locale = 'en-US'): string {
  if (perSecond === null || !Number.isFinite(perSecond)) return UNKNOWN
  const digits = Math.abs(perSecond) < 10 ? 2 : 0
  return `${localizedNumber(perSecond, locale, { maximumFractionDigits: digits, useGrouping: true })}/s`
}
