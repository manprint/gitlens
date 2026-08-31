import { localizedNumber } from './shared'

export function formatCount(n: number, locale = 'en-US'): string {
  return localizedNumber(n, locale, {
    maximumFractionDigits: 0,
    useGrouping: true,
  })
}
