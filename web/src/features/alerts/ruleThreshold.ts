import type { AlertRule } from '@/api/alerts'

function number(value: number): string {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 4 }).format(value)
}

function percent(ratio: number): string {
  return `${number(Math.round(ratio * 10000) / 100)}%`
}

type Formatter = (threshold: number) => string

const seconds: Formatter = (threshold) => `above ${number(threshold)} s`
const anyWhenZero =
  (any: string, other: Formatter): Formatter =>
  (threshold) =>
    threshold === 0 ? any : other(threshold)

/**
 * The threshold of each rule with its real unit, taken from the rule
 * definitions in internal/alert (built-in) and the seeded rows of alert_rules.
 */
const FORMATTERS: Readonly<Record<string, Formatter>> = {
  agent_down: () => 'no report',
  check_failing: () => 'any failing check',
  agent_buffer_full: () => 'any dropped sample',
  cardinality_budget_exceeded: () => 'any dropped series',
  clock_skew: seconds,
  'replica.lag_high': seconds,
  'replica.all_standbys_lagging': seconds,
  'replica.no_sync_standby': (threshold) => `below ${number(threshold)} standbys`,
  'replica.redundancy_reduced': (threshold) => `more than ${number(threshold)} missing standbys`,
  'conn.near_max': (threshold) => `above ${percent(threshold)} of max_connections`,
  'conn.idle_in_transaction': seconds,
  'txn.long_running': seconds,
  'txn.wraparound_risk': (threshold) => `above ${number(threshold)} transactions`,
  'db.deadlock_rate': anyWhenZero(
    'any deadlock',
    (threshold) => `above ${number(threshold)} deadlocks per second`,
  ),
  'archive.failing': anyWhenZero('last archive attempt failed', (threshold) => number(threshold)),
  'disk.free_low': (threshold) => `below ${percent(threshold)} free`,
}

const EVENT_RULES = new Set([
  'instance_unreachable',
  'no_primary_in_cluster',
  'failover_detected',
  'split_brain_detected',
  'slot_inactive',
  'slot_lost',
])

/** The threshold of a rule in words and units; a rule raised by an event has none. */
export function thresholdText(rule: AlertRule): string {
  if (EVENT_RULES.has(rule.id)) return 'on the event'
  if (rule.threshold === undefined) return '—'
  const format = FORMATTERS[rule.id]
  return format === undefined ? number(rule.threshold) : format(rule.threshold)
}
