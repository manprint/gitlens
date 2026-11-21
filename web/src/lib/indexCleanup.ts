import { localeInfo } from '@/i18n/locales'
import { activeLocale, translate } from '@/i18n/store'
import { formatBytes } from './format'
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
  /** The same sentence in English: the SQL script stays English. */
  whyEn: string
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

function duplicateGroup(row: IndexRow, byName: ReadonlyMap<string, IndexRow>): IndexRow[] {
  return [row, ...row.duplicateOf.flatMap((name) => byName.get(`${row.datname}.${name}`) ?? [])]
}

function duplicateOfConstraint(
  row: IndexRow,
  byName: ReadonlyMap<string, IndexRow>,
  locale: string,
): Pick<CleanupCandidate, 'reason' | 'why' | 'coveredBy' | 'basis'> | null {
  if (row.duplicateOf.length === 0) return null
  const keeper = keeperOf(duplicateGroup(row, byName))
  if (keeper.key === row.key || !enforcesConstraint(keeper)) return null
  return {
    reason: 'duplicate',
    why: translate(
      'indexes.lib.cleanup.why.duplicateConstraint',
      { keeper: `${keeper.schema}.${keeper.name}` },
      locale,
    ),
    coveredBy: `${keeper.schema}.${keeper.name}`,
    basis: 'advisor',
  }
}

function classify(
  row: IndexRow,
  byName: ReadonlyMap<string, IndexRow>,
  window: number | null,
  locale: string,
): Pick<CleanupCandidate, 'reason' | 'why' | 'coveredBy' | 'basis'> | null {
  if (row.primary) return null
  // A unique index is listed only as the duplicate of another unique or primary
  // index, which keeps the column unique once it is gone.
  if (row.unique) return duplicateOfConstraint(row, byName, locale)
  if (!row.valid || row.invalidFinding !== null) {
    return {
      reason: 'invalid',
      why: translate('indexes.lib.cleanup.why.invalid', {}, locale),
      coveredBy: null,
      basis: 'advisor',
    }
  }
  if (row.duplicateOf.length > 0) {
    const keeper = keeperOf(duplicateGroup(row, byName))
    if (keeper.key !== row.key) {
      return {
        reason: 'duplicate',
        why: translate(
          'indexes.lib.cleanup.why.duplicate',
          { keeper: `${keeper.schema}.${keeper.name}` },
          locale,
        ),
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
          ? translate('indexes.lib.cleanup.why.redundant', {}, locale)
          : translate('indexes.lib.cleanup.why.redundantCovering', { covering }, locale),
      coveredBy: covering,
      basis: 'advisor',
    }
  }
  if (row.unusedFinding !== null) {
    const basis = observationBasis(row.unusedFinding.evidence, locale)
    return {
      reason: 'unused',
      why:
        basis === null
          ? translate('indexes.lib.cleanup.why.unused', {}, locale)
          : translate('indexes.lib.cleanup.why.unusedBasis', { basis }, locale),
      coveredBy: null,
      basis: 'advisor',
    }
  }
  if (row.lastScanStatus === 'never_since_reset') {
    return {
      reason: 'never-scanned',
      why: neverScannedWhy(row, window, locale),
      coveredBy: null,
      basis: 'statistics',
    }
  }
  return null
}

/**
 * The advisor leaves out an index of this size or less (unusedIndexBytes in
 * internal/advisor/rules_relation.go): dropping it gives back too little.
 */
export const UNUSED_INDEX_MIN_BYTES = 10 * 1024 * 1024

/**
 * Why an index with no scan since the statistics reset has no unused-index
 * finding. A small index is left out by the advisor. A larger one is called
 * unused only when no scan shows over the whole window, in PostgreSQL's
 * statistics or in pglens's history, here and on the standbys: a statistics
 * reset newer than the window, a short history, a scan seen before the reset
 * or on a standby, or an evaluation still to come all keep the finding away.
 * Primary keys and unique indexes never reach this point (classify).
 */
function neverScannedWhy(row: IndexRow, window: number | null, locale: string): string {
  const counter = translate('indexes.lib.cleanup.neverScanned.counter', {}, locale)
  if (row.bytes !== null && row.bytes <= UNUSED_INDEX_MIN_BYTES) {
    const size = formatBytes(UNUSED_INDEX_MIN_BYTES, intlOf(locale))
    return `${counter} ${translate('indexes.lib.cleanup.neverScanned.leftOut', { size }, locale)}`
  }
  const phrase = windowPhrase(window, locale)
  return `${counter} ${translate('indexes.lib.cleanup.neverScanned.window', { window: phrase }, locale)}`
}

type Evidence = Record<string, unknown> | null | undefined

function seconds(evidence: Evidence, key: string): number | null {
  const value = evidence?.[key]
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null
}

/** The Intl locale of a console language: the format of its numbers and sizes. */
function intlOf(locale: string): string {
  return localeInfo(locale).intl
}

/** A count of days, hours or seconds, in the words the window and span texts use. */
function unitText(unit: 'days' | 'hours' | 'seconds', n: number, locale: string): string {
  const args = { count: n, n: String(n) }
  if (unit === 'days') return translate('indexes.lib.cleanup.unit.days', args, locale)
  if (unit === 'hours') return translate('indexes.lib.cleanup.unit.hours', args, locale)
  return translate('indexes.lib.cleanup.unit.seconds', args, locale)
}

/** A window in the unit that reads it exactly: "7 days", "36 hours", "5400 seconds". */
function windowLabel(seconds: number, locale: string): string {
  if (seconds % 86_400 === 0) return unitText('days', seconds / 86_400, locale)
  if (seconds % 3_600 !== 0) {
    return translate('indexes.lib.cleanup.window.seconds', { n: String(seconds) }, locale)
  }
  return unitText('hours', seconds / 3_600, locale)
}

/**
 * An observed span in the unit the window is written in ("38 hours" next to
 * "36 hours", "9 days" next to "7 days"), rounded down so it never claims more
 * history than there is. A span at least as long as the window never reads
 * shorter than it, because the window is a whole number of that unit.
 */
function spanText(total: number, window: number, locale: string): string {
  if (window % 86_400 === 0) return unitText('days', Math.floor(total / 86_400), locale)
  if (window % 3_600 === 0) return unitText('hours', Math.floor(total / 3_600), locale)
  return unitText('seconds', Math.floor(total), locale)
}

/** The day of an RFC 3339 time, as the advisor writes it in the finding detail; null when unreadable. */
function resetDay(value: unknown): string | null {
  if (typeof value !== 'string') return null
  const time = new Date(value)
  return Number.isNaN(time.getTime()) ? null : time.toISOString().slice(0, 10)
}

/**
 * What the advisor's unused-index verdict rests on, from the finding's
 * evidence (13.2, D22b): "PostgreSQL statistics cover 9 days; the window is 7
 * days". A verdict from an index never scanned since the statistics reset
 * (evidence `window: "since statistics reset"`) rests on that reset, not on
 * pglens's history: "PostgreSQL statistics show no scan since their reset on
 * 2026-10-01". A verdict from an index PostgreSQL last scanned before the
 * window (evidence `last_scan`) rests on that scan: "PostgreSQL statistics
 * show the last scan on 2026-10-20". Null when the finding predates these
 * fields, or when its observation is shorter than the window, which no verdict
 * rests on.
 */
export function observationBasis(
  evidence: Evidence,
  locale: string = activeLocale(),
): string | null {
  const window = seconds(evidence, 'window_seconds')
  const windowPart =
    window === null || window === 0
      ? null
      : translate(
          'indexes.lib.cleanup.basis.window',
          { window: windowLabel(window, locale) },
          locale,
        )
  const joined = (basis: string): string =>
    windowPart === null
      ? basis
      : translate('indexes.lib.cleanup.basis.withWindow', { basis, window: windowPart }, locale)
  if (evidence?.window === 'since statistics reset') {
    const day = resetDay(evidence.stats_reset)
    return joined(
      day === null
        ? translate('indexes.lib.cleanup.basis.resetUnknown', {}, locale)
        : translate('indexes.lib.cleanup.basis.resetDay', { day }, locale),
    )
  }
  const lastScan = resetDay(evidence?.last_scan)
  if (lastScan !== null) {
    return joined(translate('indexes.lib.cleanup.basis.lastScan', { day: lastScan }, locale))
  }
  const observed = seconds(evidence, 'observed_for_seconds')
  if (observed === null || window === null || window === 0 || observed < window) return null
  const span = spanText(observed, window, locale)
  return joined(
    evidence?.observed_source === 'postgres_stats'
      ? translate('indexes.lib.cleanup.basis.statsCover', { span }, locale)
      : translate('indexes.lib.cleanup.basis.historyCovers', { span }, locale),
  )
}

/**
 * The unused-index window the advisor applies, in seconds: read from the
 * evidence of any of its findings, which tells the window it judged with, else
 * the configured window (`getAdvisorWindows`), else null. The window is one
 * setting for the whole server, so any finding tells it.
 */
function unusedWindowSeconds(
  rows: readonly IndexRow[],
  configured: number | null | undefined,
): number | null {
  for (const row of rows) {
    const window = seconds(row.unusedFinding?.evidence, 'window_seconds')
    if (window !== null && window > 0) return window
  }
  return configured !== null && configured !== undefined && configured > 0 ? configured : null
}

/** "7 days" for a known window, else a phrase that names no length. */
export function windowPhrase(window: number | null, locale: string = activeLocale()): string {
  return window === null
    ? translate('indexes.lib.cleanup.window.fullWindow', {}, locale)
    : windowLabel(window, locale)
}

/** The unused-index window as the cleanup texts name it: "7 days", or a phrase that names no length. */
export function unusedWindowPhrase(
  rows: readonly IndexRow[],
  configuredWindow: number | null = null,
  locale: string = activeLocale(),
): string {
  return windowPhrase(unusedWindowSeconds(rows, configuredWindow), locale)
}

/**
 * The cleanup list. `configuredWindow` is the unused-index window from the
 * settings, in seconds, used when no finding tells the window.
 */
export function buildCleanup(
  rows: readonly IndexRow[],
  writeRates: ReadonlyMap<string, number>,
  configuredWindow: number | null = null,
): CleanupCandidate[] {
  const byName = new Map(rows.map((row) => [`${row.datname}.${row.schema}.${row.name}`, row]))
  const window = unusedWindowSeconds(rows, configuredWindow)
  const candidates: CleanupCandidate[] = []
  for (const row of rows) {
    const found = classify(row, byName, window, activeLocale())
    if (found === null) continue
    const english = classify(row, byName, window, 'en')
    candidates.push({
      row,
      ...found,
      whyEn: english?.why ?? found.why,
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
export function cleanupScript(
  candidates: readonly CleanupCandidate[],
  configuredWindow: number | null = null,
): string {
  const confirmed = candidates.filter((candidate) => candidate.basis === 'advisor')
  const provisional = candidates.filter((candidate) => candidate.basis === 'statistics')
  const lines = [
    '-- Review each statement before running it. pglens does not execute this script.',
    '-- DROP INDEX CONCURRENTLY cannot run inside a transaction block.',
    '',
  ]
  const describe = (candidate: CleanupCandidate) =>
    `-- ${candidate.reason}: ${candidate.row.schema}.${candidate.row.name} on ${candidate.row.table}: ${candidate.whyEn}`
  for (const candidate of confirmed) lines.push(describe(candidate), candidate.dropStatement)
  if (provisional.length > 0) {
    lines.push(
      '',
      `-- Raw counters only. The advisor has not called these indexes unused (that takes ${windowPhrase(
        unusedWindowSeconds(
          candidates.map((candidate) => candidate.row),
          configuredWindow,
        ),
        'en',
      )}`,
      '-- with no scan), so none of them is active below. Uncomment one only after you have checked it.',
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
