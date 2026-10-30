import { useMemo, type ReactNode } from 'react'
import {
  columnVisibilityFeature,
  flexRender,
  rowSortingFeature,
  tableFeatures,
  useTable,
  type ColumnDef,
  type RowData,
  type SortingState,
  type Updater,
} from '@tanstack/react-table'
import { Columns3 } from 'lucide-react'

import {
  useLocalTableState,
  useTableState,
  type TableDefaults,
  type TableStateControls,
} from '@/hooks/useTableState'
import {
  applyFilters,
  filterOptions,
  activeFilterIds,
  matchesSearch,
  PAGE_SIZES,
  paginate,
  sortRows,
  type Comparable,
  type FilterOption,
  type FilterSpec,
  type SearchField,
} from '@/lib/tableControls'

import { Truncated } from '../state/Truncated'
import { SortButtons } from './SortButtons'
import { TablePagination } from './TablePagination'
import { TableToolbar } from './TableToolbar'

const dataTableFeatures = tableFeatures({
  columnVisibilityFeature,
  rowSortingFeature,
})

export interface DataTableToolbar<TData> {
  /** Fields the search box matches (case-insensitive substring). */
  searchFields: readonly SearchField<TData>[]
  filters?: readonly FilterSpec<TData>[]
  /** Overrides the `Search <table>` label. */
  searchLabel?: string
}

export interface DataTableProps<TData extends RowData> {
  columns: ColumnDef<typeof dataTableFeatures, TData>[]
  data: TData[]
  emptyState: ReactNode
  /** The full result size. Truncation is reported only when it exceeds the rows shown. */
  total?: number
  /** The server's row budget. It reports truncation only together with total. */
  budget?: number
  scope?: string
  ariaLabel?: string
  /** Search box and filters above the table (D12). */
  toolbar?: DataTableToolbar<TData>
  /** URL prefix of the table state (`<tableId>.q`, `.sort`, `.page`, …); without it the state is local. */
  tableId?: string
  /** Order when the URL names none. */
  defaultSort?: { id: string; desc: boolean }
}

function renderPrimitiveValue(value: unknown): string {
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
    return String(value)
  }
  return ''
}

function comparable(value: unknown): Comparable {
  if (value === null || value === undefined) return value
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
    return value
  }
  // Dates sort by time; other objects have no order and sort with the missing values.
  return value instanceof Date ? value.getTime() : null
}

function defaultsOf(defaultSort: DataTableProps<RowData>['defaultSort']): TableDefaults {
  return defaultSort ? { sort: defaultSort.id, dir: defaultSort.desc ? 'desc' : 'asc' } : {}
}

/**
 * A searchable, filterable, sortable table paginated at 20 rows (D12, D13,
 * D23). With `tableId` its state lives in the URL, otherwise in the
 * component. The two variants are separate components so that no hook is
 * ever called conditionally.
 */
export function DataTable<TData extends RowData>(props: DataTableProps<TData>) {
  return props.tableId === undefined ? (
    <DataTableLocal {...props} />
  ) : (
    <DataTableUrl {...props} tableId={props.tableId} />
  )
}

function DataTableUrl<TData extends RowData>(props: DataTableProps<TData> & { tableId: string }) {
  const controls = useTableState(props.tableId, defaultsOf(props.defaultSort))
  return <DataTableView {...props} controls={controls} />
}

function DataTableLocal<TData extends RowData>(props: DataTableProps<TData>) {
  const controls = useLocalTableState(defaultsOf(props.defaultSort))
  return <DataTableView {...props} controls={controls} />
}

function DataTableView<TData extends RowData>({
  columns,
  data,
  emptyState,
  total = data.length,
  budget,
  scope = 'rows',
  ariaLabel = 'Data table',
  toolbar,
  controls,
}: DataTableProps<TData> & { controls: TableStateControls }) {
  const { state, set, clear } = controls
  const filterSpecs = useMemo(() => toolbar?.filters ?? [], [toolbar?.filters])
  const filtersKey = JSON.stringify(state.filters)

  const searchFields = toolbar?.searchFields
  const filtered = useMemo(() => {
    if (!searchFields) return data
    const searched = data.filter((row) => matchesSearch(row, state.q, searchFields))
    return applyFilters(searched, filterSpecs, JSON.parse(filtersKey) as Record<string, string>)
  }, [data, searchFields, filterSpecs, state.q, filtersKey])
  // Only what applyFilters applies: unknown ids and unusable values in the URL do not count.
  const activeFilterCount =
    (toolbar && state.q.trim() !== '' ? 1 : 0) +
    activeFilterIds(filterSpecs, JSON.parse(filtersKey) as Record<string, string>).length

  const options = useMemo(() => {
    const byId: Record<string, FilterOption[]> = {}
    for (const spec of filterSpecs) {
      if (spec.kind === 'min') continue
      byId[spec.id] =
        spec.options?.slice() ?? filterOptions(data, spec).map((value) => ({ value, label: value }))
    }
    return byId
  }, [data, filterSpecs])

  const sorting: SortingState =
    state.sort === null ? [] : [{ id: state.sort, desc: state.dir === 'desc' }]
  const table = useTable({
    features: dataTableFeatures,
    columns,
    data: filtered,
    manualSorting: true,
    // One sort column: the URL holds one, so a shift-click must not start a multi-sort.
    enableMultiSort: false,
    state: { sorting },
    onSortingChange: (updater: Updater<SortingState>) => {
      const next = typeof updater === 'function' ? updater(sorting) : updater
      const first = next[0]
      set(first ? { sort: first.id, dir: first.desc ? 'desc' : 'asc' } : { sort: null })
    },
  })

  const coreRows = table.getRowModel().rows
  const sortColumn =
    state.sort === null
      ? undefined
      : table.getAllLeafColumns().find((column) => column.id === state.sort)
  // A URL may name a column that does not exist or cannot be sorted: keep the data's order.
  const sortId = sortColumn?.getCanSort() ? sortColumn.id : null
  const sortedRows = useMemo(
    () =>
      sortId === null
        ? coreRows
        : sortRows(coreRows, sortId, state.dir, {
            [sortId]: (row) => comparable(row.getValue(sortId)),
          }),
    [coreRows, sortId, state.dir],
  )
  const page = paginate(sortedRows, state.page, state.size)

  if (data.length === 0) {
    return <div role="status">{emptyState}</div>
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end justify-between gap-3">
        {toolbar ? (
          <TableToolbar
            activeFilterCount={activeFilterCount}
            filters={filterSpecs}
            label={ariaLabel}
            onChange={set}
            onClear={clear}
            options={options}
            state={state}
            {...(toolbar.searchLabel === undefined ? {} : { searchLabel: toolbar.searchLabel })}
          />
        ) : (
          <span />
        )}
        <details className="relative">
          <summary className="border-input bg-card text-muted-foreground hover:bg-muted hover:text-foreground inline-flex h-8 list-none items-center gap-1.5 rounded-md border px-3 text-xs font-medium shadow-xs transition-colors [&::-webkit-details-marker]:hidden">
            <Columns3 aria-hidden="true" className="size-3.5" />
            Columns
          </summary>
          <div className="bg-popover shadow-raised absolute right-0 z-20 mt-1.5 max-h-80 min-w-56 space-y-2 overflow-auto rounded-lg border p-3">
            {table.getAllLeafColumns().map((column) => {
              if (!column.getCanHide()) return null
              const label =
                typeof column.columnDef.header === 'string' ? column.columnDef.header : column.id
              return (
                <label key={column.id} className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={column.getIsVisible()}
                    onChange={column.getToggleVisibilityHandler()}
                  />
                  {label}
                </label>
              )
            })}
          </div>
        </details>
      </div>

      {filtered.length === 0 ? (
        <div role="status" className="bg-card text-muted-foreground rounded-lg border p-4 text-sm">
          <strong className="text-foreground font-medium">No rows match</strong> the search or
          filters. Clear filters to show all {data.length} {scope}.
        </div>
      ) : (
        <div
          data-testid="data-table-scroll"
          className={
            // 20 rows fit the page; larger pages scroll inside the card under the sticky header.
            state.size > PAGE_SIZES[0]
              ? 'bg-card max-h-[80vh] overflow-auto rounded-lg border'
              : 'bg-card overflow-x-auto rounded-lg border'
          }
        >
          <table aria-label={ariaLabel} className="w-full border-collapse text-sm">
            <caption className="sr-only">{ariaLabel}</caption>
            <thead className="bg-muted/90 sticky top-0 z-[1] backdrop-blur">
              {table.getHeaderGroups().map((headerGroup) => (
                <tr key={headerGroup.id}>
                  {headerGroup.headers.map((header) => {
                    const column = header.column
                    const sort = sortId === column.id ? state.dir : null
                    const label =
                      typeof column.columnDef.header === 'string'
                        ? column.columnDef.header
                        : column.id
                    return (
                      <th
                        key={header.id}
                        scope="col"
                        aria-sort={
                          sort === 'asc' ? 'ascending' : sort === 'desc' ? 'descending' : 'none'
                        }
                        className="text-muted-foreground border-b px-3 py-2 text-left text-xs font-semibold tracking-wide whitespace-nowrap uppercase"
                      >
                        {header.isPlaceholder ? null : column.getCanSort() ? (
                          <span className="inline-flex items-center gap-1">
                            <button
                              type="button"
                              className="hover:text-foreground inline-flex items-center font-semibold tracking-wide uppercase transition-colors"
                              onClick={column.getToggleSortingHandler()}
                            >
                              {flexRender(column.columnDef.header, header.getContext())}
                            </button>
                            <SortButtons
                              direction={sort}
                              label={label}
                              onChange={(direction) =>
                                set(
                                  direction === null
                                    ? { sort: null }
                                    : { sort: column.id, dir: direction },
                                )
                              }
                            />
                          </span>
                        ) : (
                          flexRender(column.columnDef.header, header.getContext())
                        )}
                      </th>
                    )
                  })}
                </tr>
              ))}
            </thead>
            <tbody data-testid="data-table-rows">
              {page.rows.map((row) => (
                <tr
                  key={row.id}
                  className="hover:bg-muted/40 border-b transition-colors last:border-b-0"
                >
                  {row.getVisibleCells().map((cell) => (
                    <td key={cell.id} className="px-3 py-2.5 align-top tabular-nums">
                      {flexRender(cell.column.columnDef.cell, cell.getContext()) ??
                        renderPrimitiveValue(cell.getValue())}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <footer className="text-muted-foreground flex flex-wrap items-center justify-between gap-3 text-xs">
        <div className="space-y-1">
          <span className="block">
            Showing {page.from}–{page.to} of {filtered.length} {scope}
            {filtered.length < data.length ? ` (filtered from ${data.length})` : null}.
          </span>
          {budget !== undefined && total > data.length ? (
            <Truncated shown={data.length} budget={budget} scope={scope} />
          ) : null}
        </div>
        {filtered.length > PAGE_SIZES[0] ? (
          <TablePagination
            ariaLabel={`${ariaLabel} pagination`}
            onPage={(next) => set({ page: next })}
            onPageSize={(size) => set({ size })}
            page={page.page}
            pageCount={page.pageCount}
            pageSize={state.size}
          />
        ) : null}
      </footer>
    </div>
  )
}
