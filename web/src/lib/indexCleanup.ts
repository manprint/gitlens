import type { IndexRow } from './indexAnalysis'

type RelationItem = Record<string, unknown>

/** Why an index is on the cleanup list, strongest evidence first. */
export type CleanupReason = 'invalid' | 'duplicate' | 'redundant' | 'unused' | 'never-scanned'

const REASON_ORDER: readonly CleanupReason[] = [
  'invalid',
  'duplicate',
  'redundant',
  'unused',
  'never-scanned',
]

export interface CleanupCandidate {
  row: IndexRow
  reason: CleanupReason
  /** One sentence for the operator: what was found, not a verdict. */
  why: string
  /** The index that keeps serving the same queries, for a duplicate or a redundant prefix. */
  coveredBy: string | null
  /** Space the drop gives back. */
  reclaimBytes: number
  /** Index entries written per second on the table: what this index costs on every write. Null when the table's statistics were not reported. */
  writeRate: number | null
  /** `advisor` when the advisor reached the verdict, `statistics` when it is only the raw counters. */
  basis: 'advisor' | 'statistics'
  dropStatement: string
  /** Rebuilds the index as it was; null when the definition was not reported. */
  recreateStatement: string | null
}

/** What a set of candidates would give back. */
export interface CleanupTotals {
  count: number
  reclaimBytes: number
  /** Index entries per second the candidates cost on writes, over the candidates whose table is known. */
  writeRate: number
  /** Candidates whose table has no write statistics, so they add nothing to `writeRate`. */
  writeRateUnknown: number
}

export interface CleanupSummary {
  count: number
  /** The candidates the advisor stands behind. */
  confirmed: CleanupTotals
  /** The candidates that rest on raw counters only: kept apart so they do not inflate the totals. */
  provisional: CleanupTotals
  /** The share of the index size on the server that the confirmed candidates hold. */
  confirmedShare: number | null
  byReason: Record<CleanupReason, number>
}

export function quoteIdentifier(name: string): string {
  return `"${name.replaceAll('"', '""')}"`
}

/** The index name as `schema.name` is kept qualified; quote each part. */
export function dropStatement(row: IndexRow): string {
  return `DROP INDEX CONCURRENTLY IF EXISTS ${quoteIdentifier(row.schema)}.${quoteIdentifier(row.name)};`
}

/** The definition from pg_get_indexdef, built without blocking writes. */
export function recreateStatement(row: IndexRow): string | null {
  const definition = row.definition?.trim()
  if (!definition) return null
  const built = definition.replace(
    /^create\s+(unique\s+)?index\s+(?!concurrently\b)/i,
    (_, unique) => (unique ? 'CREATE UNIQUE INDEX CONCURRENTLY ' : 'CREATE INDEX CONCURRENTLY '),
  )
  return built === definition && !/concurrently/i.test(definition) ? null : `${built};`
}

/**
 * Index entries written per second for each table: every insert, and every
 * update that is not heap-only, adds an entry to every index of the table.
 * Deletes are left out: vacuum removes their entries later.
 */
export function indexWriteRates(items: readonly RelationItem[]): Map<string, number> {
  const rates = new Map<string, number>()
  const num = (item: RelationItem, key: string) => {
    const value = item[key]
    return typeof value === 'number' && Number.isFinite(value) ? value : null
  }
  const text = (item: RelationItem, key: string) => {
    const value = item[key]
    return typeof value === 'string' ? value : ''
  }
  for (const item of items) {
    const inserts = num(item, 'n_tup_ins_rate')
    const updates = num(item, 'n_tup_upd_rate')
    if (inserts === null && updates === null) continue
    const hot = Math.min(num(item, 'n_tup_hot_upd_rate') ?? 0, updates ?? 0)
    const key = `${text(item, 'datname')}.${text(item, 'schemaname')}.${text(item, 'relname')}`
    rates.set(key, (inserts ?? 0) + (updates ?? 0) - hot)
  }
  return rates
}

/** Which of two duplicates to keep: a constraint first, then the one queries use, then the name. */
function keeperOf(group: readonly IndexRow[]): IndexRow {
  const rank = (row: IndexRow) => (row.primary ? 2 : row.unique ? 1 : 0)
  return [...group].sort(
    (a, b) =>
      rank(b) - rank(a) ||
      (b.scanRate ?? -1) - (a.scanRate ?? -1) ||
      (b.bytes ?? 0) - (a.bytes ?? 0) ||
      a.name.localeCompare(b.name),
  )[0]!
}

function enforcesConstraint(row: IndexRow): boolean {
  return row.primary || row.unique
}

function classify(
  row: IndexRow,
  byName: ReadonlyMap<string, IndexRow>,
): Pick<CleanupCandidate, 'reason' | 'why' | 'coveredBy' | 'basis'> | null {
  if (enforcesConstraint(row)) return null
  if (!row.valid || row.invalidFinding !== null) {
    return {
      reason: 'invalid',
      why: 'The index is invalid: queries cannot use it, and a failed build leaves it behind.',
      coveredBy: null,
      basis: 'advisor',
    }
  }
  if (row.duplicateOf.length > 0) {
    const group = [
      row,
      ...row.duplicateOf.flatMap((name) => byName.get(`${row.datname}.${name}`) ?? []),
    ]
    const keeper = keeperOf(group)
    if (keeper.key !== row.key) {
      return {
        reason: 'duplicate',
        why: `It has the same definition as ${keeper.schema}.${keeper.name}, which stays.`,
        coveredBy: `${keeper.schema}.${keeper.name}`,
        basis: 'advisor',
      }
    }
  }
  if (row.redundantFinding !== null) {
    const extended = row.redundantFinding.evidence?.extended
    const covering = typeof extended === 'string' ? extended : null
    return {
      reason: 'redundant',
      why:
        covering === null
          ? 'Its columns are a prefix of a longer index.'
          : `Its columns are a prefix of ${covering}, which can serve the same lookups.`,
      coveredBy: covering,
      basis: 'advisor',
    }
  }
  if (row.unusedFinding !== null) {
    return {
      reason: 'unused',
      why: 'The advisor saw no scans of this index over its usage history.',
      coveredBy: null,
      basis: 'advisor',
    }
  }
  if (row.lastScanStatus === 'never_since_reset') {
    return {
      reason: 'never-scanned',
      why: 'No scan since the statistics were last reset. The advisor has not yet seen 7 days of history, so this is the raw counter, not a verdict.',
      coveredBy: null,
      basis: 'statistics',
    }
  }
  return null
}

export function buildCleanup(
  rows: readonly IndexRow[],
  writeRates: ReadonlyMap<string, number>,
): CleanupCandidate[] {
  const byName = new Map(rows.map((row) => [`${row.datname}.${row.schema}.${row.name}`, row]))
  const candidates: CleanupCandidate[] = []
  for (const row of rows) {
    const found = classify(row, byName)
    if (found === null) continue
    candidates.push({
      row,
      ...found,
      reclaimBytes: row.bytes ?? 0,
      writeRate: writeRates.get(row.tableKey) ?? null,
      dropStatement: dropStatement(row),
      recreateStatement: recreateStatement(row),
    })
  }
  return candidates.sort(
    (a, b) =>
      REASON_ORDER.indexOf(a.reason) - REASON_ORDER.indexOf(b.reason) ||
      b.reclaimBytes - a.reclaimBytes ||
      a.row.key.localeCompare(b.row.key),
  )
}

const emptyTotals = (): CleanupTotals => ({
  count: 0,
  reclaimBytes: 0,
  writeRate: 0,
  writeRateUnknown: 0,
})

export function summariseCleanup(
  candidates: readonly CleanupCandidate[],
  totalIndexBytes: number,
): CleanupSummary {
  const summary: CleanupSummary = {
    count: candidates.length,
    confirmed: emptyTotals(),
    provisional: emptyTotals(),
    confirmedShare: null,
    byReason: { invalid: 0, duplicate: 0, redundant: 0, unused: 0, 'never-scanned': 0 },
  }
  for (const candidate of candidates) {
    const totals = candidate.basis === 'advisor' ? summary.confirmed : summary.provisional
    totals.count += 1
    totals.reclaimBytes += candidate.reclaimBytes
    summary.byReason[candidate.reason] += 1
    if (candidate.writeRate === null) totals.writeRateUnknown += 1
    else totals.writeRate += candidate.writeRate
  }
  summary.confirmedShare =
    totalIndexBytes > 0 ? summary.confirmed.reclaimBytes / totalIndexBytes : null
  return summary
}

/**
 * A script to review and run by hand: pglens never executes it. The drops can
 * not run inside a transaction block, and the rebuild statements undo them.
 * Only the candidates the advisor stands behind are active. Those that rest on
 * raw counters are commented out: a few hours of counters are not a verdict,
 * and one paste of the whole script must not drop them.
 */
export function cleanupScript(candidates: readonly CleanupCandidate[]): string {
  const confirmed = candidates.filter((candidate) => candidate.basis === 'advisor')
  const provisional = candidates.filter((candidate) => candidate.basis === 'statistics')
  const lines = [
    '-- Review each statement before running it. pglens does not execute this script.',
    '-- DROP INDEX CONCURRENTLY cannot run inside a transaction block.',
    '',
  ]
  const describe = (candidate: CleanupCandidate) =>
    `-- ${candidate.reason}: ${candidate.row.schema}.${candidate.row.name} on ${candidate.row.table}: ${candidate.why}`
  for (const candidate of confirmed) lines.push(describe(candidate), candidate.dropStatement)
  if (provisional.length > 0) {
    lines.push(
      '',
      '-- Raw counters only. The advisor has not yet seen 7 days of usage history for these',
      '-- indexes, so none of them is active below. Uncomment one only after you have checked it.',
    )
    for (const candidate of provisional)
      lines.push(describe(candidate), `-- ${candidate.dropStatement}`)
  }
  const rebuilds = candidates.flatMap((candidate) =>
    candidate.recreateStatement === null ? [] : [candidate.recreateStatement],
  )
  if (rebuilds.length > 0) {
    lines.push('', '-- To undo a drop, rebuild the index:')
    for (const rebuild of rebuilds) lines.push(`-- ${rebuild}`)
  }
  return lines.join('\n')
}
