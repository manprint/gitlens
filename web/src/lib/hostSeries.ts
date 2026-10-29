import { scaleSeries, type MetricSeriesPoint } from './metrics'

/**
 * `total − part` per bucket, for the used memory (total − available) and the
 * used filesystem (total − free). Each query resolves "now" when it is sent,
 * and the server buckets from the window start, so two series of one range
 * can be offset by a few milliseconds: a bucket of `total` pairs with the
 * nearest bucket of `part` within `toleranceMs` (half a step). A bucket
 * without a partner is dropped; a gap in either series is a gap in the result.
 */
export function subtractSeries(
  total: readonly MetricSeriesPoint[],
  part: readonly MetricSeriesPoint[],
  toleranceMs = 0,
): MetricSeriesPoint[] {
  const parts = part
    .map((point) => ({ ms: Date.parse(point.ts), value: point.value }))
    .filter((point) => Number.isFinite(point.ms))
    .sort((left, right) => left.ms - right.ms)
  return total.flatMap((point) => {
    const other = nearest(parts, Date.parse(point.ts), toleranceMs)
    if (other === undefined) return []
    return [
      {
        ts: point.ts,
        value: point.value === null || other.value === null ? null : point.value - other.value,
      },
    ]
  })
}

/** The point of `sorted` closest to `ms`, if it lies within `toleranceMs`. */
function nearest<T extends { ms: number }>(
  sorted: readonly T[],
  ms: number,
  toleranceMs: number,
): T | undefined {
  if (!Number.isFinite(ms)) return undefined
  let low = 0
  let high = sorted.length
  while (low < high) {
    const middle = (low + high) >> 1
    if ((sorted[middle]?.ms ?? Infinity) < ms) low = middle + 1
    else high = middle
  }
  const candidates = [sorted[low - 1], sorted[low]].filter(
    (candidate): candidate is T =>
      candidate !== undefined && Math.abs(candidate.ms - ms) <= toleranceMs,
  )
  // On a tie, exactly half a step either side, the earlier bucket wins.
  return candidates.sort(
    (left, right) => Math.abs(left.ms - ms) - Math.abs(right.ms - ms) || left.ms - right.ms,
  )[0]
}

/** A 0–1 ratio series as 0–100 percent, keeping gaps as gaps. */
export function toPercent(series: readonly MetricSeriesPoint[]): MetricSeriesPoint[] {
  return scaleSeries(series, 100)
}

/** Share of `total` in use given its free part; null when unmeasured or empty. */
export function usedRatio(total: number | null, free: number | null): number | null {
  if (total === null || free === null || !Number.isFinite(total) || !Number.isFinite(free)) {
    return null
  }
  return total > 0 ? (total - free) / total : null
}

/** The smallest 1, 2, 2.5 or 5 × 10ⁿ at or above `value`, so the top is a tick. */
export function niceCeil(value: number): number {
  if (!Number.isFinite(value) || value <= 0) return 1
  const magnitude = 10 ** Math.floor(Math.log10(value))
  const factor = [1, 2, 2.5, 5, 10].find((candidate) => value <= candidate * magnitude) ?? 10
  return factor * magnitude
}
