import { activeIntl } from '@/i18n/store'
import { localizedNumber } from './shared'

/** A number with exactly `digits` decimals, in the separators of the active language. */
export function formatDecimal(value: number, digits: number, locale = activeIntl()): string {
  return localizedNumber(value, locale, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  })
}
