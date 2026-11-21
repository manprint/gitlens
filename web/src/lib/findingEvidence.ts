import { activeIntl, translate } from '@/i18n/store'

import { formatBytes, formatDuration, formatMilliseconds, formatTimestamp } from './format'

/*
 * The evidence of a finding as label and value a reader understands: units,
 * rounding, percentages and times in the console zone, not the raw JSON
 * (761.3274609999993, 0.42, a queryid rounded past 2^53).
 */

export interface EvidenceRow {
  /** The evidence key the advisor wrote, kept for the tooltip. */
  key: string
  label: string
  value: string
}

const LABELED = [
  'queryid',
  'baseline_ms',
  'observed_ms',
  'threshold_ms',
  'share',
  'bytes_per_call',
  'work_mem_bytes',
  'pg_temp_files_total',
  'read_ratio',
  'age_xids',
  'age',
  'dead_tuples',
  'rows',
  'waiting_pid',
  'root_blocker_pid',
  'wait_seconds',
  'holder_kind',
  'used_ratio',
  'idle_share',
  'failed_ratio',
  'bloat_ratio',
  'seq_scan_rate',
  'idx_scan_rate',
  'monitored_days',
  'history_days',
  'last_archived_age_seconds',
  'ready_files',
  'archiver_ready_files',
  'lag_bytes',
  'max_connections',
  'connections',
  'wal_keep_size_bytes',
  'max_wal_size_bytes',
  'wal_dir_bytes',
  'wal_dir_files',
  'table_bytes',
  'size_bytes',
  'last_value',
  'limit',
  'remaining',
  'count',
] as const
type LabeledKey = (typeof LABELED)[number]
const LABELED_KEYS: ReadonlySet<string> = new Set(LABELED)

function evidenceLabel(key: string): string {
  return LABELED_KEYS.has(key) ? translate(`findings.evidence.${key as LabeledKey}`) : humanize(key)
}

// Ratios between 0 and 1 the advisor writes; shown as percentages.
const RATIO = /(^|_)(ratio|share)$/
// Rules whose `threshold` is a ratio, not a count.
const RATIO_THRESHOLD_RULES = new Set([
  'query.total_time_share',
  'query.cache_miss_high',
  'conn.idle_share_high',
  'conn.saturation',
  'txn.high_rollback_ratio',
  'table.bloat_high',
  'index.bloat_high',
  'sequence.exhaustion_risk',
])
const ISO_TIME = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/

function humanize(key: string): string {
  const words = key
    .replace(/_(ms|bytes|seconds|per_second)$/, '')
    .replace(/_/g, ' ')
    .trim()
  return words.charAt(0).toUpperCase() + words.slice(1)
}

const integer = () => new Intl.NumberFormat(activeIntl(), { maximumFractionDigits: 0 })
const decimal = () => new Intl.NumberFormat(activeIntl(), { maximumFractionDigits: 2 })
const percent = () =>
  new Intl.NumberFormat(activeIntl(), { style: 'percent', maximumFractionDigits: 1 })

function formatNumber(key: string, value: number, ruleId: string): string {
  if (key.endsWith('_ms')) return formatMilliseconds(value)
  if (key.endsWith('_bytes') || key === 'bytes_per_call') return formatBytes(value)
  if (key.endsWith('_seconds')) return formatDuration(value)
  if (key.endsWith('_per_second') || key.endsWith('_rate')) return `${decimal().format(value)}/s`
  if (RATIO.test(key) || (key === 'threshold' && RATIO_THRESHOLD_RULES.has(ruleId))) {
    return percent().format(value)
  }
  return Number.isInteger(value) ? integer().format(value) : decimal().format(value)
}

function formatValue(key: string, value: unknown, ruleId: string): string {
  if (value === null || value === undefined || value === '') return '—'
  if (typeof value === 'number') return formatNumber(key, value, ruleId)
  if (typeof value === 'boolean') return value ? 'yes' : 'no'
  if (typeof value === 'string') {
    if (ISO_TIME.test(value) && Number.isFinite(Date.parse(value))) return formatTimestamp(value)
    const number = Number(value)
    if (
      key !== 'queryid' &&
      value.trim() !== '' &&
      Number.isFinite(number) &&
      /_(ms|bytes)$/.test(key)
    ) {
      return formatNumber(key, number, ruleId)
    }
    return value
  }
  if (Array.isArray(value)) {
    return value.every((item) => typeof item !== 'object' || item === null)
      ? value.map((item) => String(item as string | number | boolean | null | undefined)).join(', ')
      : JSON.stringify(value)
  }
  try {
    return JSON.stringify(value) ?? ''
  } catch {
    return ''
  }
}

/**
 * The evidence rows of a finding, in the advisor's key order, without the keys
 * in `omit` (the queryid is shown with the statement, exact, from its name).
 */
export function evidenceRows(
  evidence: Record<string, unknown> | undefined,
  ruleId: string,
  omit: readonly string[] = [],
): EvidenceRow[] {
  return Object.entries(evidence ?? {})
    .filter(([key]) => !omit.includes(key))
    .map(([key, value]) => ({
      key,
      label: evidenceLabel(key),
      value: formatValue(key, value, ruleId),
    }))
}
