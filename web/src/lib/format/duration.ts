import { localizedNumber, UNKNOWN } from './shared'

export function formatDuration(seconds: number | null, locale = 'en-US'): string {
  if (seconds === null || !Number.isFinite(seconds)) return UNKNOWN
  if (seconds === 0) return '0s'

  const sign = seconds < 0 ? '-' : ''
  // Round to the printed precision before splitting, so a remainder of 59.999 s
  // carries into the minutes instead of printing as 60s after them.
  let remaining = Math.round(Math.abs(seconds) * 100) / 100
  const days = Math.floor(remaining / 86_400)
  remaining -= days * 86_400
  const hours = Math.floor(remaining / 3_600)
  remaining -= hours * 3_600
  const minutes = Math.floor(remaining / 60)
  remaining -= minutes * 60
  const parts: string[] = []

  if (days > 0) parts.push(`${localizedNumber(days, locale, {})}d`)
  if (hours > 0 || parts.length > 0) parts.push(`${localizedNumber(hours, locale, {})}h`)
  if (minutes > 0 || parts.length > 0) parts.push(`${localizedNumber(minutes, locale, {})}m`)
  if (remaining > 0 || parts.length === 0) {
    parts.push(`${localizedNumber(remaining, locale, { maximumFractionDigits: 2 })}s`)
  }

  return `${sign}${parts.join(' ')}`
}
