export interface ActivityMetricLike {
  ts?: string | null
  value?: number | null
  labels?: Readonly<Record<string, string>> | null
}

export type ActivityMetrics = Readonly<Record<string, readonly ActivityMetricLike[] | undefined>>

export const CONNECTION_SATURATION_THRESHOLD = 0.8
export const WRAPAROUND_RISK_THRESHOLD = 1_000_000_000

function finiteValue(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function timestampValue(value: unknown): number | null {
  if (typeof value !== 'string') return null
  const timestamp = new Date(value).getTime()
  return Number.isFinite(timestamp) ? timestamp : null
}

function seriesFor(metrics: ActivityMetrics, name: string): readonly ActivityMetricLike[] {
  return metrics[name] ?? []
}

export function latestMetric(
  metrics: readonly ActivityMetricLike[] | undefined,
): ActivityMetricLike | null {
  if (!metrics || metrics.length === 0) return null
  return (
    [...metrics].sort(
      (left, right) => (timestampValue(right.ts) ?? 0) - (timestampValue(left.ts) ?? 0),
    )[0] ?? null
  )
}

export function latestValue(metrics: ActivityMetrics, name: string): number | null {
  return finiteValue(latestMetric(seriesFor(metrics, name))?.value)
}

export function latestSampleAt(metrics: ActivityMetrics): string | null {
  const latest = Object.values(metrics)
    .flatMap((series) => series ?? [])
    .map((metric) => ({ metric, timestamp: timestampValue(metric.ts) }))
    .filter(
      (entry): entry is { metric: ActivityMetricLike; timestamp: number } =>
        entry.timestamp !== null,
    )
    .sort((left, right) => right.timestamp - left.timestamp)[0]
  return latest?.metric.ts ?? null
}

function labelKey(labels: Readonly<Record<string, string>> | null | undefined): string {
  if (!labels) return ''
  return Object.entries(labels)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([key, value]) => `${key}=${value}`)
    .join('&')
}

export interface ActivityBreakdown {
  label: string
  value: number
}

export function breakdown(
  metrics: ActivityMetrics,
  name: string,
  labelName: string,
): ActivityBreakdown[] {
  const seriesByLabels = new Map<string, ActivityMetricLike[]>()
  for (const metric of seriesFor(metrics, name)) {
    const key = labelKey(metric.labels)
    const series = seriesByLabels.get(key) ?? []
    series.push(metric)
    seriesByLabels.set(key, series)
  }

  const grouped = new Map<string, number>()
  for (const series of seriesByLabels.values()) {
    const metric = latestMetric(series)
    const label = metric?.labels?.[labelName] ?? 'Unknown'
    const value = finiteValue(metric?.value)
    if (value !== null) grouped.set(label, (grouped.get(label) ?? 0) + value)
  }

  return [...grouped.entries()]
    .map(([label, value]) => ({ label, value }))
    .sort((left, right) => right.value - left.value || left.label.localeCompare(right.label))
}

export function oldestStateAge(metrics: ActivityMetrics): number | null {
  const ages = breakdown(metrics, 'pg_max_state_age_seconds', 'state').map((row) => row.value)
  return ages.length > 0 ? Math.max(...ages) : null
}

export interface ConnectionSaturation {
  ratio: number | null
  threshold: number
  saturated: boolean
}

export function connectionSaturation(metrics: ActivityMetrics): ConnectionSaturation {
  const ratio = latestValue(metrics, 'pg_connections_used_ratio')
  return {
    ratio,
    threshold: CONNECTION_SATURATION_THRESHOLD,
    saturated: ratio !== null && ratio > CONNECTION_SATURATION_THRESHOLD,
  }
}

export function counterRate(series: readonly ActivityMetricLike[]): number | null {
  const samples = series
    .map((metric) => ({ timestamp: timestampValue(metric.ts), value: finiteValue(metric.value) }))
    .filter(
      (sample): sample is { timestamp: number; value: number } =>
        sample.timestamp !== null && sample.value !== null,
    )
    .sort((left, right) => left.timestamp - right.timestamp)
  if (samples.length < 2) return null

  let delta = 0
  let elapsed = 0
  for (let index = 1; index < samples.length; index += 1) {
    const previous = samples[index - 1]!
    const current = samples[index]!
    const seconds = (current.timestamp - previous.timestamp) / 1000
    if (seconds <= 0) continue
    delta += current.value >= previous.value ? current.value - previous.value : current.value
    elapsed += seconds
  }
  return elapsed > 0 ? delta / elapsed : null
}

export function deadlockRate(metrics: ActivityMetrics): number | null {
  const series = seriesFor(metrics, 'pg_deadlocks_total')
  if (series.length === 0) return null

  const groups = new Map<string, ActivityMetricLike[]>()
  for (const metric of series) {
    const key = labelKey(metric.labels)
    const group = groups.get(key) ?? []
    group.push(metric)
    groups.set(key, group)
  }

  const rates = [...groups.values()]
    .map(counterRate)
    .filter((rate): rate is number => rate !== null)
  return rates.length > 0 ? rates.reduce((total, rate) => total + rate, 0) : null
}

export function hasReported(metrics: ActivityMetrics, name: string): boolean {
  return seriesFor(metrics, name).length > 0
}
