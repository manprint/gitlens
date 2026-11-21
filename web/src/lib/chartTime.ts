import { activeIntl, translate } from '@/i18n/store'
import type { MetricSeriesPoint } from './metrics'
import { findResets } from './metrics'
import { displayTimeZone, wallClock, zoneLabel, type WallClock } from './timezone'

const SHORT_SPAN_MS = 36 * 60 * 60 * 1_000

const pad = (value: number) => String(value).padStart(2, '0')
const clock = (w: WallClock) => `${pad(w.hour)}:${pad(w.minute)}`
const dayFormats = new Map<string, Intl.DateTimeFormat>()
/** Short month and day in the console language: `Nov 21` in English, `21 nov` in Italian. */
const day = (w: WallClock) => {
  const locale = activeIntl()
  let format = dayFormats.get(locale)
  if (format === undefined) {
    format = new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', timeZone: 'UTC' })
    dayFormats.set(locale, format)
  }
  return format.format(Date.UTC(w.year, w.month - 1, w.day))
}
const sameDay = (a: WallClock, b: WallClock) =>
  a.year === b.year && a.month === b.month && a.day === b.day

/** Time-axis label in `zone`: `HH:mm` up to a 36 h span, month and day then `HH:mm` beyond. */
export function formatAxisTime(ms: number, spanMs: number, zone = displayTimeZone()): string {
  const w = wallClock(ms, zone)
  return spanMs <= SHORT_SPAN_MS ? clock(w) : `${day(w)} ${clock(w)}`
}

/** Tooltip timestamp: `YYYY-MM-DD HH:mm:ss UTC+1`. */
export function formatTooltipTime(ms: number, zone = displayTimeZone()): string {
  const w = wallClock(ms, zone)
  return `${w.year}-${pad(w.month)}-${pad(w.day)} ${clock(w)}:${pad(w.second)} ${zoneLabel(zone, ms)}`
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
export function formatRangeLabel(
  from: Date,
  to: Date,
  stepSeconds: number,
  zone = displayTimeZone(),
): string {
  const start = wallClock(from.getTime(), zone)
  const stop = wallClock(to.getTime(), zone)
  const end = sameDay(start, stop) ? clock(stop) : `${day(stop)} ${clock(stop)}`
  const step =
    stepSeconds > 0
      ? ` · ${translate('shell.chart.step', { step: formatStepLabel(stepSeconds) })}`
      : ''
  return `${day(start)} ${clock(start)} → ${end} ${zoneLabel(zone, to.getTime())}${step}`
}

/**
 * Runs of consecutive interior null buckets: one outage spanning 49 buckets
 * is one gap, not 49 counter resets.
 */
export function countGaps(series: readonly (MetricSeriesPoint | number | null)[]): number {
  return findResets(series).filter((index, position, all) => all[position - 1] !== index - 1).length
}
