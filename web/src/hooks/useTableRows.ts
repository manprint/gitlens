import { useMemo } from 'react'

import { useFilteredRows, type FilteredRows, type TableToolbarSpec } from '@/hooks/useFilteredRows'
import { useTableState, type TableDefaults, type TableStateControls } from '@/hooks/useTableState'
import {
  paginate,
  sortRows,
  type Accessor,
  type Page,
  type SortDirection,
} from '@/lib/tableControls'

export interface TableRows<T> {
  controls: TableStateControls
  filtered: readonly T[]
  page: Page<T>
  /** The column the rows are sorted by, when it exists and is sortable. */
  sortId: string | null
  options: FilteredRows<T>['options']
  activeFilterCount: number
  filters: FilteredRows<T>['filters']
}

/**
 * Search, filters, sort and page of a bespoke table, its state in the URL
 * under `<tableId>.*` (D12, D23). `accessors` maps sortable column ids to raw
 * values (a key outside it is ignored); it, the toolbar spec and `compare` must
 * be stable (module constants or memoised).
 */
export function useTableRows<T>(
  rows: readonly T[],
  tableId: string,
  toolbar: TableToolbarSpec<T> | undefined,
  accessors: Readonly<Record<string, Accessor<T>>>,
  defaults: TableDefaults = {},
  /** A domain comparator that stays the source of truth for the order (e.g. `compareDeadRows`). */
  compare?: (a: T, b: T, key: string, direction: SortDirection) => number,
): TableRows<T> {
  const controls = useTableState(tableId, defaults)
  const { state } = controls
  const { filtered, options, activeFilterCount, filters } = useFilteredRows(rows, state, toolbar)
  const sortId = state.sort !== null && accessors[state.sort] ? state.sort : null
  const sorted = useMemo(() => {
    if (sortId === null) return filtered
    if (compare) return [...filtered].sort((a, b) => compare(a, b, sortId, state.dir))
    return sortRows(filtered, sortId, state.dir, accessors)
  }, [accessors, compare, filtered, sortId, state.dir])
  const page = paginate(sorted, state.page, state.size)
  return {
    controls: { ...controls, activeFilterCount },
    filtered,
    page,
    sortId,
    options,
    activeFilterCount,
    filters,
  }
}
