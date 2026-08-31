import { localizedNumber } from './shared'

export function formatPercent(
  numerator: number,
  denominator: number,
  locale = 'en-US',
): string | null {
  if (denominator === 0 || !Number.isFinite(numerator) || !Number.isFinite(denominator)) {
    return null
  }
  return localizedNumber(numerator / denominator, locale, {
    style: 'percent',
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  })
}
