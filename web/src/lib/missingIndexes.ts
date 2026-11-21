import type { Schemas } from '@/api/types'
import { localeInfo } from '@/i18n/locales'
import type { MessageKey } from '@/i18n/messages/en'
import { activeLocale, translate } from '@/i18n/store'
import { formatBytes, formatCount, formatMilliseconds } from '@/lib/format'
import { foreignKeyGaps } from '@/lib/indexAnalysis'
import { keyText, weightText, type IndexSuggestion } from '@/lib/indexSuggestions'

type Finding = Schemas['Finding']
type TableItem = Record<string, unknown>

/** Where a missing index was found. */
export type MissingSource = 'queries' | 'foreign-keys' | 'spatial' | 'scans'

/**
 * What the DBA should do: create it, test it first, check the workload
 * before deciding, or leave it.
 */
export type Recommendation = 'create' | 'test' | 'check' | 'skip'

export const RECOMMENDATION_ORDER: readonly Recommendation[] = ['create', 'test', 'check', 'skip']

const RECOMMENDATION_LABEL_KEYS: Readonly<Record<Recommendation, MessageKey>> = {
  create: 'indexes.lib.missing.label.create',
  test: 'indexes.lib.missing.label.test',
  check: 'indexes.lib.missing.label.check',
  skip: 'indexes.lib.missing.label.skip',
}

const RECOMMENDATION_HINT_KEYS: Readonly<Record<Recommendation, MessageKey>> = {
  create: 'indexes.lib.missing.hint.create',
  test: 'indexes.lib.missing.hint.test',
  check: 'indexes.lib.missing.hint.check',
  skip: 'indexes.lib.missing.hint.skip',
}

const SOURCE_LABEL_KEYS: Readonly<Record<MissingSource, MessageKey>> = {
  queries: 'indexes.lib.missing.source.queries',
  'foreign-keys': 'indexes.lib.missing.source.foreignKeys',
  spatial: 'indexes.lib.missing.source.spatial',
  scans: 'indexes.lib.missing.source.scans',
}

const SOURCE_HINT_KEYS: Readonly<Record<MissingSource, MessageKey>> = {
  queries: 'indexes.lib.missing.sourceHint.queries',
  'foreign-keys': 'indexes.lib.missing.sourceHint.foreignKeys',
  spatial: 'indexes.lib.missing.sourceHint.spatial',
  scans: 'indexes.lib.missing.sourceHint.scans',
}

/**
 * Texts keyed by a name, each read in the console language when it is
 * accessed, never when the module loads.
 */
function lazyTexts<K extends string>(
  keys: Readonly<Record<K, MessageKey>>,
): Readonly<Record<K, string>> {
  const texts = {} as Record<K, string>
  for (const name of Object.keys(keys) as K[]) {
    Object.defineProperty(texts, name, { enumerable: true, get: () => translate(keys[name]) })
  }
  return texts
}

export const RECOMMENDATION_LABEL = lazyTexts(RECOMMENDATION_LABEL_KEYS)
export const RECOMMENDATION_HINT = lazyTexts(RECOMMENDATION_HINT_KEYS)
export const SOURCE_LABEL = lazyTexts(SOURCE_LABEL_KEYS)
export const SOURCE_HINT = lazyTexts(SOURCE_HINT_KEYS)

export interface MissingStep {
  title: string
  text: string
  sql?: string
}

export interface MissingIndex {
  key: string
  source: MissingSource
  database: string
  schema: string
  table: string
  /** The key columns, `—` when unknown (full table scans). */
  columns: string
  recommendation: Recommendation
  /** One plain sentence: why the index is missing and what it would change. */
  why: string
  /** The same sentence in English: the psql script stays English. */
  whyEn: string
  /** The statement that creates the index, null when the columns are not known yet. */
  command: string | null
  /** Check, create, verify and undo, in order. */
  steps: MissingStep[]
  /** Heap size of the table, when reported. */
  tableBytes: number | null
  /** Ranks rows of the same recommendation: bigger first. */
  weight: number
  suggestion?: IndexSuggestion
  finding?: Finding
}

/** A missing index before its English sentence is added. */
type Built = Omit<MissingIndex, 'whyEn'>

/** Thresholds of the full-table-scan source; the guide quotes them. */
export const SCAN_MIN_TABLE_BYTES = 8 * 1024 * 1024
export const SCAN_MIN_ROWS_PER_SECOND = 100
/** A query suggestion below both is a small gain. */
export const LOW_GAIN_MS_PER_SECOND = 1
export const LOW_GAIN_WEIGHT_PCT = 1

const PLAIN_IDENTIFIER = /^[a-z_][a-z0-9_$]*$/

/** An identifier as psql takes it: bare when it is plain lower case, quoted otherwise. */
export function ident(name: string): string {
  return PLAIN_IDENTIFIER.test(name) ? name : `"${name.replaceAll('"', '""')}"`
}

function qualified(schema: string, table: string): string {
  return `${ident(schema)}.${ident(table)}`
}

/** `public.orders` → [`public`, `orders`]; a bare name is in `public`. */
export function splitTable(name: string): [string, string] {
  const dot = name.indexOf('.')
  return dot < 0 ? ['public', name] : [name.slice(0, dot), name.slice(dot + 1)]
}

/** A new index name within PostgreSQL's 63-byte limit. */
export function indexName(table: string, columns: readonly string[], suffix: string): string {
  const base = `${table}_${columns.join('_')}`.replace(/[^A-Za-z0-9_]+/g, '_').toLowerCase()
  const tail = `_${suffix}`
  return `${base.slice(0, 63 - tail.length)}${tail}`
}

/** The index name a `CREATE INDEX` statement gives, or null. */
export function createdIndexName(statement: string): string | null {
  const match =
    /index\s+(?:concurrently\s+)?(?:if\s+not\s+exists\s+)?("(?:[^"]|"")+"|[\w$]+)\s+on\s/i.exec(
      statement,
    )
  return match?.[1] ?? null
}

function dropCommand(schema: string, name: string): string {
  const bare = name.startsWith('"') ? name : ident(name)
  return `DROP INDEX CONCURRENTLY IF EXISTS ${ident(schema)}.${bare};`
}

function usageCheck(schema: string, name: string): string {
  const bare = name.startsWith('"') ? name.slice(1, -1).replaceAll('""', '"') : name
  return `SELECT idx_scan, last_idx_scan, pg_size_pretty(pg_relation_size(indexrelid)) AS size
FROM pg_stat_user_indexes
WHERE schemaname = '${schema.replaceAll("'", "''")}' AND indexrelname = '${bare.replaceAll("'", "''")}';`
}

/** The Intl locale of a console language: the format of its numbers and sizes. */
function intlOf(locale: string): string {
  return localeInfo(locale).intl
}

/** A console text in English: the psql script stays English whatever the language. */
const english = (key: MessageKey): string => translate(key, {}, 'en')

function createStep(command: string, locale: string): MissingStep {
  return {
    title: translate('indexes.lib.missing.step.title.create', {}, locale),
    text: translate('indexes.lib.missing.step.create', {}, locale),
    sql: command,
  }
}

function undoStep(schema: string, name: string, locale: string): MissingStep {
  return {
    title: translate('indexes.lib.missing.step.title.undo', {}, locale),
    text: translate('indexes.lib.missing.step.undo', {}, locale),
    sql: dropCommand(schema, name),
  }
}

function num(item: TableItem, key: string): number | null {
  const value = item[key]
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function str(item: TableItem, key: string): string {
  const value = item[key]
  return typeof value === 'string' ? value : ''
}

function tableKey(database: string, schema: string, table: string): string {
  return `${database}.${schema}.${table}`
}

function fromSuggestion(row: IndexSuggestion, locale: string): Built {
  const intl = intlOf(locale)
  const columns = keyText(row.columns)
  const name = createdIndexName(row.create_statement) ?? indexName(row.table, [columns], 'idx')
  const lowGain =
    row.read_cost_ms_per_s < LOW_GAIN_MS_PER_SECOND && row.weight_pct < LOW_GAIN_WEIGHT_PCT
  const recommendation: Recommendation =
    row.verdict === 'write_heavy'
      ? 'skip'
      : lowGain
        ? 'check'
        : row.verdict === 'strong' && row.confidence !== 'low'
          ? 'create'
          : 'test'
  const statements = translate(
    'indexes.lib.missing.why.statements',
    { count: row.statements.length, n: formatCount(row.statements.length, intl) },
    locale,
  )
  const gain = translate(
    'indexes.lib.missing.why.gain',
    {
      statements,
      columns,
      cost: formatMilliseconds(row.read_cost_ms_per_s, intl),
      weight: weightText(row.weight_pct),
    },
    locale,
  )
  const why =
    recommendation === 'skip'
      ? translate('indexes.lib.missing.why.skip', { gain }, locale)
      : recommendation === 'check'
        ? translate('indexes.lib.missing.why.check', { gain }, locale)
        : row.confidence === 'low'
          ? translate('indexes.lib.missing.why.lowConfidence', { gain }, locale)
          : gain
  const sample = row.statements[0]?.query_text
  return {
    key: `queries:${row.id}`,
    source: 'queries',
    database: row.database,
    schema: row.schema,
    table: row.table,
    columns,
    recommendation,
    why,
    command: row.create_statement,
    tableBytes: null,
    weight: row.read_cost_ms_per_s,
    suggestion: row,
    steps: [
      {
        title: translate('indexes.lib.missing.step.title.check', {}, locale),
        text: translate('indexes.lib.missing.step.checkQuery', {}, locale),
        sql: `EXPLAIN (ANALYZE, BUFFERS)\n${sample ?? `SELECT … FROM ${qualified(row.schema, row.table)} WHERE …`};`,
      },
      createStep(row.create_statement, locale),
      {
        title: translate('indexes.lib.missing.step.title.verify', {}, locale),
        text: translate('indexes.lib.missing.step.verifyQuery', {}, locale),
        sql: usageCheck(row.schema, name),
      },
      undoStep(row.schema, name, locale),
    ],
  }
}

interface TableRates {
  deleteRate: number | null
  updateRate: number | null
}

function fromForeignKey(
  gap: ReturnType<typeof foreignKeyGaps>[number],
  rates: ReadonlyMap<string, TableRates>,
  locale: string,
): Built {
  const intl = intlOf(locale)
  const [schema, table] = splitTable(gap.table)
  const [refSchema, refTable] = splitTable(gap.referenced)
  const columns = gap.columns
    .split(',')
    .map((column) => column.trim())
    .filter((column) => column !== '')
  const bytesValue = gap.finding.evidence?.table_bytes
  const tableBytes = typeof bytesValue === 'number' ? bytesValue : null
  const name = indexName(table, columns, 'idx')
  const command = `CREATE INDEX CONCURRENTLY IF NOT EXISTS ${ident(name)} ON ${qualified(schema, table)} (${columns.map(ident).join(', ')});`
  const parent = rates.get(tableKey(gap.datname, refSchema, refTable))
  const deletes = parent?.deleteRate ?? null
  const size =
    tableBytes === null
      ? translate('indexes.lib.missing.why.wholeTable', {}, locale)
      : translate(
          'indexes.lib.missing.why.allOfIt',
          { bytes: formatBytes(tableBytes, intl) },
          locale,
        )
  const big = tableBytes !== null && tableBytes >= 10 * 1024 * 1024
  const recommendation: Recommendation =
    deletes !== null && deletes > 0 ? (big ? 'create' : 'test') : 'check'
  const scan = translate(
    'indexes.lib.missing.why.fkScan',
    { referenced: gap.referenced, table: gap.table, size },
    locale,
  )
  const why =
    deletes !== null && deletes > 0
      ? translate(
          'indexes.lib.missing.why.fkDeleting',
          { scan, referenced: gap.referenced, columns: columns.join(', ') },
          locale,
        )
      : translate(
          'indexes.lib.missing.why.fkIdle',
          { scan, referenced: gap.referenced, columns: columns.join(', ') },
          locale,
        )
  return {
    key: `foreign-keys:${gap.datname}.${gap.table}.${gap.constraint}`,
    source: 'foreign-keys',
    database: gap.datname,
    schema,
    table,
    columns: columns.join(', '),
    recommendation,
    why,
    command,
    tableBytes,
    weight: tableBytes ?? 0,
    finding: gap.finding,
    steps: [
      {
        title: translate('indexes.lib.missing.step.title.check', {}, locale),
        text: translate(
          'indexes.lib.missing.step.checkForeignKey',
          { referenced: gap.referenced },
          locale,
        ),
        sql: `SELECT n_tup_del, n_tup_upd, last_autovacuum
FROM pg_stat_user_tables
WHERE relid = '${qualified(refSchema, refTable).replaceAll("'", "''")}'::regclass;`,
      },
      createStep(command, locale),
      {
        title: translate('indexes.lib.missing.step.title.verify', {}, locale),
        text: translate(
          'indexes.lib.missing.step.verifyForeignKey',
          { referenced: gap.referenced },
          locale,
        ),
        sql: `BEGIN;
EXPLAIN (ANALYZE) DELETE FROM ${qualified(refSchema, refTable)} WHERE … = …; -- one key value
ROLLBACK;`,
      },
      undoStep(schema, name, locale),
    ],
  }
}

function fromSpatial(finding: Finding, locale: string): Built | null {
  const intl = intlOf(locale)
  const evidence = finding.evidence ?? {}
  const tableName = typeof evidence.table === 'string' ? evidence.table : ''
  const column = typeof evidence.column === 'string' ? evidence.column : ''
  if (tableName === '' || column === '') return null
  const [schema, table] = splitTable(tableName)
  const rows = typeof evidence.rows === 'number' ? evidence.rows : null
  const type = typeof evidence.type === 'string' ? evidence.type : 'geometry'
  const name = indexName(table, [column], 'gist')
  const command = `CREATE INDEX CONCURRENTLY IF NOT EXISTS ${ident(name)} ON ${qualified(schema, table)} USING gist (${ident(column)});`
  const reads =
    rows === null
      ? translate('indexes.lib.missing.why.wholeTable', {}, locale)
      : translate('indexes.lib.missing.why.allRows', { n: formatCount(rows, intl) }, locale)
  return {
    key: `spatial:${finding.finding_id}`,
    source: 'spatial',
    database: finding.datname ?? '',
    schema,
    table,
    columns: column,
    recommendation: 'check',
    why: translate('indexes.lib.missing.why.spatial', { type, column, reads }, locale),
    command,
    tableBytes: null,
    weight: rows ?? 0,
    finding,
    steps: [
      {
        title: translate('indexes.lib.missing.step.title.check', {}, locale),
        text: translate('indexes.lib.missing.step.checkSpatial', { column }, locale),
        sql: `SELECT calls, round(total_exec_time) AS total_ms, left(query, 200) AS query
FROM pg_stat_statements
WHERE query ILIKE '%${table.replaceAll("'", "''")}%' AND query ILIKE '%${column.replaceAll("'", "''")}%'
ORDER BY total_exec_time DESC LIMIT 20;`,
      },
      createStep(command, locale),
      {
        title: translate('indexes.lib.missing.step.title.verify', {}, locale),
        text: translate('indexes.lib.missing.step.verifySpatial', {}, locale),
        sql: `EXPLAIN (ANALYZE, BUFFERS)
SELECT count(*) FROM ${qualified(schema, table)}
WHERE ${ident(column)} && ST_MakeEnvelope(…); -- an area your queries use`,
      },
      undoStep(schema, name, locale),
    ],
  }
}

function fromScans(item: TableItem, locale: string): Built | null {
  const intl = intlOf(locale)
  const database = str(item, 'datname')
  const schema = str(item, 'schemaname')
  const table = str(item, 'relname')
  const tableBytes = num(item, 'table_bytes')
  const rowsPerSecond = num(item, 'seq_tup_read_rate')
  const scansPerSecond = num(item, 'seq_scan_rate')
  if (
    table === '' ||
    tableBytes === null ||
    tableBytes < SCAN_MIN_TABLE_BYTES ||
    rowsPerSecond === null ||
    rowsPerSecond < SCAN_MIN_ROWS_PER_SECOND
  ) {
    return null
  }
  const perScan =
    scansPerSecond !== null && scansPerSecond > 0
      ? ` ${translate(
          'indexes.lib.missing.why.perScan',
          { n: formatCount(Math.round(rowsPerSecond / scansPerSecond), intl) },
          locale,
        )}`
      : ''
  return {
    key: `scans:${tableKey(database, schema, table)}`,
    source: 'scans',
    database,
    schema,
    table,
    columns: '—',
    recommendation: 'check',
    why: translate(
      'indexes.lib.missing.why.scans',
      {
        rate: formatCount(Math.round(rowsPerSecond), intl),
        size: formatBytes(tableBytes, intl),
        perScan,
      },
      locale,
    ),
    command: null,
    tableBytes,
    weight: rowsPerSecond,
    steps: [
      {
        title: translate('indexes.lib.missing.step.title.findStatements', {}, locale),
        text: translate('indexes.lib.missing.step.findStatements', {}, locale),
        sql: `SELECT queryid, calls, round(total_exec_time) AS total_ms, rows, left(query, 300) AS query
FROM pg_stat_statements
WHERE query ILIKE '%${table.replaceAll("'", "''")}%'
ORDER BY total_exec_time DESC LIMIT 20;`,
      },
      {
        title: translate('indexes.lib.missing.step.title.readPlan', {}, locale),
        text: translate('indexes.lib.missing.step.readPlan', { table }, locale),
        sql: 'EXPLAIN (ANALYZE, BUFFERS) <statement with real values>;',
      },
      {
        title: translate('indexes.lib.missing.step.title.create', {}, locale),
        text: translate('indexes.lib.missing.step.createScan', {}, locale),
        sql: `CREATE INDEX CONCURRENTLY IF NOT EXISTS ${ident(indexName(table, ['col'], 'idx'))} ON ${qualified(schema, table)} (col);`,
      },
      {
        title: translate('indexes.lib.missing.step.title.verify', {}, locale),
        text: translate('indexes.lib.missing.step.verifyScan', {}, locale),
      },
    ],
  }
}

/** The item in `locale`, with its sentence also in English for the psql script. */
function withEnglish(locale: string, build: (locale: string) => Built | null): MissingIndex | null {
  const item = build(locale)
  if (item === null) return null
  const whyEn = locale === 'en' ? item.why : (build('en')?.why ?? item.why)
  return { ...item, whyEn }
}

export interface MissingInput {
  suggestions: readonly IndexSuggestion[]
  findings: readonly Finding[]
  tables: readonly TableItem[]
  /** The database picked on the page; empty for all. */
  database: string
}

/**
 * Every missing index the page can point to, best first: from the statements
 * (index suggestions), the unindexed foreign keys and spatial columns of the
 * advisor, and the tables read by full scans that no suggestion covers.
 * The texts are in `locale`, the console language by default.
 */
export function buildMissingIndexes(
  { suggestions, findings, tables, database }: MissingInput,
  locale: string = activeLocale(),
): MissingIndex[] {
  const inScope = (name: string) => database === '' || name === database
  const rates = new Map<string, TableRates>(
    tables.map((item) => [
      tableKey(str(item, 'datname'), str(item, 'schemaname'), str(item, 'relname')),
      { deleteRate: num(item, 'n_tup_del_rate'), updateRate: num(item, 'n_tup_upd_rate') },
    ]),
  )
  const items: MissingIndex[] = []
  const add = (build: (locale: string) => Built | null) => {
    const item = withEnglish(locale, build)
    if (item !== null) items.push(item)
  }
  for (const row of suggestions) {
    if (inScope(row.database)) add((l) => fromSuggestion(row, l))
  }
  for (const gap of foreignKeyGaps(findings)) {
    if (inScope(gap.datname) && gap.table !== '' && gap.columns !== '') {
      add((l) => fromForeignKey(gap, rates, l))
    }
  }
  for (const finding of findings) {
    if (finding.rule_id !== 'postgis.spatial_unindexed' || finding.state !== 'open') continue
    if (!inScope(finding.datname ?? '')) continue
    add((l) => fromSpatial(finding, l))
  }
  const suggested = new Set(items.map((item) => tableKey(item.database, item.schema, item.table)))
  for (const table of tables) {
    const item = withEnglish(locale, (l) => fromScans(table, l))
    if (item === null || !inScope(item.database)) continue
    if (suggested.has(tableKey(item.database, item.schema, item.table))) continue
    items.push(item)
  }
  return items.sort(
    (a, b) =>
      RECOMMENDATION_ORDER.indexOf(a.recommendation) -
        RECOMMENDATION_ORDER.indexOf(b.recommendation) ||
      b.weight - a.weight ||
      a.key.localeCompare(b.key),
  )
}

export interface MissingSummary {
  total: number
  byRecommendation: Record<Recommendation, number>
  bySource: Record<MissingSource, number>
}

export function summariseMissing(items: readonly MissingIndex[]): MissingSummary {
  const summary: MissingSummary = {
    total: items.length,
    byRecommendation: { create: 0, test: 0, check: 0, skip: 0 },
    bySource: { queries: 0, 'foreign-keys': 0, spatial: 0, scans: 0 },
  }
  for (const item of items) {
    summary.byRecommendation[item.recommendation] += 1
    summary.bySource[item.source] += 1
  }
  return summary
}

/**
 * A psql script of the listed indexes, grouped by database. Create and Test
 * first rows are active; Check workload rows are commented out, to enable one
 * by one after their check; Not worth it rows and rows without a command are
 * left out. pglens never runs it. The script is English whatever the language.
 */
export function missingIndexScript(items: readonly MissingIndex[], server: string): string {
  const lines = [
    `-- Missing indexes for ${server}, generated by pglens. pglens never runs this script.`,
    '-- Run it in psql, off-peak, one statement at a time: CREATE INDEX CONCURRENTLY cannot run',
    '-- inside a transaction block, does not block reads or writes, and needs free disk about',
    '-- the size of the index. If one fails it leaves an INVALID index: drop it and retry.',
    "-- Optional, to give up instead of queueing behind long transactions: SET lock_timeout = '10s';",
  ]
  const usable = items.filter((item) => item.command !== null && item.recommendation !== 'skip')
  const databases = [...new Set(usable.map((item) => item.database))].sort()
  for (const database of databases) {
    lines.push('', `\\connect ${ident(database)}`)
    for (const item of usable.filter((candidate) => candidate.database === database)) {
      lines.push(
        '',
        `-- ${english(RECOMMENDATION_LABEL_KEYS[item.recommendation])} · ${english(SOURCE_LABEL_KEYS[item.source])} · ${item.schema}.${item.table} (${item.columns})`,
        `-- ${item.whyEn}`,
      )
      if (item.recommendation === 'check') {
        lines.push('-- Enable after the check described on the page:', `-- ${item.command}`)
      } else {
        lines.push(item.command!)
      }
    }
  }
  if (usable.length === 0) lines.push('', '-- No index to create in this list.')
  return lines.join('\n')
}
