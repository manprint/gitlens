import { localizedNumber } from './shared'

export function formatCount(n: number, locale = 'en-US'): string {
  return localizedNumber(n, locale, {
    maximumFractionDigits: 0,
    useGrouping: true,
  })
}

/**
 * Formats an integer carried as a decimal string, such as a bigint sequence
 * value, which a double cannot hold exactly. A string that is not an integer
 * is returned as it is.
 */
export function formatIntegerString(value: string, locale = 'en-US'): string {
  if (value.trim() === '') return value
  let parsed: bigint
  try {
    parsed = BigInt(value)
  } catch {
    return value
  }
  return new Intl.NumberFormat(locale, { useGrouping: true }).format(parsed)
}
