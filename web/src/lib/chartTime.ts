import type { MetricSeriesPoint } from './metrics'
import { findResets } from './metrics'

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const SHORT_SPAN_MS = 36 * 60 * 60 * 1_000

const pad = (value: number) => String(value).padStart(2, '0')
const clock = (date: Date) => `${pad(date.getUTCHours())}:${pad(date.getUTCMinutes())}`
const day = (date: Date) => `${MONTHS[date.getUTCMonth()] ?? ''} ${date.getUTCDate()}`
const sameUTCDay = (a: Date, b: Date) =>
  a.getUTCFullYear() === b.getUTCFullYear() &&
  a.getUTCMonth() === b.getUTCMonth() &&
  a.getUTCDate() === b.getUTCDate()

/** Time-axis label in UTC: `HH:mm` up to a 36 h span, `MMM d HH:mm` beyond. */
export function formatAxisTime(ms: number, spanMs: number): string {
  const date = new Date(ms)
  return spanMs <= SHORT_SPAN_MS ? clock(date) : `${day(date)} ${clock(date)}`
}

/** Tooltip timestamp: `YYYY-MM-DD HH:mm:ss UTC`. */
export function formatTooltipTime(ms: number): string {
  const date = new Date(ms)
  return `${date.toISOString().slice(0, 10)} ${clock(date)}:${pad(date.getUTCSeconds())} UTC`
}

/** Step as the shortest exact unit: `15s`, `1m`, `5m`, `1h`, `1d`. */
export function formatStepLabel(seconds: number): string {
  if (seconds > 0 && seconds % 86_400 === 0) return `${seconds / 86_400}d`
  if (seconds > 0 && seconds % 3_600 === 0) return `${seconds / 3_600}h`
  if (seconds > 0 && seconds % 60 === 0) return `${seconds / 60}m`
  return `${seconds}s`
}

/**
 * `Oct 29 09:41 → 10:41 UTC · step 1m`; the end repeats the date when it is another day.
 * An unknown step (zero or less) drops the step part.
 */
export function formatRangeLabel(from: Date, to: Date, stepSeconds: number): string {
  const end = sameUTCDay(from, to) ? clock(to) : `${day(to)} ${clock(to)}`
  const step = stepSeconds > 0 ? ` · step ${formatStepLabel(stepSeconds)}` : ''
  return `${day(from)} ${clock(from)} → ${end} UTC${step}`
}

/** Null buckets with measured values on both sides (counter resets or gaps). */
export function countResets(series: readonly (MetricSeriesPoint | number | null)[]): number {
  return findResets(series).length
}
