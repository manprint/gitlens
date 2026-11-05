import {
  Fragment,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'

import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { useTableRows } from '@/hooks/useTableRows'
import type { TableDefaults } from '@/hooks/useTableState'
import type { CaptionId, ColumnHelpId } from '@/lib/captions'
import { PAGE_SIZES, type Accessor, type SortDirection } from '@/lib/tableControls'
import { cn } from '@/lib/utils'

import {
  FULLHD_CELL,
  FULLHD_FIXED,
  FULLHD_HEADER_CELL,
  FULLHD_ROW,
  FULLHD_TABLE,
  FULLHD_TRUNCATE,
  FULLHD_BOX_CAP,
} from './fullhdTable'
import { Caption } from './Caption'
import { ColumnHint, InfoHint } from './InfoHint'
import { SortButtons } from './SortButtons'
import { NoRowsMatch, TableFooter } from './TableFooter'
import { TableToolbar } from './TableToolbar'

export interface SimpleColumn<T> {
  id: string
  /** Header text; also the column name of its sort buttons. */
  header: string
  /** Hide the header text visually (an icon-only or actions column). */
  srOnlyHeader?: boolean
  /** The raw value the column sorts by; without it the column is not sortable. */
  sortValue?: Accessor<T>
  /** The direction of a first press on the header label (default ascending). */
  firstDirection?: SortDirection
  cell: (row: T) => ReactNode
  /** Render the cell as a row header (`th scope="row"`). */
  rowHeader?: boolean
  className?: string
  /** Column width of the fixed table from 1600 px (D26), e.g. `12rem` or `30%`. */
  width?: string
  /** A free-text column: one line with an ellipsis from 1600 px, the full text in `title`. */
  truncate?: (row: T) => string
  /** A short hint about the column (D30): an ⓘ after the header label. */
  help?: ColumnHelpId
}

function cellContent<T>(column: SimpleColumn<T>, row: T): ReactNode {
  if (!column.truncate) return column.cell(row)
  return (
    <span className={FULLHD_TRUNCATE} title={column.truncate(row)}>
      {column.cell(row)}
    </span>
  )
}

export interface SimpleTableProps<T> {
  ariaLabel: string
  /** The table's caption (D30): a one-line sentence above the toolbar, with an ⓘ for the details. */
  captionId?: CaptionId
  /** URL prefix of the table state. */
  tableId: string
  rows: readonly T[]
  rowKey: (row: T) => string
  columns: readonly SimpleColumn<T>[]
  toolbar?: TableToolbarSpec<T>
  defaultSort?: { id: string; desc: boolean }
  /** Shown when there are no rows at all (not when filters hide them). */
  emptyState: ReactNode
  scope?: string
  caption?: string
  /** Expandable rows: the detail shown under an expanded row. */
  renderDetail?: (row: T) => ReactNode
  /** The accessible name of a row's expand button, e.g. `Show details of orders`. */
  detailLabel?: (row: T, expanded: boolean) => string
  /** The server's full result size and row budget (truncation note). */
  total?: number
  budget?: number
  /** A domain comparator that stays the source of truth for the order. */
  compare?: (a: T, b: T, key: string, direction: SortDirection) => number
  /**
   * Names the header label buttons (e.g. `Sort Tables by Size`) and marks the
   * sorted one `aria-pressed` with an arrow, for tables whose tests and users
   * know those buttons. Without it the label button is named by the header.
   */
  headerToggleName?: (column: SimpleColumn<T>) => string
  /** Classes of the scroll box around the table. */
  className?: string
  /**
   * The key of a row to show: the table turns to its page, expands it and
   * gives its toggle focus (a search hit, D20). A row its own search or
   * filters hide clears them first; without the row it does nothing.
   */
  focusKey?: string | undefined
  /** Called once per `focusKey`: true once the row is shown, false when no row has that key. */
  onFocusDone?: (found: boolean) => void
}

/**
 * A bespoke-markup table with the shared toolbar, sort buttons and pager:
 * for tables whose rows expand or carry custom cells (9.3). An expanded row
 * stays open while the page is re-sorted and closes on a page change.
 */
export function SimpleTable<T>({
  ariaLabel,
  captionId,
  tableId,
  rows,
  rowKey,
  columns,
  toolbar,
  defaultSort,
  emptyState,
  scope = 'rows',
  caption = ariaLabel,
  renderDetail,
  detailLabel = (_row, expanded) => (expanded ? 'Hide details' : 'Show details'),
  total,
  budget,
  compare,
  headerToggleName,
  className,
  focusKey,
  onFocusDone,
}: SimpleTableProps<T>) {
  const accessors = useMemo(() => {
    const byId: Record<string, Accessor<T>> = {}
    for (const column of columns) if (column.sortValue) byId[column.id] = column.sortValue
    return byId
  }, [columns])
  const defaults: TableDefaults = defaultSort
    ? { sort: defaultSort.id, dir: defaultSort.desc ? 'desc' : 'asc' }
    : {}
  const view = useTableRows(rows, tableId, toolbar, accessors, defaults, compare)
  const { controls, page, sortId } = view
  const { state, set } = controls
  // A page change collapses the expanded rows (adjusting state while rendering:
  // https://react.dev/learn/you-might-not-need-an-effect).
  const [openKeys, setOpenKeys] = useState<ReadonlySet<string>>(() => new Set())
  const [shownPage, setShownPage] = useState(page.page)
  if (shownPage !== page.page) {
    setShownPage(page.page)
    setOpenKeys(new Set())
  }

  // A row to show (D20): its page first, then it opens, in the same render as the page.
  const focusIndex =
    focusKey === undefined ? -1 : view.sorted.findIndex((row) => rowKey(row) === focusKey)
  const focusPage = focusIndex < 0 ? null : Math.floor(focusIndex / state.size) + 1
  const focusHidden =
    focusPage === null &&
    focusKey !== undefined &&
    view.activeFilterCount > 0 &&
    rows.some((row) => rowKey(row) === focusKey)
  const [focused, setFocused] = useState<string | null>(null)
  if (focusKey === undefined && focused !== null) setFocused(null)
  if (focusKey !== undefined && focused !== focusKey && focusPage === page.page) {
    setFocused(focusKey)
    setOpenKeys(new Set([focusKey]))
  }
  const focusToggle = useRef<HTMLButtonElement>(null)
  const reported = useRef<string | null>(null)
  useEffect(() => {
    if (focusKey === undefined) {
      reported.current = null
      return
    }
    if (reported.current === focusKey) return
    if (focusHidden) {
      controls.clear()
    } else if (focusPage === null) {
      reported.current = focusKey
      onFocusDone?.(false)
    } else if (focusPage !== page.page) {
      set({ page: focusPage })
    } else if (focused === focusKey) {
      reported.current = focusKey
      focusToggle.current?.focus()
      focusToggle.current?.scrollIntoView?.({ block: 'center' })
      onFocusDone?.(true)
    }
  }, [controls, focusHidden, focusKey, focusPage, focused, onFocusDone, page.page, set])

  // The header label: the first press picks the column's first direction, the next flips it.
  function toggleSort(column: SimpleColumn<T>) {
    set(
      sortId === column.id
        ? { dir: state.dir === 'asc' ? 'desc' : 'asc' }
        : { sort: column.id, dir: column.firstDirection ?? 'asc' },
    )
  }

  function toggle(key: string) {
    const next = new Set(openKeys)
    if (next.has(key)) next.delete(key)
    else next.add(key)
    setOpenKeys(next)
  }

  if (rows.length === 0) return <div role="status">{emptyState}</div>

  const columnCount = columns.length + (renderDetail ? 1 : 0)
  return (
    <div
      className="space-y-3"
      data-table-card
      {...(captionId === undefined ? {} : { 'data-card': '', 'data-caption-id': captionId })}
    >
      {captionId !== undefined ? (
        <div className="flex w-full min-w-0 items-center gap-1">
          <Caption className="text-xs" id={captionId} marked={false} oneLine />
          <InfoHint className="shrink-0" id={captionId} label={ariaLabel} />
        </div>
      ) : null}
      {toolbar ? (
        <TableToolbar
          activeFilterCount={view.activeFilterCount}
          filters={view.filters}
          label={ariaLabel}
          onChange={set}
          onClear={controls.clear}
          options={view.options}
          state={state}
          {...(toolbar.searchLabel === undefined ? {} : { searchLabel: toolbar.searchLabel })}
        />
      ) : null}
      {view.filtered.length === 0 ? (
        <NoRowsMatch loaded={rows.length} scope={scope} />
      ) : (
        // Focusable so a wide table scrolls sideways from the keyboard.
        <div
          data-testid="data-table-scroll"
          tabIndex={0}
          className={cn(
            state.size > PAGE_SIZES[0]
              ? FULLHD_BOX_CAP
              : 'bg-card overflow-x-auto rounded-lg border',
            className,
          )}
        >
          <table
            aria-label={ariaLabel}
            className={cn(
              FULLHD_TABLE,
              columns.some((column) => column.width !== undefined || column.truncate) &&
                FULLHD_FIXED,
            )}
          >
            <caption className="sr-only">{caption}</caption>
            <thead className="bg-muted/90 sticky top-0 z-[1] backdrop-blur">
              <tr>
                {renderDetail ? (
                  <th scope="col" className="w-8 border-b px-2">
                    <span className="sr-only">Details</span>
                  </th>
                ) : null}
                {columns.map((column) => {
                  const direction = sortId === column.id ? state.dir : null
                  const hint =
                    column.help === undefined ? null : (
                      <ColumnHint id={column.help} label={column.header} />
                    )
                  return (
                    <th
                      key={column.id}
                      scope="col"
                      aria-sort={
                        direction === 'asc'
                          ? 'ascending'
                          : direction === 'desc'
                            ? 'descending'
                            : 'none'
                      }
                      className={cn(
                        'text-muted-foreground border-b px-3 py-2 text-left text-xs font-semibold tracking-wide whitespace-nowrap uppercase',
                        FULLHD_HEADER_CELL,
                        column.width !== undefined && 'min-[1600px]:w-(--col-w)',
                      )}
                      style={
                        column.width === undefined
                          ? undefined
                          : ({ '--col-w': column.width } as CSSProperties)
                      }
                    >
                      {column.sortValue ? (
                        <span className="inline-flex items-center gap-1">
                          <button
                            type="button"
                            className="hover:text-foreground inline-flex items-center font-semibold tracking-wide uppercase"
                            onClick={() => toggleSort(column)}
                            {...(headerToggleName
                              ? {
                                  'aria-label': headerToggleName(column),
                                  'aria-pressed': direction !== null,
                                }
                              : {})}
                          >
                            {column.header}
                            {headerToggleName && direction !== null
                              ? direction === 'asc'
                                ? ' ↑'
                                : ' ↓'
                              : null}
                          </button>
                          <SortButtons
                            direction={direction}
                            label={column.header}
                            onChange={(next) =>
                              set(next === null ? { sort: null } : { sort: column.id, dir: next })
                            }
                          />
                          {hint}
                        </span>
                      ) : (
                        <>
                          <span className={column.srOnlyHeader ? 'sr-only' : undefined}>
                            {column.header}
                          </span>
                          {hint}
                        </>
                      )}
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody data-testid="simple-table-rows">
              {page.rows.map((row) => {
                const key = rowKey(row)
                const open = openKeys.has(key)
                return (
                  <Fragment key={key}>
                    <tr
                      className={cn(
                        'hover:bg-muted/40 border-b transition-colors last:border-b-0',
                        FULLHD_ROW,
                      )}
                    >
                      {renderDetail ? (
                        <td className="px-2 py-2 align-top min-[1600px]:py-1 min-[1600px]:align-middle min-[1600px]:[&_*]:align-top">
                          <button
                            ref={key === focused ? focusToggle : undefined}
                            type="button"
                            aria-expanded={open}
                            aria-label={detailLabel(row, open)}
                            onClick={() => toggle(key)}
                            className="text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 inline-flex size-6 items-center justify-center rounded outline-none focus-visible:ring-[3px] min-[1600px]:size-5"
                          >
                            {open ? (
                              <ChevronDown aria-hidden="true" className="size-4" />
                            ) : (
                              <ChevronRight aria-hidden="true" className="size-4" />
                            )}
                          </button>
                        </td>
                      ) : null}
                      {columns.map((column) =>
                        column.rowHeader ? (
                          <th
                            key={column.id}
                            scope="row"
                            className={cn(
                              'px-3 py-2.5 text-left align-top font-medium',
                              FULLHD_CELL,
                              column.className,
                            )}
                          >
                            {cellContent(column, row)}
                          </th>
                        ) : (
                          <td
                            key={column.id}
                            className={cn(
                              'px-3 py-2.5 align-top tabular-nums',
                              FULLHD_CELL,
                              column.className,
                            )}
                          >
                            {cellContent(column, row)}
                          </td>
                        ),
                      )}
                    </tr>
                    {renderDetail && open ? (
                      <tr className="bg-muted/20 border-b">
                        <td colSpan={columnCount} className="px-4 py-3">
                          {renderDetail(row)}
                        </td>
                      </tr>
                    ) : null}
                  </Fragment>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
      <TableFooter
        ariaLabel={ariaLabel}
        controls={controls}
        loaded={rows.length}
        page={page}
        scope={scope}
        {...(total === undefined ? {} : { total })}
        {...(budget === undefined ? {} : { budget })}
      />
    </div>
  )
}
