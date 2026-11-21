import type { components } from '@/api/generated'
import { formatBytes, formatDecimal, formatMilliseconds, formatRate } from '@/lib/format'

export type ComparePreset = 'previous' | 'yesterday' | 'week' | 'custom'
export type CompareStatus = 'both' | 'new' | 'gone'
export type CompareMode = 'view' | 'compare'

export interface Period {
  from: Date
  to: Date
}

export const COMPARE_PRESETS: readonly ComparePreset[] = ['previous', 'yesterday', 'week', 'custom']

/** A row whose ratio reaches this got worse enough to be marked. */
export const WORSE_RATIO = 2

const MINUTE_MS = 60_000
const DAY_MS = 24 * 60 * MINUTE_MS

export function readCompareMode(value: string | null): CompareMode {
  return value === 'compare' ? 'compare' : 'view'
}

export function readComparePreset(value: string | null): ComparePreset {
  return COMPARE_PRESETS.find((preset) => preset === value) ?? 'previous'
}

function shifted(from: Date, to: Date, ms: number): Period {
  return { from: new Date(from.getTime() - ms), to: new Date(to.getTime() - ms) }
}

/**
 * Period A for period B: the stretch just before B, the same hours a day or a
 * week earlier, or the custom period (null until both of its ends are valid).
 */
export function presetPeriods(
  preset: ComparePreset,
  bFrom: Date,
  bTo: Date,
  custom?: Partial<Period>,
): Period | null {
  switch (preset) {
    case 'previous':
      return shifted(bFrom, bTo, bTo.getTime() - bFrom.getTime())
    case 'yesterday':
      return shifted(bFrom, bTo, DAY_MS)
    case 'week':
      return shifted(bFrom, bTo, 7 * DAY_MS)
    case 'custom':
      return custom?.from && custom.to && custom.from < custom.to
        ? { from: custom.from, to: custom.to }
        : null
  }
}

/** B is the page's range, its ends on whole minutes so the request key stays put. */
export function periodB(from: Date, to: Date): Period {
  const floor = (d: Date) => new Date(Math.floor(d.getTime() / MINUTE_MS) * MINUTE_MS)
  return { from: floor(from), to: floor(to) }
}

export function parseInstant(value: string | null): Date | undefined {
  if (!value) return undefined
  const ms = Date.parse(value)
  return Number.isNaN(ms) ? undefined : new Date(ms)
}

export function periodParams(a: Period, b: Period) {
  return {
    a_from: a.from.toISOString(),
    a_to: a.to.toISOString(),
    b_from: b.from.toISOString(),
    b_to: b.to.toISOString(),
  }
}

/**
 * The ratio column: `new` and `gone` for a row seen in one period only, `—`
 * without a ratio (A was 0), else B / A as `×3.0`.
 */
export function formatRatio(r: number | null | undefined, status: CompareStatus = 'both'): string {
  if (status === 'new') return 'new'
  if (status === 'gone') return 'gone'
  if (r === null || r === undefined || !Number.isFinite(r)) return '—'
  return `×${formatDecimal(r, 1)}`
}

export function isWorse(r: number | null | undefined, status: CompareStatus): boolean {
  return status === 'both' && r != null && r >= WORSE_RATIO
}

export type StatementCompareMetric = keyof components['schemas']['StatementCompareMetrics']

/** The metric each statement ranking of the list compares. */
export const STATEMENT_COMPARE_METRIC = {
  total_exec_time: 'total_exec_time_ms',
  mean_exec_time: 'mean_exec_time',
  calls: 'calls',
  rows: 'rows',
  shared_blks_read: 'shared_blks_read',
  temp_blks_written: 'temp_blks_written',
  wal_bytes: 'wal_bytes',
} as const satisfies Record<string, StatementCompareMetric>

/** A compared value: per second, except the mean time of a call. */
export function formatCompareValue(
  metric: StatementCompareMetric,
  value: number | null | undefined,
): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—'
  switch (metric) {
    case 'total_exec_time_ms':
      return `${formatMilliseconds(value)}/s`
    case 'mean_exec_time':
      return formatMilliseconds(value)
    case 'wal_bytes':
      return `${formatBytes(value)}/s`
    case 'calls':
    case 'rows':
    case 'shared_blks_hit':
    case 'shared_blks_read':
    case 'temp_blks_written':
      return formatRate(value)
  }
}

/** A change B − A with its sign; the size is formatted as the values are. */
export function formatChange(
  value: number | null | undefined,
  format: (size: number) => string,
): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—'
  const sign = value > 0 ? '+' : value < 0 ? '−' : ''
  return `${sign}${format(Math.abs(value))}`
}
