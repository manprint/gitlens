import { localizedNumber, UNKNOWN } from './shared'

const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB'] as const

// The value as it is printed: whole units for bytes, one decimal place above.
function printedValue(value: number, unitIndex: number): number {
  const scale = unitIndex === 0 ? 1 : 10
  return Math.round(value * scale) / scale
}

export function formatBytes(n: number | null, locale = 'en-US'): string {
  if (n === null || !Number.isFinite(n)) return UNKNOWN
  if (n === 0) return '0 B'

  const absolute = Math.abs(n)
  // A value under one byte has a negative logarithm, so it stays in bytes.
  let unitIndex = Math.min(
    Math.max(Math.floor(Math.log(absolute) / Math.log(1024)), 0),
    UNITS.length - 1,
  )
  // Rounding can carry into the next unit (1023.9 B prints as 1.0 KiB), so the
  // unit is checked after rounding, not before.
  if (
    unitIndex < UNITS.length - 1 &&
    printedValue(absolute / 1024 ** unitIndex, unitIndex) >= 1024
  ) {
    unitIndex += 1
  }

  const value = n / 1024 ** unitIndex
  // Whole bytes would print a fraction of a byte as 0 or 1, so keep its
  // significant digits instead.
  const number =
    absolute < 1
      ? localizedNumber(value, locale, { maximumSignificantDigits: 2, useGrouping: false })
      : localizedNumber(value, locale, {
          minimumFractionDigits: unitIndex === 0 ? 0 : 1,
          maximumFractionDigits: unitIndex === 0 ? 0 : 1,
          useGrouping: false,
        })
  return `${number} ${UNITS[unitIndex]}`
}
