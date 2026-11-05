import type { Schemas } from '@/api/types'
import { formatBytes, formatRate } from '@/lib/format'
import type { SortDirection } from '@/lib/tableControls'

export type IndexSuggestion = Schemas['IndexSuggestion']
export type IndexSuggestionsResponse = Schemas['IndexSuggestionsResponse']
export type SuggestionCoverage = Schemas['IndexSuggestionCoverage']

/** The windows the Suggestions tab offers; the API accepts 1h to 7d. */
export const SUGGESTION_WINDOWS = ['1h', '6h', '24h', '7d'] as const
export type SuggestionWindow = (typeof SUGGESTION_WINDOWS)[number]
export const DEFAULT_SUGGESTION_WINDOW: SuggestionWindow = '24h'
/** The most suggestions the tab asks for. */
export const SUGGESTION_LIMIT = 100

export function readSuggestionWindow(value: string | null): SuggestionWindow {
  return SUGGESTION_WINDOWS.find((window) => window === value) ?? DEFAULT_SUGGESTION_WINDOW
}

export const ESTIMATE_CALLOUT =
  'Suggestions are estimates from statement statistics; pglens never creates an index.'

const NOTE_LABELS: Readonly<Record<string, string>> = {
  small_table:
    'The table has fewer than 1,000 rows: a sequential scan is usually as fast as an index.',
  low_selectivity:
    'The first key column has few distinct values, so the index narrows little: the verdict is one step lower.',
  width_assumed:
    'Some column widths are not reported: 32 bytes were assumed for each, so the size is rougher.',
  size_unknown: 'The table size is not reported, so no size estimate is given.',
  column_unknown: 'A column of the statement is not in the reported table columns.',
  type_not_btree: 'A column type cannot be indexed with a btree and was left out of the key.',
  selectivity_unknown:
    'Column statistics are not available, so selectivity is unknown and the confidence is lowered.',
  writes_unknown:
    'No write rates are reported for the table: the read/write ratio is not known and the verdict is capped.',
}

/** The sentence for a note code; a code this build does not know is shown as it is. */
export function noteLabel(code: string): string {
  return NOTE_LABELS[code] ?? code
}

const VERDICT_LABELS: Readonly<Record<string, string>> = {
  strong: 'Strong',
  moderate: 'Moderate',
  write_heavy: 'Write-heavy',
}

export function verdictLabel(verdict: string): string {
  return VERDICT_LABELS[verdict] ?? verdict
}

/** The rule behind the verdict, shown as help (a documented heuristic, not a measurement). */
export const VERDICT_RULE =
  'Verdict is read/write ratio of the table: calls per second of the statements that use the key divided by the row changes per second that must touch an index (inserts, deletes, and updates that are not heap-only). 10 or more is strong, 1 or more is moderate, below 1 is write-heavy.'

export function verdictVariant(verdict: string): 'success' | 'warning' | 'secondary' {
  if (verdict === 'strong') return 'success'
  if (verdict === 'write_heavy') return 'warning'
  return 'secondary'
}

/** The estimated index size with a `~` prefix: it is a planner-based estimate (I-5). */
export function estimateLabel(bytes: number | null): string {
  return bytes === null ? 'Unknown' : `~${formatBytes(bytes)}`
}

export function keyText(columns: IndexSuggestion['columns']): string {
  return columns.map((column) => (column.desc ? `${column.name} DESC` : column.name)).join(', ')
}

export function tableText(suggestion: IndexSuggestion): string {
  return `${suggestion.database} · ${suggestion.schema}.${suggestion.table}`
}

/** Inserts, updates and deletes per second, in that order. */
export function writesText(write: IndexSuggestion['write']): string {
  return [write.insert_rate, write.update_rate, write.delete_rate]
    .map((rate) => formatRate(rate))
    .join(' · ')
}

export function weightText(weightPct: number): string {
  return `${weightPct.toFixed(weightPct >= 10 ? 0 : 1)}%`
}

const SKIP_LABELS: Readonly<Record<string, string>> = {
  no_text: 'no stored text',
  or_group: 'OR condition',
  expression: 'column inside an expression',
  unresolved_alias: 'unresolved alias',
  parse_error: 'could not be parsed',
  unsupported_statement: 'not a query on a table',
  catalog: 'system catalogs only',
  small_table: 'small table',
}

/** `Analysed N of M statements (K skipped: 3 small table, 1 OR condition).` */
export function coverageText(coverage: SuggestionCoverage): string {
  const skipped = Object.entries(coverage.skipped)
    .filter(([, count]) => count > 0)
    .sort(([a, x], [b, y]) => y - x || a.localeCompare(b))
  const total = skipped.reduce((sum, [, count]) => sum + count, 0)
  const head = `Analysed ${coverage.statements_analysed} of ${coverage.statements_considered} statements`
  if (skipped.length === 0) return `${head}.`
  const reasons = skipped.map(([reason, count]) => `${count} ${SKIP_LABELS[reason] ?? reason}`)
  return `${head} (${total} skipped: ${reasons.join(', ')}).`
}

/** No statement in the window: the statistics are not collected, as opposed to nothing to suggest. */
export function statisticsMissing(response: IndexSuggestionsResponse): boolean {
  return response.coverage.statements_considered === 0
}

export function matchesSuggestion(suggestion: IndexSuggestion, needle: string): boolean {
  const term = needle.trim().toLowerCase()
  if (term === '') return true
  return [
    suggestion.table,
    suggestion.schema,
    suggestion.database,
    keyText(suggestion.columns),
    ...suggestion.statements.map((statement) => statement.query_text),
  ].some((text) => text.toLowerCase().includes(term))
}

export type SuggestionSortKey =
  'table' | 'key' | 'weight' | 'cost' | 'size' | 'writes' | 'verdict' | 'statements'

const VERDICT_ORDER: Readonly<Record<string, number>> = { strong: 3, moderate: 2, write_heavy: 1 }

function writeTotal(write: IndexSuggestion['write']): number {
  return write.insert_rate + write.update_rate + write.delete_rate
}

/** Orders two suggestions; an unknown size sorts below every known one in both directions. */
export function compareSuggestions(
  a: IndexSuggestion,
  b: IndexSuggestion,
  key: SuggestionSortKey,
  direction: SortDirection,
): number {
  const sign = direction === 'desc' ? -1 : 1
  if (key === 'size') {
    if (a.est_size_bytes === null && b.est_size_bytes === null) return 0
    if (a.est_size_bytes === null) return 1
    if (b.est_size_bytes === null) return -1
    return sign * (a.est_size_bytes - b.est_size_bytes)
  }
  switch (key) {
    case 'table':
      return sign * tableText(a).localeCompare(tableText(b))
    case 'key':
      return sign * keyText(a.columns).localeCompare(keyText(b.columns))
    case 'weight':
      return sign * (a.weight_pct - b.weight_pct)
    case 'cost':
      return sign * (a.read_cost_ms_per_s - b.read_cost_ms_per_s)
    case 'writes':
      return sign * (writeTotal(a.write) - writeTotal(b.write))
    case 'verdict':
      return sign * ((VERDICT_ORDER[a.verdict] ?? 0) - (VERDICT_ORDER[b.verdict] ?? 0))
    case 'statements':
      return sign * (a.statements.length - b.statements.length)
  }
}

/** The query detail page of one statement. */
export function statementLink(instanceId: string, queryid: number): string {
  return `/instances/${encodeURIComponent(instanceId)}/queries/${encodeURIComponent(String(queryid))}`
}
