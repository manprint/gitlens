import { enUS, it } from 'date-fns/locale'

export const UNKNOWN = '—'

export function dateFnsLocale(locale: string) {
  return locale.toLowerCase().startsWith('it') ? it : enUS
}

export function localizedNumber(value: number, locale: string, options: Intl.NumberFormatOptions) {
  return new Intl.NumberFormat(locale, options).format(value)
}
