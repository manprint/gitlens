import { format } from 'date-fns'

import { dateFnsLocale, UNKNOWN } from './shared'

export function formatTimestamp(iso: string, tz: string, locale = 'en-US'): string {
  const date = new Date(iso)
  if (!Number.isFinite(date.getTime())) return UNKNOWN

  const parts = new Intl.DateTimeFormat(locale, {
    timeZone: tz,
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

  const wallClock = new Date(
    Date.UTC(
      Number(parts.year),
      Number(parts.month) - 1,
      Number(parts.day),
      Number(parts.hour),
      Number(parts.minute),
      Number(parts.second),
    ),
  )
  const timeZoneName = parts.timeZoneName ?? 'GMT'
  const offset =
    timeZoneName === 'GMT' || timeZoneName === 'GMT+0' || timeZoneName === 'GMT-0'
      ? 'UTC'
      : timeZoneName.replace('GMT', 'UTC')
  return `${format(wallClock, 'yyyy-MM-dd HH:mm:ss', { locale: dateFnsLocale(locale) })} ${offset}`
}
