import { useMemo } from 'react'

import type { TableState } from '@/hooks/useTableState'
import {
  activeFilterIds,
  applyFilters,
  filterOptions,
  matchesSearch,
  type FilterOption,
  type FilterSpec,
  type SearchField,
} from '@/lib/tableControls'

/** A table's search box and filters (D12). */
export interface TableToolbarSpec<T> {
  /** Fields the search box matches (case-insensitive substring). */
  searchFields: readonly SearchField<T>[]
  filters?: readonly FilterSpec<T>[]
  /** Overrides the `Search <table>` label. */
  searchLabel?: string
}

export interface FilteredRows<T> {
  /** The rows kept by the search and the filters, in input order. */
  filtered: readonly T[]
  /** The choices of each select/multi filter, by filter id. */
  options: Record<string, FilterOption[]>
  /** Active filters, the search counting as one; unknown ids and unusable values do not count. */
  activeFilterCount: number
  filters: readonly FilterSpec<T>[]
}

const NO_FILTERS: readonly FilterSpec<never>[] = []

/**
 * Applies a table's search and filters to its loaded rows before they reach
 * the table (D12). Specs must be module constants or memoised: the result is
 * memoised on them.
 */
export function useFilteredRows<T>(
  data: readonly T[],
  state: TableState,
  toolbar: TableToolbarSpec<T> | undefined,
): FilteredRows<T> {
  const filters = (toolbar?.filters ?? NO_FILTERS) as readonly FilterSpec<T>[]
  const searchFields = toolbar?.searchFields
  const filtersKey = JSON.stringify(state.filters)

  const filtered = useMemo(() => {
    if (!searchFields) return data
    const values = JSON.parse(filtersKey) as Record<string, string>
    const searched = data.filter((row) => matchesSearch(row, state.q, searchFields))
    return applyFilters(searched, filters, values)
  }, [data, searchFields, filters, state.q, filtersKey])

  const options = useMemo(() => {
    const byId: Record<string, FilterOption[]> = {}
    for (const spec of filters) {
      if (spec.kind === 'min') continue
      byId[spec.id] =
        spec.options?.slice() ?? filterOptions(data, spec).map((value) => ({ value, label: value }))
    }
    return byId
  }, [data, filters])

  const activeFilterCount =
    (toolbar && state.q.trim() !== '' ? 1 : 0) +
    activeFilterIds(filters, JSON.parse(filtersKey) as Record<string, string>).length

  return { filtered, options, activeFilterCount, filters }
}
