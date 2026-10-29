import type { Schemas } from '@/api/types'

import { parseIndexDefinition, type ParsedIndexDefinition } from './indexDefinition'

type Finding = Schemas['Finding']
type RelationItem = Record<string, unknown>

/** The advisor rules that speak about indexes. */
export const INDEX_RULES = {
  unused: 'index.unused',
  duplicate: 'index.duplicate',
  redundantPrefix: 'index.redundant_prefix',
  invalid: 'index.invalid',
  divergence: 'index.divergence',
  foreignKey: 'index.fk_unindexed',
  bloat: 'index.bloat_high',
} as const

export type LastScanStatus = 'scanned' | 'never_since_reset' | 'not_available'

export interface IndexRow {
  /** `database.schema.index`: unique across the server. */
  key: string
  datname: string
  schema: string
  /** The index name, without its schema. */
  name: string
  table: string
  tableKey: string
  definition: string | null
  parsed: ParsedIndexDefinition
  bytes: number | null
  /** Per-second rates over the last collection interval, not totals. */
  scanRate: number | null
  tupleReadRate: number | null
  tupleFetchRate: number | null
  cacheHitRatio: number | null
  lastScanAt: string | null
  lastScanStatus: LastScanStatus
  primary: boolean
  unique: boolean
  valid: boolean
  /** Advisor verdicts for this index. They come from findings, never recomputed here. */
  unusedFinding: Finding | null
  redundantFinding: Finding | null
  invalidFinding: Finding | null
  divergenceFinding: Finding | null
  /** The other indexes this one duplicates, when the advisor reports a duplicate group. */
  duplicateOf: string[]
}

function text(item: RelationItem, key: string): string {
  const value = item[key]
  return typeof value === 'string' ? value : ''
}

function number(item: RelationItem, key: string): number | null {
  const value = item[key]
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function flag(item: RelationItem, key: string, fallback: boolean): boolean {
  const value = item[key]
  return typeof value === 'boolean' ? value : fallback
}

function lastScanStatus(item: RelationItem): LastScanStatus {
  const value = item.last_scan_status
  return value === 'scanned' || value === 'never_since_reset' ? value : 'not_available'
}

/** Only the findings an operator can still act on: muted and resolved ones are not shown. */
export function indexFindings(findings: readonly Finding[]): Finding[] {
  return findings.filter(
    (finding) =>
      finding.rule_id.startsWith('index.') &&
      (finding.state === 'open' || finding.state === 'degraded'),
  )
}

function evidenceIndexes(finding: Finding): string[] {
  const names = finding.evidence?.indexes
  return Array.isArray(names)
    ? names.filter((name): name is string => typeof name === 'string')
    : []
}

export function buildIndexRows(
  items: readonly RelationItem[],
  findings: readonly Finding[],
): IndexRow[] {
  const byObject = new Map<string, Finding>()
  const duplicates = new Map<string, string[]>()
  for (const finding of indexFindings(findings)) {
    const database = finding.datname ?? ''
    const object = finding.object_name ?? ''
    if (finding.rule_id === INDEX_RULES.duplicate) {
      const group = evidenceIndexes(finding)
      for (const name of group) {
        duplicates.set(
          `${database}.${name}`,
          group.filter((other) => other !== name),
        )
      }
    } else if (finding.state === 'open') {
      byObject.set(`${finding.rule_id}|${database}.${object}`, finding)
    }
  }
  const found = (rule: string, database: string, qualified: string) =>
    byObject.get(`${rule}|${database}.${qualified}`) ?? null

  return items.map((item) => {
    const datname = text(item, 'datname')
    const schema = text(item, 'schemaname')
    const name = text(item, 'indexrelname')
    const table = text(item, 'relname')
    const qualified = `${schema}.${name}`
    const definition = text(item, 'definition') || null
    return {
      key: `${datname}.${qualified}`,
      datname,
      schema,
      name,
      table,
      tableKey: `${datname}.${schema}.${table}`,
      definition,
      parsed: parseIndexDefinition(definition),
      bytes: number(item, 'index_bytes'),
      scanRate: number(item, 'idx_scan_rate'),
      tupleReadRate: number(item, 'idx_tup_read_rate'),
      tupleFetchRate: number(item, 'idx_tup_fetch_rate'),
      cacheHitRatio: number(item, 'cache_hit_ratio'),
      lastScanAt: text(item, 'last_scan_at') || null,
      lastScanStatus: lastScanStatus(item),
      primary: flag(item, 'is_primary', false),
      unique: flag(item, 'is_unique', false),
      valid: flag(item, 'is_valid', true),
      unusedFinding: found(INDEX_RULES.unused, datname, qualified),
      redundantFinding: found(INDEX_RULES.redundantPrefix, datname, qualified),
      invalidFinding: found(INDEX_RULES.invalid, datname, qualified),
      divergenceFinding:
        [...byObject.entries()].find(([key]) =>
          key.startsWith(`${INDEX_RULES.divergence}|${datname}.${qualified}@`),
        )?.[1] ?? null,
      duplicateOf: duplicates.get(`${datname}.${qualified}`) ?? [],
    }
  })
}

export interface Tally {
  label: string
  count: number
  bytes: number
}

function tally(rows: readonly IndexRow[], labelOf: (row: IndexRow) => string): Tally[] {
  const groups = new Map<string, Tally>()
  for (const row of rows) {
    const label = labelOf(row)
    const entry = groups.get(label) ?? { label, count: 0, bytes: 0 }
    entry.count += 1
    entry.bytes += row.bytes ?? 0
    groups.set(label, entry)
  }
  return [...groups.values()].sort((a, b) => b.count - a.count || b.bytes - a.bytes)
}

export interface IndexSummary {
  total: number
  bytes: number
  primary: number
  unique: number
  partial: number
  expression: number
  composite: number
  invalid: number
  /** Never scanned since the statistics reset: raw stats, not the advisor's verdict. */
  neverScanned: number
  neverScannedBytes: number
  byMethod: Tally[]
  byDatabase: Tally[]
}

export function summariseIndexes(rows: readonly IndexRow[]): IndexSummary {
  const summary: IndexSummary = {
    total: rows.length,
    bytes: 0,
    primary: 0,
    unique: 0,
    partial: 0,
    expression: 0,
    composite: 0,
    invalid: 0,
    neverScanned: 0,
    neverScannedBytes: 0,
    byMethod: tally(rows, (row) => row.parsed.method ?? 'unknown'),
    byDatabase: tally(rows, (row) => row.datname),
  }
  for (const row of rows) {
    summary.bytes += row.bytes ?? 0
    if (row.primary) summary.primary += 1
    if (row.unique && !row.primary) summary.unique += 1
    if (row.parsed.predicate !== null) summary.partial += 1
    if (row.parsed.columns.some((column) => column.expression)) summary.expression += 1
    if (row.parsed.columns.length > 1) summary.composite += 1
    if (!row.valid) summary.invalid += 1
    // A primary key or a unique index enforces a constraint even when no query uses it.
    if (row.lastScanStatus === 'never_since_reset' && !row.primary && !row.unique) {
      summary.neverScanned += 1
      summary.neverScannedBytes += row.bytes ?? 0
    }
  }
  return summary
}

export interface TableGroup {
  tableKey: string
  datname: string
  schema: string
  table: string
  indexes: IndexRow[]
  bytes: number
}

/** Tables with the most indexes first: the ones whose write cost is worth reviewing. */
export function groupByTable(rows: readonly IndexRow[]): TableGroup[] {
  const groups = new Map<string, TableGroup>()
  for (const row of rows) {
    const group = groups.get(row.tableKey) ?? {
      tableKey: row.tableKey,
      datname: row.datname,
      schema: row.schema,
      table: row.table,
      indexes: [],
      bytes: 0,
    }
    group.indexes.push(row)
    group.bytes += row.bytes ?? 0
    groups.set(row.tableKey, group)
  }
  return [...groups.values()].sort(
    (a, b) => b.indexes.length - a.indexes.length || b.bytes - a.bytes,
  )
}

export interface ForeignKeyGap {
  datname: string
  table: string
  constraint: string
  columns: string
  referenced: string
  finding: Finding
}

/** Foreign keys with no covering index: they have no index row, so they are listed apart. */
export function foreignKeyGaps(findings: readonly Finding[]): ForeignKeyGap[] {
  return indexFindings(findings)
    .filter((finding) => finding.rule_id === INDEX_RULES.foreignKey && finding.state === 'open')
    .map((finding) => {
      const evidence = finding.evidence ?? {}
      const read = (key: string) => (typeof evidence[key] === 'string' ? evidence[key] : '')
      return {
        datname: finding.datname ?? '',
        table: read('table'),
        constraint: read('constraint'),
        columns: read('columns'),
        referenced: read('referenced'),
        finding,
      }
    })
}

export type RowFilter =
  'all' | 'attention' | 'unused' | 'duplicate' | 'partial' | 'unique' | 'invalid'

export function needsAttention(row: IndexRow): boolean {
  return (
    row.unusedFinding !== null ||
    row.redundantFinding !== null ||
    row.invalidFinding !== null ||
    row.divergenceFinding !== null ||
    row.duplicateOf.length > 0 ||
    !row.valid
  )
}

export function matchesFilter(row: IndexRow, filter: RowFilter): boolean {
  switch (filter) {
    case 'attention':
      return needsAttention(row)
    case 'unused':
      return (
        row.unusedFinding !== null ||
        (row.lastScanStatus === 'never_since_reset' && !row.primary && !row.unique)
      )
    case 'duplicate':
      return row.duplicateOf.length > 0 || row.redundantFinding !== null
    case 'partial':
      return row.parsed.predicate !== null
    case 'unique':
      return row.unique || row.primary
    case 'invalid':
      return !row.valid
    case 'all':
      return true
  }
}

/** Matches the name, table, columns, method and definition, case-insensitively. */
export function matchesSearch(row: IndexRow, needle: string): boolean {
  const query = needle.trim().toLowerCase()
  if (query === '') return true
  const haystack = [
    row.name,
    row.table,
    row.schema,
    row.datname,
    row.parsed.method ?? '',
    ...row.parsed.columns.map((column) => column.text),
    ...row.parsed.include.map((column) => column.text),
    row.definition ?? '',
  ]
  return haystack.some((part) => part.toLowerCase().includes(query))
}
