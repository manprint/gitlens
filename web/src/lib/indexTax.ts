import type { IndexRow } from './indexAnalysis'

type RelationItem = Record<string, unknown>

/**
 * What the indexes of one table cost (D33, I-5): every figure that rests on
 * rates or on counters is an estimate, and the unused size says whether the
 * advisor stands behind it or it is a raw counter.
 */
export interface IndexTax {
  /** `database.schema.table`: unique across the server. */
  key: string
  datname: string
  schema: string
  table: string
  indexCount: number
  /** Sum of the sizes the agent reported; an index without a size adds nothing. */
  indexBytes: number
  /** The heap alone (`table_bytes`); null when the table's statistics were not reported. */
  tableBytes: number | null
  /** Index bytes over heap bytes; null without a heap size to compare with (never NaN or Infinity). */
  indexToTableRatio: number | null
  /** Space of the indexes that look unused: not unique, not primary, advisor verdict or no scan since the reset. */
  unusedBytes: number
  unusedCount: number
  /** The part of `unusedBytes` the advisor stands behind. */
  unusedConfirmedBytes: number
  /** `advisor` when every unused index is an advisor verdict, `statistics` when one rests on raw counters, null with none. */
  unusedBasis: 'advisor' | 'statistics' | null
  /** Inserts, deletes and non-HOT updates per second; null when the table's rates were not reported. */
  writeRate: number | null
  /** `writeRate × indexCount`: index entries written per second. Zero writes give 0; null with no rates. */
  writeAmplification: number | null
}

function num(item: RelationItem, key: string): number | null {
  const value = item[key]
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function text(item: RelationItem, key: string): string {
  const value = item[key]
  return typeof value === 'string' ? value : ''
}

interface TableFacts {
  tableBytes: number | null
  writeRate: number | null
}

/**
 * Table size and write rate per table. An insert or a delete touches every
 * index; a HOT update touches none, so only the updates that are not heap-only
 * count. The HOT share is capped at the updates, never negative.
 */
function tableFacts(items: readonly RelationItem[]): Map<string, TableFacts> {
  const facts = new Map<string, TableFacts>()
  for (const item of items) {
    const inserts = num(item, 'n_tup_ins_rate')
    const updates = num(item, 'n_tup_upd_rate')
    const deletes = num(item, 'n_tup_del_rate')
    const hot = Math.min(num(item, 'n_tup_hot_upd_rate') ?? 0, updates ?? 0)
    const reported = inserts !== null || updates !== null || deletes !== null
    facts.set(`${text(item, 'datname')}.${text(item, 'schemaname')}.${text(item, 'relname')}`, {
      tableBytes: num(item, 'table_bytes'),
      writeRate: reported ? (inserts ?? 0) + (updates ?? 0) - hot + (deletes ?? 0) : null,
    })
  }
  return facts
}

/** An index that enforces a constraint is never counted as unused: dropping it is not an option. */
function looksUnused(row: IndexRow): 'advisor' | 'statistics' | null {
  if (row.primary || row.unique) return null
  if (row.unusedFinding !== null) return 'advisor'
  return row.lastScanStatus === 'never_since_reset' ? 'statistics' : null
}

/**
 * The index tax of every table that has an index in `rows`, largest index size
 * first. `tables` are the table statistics of the same server (`getInstanceTables`
 * items). A table the statistics do not cover keeps its index figures and has
 * no ratio and no write amplification.
 */
export function buildIndexTax(
  rows: readonly IndexRow[],
  tables: readonly RelationItem[],
): IndexTax[] {
  const facts = tableFacts(tables)
  const byTable = new Map<string, IndexTax>()
  for (const row of rows) {
    let tax = byTable.get(row.tableKey)
    if (tax === undefined) {
      tax = {
        key: row.tableKey,
        datname: row.datname,
        schema: row.schema,
        table: row.table,
        indexCount: 0,
        indexBytes: 0,
        tableBytes: null,
        indexToTableRatio: null,
        unusedBytes: 0,
        unusedCount: 0,
        unusedConfirmedBytes: 0,
        unusedBasis: null,
        writeRate: null,
        writeAmplification: null,
      }
      byTable.set(row.tableKey, tax)
    }
    tax.indexCount += 1
    tax.indexBytes += row.bytes ?? 0
    const basis = looksUnused(row)
    if (basis !== null) {
      tax.unusedCount += 1
      tax.unusedBytes += row.bytes ?? 0
      if (basis === 'advisor') tax.unusedConfirmedBytes += row.bytes ?? 0
      tax.unusedBasis =
        basis === 'statistics' || tax.unusedBasis === 'statistics' ? 'statistics' : 'advisor'
    }
  }
  for (const tax of byTable.values()) {
    const known = facts.get(tax.key)
    tax.tableBytes = known?.tableBytes ?? null
    tax.indexToTableRatio =
      tax.tableBytes !== null && tax.tableBytes > 0 ? tax.indexBytes / tax.tableBytes : null
    tax.writeRate = known?.writeRate ?? null
    tax.writeAmplification = tax.writeRate === null ? null : tax.writeRate * tax.indexCount
  }
  return [...byTable.values()].sort(
    (a, b) => b.indexBytes - a.indexBytes || a.key.localeCompare(b.key),
  )
}

export interface IndexTaxSummary {
  tables: number
  indexCount: number
  indexBytes: number
  unusedBytes: number
  unusedConfirmedBytes: number
  /** Index entries per second across the tables whose rates were reported; null when none were. */
  writeAmplification: number | null
  /** Tables whose write rates were reported. */
  measuredTables: number
}

export function summariseIndexTax(taxes: readonly IndexTax[]): IndexTaxSummary {
  const summary: IndexTaxSummary = {
    tables: taxes.length,
    indexCount: 0,
    indexBytes: 0,
    unusedBytes: 0,
    unusedConfirmedBytes: 0,
    writeAmplification: null,
    measuredTables: 0,
  }
  for (const tax of taxes) {
    summary.indexCount += tax.indexCount
    summary.indexBytes += tax.indexBytes
    summary.unusedBytes += tax.unusedBytes
    summary.unusedConfirmedBytes += tax.unusedConfirmedBytes
    if (tax.writeAmplification !== null) {
      summary.writeAmplification = (summary.writeAmplification ?? 0) + tax.writeAmplification
      summary.measuredTables += 1
    }
  }
  return summary
}
