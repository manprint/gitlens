import { UNKNOWN } from './shared'

export function formatTimestamp(iso: string, tz: string, locale = 'en-US'): string {
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
