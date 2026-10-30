import { useMemo, type CSSProperties, type ReactNode } from 'react'
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
import { useFilteredRows, type TableToolbarSpec } from '@/hooks/useFilteredRows'
import { PAGE_SIZES, paginate, sortRows, type Comparable } from '@/lib/tableControls'
import type { CaptionId, ColumnHelpId } from '@/lib/captions'

import { cn } from '@/lib/utils'

import {
  FULLHD_CELL,
  FULLHD_FIXED,
  FULLHD_HEADER_CELL,
  FULLHD_ROW,
  FULLHD_TABLE,
  FULLHD_TRUNCATE,
} from './fullhdTable'
import { Caption } from './Caption'
import { ColumnHint, InfoHint } from './InfoHint'
import { SortButtons } from './SortButtons'
import { NoRowsMatch, TableFooter } from './TableFooter'
import { TableToolbar } from './TableToolbar'

const dataTableFeatures = tableFeatures({
  columnVisibilityFeature,
  rowSortingFeature,
  // Types `meta` in the column definitions (width, truncate, title). TanStack reads
  // the type of this slot only, so the assertion is what carries it.
  // eslint-disable-next-line @typescript-eslint/no-unnecessary-type-assertion
  columnMeta: {} as DataTableColumnMeta,
})

/** Search box and filters above the table (D12). */
export type DataTableToolbar<TData> = TableToolbarSpec<TData>

export interface DataTableBaseProps<TData extends RowData> {
  columns: ColumnDef<typeof dataTableFeatures, TData>[]
  data: TData[]
  emptyState: ReactNode
  /** The full result size. Truncation is reported only when it exceeds the rows shown. */
  total?: number
  /** Page controls shown in the toolbar line beside the search box (D26). */
  toolbarExtra?: ReactNode
  /** The server's row budget. It reports truncation only together with total. */
  budget?: number
  /** A sentence after the footer's visible range (the match count of a server search, D20). */
  footerNote?: ReactNode
  scope?: string
  ariaLabel?: string
  /** Search box and filters above the table (D12). */
  toolbar?: DataTableToolbar<TData>
  /** URL prefix of the table state (`<tableId>.q`, `.sort`, `.page`, …); without it the state is local. */
  tableId?: string
  /** Order when the URL names none. */
  defaultSort?: { id: string; desc: boolean }
  /**
   * A stable key per row (TanStack `getRowId`). Without it a row is keyed by its
   * position, so a row whose cell keeps state (a dialog, focus) must pass one.
   */
  getRowId?: (row: TData) => string
}

/**
 * A captioned table (D30) shows its caption in the toolbar row with an ⓘ named
 * after the table, so it must have its own `ariaLabel`.
 */
export type DataTableProps<TData extends RowData> = DataTableBaseProps<TData> &
  ({ captionId?: undefined } | { captionId: CaptionId; ariaLabel: string })

/**
 * Per-column layout hints, read from `columnDef.meta` (D26): `width` sizes the
 * column of the fixed table from 1600 px; `truncate` keeps a free-text cell on
 * one line with the full text in its `title` (from `title`, else the value).
 */
export interface DataTableColumnMeta<TData = unknown> {
  width?: string
  truncate?: boolean
  title?: (row: TData) => string
  /** A short explanation of the column, behind an ⓘ after its header (D30). */
  help?: ColumnHelpId
}

function columnMeta<TData>(columnDef: { meta?: unknown }): DataTableColumnMeta<TData> {
  return columnDef.meta ?? {}
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
 * A searchable, filterable, sortable table paginated at the `tablePageSize`
 * preference, 20 rows by default (D12, D13, D23, 11.2). With `tableId` its
 * state lives in the URL, otherwise in the component. The two variants are
 * separate components so that no hook is ever called conditionally.
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
  footerNote,
  toolbarExtra,
  captionId,
  scope = 'rows',
  ariaLabel = 'Data table',
  toolbar,
  getRowId,
  controls,
}: DataTableProps<TData> & { controls: TableStateControls }) {
  const { state, set, clear } = controls
  const {
    filtered,
    options,
    activeFilterCount,
    filters: filterSpecs,
  } = useFilteredRows(data, state, toolbar)
  // Fixed layout only when a column carries a width or truncation hint (D26).
  const hinted = columns.some((column) => {
    const meta = columnMeta(column)
    return meta.width !== undefined || meta.truncate === true
  })

  const sorting: SortingState =
    state.sort === null ? [] : [{ id: state.sort, desc: state.dir === 'desc' }]
  const table = useTable({
    features: dataTableFeatures,
    columns,
    data: filtered,
    ...(getRowId === undefined ? {} : { getRowId: (row: TData) => getRowId(row) }),
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
    <div
      className="space-y-3"
      data-table-card
      {...(captionId === undefined ? {} : { 'data-caption-id': captionId })}
    >
      <div className="flex flex-wrap items-end justify-between gap-3">
        {toolbar ? (
          // The toolbar takes the free width so its filters do not wrap beside Columns.
          <div className="min-w-0 flex-1">
            <TableToolbar
              activeFilterCount={activeFilterCount}
              filters={filterSpecs}
              label={ariaLabel}
              onChange={set}
              onClear={clear}
              options={options}
              state={state}
              {...(toolbar.searchLabel === undefined ? {} : { searchLabel: toolbar.searchLabel })}
              {...(toolbarExtra === undefined ? {} : { extra: toolbarExtra })}
            />
          </div>
        ) : toolbarExtra ? (
          <div className="flex min-w-0 flex-1 flex-wrap items-end gap-3">{toolbarExtra}</div>
        ) : (
          <span />
        )}
        {captionId !== undefined ? (
          // Below 1600 px the caption is a full line above the controls; from 1600 px it
          // sits beside them, one truncated line of a bounded width (D26).
          <div className="order-first flex w-full min-w-0 items-center gap-1 self-center min-[1600px]:order-none min-[1600px]:w-auto min-[1600px]:max-w-md min-[1600px]:min-w-48 min-[1600px]:flex-initial">
            <Caption className="text-xs" id={captionId} marked={false} oneLine />
            <InfoHint className="shrink-0" id={captionId} label={ariaLabel} />
          </div>
        ) : null}
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
        <NoRowsMatch loaded={data.length} scope={scope} />
      ) : (
        <div
          data-testid="data-table-scroll"
          // Focusable so a wide table scrolls sideways from the keyboard.
          tabIndex={0}
          className={
            // 20 rows fit the page; larger pages scroll inside the card under the sticky header.
            state.size > PAGE_SIZES[0]
              ? 'bg-card max-h-[80vh] overflow-auto rounded-lg border'
              : 'bg-card overflow-x-auto rounded-lg border'
          }
        >
          <table aria-label={ariaLabel} className={cn(FULLHD_TABLE, hinted && FULLHD_FIXED)}>
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
                    const { width, help } = columnMeta(column.columnDef)
                    const hint = help === undefined ? null : <ColumnHint id={help} label={label} />
                    return (
                      <th
                        key={header.id}
                        scope="col"
                        aria-sort={
                          sort === 'asc' ? 'ascending' : sort === 'desc' ? 'descending' : 'none'
                        }
                        className={cn(
                          'text-muted-foreground border-b px-3 py-2 text-left text-xs font-semibold tracking-wide whitespace-nowrap uppercase',
                          FULLHD_HEADER_CELL,
                          width !== undefined && 'min-[1600px]:w-(--col-w)',
                        )}
                        style={
                          width === undefined ? undefined : ({ '--col-w': width } as CSSProperties)
                        }
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
                            {hint}
                          </span>
                        ) : (
                          <>
                            {flexRender(column.columnDef.header, header.getContext())}
                            {hint}
                          </>
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
                  className={cn(
                    'hover:bg-muted/40 border-b transition-colors last:border-b-0',
                    FULLHD_ROW,
                  )}
                >
                  {row.getVisibleCells().map((cell) => {
                    const content =
                      flexRender(cell.column.columnDef.cell, cell.getContext()) ??
                      renderPrimitiveValue(cell.getValue())
                    const meta = columnMeta(cell.column.columnDef)
                    return (
                      <td
                        key={cell.id}
                        className={cn('px-3 py-2.5 align-top tabular-nums', FULLHD_CELL)}
                      >
                        {meta.truncate ? (
                          <span
                            className={FULLHD_TRUNCATE}
                            title={
                              meta.title?.(row.original) ??
                              (renderPrimitiveValue(cell.getValue()) || undefined)
                            }
                          >
                            {content}
                          </span>
                        ) : (
                          content
                        )}
                      </td>
                    )
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <TableFooter
        ariaLabel={ariaLabel}
        controls={controls}
        loaded={data.length}
        page={page}
        scope={scope}
        total={total}
        {...(budget === undefined ? {} : { budget })}
        {...(footerNote === undefined ? {} : { note: footerNote })}
      />
    </div>
  )
}
