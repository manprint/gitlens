import type { Schemas } from '@/api/types'

type Finding = Schemas['Finding']
type RelationItem = Record<string, unknown>

/** The advisor rules that speak about a table's dead rows, vacuum and analyze state. */
export const DEAD_ROW_RULES = [
  'table.dead_tuples_high',
  'table.never_autovacuumed',
  'table.autoanalyze_stale',
  'table.wraparound_risk',
] as const

/**
 * The dead-tuple count below which a table is not worth singling out: the
 * advisor's table.dead_tuples_high needs at least this many. Without it the
 * highest ratio is a three-row catalog table at 300%.
 */
export const WORST_FLOOR = 10_000

/** PostgreSQL's own defaults, used when the instance's settings are not known yet. */
export const DEFAULT_AUTOVACUUM = { threshold: 50, scaleFactor: 0.2 } as const

export interface AutovacuumSettings {
  /** autovacuum_vacuum_threshold: dead tuples added to the scaled part. */
  threshold: number
  /** autovacuum_vacuum_scale_factor: the share of live tuples. */
  scaleFactor: number
  /** `instance` when both values were read from the server, `default` when assumed. */
  source: 'instance' | 'default'
}

function positive(value: string | null | undefined): number | null {
  if (value === undefined || value === null) return null
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null
}

/**
 * Reads the autovacuum trigger settings out of the instance's settings. A
 * table's own reloptions can override them and are not collected, so the
 * trigger is an approximation either way.
 */
export function readAutovacuumSettings(
  settings: readonly { name: string; value: string | null }[],
): AutovacuumSettings {
  const value = (name: string) => settings.find((setting) => setting.name === name)?.value
  const threshold = positive(value('autovacuum_vacuum_threshold'))
  const scaleFactor = positive(value('autovacuum_vacuum_scale_factor'))
  return {
    threshold: threshold ?? DEFAULT_AUTOVACUUM.threshold,
    scaleFactor: scaleFactor ?? DEFAULT_AUTOVACUUM.scaleFactor,
    source: threshold !== null && scaleFactor !== null ? 'instance' : 'default',
  }
}

/** Dead tuples at which autovacuum starts on a table with this many live tuples. */
export function autovacuumTrigger(live: number, settings: AutovacuumSettings): number {
  return settings.threshold + settings.scaleFactor * Math.max(live, 0)
}

export interface DeadRow {
  /** `database.schema.table`: unique across the server. */
  key: string
  datname: string
  schema: string
  table: string
  dead: number
  live: number
  /** Dead over live tuples; null when the table has no live tuple to compare with. */
  deadRatio: number | null
  /** Table, indexes and TOAST. */
  bytes: number | null
  /** The heap alone. */
  tableBytes: number | null
  /** An estimate of the heap space the dead tuples hold: the heap times their share of all tuples. */
  deadBytes: number | null
  lastVacuumAt: string | null
  lastVacuumKind: 'manual' | 'auto' | null
  lastAnalyzeAt: string | null
  modSinceAnalyze: number | null
  /** Heap-only updates over updates. Null when the table was not updated in the interval. */
  hotRatio: number | null
  /** Inserts, updates and deletes per second. */
  writeRate: number | null
  updateRate: number | null
  /** Autovacuum runs per day, from the per-second rate. */
  autovacuumPerDay: number | null
  seqScanRate: number | null
  frozenAge: number | null
  /** Dead tuples at which autovacuum is expected to start on this table. */
  trigger: number
  /** Dead tuples over that trigger: 1 or more means autovacuum is due. */
  triggerShare: number
  neverVacuumed: boolean
  /** Open advisor findings on this table. They are the verdicts; nothing here recomputes them. */
  findings: Finding[]
}

function text(item: RelationItem, key: string): string {
  const value = item[key]
  return typeof value === 'string' ? value : ''
}

function number(item: RelationItem, key: string): number | null {
  const value = item[key]
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function laterOf(a: string, b: string): { at: string; kind: 'manual' | 'auto' } | null {
  const left = a === '' ? Number.NaN : Date.parse(a)
  const right = b === '' ? Number.NaN : Date.parse(b)
  if (Number.isNaN(left) && Number.isNaN(right)) return null
  if (Number.isNaN(right) || left >= right) return { at: a, kind: 'manual' }
  return { at: b, kind: 'auto' }
}

function deadFindings(findings: readonly Finding[]): Map<string, Finding[]> {
  const byTable = new Map<string, Finding[]>()
  for (const finding of findings) {
    if (!(DEAD_ROW_RULES as readonly string[]).includes(finding.rule_id)) continue
    if (finding.state !== 'open') continue
    const key = `${finding.datname ?? ''}.${finding.object_name ?? ''}`
    byTable.set(key, [...(byTable.get(key) ?? []), finding])
  }
  return byTable
}

export function buildDeadRows(
  items: readonly RelationItem[],
  findings: readonly Finding[],
  settings: AutovacuumSettings,
): DeadRow[] {
  const verdicts = deadFindings(findings)
  return items.map((item) => {
    const datname = text(item, 'datname')
    const schema = text(item, 'schemaname')
    const table = text(item, 'relname')
    const dead = number(item, 'n_dead_tup') ?? 0
    const live = number(item, 'n_live_tup') ?? 0
    const tableBytes = number(item, 'table_bytes')
    const vacuum = laterOf(text(item, 'last_vacuum_at'), text(item, 'last_autovacuum_at'))
    const inserts = number(item, 'n_tup_ins_rate')
    const updates = number(item, 'n_tup_upd_rate')
    const deletes = number(item, 'n_tup_del_rate')
    const writes =
      inserts === null && updates === null && deletes === null
        ? null
        : (inserts ?? 0) + (updates ?? 0) + (deletes ?? 0)
    const autovacuums = number(item, 'autovacuum_rate')
    const trigger = autovacuumTrigger(live, settings)
    const total = dead + live
    return {
      key: `${datname}.${schema}.${table}`,
      datname,
      schema,
      table,
      dead,
      live,
      deadRatio: number(item, 'dead_ratio') ?? (live > 0 ? dead / live : null),
      bytes: number(item, 'total_bytes'),
      tableBytes,
      deadBytes: tableBytes === null || total <= 0 ? null : tableBytes * (dead / total),
      lastVacuumAt: vacuum?.at ?? null,
      lastVacuumKind: vacuum?.kind ?? null,
      lastAnalyzeAt:
        laterOf(text(item, 'last_analyze_at'), text(item, 'last_autoanalyze_at'))?.at ?? null,
      modSinceAnalyze: number(item, 'n_mod_since_analyze'),
      hotRatio: number(item, 'hot_update_ratio'),
      writeRate: writes,
      updateRate: updates,
      autovacuumPerDay: autovacuums === null ? null : autovacuums * 86_400,
      seqScanRate: number(item, 'seq_scan_rate'),
      frozenAge: number(item, 'relfrozenxid_age'),
      trigger,
      triggerShare: trigger > 0 ? dead / trigger : 0,
      neverVacuumed: vacuum === null && live > 0,
      findings: verdicts.get(`${datname}.${schema}.${table}`) ?? [],
    }
  })
}

export interface DeadRowsSummary {
  tables: number
  /** Tables with at least one dead tuple. */
  withDead: number
  totalDead: number
  /** An estimate: the sum of the rows' dead bytes, over the rows that have one. */
  deadBytes: number
  /** The highest dead-to-live ratio among the tables with at least WORST_FLOOR dead tuples. */
  worst: DeadRow | null
  /** Tables whose dead tuples are past the autovacuum trigger. */
  overTrigger: number
  neverVacuumed: number
}

export function summariseDeadRows(rows: readonly DeadRow[]): DeadRowsSummary {
  const summary: DeadRowsSummary = {
    tables: rows.length,
    withDead: 0,
    totalDead: 0,
    deadBytes: 0,
    worst: null,
    overTrigger: 0,
    neverVacuumed: 0,
  }
  for (const row of rows) {
    if (row.dead > 0) summary.withDead += 1
    summary.totalDead += row.dead
    summary.deadBytes += row.deadBytes ?? 0
    if (row.triggerShare >= 1) summary.overTrigger += 1
    if (row.neverVacuumed) summary.neverVacuumed += 1
    if (
      row.deadRatio !== null &&
      row.dead >= WORST_FLOOR &&
      (summary.worst?.deadRatio ?? -1) < row.deadRatio
    ) {
      summary.worst = row
    }
  }
  return summary
}

export type DeadSortKey =
  'table' | 'dead' | 'ratio' | 'trigger' | 'size' | 'vacuum' | 'hot' | 'writes'

function sortValue(row: DeadRow, key: DeadSortKey): string | number | null {
  switch (key) {
    case 'table':
      return `${row.schema}.${row.table}`.toLowerCase()
    case 'dead':
      return row.dead
    case 'ratio':
      return row.deadRatio
    case 'trigger':
      return row.triggerShare
    case 'size':
      return row.bytes
    case 'vacuum':
      return row.lastVacuumAt === null ? null : Date.parse(row.lastVacuumAt)
    case 'hot':
      return row.hotRatio
    case 'writes':
      return row.writeRate
  }
}

/** Orders the rows; a missing value sorts last in either direction. */
export function compareDeadRows(
  a: DeadRow,
  b: DeadRow,
  key: DeadSortKey,
  descending: boolean,
): number {
  const left = sortValue(a, key)
  const right = sortValue(b, key)
  if (left === null && right === null) return a.key.localeCompare(b.key)
  if (left === null) return 1
  if (right === null) return -1
  const order = left < right ? -1 : left > right ? 1 : 0
  return (descending ? -order : order) || a.key.localeCompare(b.key)
}

/** Matches the table name, schema and database, case-insensitively. */
export function matchesDeadSearch(row: DeadRow, needle: string): boolean {
  const query = needle.trim().toLowerCase()
  if (query === '') return true
  return [row.table, row.schema, row.datname].some((part) => part.toLowerCase().includes(query))
}

export type DeadFilter = 'all' | 'dead' | 'due' | 'never' | 'flagged'

export function matchesDeadFilter(row: DeadRow, filter: DeadFilter): boolean {
  switch (filter) {
    case 'dead':
      return row.dead > 0
    case 'due':
      return row.triggerShare >= 1
    case 'never':
      return row.neverVacuumed
    case 'flagged':
      return row.findings.length > 0
    case 'all':
      return true
  }
}

export interface TrendPoint {
  ts: string
  n_dead_tup: number | null
}

/** The dead-tuple values of a series, in order, without the buckets that have no reading. */
export function trendValues(points: readonly TrendPoint[]): number[] {
  return points.flatMap((point) => (point.n_dead_tup === null ? [] : [point.n_dead_tup]))
}

/**
 * How the dead tuples moved over the window: the last value against the first.
 * Fewer than two readings, or a first value of zero with nothing after it, is
 * flat; growth from zero is reported as growing.
 */
export function trendDirection(values: readonly number[]): 'growing' | 'shrinking' | 'flat' {
  const first = values[0]
  const last = values.at(-1)
  if (first === undefined || last === undefined || values.length < 2) return 'flat'
  const change = last - first
  const tolerance = Math.max(first, last, 1) * 0.05
  if (change > tolerance) return 'growing'
  if (change < -tolerance) return 'shrinking'
  return 'flat'
}
