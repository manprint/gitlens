import { formatPercent } from './format'

export interface MetricSeriesPoint {
  ts: string
  value: number | null
}

type MetricValue = MetricSeriesPoint | number | null | undefined

function valueOf(point: MetricValue): number | null {
  if (point === null || point === undefined) return null
  return typeof point === 'number' ? point : point.value
}

/** Return the indexes of null buckets that have measured values on both sides. */
export function findResets(series: readonly MetricValue[]): number[] {
  const values = series.map(valueOf)
  const resets: number[] = []
  let hasValueBefore = false

  for (let index = 0; index < values.length; index += 1) {
    const value = values[index] ?? null
    if (value === null) {
      const hasValueAfter = values.slice(index + 1).some((candidate) => candidate !== null)
      if (hasValueBefore && hasValueAfter) resets.push(index)
      continue
    }
    hasValueBefore = true
  }

  return resets
}

export function latestValue(series: readonly MetricSeriesPoint[]): number | null {
  return series[series.length - 1]?.value ?? null
}

/** Multiply every measured bucket by a constant, keeping gaps as gaps. */
export function scaleSeries(
  series: readonly MetricSeriesPoint[],
  factor: number,
): MetricSeriesPoint[] {
  return series.map((point) => ({
    ts: point.ts,
    value: point.value === null ? null : point.value * factor,
  }))
}

/**
 * Integrates a per-second rate series into a total over the range.
 *
 * The server stores every counter as the per-second rate between two scrapes,
 * so a bucket's value is the rate over the time since the previous sample. It
 * is weighted by the time since the previous measured bucket, and the first
 * one by a single step. The overview used to differentiate these rates again
 * as if they were raw counters, and to subtract them for range totals: the
 * results were the change of a rate, not a rate or a total.
 */
export function integrateRate(
  series: readonly MetricSeriesPoint[],
  stepSeconds: number,
): number | null {
  let total = 0
  let measured = false
  let previous: number | null = null

  for (const point of series) {
    const timestamp = Date.parse(point.ts)
    if (point.value === null || !Number.isFinite(timestamp)) continue
    const seconds = previous === null ? stepSeconds : (timestamp - previous) / 1_000
    if (seconds > 0) {
      total += point.value * seconds
      measured = true
    }
    previous = timestamp
  }

  return measured ? total : null
}

export function cacheHitRatioValue(hits: number | null, reads: number | null): string | null {
  if (hits === null || reads === null) return null
  return formatPercent(hits, hits + reads)
}

export function cacheHitRatioSeries(
  hits: readonly MetricSeriesPoint[],
  reads: readonly MetricSeriesPoint[],
): MetricSeriesPoint[] {
  return hits.map((hit, index) => {
    const read = reads[index]
    if (!read || hit.value === null || read.value === null) {
      return { ts: hit.ts, value: null }
    }

    const denominator = hit.value + read.value
    return {
      ts: hit.ts,
      value: denominator === 0 ? null : hit.value / denominator,
    }
  })
}
