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

/**
 * How many trailing buckets latestValue looks back through for a measured one.
 * The newest bucket of a range that ends now is still filling: the agent pushes
 * every 15 seconds and scrapes database statistics every 30, so at a short query
 * step it is often empty, and the bucket before it can be too while a push is in
 * flight. Three buckets cover that; a longer run of empty buckets is a real gap
 * (an agent that stopped pushing), and a tile must then read as unknown rather
 * than show a value from before the outage.
 */
export const LATEST_VALUE_BUCKETS = 3

/**
 * The floor of the age bound of latestValue. The bound is twice the series step,
 * the two buckets before the newest that LATEST_VALUE_BUCKETS lets it read, but
 * never less than two minutes: at a step under a minute a database statistic
 * reaches the server up to 45 seconds (one scrape and one push) after its bucket,
 * so two steps alone would turn a healthy agent's tile to unknown.
 */
export const LATEST_VALUE_MIN_AGE_MS = 2 * 60_000

/**
 * The latest measured value of a series: the last non-null point among its
 * final LATEST_VALUE_BUCKETS points whose bucket starts at most
 * max(2 × step, LATEST_VALUE_MIN_AGE_MS) before the newest bucket, or null when
 * there is none. The server returns every bucket up to the end of the range,
 * so the newest bucket is the range end; the step is the spacing of the last
 * two buckets. The time bound keeps a series with uneven or missing buckets
 * from showing a value older than the bucket count implies; a point whose
 * timestamp does not parse is not used. latestValue used to read the last
 * point only, so the Overview tiles showed "—" whenever the in-progress bucket
 * had no sample yet, on about one load in three.
 */
export function latestValue(series: readonly MetricSeriesPoint[]): number | null {
  const last = series[series.length - 1]
  if (last === undefined) return null
  const end = Date.parse(last.ts)
  const previous = series[series.length - 2]
  const step = previous === undefined ? 0 : end - Date.parse(previous.ts)
  const maxAge = Math.max(2 * (Number.isFinite(step) ? step : 0), LATEST_VALUE_MIN_AGE_MS)
  for (
    let index = series.length - 1;
    index >= Math.max(0, series.length - LATEST_VALUE_BUCKETS);
    index -= 1
  ) {
    const point = series[index]
    if (point === undefined || point.value === null) continue
    const age = end - Date.parse(point.ts)
    if (Number.isFinite(age) && age >= 0 && age <= maxAge) return point.value
  }
  return null
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
