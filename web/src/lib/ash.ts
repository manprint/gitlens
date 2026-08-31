import type { Schemas } from '@/api/types'

export type AshBucket = Pick<
  Schemas['AshBucket'],
  'ts' | 'samples' | 'ticks' | 'avg_active_sessions'
> & {
  datname?: string | undefined
  wait_event_type?: string | undefined
  wait_event?: string | undefined
  state?: string | undefined
  /** Runtime JSON may preserve an int64 query id as text. */
  queryid?: number | string | null | undefined
}

export type AshTopEntry = Omit<Schemas['AshTopEntry'], 'queryid'> & {
  /** Keep query ids lossless when the API response exceeds JS safe integers. */
  queryid: number | string
}

export type AshGroupBy = 'wait_event_type' | 'wait_event' | 'queryid' | 'state' | 'datname'

export interface AshPoint {
  ts: string
  value: number | null
}

export interface AshSeries {
  name: string
  points: AshPoint[]
}

export interface FoldedSeriesResult {
  series: AshSeries[]
  folded: number
}

export interface AshTopQuery extends Omit<AshTopEntry, 'queryid'> {
  queryid: string
}

const GROUP_COLUMNS: Record<AshGroupBy, keyof AshBucket> = {
  datname: 'datname',
  queryid: 'queryid',
  state: 'state',
  wait_event: 'wait_event',
  wait_event_type: 'wait_event_type',
}

const OTHER = 'other'
const CPU = 'CPU'
const MAX_INFERRED_POINTS = 10_000

function groupName(bucket: AshBucket, groupBy: AshGroupBy): string {
  const value = bucket[GROUP_COLUMNS[groupBy]]

  if (value === undefined || value === null || value === '') {
    return groupBy === 'wait_event_type' || groupBy === 'wait_event' ? CPU : OTHER
  }

  return String(value)
}

function timestampMs(timestamp: string): number | null {
  const parsed = Date.parse(timestamp)
  return Number.isFinite(parsed) ? parsed : null
}

interface AxisPoint {
  key: string
  label: string
  ms: number | null
  sampled: boolean
}

function buildAxis(buckets: readonly AshBucket[]): AxisPoint[] {
  const labels = new Map<string, { label: string; ms: number | null }>()
  for (const bucket of buckets) {
    const ms = timestampMs(bucket.ts)
    const key = ms === null ? `invalid:${bucket.ts}` : String(ms)
    if (!labels.has(key)) labels.set(key, { label: bucket.ts, ms })
  }

  const observed = [...labels.values()].sort((left, right) => {
    if (left.ms !== null && right.ms !== null) return left.ms - right.ms
    if (left.ms !== null) return -1
    if (right.ms !== null) return 1
    return left.label.localeCompare(right.label)
  })

  if (observed.length < 2 || observed.some(({ ms }) => ms === null)) {
    return observed.map(({ label, ms }) => ({
      key: ms === null ? `invalid:${label}` : String(ms),
      label,
      ms,
      sampled: true,
    }))
  }

  const positiveDeltas = observed
    .slice(1)
    .map((point, index) => point.ms! - observed[index]!.ms!)
    .filter((delta) => delta > 0)
  const step = Math.min(...positiveDeltas)
  if (!Number.isFinite(step) || step <= 0) {
    return observed.map(({ label, ms }) => ({ key: String(ms), label, ms, sampled: true }))
  }

  const axis: AxisPoint[] = []
  for (let index = 0; index < observed.length; index += 1) {
    const current = observed[index]!
    axis.push({ key: String(current.ms), label: current.label, ms: current.ms, sampled: true })

    const next = observed[index + 1]
    if (!next || axis.length >= MAX_INFERRED_POINTS) continue

    for (
      let missing = current.ms! + step;
      missing < next.ms! && axis.length < MAX_INFERRED_POINTS;
      missing += step
    ) {
      axis.push({
        key: String(missing),
        label: new Date(missing).toISOString(),
        ms: missing,
        sampled: false,
      })
    }
  }

  return axis
}

function pointValue(bucket: AshBucket): number | null {
  if (bucket.samples === 0) return 0
  if (typeof bucket.avg_active_sessions === 'number') return bucket.avg_active_sessions
  return avgActiveSessions(bucket.samples, bucket.ticks)
}

/** Align ASH groups to one axis without turning an unsampled window into zero. */
export function toStackedSeries(buckets: readonly AshBucket[], groupBy: AshGroupBy): AshSeries[] {
  if (buckets.length === 0) return []

  const axis = buildAxis(buckets)
  const byGroup = new Map<string, Map<string, AshBucket>>()
  const sampledKeys = new Set<string>()

  for (const bucket of buckets) {
    const ms = timestampMs(bucket.ts)
    const key = ms === null ? `invalid:${bucket.ts}` : String(ms)
    sampledKeys.add(key)
    const name = groupName(bucket, groupBy)
    const group = byGroup.get(name) ?? new Map<string, AshBucket>()
    group.set(key, bucket)
    byGroup.set(name, group)
  }

  return [...byGroup.keys()]
    .sort((left, right) => left.localeCompare(right))
    .map((name) => {
      const group = byGroup.get(name)!
      return {
        name,
        points: axis.map(({ key, label, sampled }) => {
          const bucket = group.get(key)
          if (bucket) return { ts: label, value: pointValue(bucket) }
          return { ts: label, value: sampled && sampledKeys.has(key) ? 0 : null }
        }),
      }
    })
}

function seriesTotal(series: AshSeries): number {
  return series.points.reduce((total, point) => total + (point.value ?? 0), 0)
}

function isCPU(series: AshSeries): boolean {
  return series.name.toLowerCase() === CPU.toLowerCase()
}

function isOther(series: AshSeries): boolean {
  return series.name.toLowerCase() === OTHER
}

/** Return a stable chart order: CPU, waits by weight, and other last. */
export function orderGroups(series: readonly AshSeries[]): AshSeries[] {
  return [...series].sort((left, right) => {
    if (isCPU(left) && !isCPU(right)) return -1
    if (!isCPU(left) && isCPU(right)) return 1
    if (isOther(left) && !isOther(right)) return 1
    if (!isOther(left) && isOther(right)) return -1

    const totalDifference = seriesTotal(right) - seriesTotal(left)
    if (totalDifference !== 0) return totalDifference
    return left.name.localeCompare(right.name)
  })
}

function foldPoints(series: readonly AshSeries[]): AshPoint[] {
  const timestamps = series[0]?.points.map((point) => point.ts) ?? []
  return timestamps.map((ts, index) => {
    const values = series.map((entry) => entry.points[index]?.value ?? null)
    const hasSampledValue = values.some((value) => value !== null)
    return {
      ts,
      value: hasSampledValue
        ? values.reduce<number>((total, value) => total + (value ?? 0), 0)
        : null,
    }
  })
}

/** Fold the tail into one explicitly named `other` series. */
export function foldOther(series: readonly AshSeries[], maxSeries: number): FoldedSeriesResult {
  const ordered = orderGroups(series)
  const limit = Math.max(1, Math.floor(maxSeries))
  if (ordered.length <= limit) return { series: ordered, folded: 0 }

  const keep = Math.max(0, limit - 1)
  const overflow = ordered.slice(keep)
  return {
    folded: overflow.length,
    series: [
      ...ordered.slice(0, keep),
      {
        name: OTHER,
        points: foldPoints(overflow),
      },
    ],
  }
}

/** Sum the sample counts represented by the supplied ASH buckets. */
export function totalSamples(buckets: readonly Pick<AshBucket, 'samples'>[]): number {
  return buckets.reduce((total, bucket) => total + bucket.samples, 0)
}

/** ASH conclusions are under-sampled until at least 60 samples exist. */
export function isUnderSampled(buckets: readonly Pick<AshBucket, 'samples'>[]): boolean {
  return totalSamples(buckets) < 60
}

/** Return null for an empty sampling window instead of manufacturing a value. */
export function avgActiveSessions(samples: number, ticks: number): number | null {
  return ticks === 0 ? null : samples / ticks
}

/** Sort top queries without converting a large query id through a JS number. */
export function topQueries(entries: readonly AshTopEntry[]): AshTopQuery[] {
  return [...entries]
    .sort((left, right) => {
      const samplesDifference = right.samples - left.samples
      if (samplesDifference !== 0) return samplesDifference
      return String(left.queryid).localeCompare(String(right.queryid))
    })
    .map((entry) => ({ ...entry, queryid: String(entry.queryid) }))
}
