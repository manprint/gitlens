import { formatAxisTime } from '@/lib/chartTime'

// Builders are pure: the same span returns the same formatter objects, so two
// options built from the same input compare equal. A small cache bounds memory.
const CACHE_SIZE = 32
const cache = new Map<
  number,
  { axis: ReturnType<typeof buildAxis>; slider: (value: number) => string }
>()

function labelFor(spanMs: number) {
  return (value: number) => (Number.isFinite(value) ? formatAxisTime(value, spanMs) : '')
}

function buildAxis(formatter: (value: number) => string) {
  return {
    type: 'time' as const,
    splitNumber: 6,
    axisLabel: {
      formatter,
      hideOverlap: true,
      showMinLabel: false,
      showMaxLabel: false,
      margin: 12,
    },
  }
}

function entryFor(spanMs: number) {
  let entry = cache.get(spanMs)
  if (!entry) {
    const label = labelFor(spanMs)
    entry = { axis: buildAxis(label), slider: label }
    const oldest = cache.keys().next()
    if (cache.size >= CACHE_SIZE && !oldest.done) cache.delete(oldest.value)
    cache.set(spanMs, entry)
  }
  return entry
}

/**
 * Shared x axis of every time-series builder (metric, ASH, lag), so their
 * labels cannot drift: short UTC labels (<= 12 characters) that ECharts thins
 * out instead of overlapping on narrow charts.
 */
export function timeAxisOption(spanMs: number) {
  return entryFor(spanMs).axis
}

/** Slider handle labels in the same short form as the axis. */
export function sliderLabelFormatter(spanMs: number) {
  return entryFor(spanMs).slider
}

/** Last minus first parseable timestamp over all series; 0 without two points. */
export function spanOf(timestamps: Iterable<string>): number {
  let first = Infinity
  let last = -Infinity
  for (const ts of timestamps) {
    const ms = Date.parse(ts)
    if (!Number.isFinite(ms)) continue
    first = Math.min(first, ms)
    last = Math.max(last, ms)
  }
  return last >= first ? last - first : 0
}
