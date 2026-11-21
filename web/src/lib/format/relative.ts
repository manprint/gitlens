import { formatDistance } from 'date-fns'

import { activeIntl } from '@/i18n/store'

import { dateFnsLocale, UNKNOWN } from './shared'

export function formatRelative(iso: string, now: Date, locale = activeIntl()): string {
  const date = new Date(iso)
  if (!Number.isFinite(date.getTime()) || !Number.isFinite(now.getTime())) return UNKNOWN
  return formatDistance(date, now, {
    addSuffix: true,
    locale: dateFnsLocale(locale),
  })
}
