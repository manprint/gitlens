import { localizedNumber, UNKNOWN } from './shared'

const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB'] as const

export function formatBytes(n: number | null, locale = 'en-US'): string {
  if (n === null || !Number.isFinite(n)) return UNKNOWN
  if (n === 0) return '0 B'

  const absolute = Math.abs(n)
  const unitIndex = Math.min(Math.floor(Math.log(absolute) / Math.log(1024)), UNITS.length - 1)
  const value = n / 1024 ** unitIndex
  const number = localizedNumber(value, locale, {
    minimumFractionDigits: unitIndex === 0 ? 0 : 1,
    maximumFractionDigits: unitIndex === 0 ? 0 : 1,
    useGrouping: false,
  })
  return `${number} ${UNITS[unitIndex]}`
}
