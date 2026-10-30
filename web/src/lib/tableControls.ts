/**
 * Pure table controls shared by `DataTable` and the bespoke tables (D12, D23):
 * search and filters run on the loaded rows before they reach a table, then
 * sorting, then pagination. Nothing here knows about React or the URL.
 */

export type SortDirection = 'asc' | 'desc'

/** A cell value as tables compare it. */
export type Comparable = string | number | boolean | null | undefined

export const PAGE_SIZES = [20, 50, 100] as const
export const DEFAULT_PAGE_SIZE = 20

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })

function isMissing(value: Comparable): value is null | undefined {
  return value === null || value === undefined || (typeof value === 'number' && Number.isNaN(value))
}

/**
 * Compare two cell values: numbers numerically, booleans false before true,
 * everything else as natural, case-insensitive text. Missing values (null,
 * undefined, NaN) sort last in both directions.
 */
export function compareValues(a: Comparable, b: Comparable, dir: SortDirection): number {
  const aMissing = isMissing(a)
  const bMissing = isMissing(b)
  if (aMissing || bMissing) return aMissing === bMissing ? 0 : aMissing ? 1 : -1

  let result: number
  if (typeof a === 'number' && typeof b === 'number') result = a - b
  else if (typeof a === 'boolean' && typeof b === 'boolean') result = Number(a) - Number(b)
  else result = collator.compare(String(a), String(b))
  return dir === 'asc' ? result : -result
}

export type Accessor<T> = (row: T) => Comparable

/** Rows sorted by `accessors[key]`; ties keep their input order. Unknown or no key: a copy as is. */
export function sortRows<T>(
  rows: readonly T[],
  key: string | null,
  dir: SortDirection,
  accessors: Readonly<Record<string, Accessor<T>>>,
): T[] {
  const accessor = key === null ? undefined : accessors[key]
  if (!accessor) return [...rows]
  return rows
    .map((row, index) => ({ row, index, value: accessor(row) }))
    .sort((x, y) => compareValues(x.value, y.value, dir) || x.index - y.index)
    .map(({ row }) => row)
}

export type SearchField<T> = (row: T) => Comparable

/** True when the trimmed query is a case-insensitive substring of any field; an empty query matches. */
export function matchesSearch<T>(row: T, q: string, fields: readonly SearchField<T>[]): boolean {
  const query = q.trim().toLocaleLowerCase()
  if (query === '') return true
  return fields.some((field) => {
    const value = field(row)
    if (value === null || value === undefined) return false
    return String(value).toLocaleLowerCase().includes(query)
  })
}

export interface FilterOption {
  value: string
  label: string
}

/**
 * One filter control of a table. `select` keeps rows whose value equals the
 * choice, `multi` rows whose value is one of the comma-separated choices,
 * `min` rows whose numeric value is at least the threshold (rows without a
 * value are dropped). `predicate` replaces the default test.
 */
export interface FilterSpec<T> {
  id: string
  label: string
  kind: 'select' | 'multi' | 'min'
  options?: readonly FilterOption[]
  accessor: Accessor<T>
  predicate?: (row: T, value: string) => boolean
}

function keeps<T>(row: T, spec: FilterSpec<T>, value: string): boolean {
  if (spec.predicate) return spec.predicate(row, value)
  const cell = spec.accessor(row)
  switch (spec.kind) {
    case 'select':
      return !isMissing(cell) && String(cell) === value
    case 'multi': {
      const choices = new Set(value.split(',').filter(Boolean))
      return !isMissing(cell) && choices.has(String(cell))
    }
    case 'min': {
      const threshold = Number(value)
      return typeof cell === 'number' && !Number.isNaN(cell) && cell >= threshold
    }
  }
}

function isActive<T>(spec: FilterSpec<T>, value: string | undefined): value is string {
  if (value === undefined || value.trim() === '') return false
  if (spec.kind === 'min') return Number.isFinite(Number(value))
  if (spec.kind === 'multi') return value.split(',').some(Boolean)
  return true
}

/** Ids of the filters whose value is in effect (unknown ids and unusable values are not). */
export function activeFilterIds<T>(
  specs: readonly FilterSpec<T>[],
  values: Readonly<Record<string, string>>,
): string[] {
  return specs.filter((spec) => isActive(spec, values[spec.id])).map((spec) => spec.id)
}

/** Rows kept by every active filter; values of unknown or empty filters are ignored. */
export function applyFilters<T>(
  rows: readonly T[],
  specs: readonly FilterSpec<T>[],
  values: Readonly<Record<string, string>>,
): T[] {
  const active = specs.filter((spec) => isActive(spec, values[spec.id]))
  if (active.length === 0) return [...rows]
  return rows.filter((row) => active.every((spec) => keeps(row, spec, values[spec.id] ?? '')))
}

/** The choices of a select/multi filter: its fixed options, else the distinct loaded values. */
export function filterOptions<T>(rows: readonly T[], spec: FilterSpec<T>): string[] {
  if (spec.options) return spec.options.map((option) => option.value)
  const values = new Set<string>()
  for (const row of rows) {
    const value = spec.accessor(row)
    if (!isMissing(value)) values.add(String(value))
  }
  return [...values].sort((a, b) => collator.compare(a, b))
}

export interface Page<T> {
  rows: T[]
  /** 1-based, clamped to `[1, pageCount]`. */
  page: number
  pageCount: number
  /** 1-based index of the first row shown, 0 when there is none. */
  from: number
  to: number
  total: number
}

/** One page of `rows`; an out-of-range page is clamped, an empty set is one empty page. */
export function paginate<T>(rows: readonly T[], page: number, pageSize: number): Page<T> {
  const size = Math.max(1, Math.floor(pageSize))
  const total = rows.length
  const pageCount = Math.max(1, Math.ceil(total / size))
  const current = Number.isFinite(page) ? Math.min(Math.max(1, Math.floor(page)), pageCount) : 1
  const start = (current - 1) * size
  const slice = rows.slice(start, start + size)
  return {
    rows: slice,
    page: current,
    pageCount,
    from: slice.length === 0 ? 0 : start + 1,
    to: start + slice.length,
    total,
  }
}
