import type { Workbook, WorkbookVariant } from '@/api/workbooks'
import type { MessageKey } from '@/i18n/messages/en'

/**
 * The settings a variant may change, with the same rules as the server
 * (`internal/command/workbook.go`, `WorkbookSettings`). TestWBK014 pins the
 * server's names to the OpenAPI enum; UI-WBK-002 pins this list to it.
 */
export type SettingKind = 'bool' | 'kb' | 'decimal' | 'int' | 'enum'

export interface SettingRule {
  name: string
  kind: SettingKind
  min?: number
  max?: number
  unit?: string
  options?: readonly string[]
}

const BOOL_SETTINGS = [
  'enable_seqscan',
  'enable_indexscan',
  'enable_indexonlyscan',
  'enable_bitmapscan',
  'enable_tidscan',
  'enable_nestloop',
  'enable_hashjoin',
  'enable_mergejoin',
  'enable_hashagg',
  'enable_sort',
  'enable_incremental_sort',
  'enable_material',
  'enable_memoize',
  'enable_gathermerge',
  'enable_parallel_append',
  'enable_parallel_hash',
  'enable_partition_pruning',
  'enable_partitionwise_join',
  'enable_partitionwise_aggregate',
  'jit',
] as const

const COST_SETTINGS = [
  'random_page_cost',
  'seq_page_cost',
  'cpu_tuple_cost',
  'cpu_index_tuple_cost',
  'cpu_operator_cost',
] as const

export const WORKBOOK_SETTINGS: readonly SettingRule[] = [
  ...BOOL_SETTINGS.map((name): SettingRule => ({ name, kind: 'bool', options: ['on', 'off'] })),
  { name: 'work_mem', kind: 'kb', min: 64, max: 262144, unit: 'kB' },
  { name: 'effective_cache_size', kind: 'kb', min: 8, max: 1073741824, unit: 'kB' },
  ...COST_SETTINGS.map((name): SettingRule => ({
    name,
    kind: 'decimal',
    min: 0,
    max: 9999.999999,
  })),
  { name: 'hash_mem_multiplier', kind: 'decimal', min: 1, max: 1000 },
  { name: 'join_collapse_limit', kind: 'int', min: 1, max: 100 },
  { name: 'from_collapse_limit', kind: 'int', min: 1, max: 100 },
  { name: 'max_parallel_workers_per_gather', kind: 'int', min: 0, max: 8 },
  {
    name: 'plan_cache_mode',
    kind: 'enum',
    options: ['auto', 'force_generic_plan', 'force_custom_plan'],
  },
]

export const MAX_SETTINGS = 16
export const MAX_HINTS_BYTES = 1024
export const MAX_PARAMS = 100
export const MAX_PARAM_BYTES = 8 << 10
export const BASELINE = 'Baseline'
/** The characters a hint may use, as the server checks them. */
export const HINT_CHARACTERS = 'A-Z a-z 0-9 space _ , ( ) # + - . "'

const WHOLE = /^[0-9]{1,10}$/
const DECIMAL = /^[0-9]{1,4}(\.[0-9]{1,6})?$/
const HINTS = /^[A-Za-z0-9 _,()#+\-."]*$/
const SET_HINT = /\bset\s*\(/i

export function settingRule(name: string): SettingRule | undefined {
  return WORKBOOK_SETTINGS.find((rule) => rule.name === name)
}

export interface Problem {
  field: 'name' | 'settings' | 'hints' | 'params'
  key: MessageKey
  values?: Record<string, string | number>
}

/** Why a setting value is refused, or null when the server would accept it. */
export function settingProblem(name: string, value: string): Problem | null {
  const rule = settingRule(name)
  if (!rule) return { field: 'settings', key: 'workbook.problem.settingUnknown', values: { name } }
  if (rule.options) {
    return rule.options.includes(value)
      ? null
      : {
          field: 'settings',
          key: 'workbook.problem.settingChoice',
          values: { name, choices: rule.options.join(', ') },
        }
  }
  const pattern = rule.kind === 'decimal' ? DECIMAL : WHOLE
  const number = Number(value)
  if (!pattern.test(value) || number < (rule.min ?? 0) || number > (rule.max ?? Infinity)) {
    return {
      field: 'settings',
      key: 'workbook.problem.settingRange',
      values: { name, min: rule.min ?? 0, max: rule.max ?? 0, unit: rule.unit ?? '' },
    }
  }
  return null
}

/** Why a hint text is refused, or null. */
export function hintsProblem(hints: string): Problem | null {
  if (new TextEncoder().encode(hints).length > MAX_HINTS_BYTES)
    return { field: 'hints', key: 'workbook.problem.hintsLength', values: { max: MAX_HINTS_BYTES } }
  if (!HINTS.test(hints))
    return {
      field: 'hints',
      key: 'workbook.problem.hintsCharacters',
      values: { allowed: HINT_CHARACTERS },
    }
  if (SET_HINT.test(hints)) return { field: 'hints', key: 'workbook.problem.hintsSet' }
  return null
}

export interface VariantDraft {
  name: string
  settings: Record<string, string>
  hints: string
  params: (string | null)[]
  analyze: boolean
}

/** Every reason the server would refuse the draft, checked before sending. */
export function draftProblems(draft: VariantDraft): Problem[] {
  const problems: Problem[] = []
  const name = draft.name.trim()
  if (name === '' || [...draft.name].length > 120)
    problems.push({ field: 'name', key: 'workbook.problem.name', values: { max: 120 } })
  const settings = Object.entries(draft.settings)
  if (settings.length > MAX_SETTINGS)
    problems.push({
      field: 'settings',
      key: 'workbook.problem.settingsCount',
      values: { max: MAX_SETTINGS },
    })
  for (const [setting, value] of settings) {
    const problem = settingProblem(setting, value)
    if (problem) problems.push(problem)
  }
  const hints = hintsProblem(draft.hints)
  if (hints) problems.push(hints)
  if (draft.params.length > MAX_PARAMS)
    problems.push({
      field: 'params',
      key: 'workbook.problem.paramsCount',
      values: { max: MAX_PARAMS },
    })
  draft.params.forEach((param, index) => {
    if (param !== null && new TextEncoder().encode(param).length > MAX_PARAM_BYTES)
      problems.push({
        field: 'params',
        key: 'workbook.problem.paramLength',
        values: { index: index + 1, max: MAX_PARAM_BYTES },
      })
  })
  return problems
}

export function draftOf(variant?: WorkbookVariant): VariantDraft {
  return {
    name: variant?.name ?? '',
    settings: { ...(variant?.settings ?? {}) },
    hints: variant?.hints ?? '',
    params: [...(variant?.params ?? [])],
    analyze: variant?.analyze ?? false,
  }
}

/** The baseline a workbook's ratios are measured against: its first variant named Baseline. */
export function baselineOf(workbook: Workbook): WorkbookVariant | undefined {
  return (
    workbook.variants.find((variant) => variant.name === BASELINE && variant.position === 0) ??
    workbook.variants.find((variant) => variant.name === BASELINE)
  )
}

/** value / baseline, or null when either is missing or the baseline is zero. */
export function ratio(value: number | null | undefined, base: number | null | undefined) {
  if (value == null || base == null || base === 0) return null
  return value / base
}

export interface VariantRow {
  variant: WorkbookVariant
  isBaseline: boolean
  costRatio: number | null
  timeRatio: number | null
}

/**
 * The rows of the variants table in position order. The time ratio compares
 * execution time when both ran with ANALYZE, else planning time.
 */
export function variantRows(workbook: Workbook): VariantRow[] {
  const base = baselineOf(workbook)
  return [...workbook.variants]
    .sort((left, right) => left.position - right.position)
    .map((variant) => {
      const isBaseline = variant.variant_id === base?.variant_id
      const done = variant.status === 'done' && base?.status === 'done'
      const time =
        variant.execution_ms != null && base?.execution_ms != null
          ? ratio(variant.execution_ms, base.execution_ms)
          : ratio(variant.planning_ms, base?.planning_ms)
      return {
        variant,
        isBaseline,
        costRatio: done && !isBaseline ? ratio(variant.total_cost, base?.total_cost) : null,
        timeRatio: done && !isBaseline ? time : null,
      }
    })
}

/** True when the error says pg_hint_plan is not loaded or is switched off. */
export function isHintPlanProblem(error: string | undefined | null): boolean {
  return Boolean(error && /hint_plan_(inactive|disabled)/.test(error))
}

/** The variants a "Run all" queues: every one not already pending, in table order. */
export function runnableIds(workbook: Workbook): string[] {
  return [...workbook.variants]
    .sort((left, right) => left.position - right.position)
    .filter((variant) => variant.status !== 'pending')
    .map((variant) => variant.variant_id)
}

/** The console path of a workbook page. */
export function workbookPath(instanceId: string, queryid: string, workbookId: string): string {
  return `/instances/${encodeURIComponent(instanceId)}/queries/${encodeURIComponent(queryid)}/workbooks/${encodeURIComponent(workbookId)}`
}
