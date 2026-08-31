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

/** Convert a monotonically increasing counter into a per-second series. */
export function counterRateSeries(series: readonly MetricSeriesPoint[]): MetricSeriesPoint[] {
  return series.map((point, index) => {
    const previous = series[index - 1]
    if (!previous || point.value === null || previous.value === null) {
      return { ...point, value: null }
    }

    const elapsedSeconds = (Date.parse(point.ts) - Date.parse(previous.ts)) / 1_000
    const delta = point.value - previous.value
    if (!Number.isFinite(elapsedSeconds) || elapsedSeconds <= 0 || delta < 0) {
      return { ...point, value: null }
    }

    return { ...point, value: delta / elapsedSeconds }
  })
}

/** Sum positive counter deltas, restarting after an API reset bucket. */
export function rangeDelta(series: readonly MetricSeriesPoint[]): number | null {
  let previous: number | null = null
  let total = 0
  let hasDelta = false

  for (const point of series) {
    if (point.value === null) {
      previous = null
      continue
    }

    if (previous !== null) {
      const delta = point.value - previous
      if (delta >= 0) {
        total += delta
        hasDelta = true
      }
    }
    previous = point.value
  }

  return hasDelta ? total : null
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
