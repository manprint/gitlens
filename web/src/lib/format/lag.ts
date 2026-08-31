import { localizedNumber, UNKNOWN } from './shared'

export function formatLag(seconds: number | null, locale = 'en-US'): string {
  if (seconds === null || !Number.isFinite(seconds)) return UNKNOWN
  const absolute = Math.abs(seconds)
  const number = localizedNumber(seconds, locale, {
    minimumFractionDigits: absolute < 1 ? 2 : 1,
    maximumFractionDigits: 2,
    useGrouping: false,
  })
  return `${number} s`
}
