import type { Schemas } from '@/api/types'
import type { MessageKey } from '@/i18n/messages/en'
import { translate } from '@/i18n/store'
import { formatBytes, formatDecimal, formatMilliseconds, formatRate } from '@/lib/format'
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

export function estimateCallout(): string {
  return translate('indexes.lib.suggestions.estimateCallout')
}

// Maps, not objects: a code such as `constructor` must not read an inherited property.
const NOTE_KEYS = new Map<string, MessageKey>([
  ['small_table', 'indexes.lib.suggestions.note.smallTable'],
  ['low_selectivity', 'indexes.lib.suggestions.note.lowSelectivity'],
  ['width_assumed', 'indexes.lib.suggestions.note.widthAssumed'],
  ['size_unknown', 'indexes.lib.suggestions.note.sizeUnknown'],
  ['column_unknown', 'indexes.lib.suggestions.note.columnUnknown'],
  ['type_not_btree', 'indexes.lib.suggestions.note.typeNotBtree'],
  ['selectivity_unknown', 'indexes.lib.suggestions.note.selectivityUnknown'],
  ['writes_unknown', 'indexes.lib.suggestions.note.writesUnknown'],
])

/** The sentence for a note code; a code this build does not know is shown as it is. */
export function noteLabel(code: string): string {
  const key = NOTE_KEYS.get(code)
  return key === undefined ? code : translate(key)
}

const VERDICT_KEYS = new Map<string, MessageKey>([
  ['strong', 'indexes.lib.suggestions.verdict.strong'],
  ['moderate', 'indexes.lib.suggestions.verdict.moderate'],
  ['write_heavy', 'indexes.lib.suggestions.verdict.writeHeavy'],
])

export function verdictLabel(verdict: string): string {
  const key = VERDICT_KEYS.get(verdict)
  return key === undefined ? verdict : translate(key)
}

/** The rule behind the verdict, shown as help (a documented heuristic, not a measurement). */
export function verdictRule(): string {
  return translate('indexes.lib.suggestions.verdictRule')
}

export function verdictVariant(verdict: string): 'success' | 'warning' | 'secondary' {
  if (verdict === 'strong') return 'success'
  if (verdict === 'write_heavy') return 'warning'
  return 'secondary'
}

/** The estimated index size with a `~` prefix: it is a planner-based estimate (I-5). */
export function estimateLabel(bytes: number | null): string {
  if (bytes === null) return translate('indexes.lib.suggestions.unknown')
  return translate('indexes.lib.suggestions.estimate', { size: formatBytes(bytes) })
}

export function keyText(columns: IndexSuggestion['columns']): string {
  return columns.map((column) => (column.desc ? `${column.name} DESC` : column.name)).join(', ')
}

export function tableText(suggestion: IndexSuggestion): string {
  return `${suggestion.database} · ${suggestion.schema}.${suggestion.table}`
}

/** A rate that is not zero is never shown as 0. */
function rateText(rate: number): string {
  return rate > 0 && rate < 0.01 ? '<0.01/s' : formatRate(rate)
}

/** Inserts, updates and deletes per second, in that order. */
export function writesText(write: IndexSuggestion['write']): string {
  return [write.insert_rate, write.update_rate, write.delete_rate].map(rateText).join(' · ')
}

/** Statement time per second; a cost that is not zero is never shown as 0. */
export function costText(msPerSecond: number): string {
  if (msPerSecond > 0 && msPerSecond < 0.001) return '<0.001 ms/s'
  return `${formatMilliseconds(msPerSecond)}/s`
}

export function weightText(weightPct: number): string {
  return `${formatDecimal(weightPct, weightPct >= 10 ? 0 : 1)}%`
}

const SKIP_KEYS = new Map<string, MessageKey>([
  ['no_text', 'indexes.lib.suggestions.skip.noText'],
  ['or_group', 'indexes.lib.suggestions.skip.orGroup'],
  ['expression', 'indexes.lib.suggestions.skip.expression'],
  ['unresolved_alias', 'indexes.lib.suggestions.skip.unresolvedAlias'],
  ['parse_error', 'indexes.lib.suggestions.skip.parseError'],
  ['unsupported_statement', 'indexes.lib.suggestions.skip.unsupportedStatement'],
  ['catalog', 'indexes.lib.suggestions.skip.catalog'],
  ['not_table', 'indexes.lib.suggestions.skip.notTable'],
  ['index_unparsed', 'indexes.lib.suggestions.skip.indexUnparsed'],
  ['small_table', 'indexes.lib.suggestions.skip.smallTable'],
])

function skipLabel(reason: string): string {
  const key = SKIP_KEYS.get(reason)
  return key === undefined ? reason : translate(key)
}

/** `Analysed N of M statements. Skipped (a statement can have several reasons): 3 small table, 1 OR condition.` */
export function coverageText(coverage: SuggestionCoverage): string {
  const skipped = Object.entries(coverage.skipped)
    .filter(([, count]) => count > 0)
    .sort(([a, x], [b, y]) => y - x || a.localeCompare(b))
  const head = translate('indexes.lib.suggestions.coverage.head', {
    count: coverage.statements_considered,
    analysed: String(coverage.statements_analysed),
    considered: String(coverage.statements_considered),
  })
  if (skipped.length === 0) return head
  const reasons = skipped.map(([reason, count]) => `${count} ${skipLabel(reason)}`)
  return `${head} ${translate('indexes.lib.suggestions.coverage.skipped', { reasons: reasons.join(', ') })}`
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
export function statementLink(instanceId: string, queryid: string): string {
  return `/instances/${encodeURIComponent(instanceId)}/queries/${encodeURIComponent(queryid)}`
}

export type IndexValidation = NonNullable<IndexSuggestion['validation']>

/** The arguments of the index_check command for a suggestion: its key and its heaviest statement. */
export function indexCheckArgs(row: IndexSuggestion) {
  const top = row.statements.reduce<IndexSuggestion['statements'][number] | undefined>(
    (best, statement) =>
      best === undefined || statement.share_pct > best.share_pct ? statement : best,
    undefined,
  )
  if (top === undefined) return null
  return {
    queryid: top.queryid,
    datname: row.database,
    index_schema: row.schema,
    index_relation: row.table,
    index_columns: row.columns.map((column) => column.name),
    index_include: [] as string[],
  }
}

function numberField(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

/**
 * The validation shown after a finished index_check command, in the shape the
 * suggestions endpoint stores. Null when the result is not an index_check result.
 */
export function validationFromResult(
  result: unknown,
  queryid: string,
  validatedAt: string,
  approximate: boolean,
): IndexValidation | null {
  if (typeof result !== 'object' || result === null) return null
  const record = result as Record<string, unknown>
  const before = numberField(record.cost_before)
  const after = numberField(record.cost_after)
  if (before === null || after === null || typeof record.index_used !== 'boolean') return null
  return {
    queryid,
    index_used: record.index_used,
    cost_before: before,
    cost_after: after,
    cost_ratio: before > 0 ? after / before : null,
    index_size_bytes: numberField(record.index_size_bytes),
    validated_at: validatedAt,
    approximate,
  }
}

/** Whether a key has a descending column: hypopg validates the ascending order. */
export function validationApproximate(row: IndexSuggestion): boolean {
  return row.columns.some((column) => column.desc)
}

export function validationVerdict(validation: IndexValidation): string {
  return translate(
    validation.index_used
      ? 'indexes.lib.validation.wouldUse'
      : 'indexes.lib.validation.wouldNotUse',
  )
}

export function validationCostText(validation: IndexValidation): string {
  const costs = translate('indexes.lib.validation.costs', {
    before: formatDecimal(validation.cost_before, 1),
    after: formatDecimal(validation.cost_after, 1),
  })
  return validation.cost_ratio === null
    ? costs
    : `${costs} ${translate('indexes.lib.validation.ratio', { ratio: formatDecimal(validation.cost_ratio, 2) })}`
}

export function validationSizeText(validation: IndexValidation): string {
  return validation.index_size_bytes === null ? '—' : formatBytes(validation.index_size_bytes)
}

/**
 * The text for a failed index_check: the install hint when hypopg is missing,
 * else the agent's error.
 */
export function validationErrorText(error: string | null | undefined, database: string): string {
  if (error?.startsWith('hypopg_missing:')) {
    return translate('indexes.lib.validation.hypopgMissing', { database })
  }
  return error ?? ''
}
