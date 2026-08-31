import { useRef, type ReactNode } from 'react'
import {
  columnVisibilityFeature,
  createSortedRowModel,
  flexRender,
  rowSortingFeature,
  tableFeatures,
  useTable,
  type ColumnDef,
  type RowData,
} from '@tanstack/react-table'
import { useVirtualizer } from '@tanstack/react-virtual'

import { Truncated } from '../state/Truncated'

const dataTableFeatures = tableFeatures({
  columnVisibilityFeature,
  rowSortingFeature,
  sortedRowModel: createSortedRowModel(),
})

export interface DataTableProps<TData extends RowData> {
  columns: ColumnDef<typeof dataTableFeatures, TData>[]
  data: TData[]
  emptyState: ReactNode
  total?: number
  budget?: number
  scope?: string
  ariaLabel?: string
}

const ESTIMATED_ROW_HEIGHT = 44
const VIRTUALIZATION_THRESHOLD = 200
const FALLBACK_VISIBLE_ROWS = 24

function renderPrimitiveValue(value: unknown): string {
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
    return String(value)
  }
  return ''
}

export function DataTable<TData extends RowData>({
  columns,
  data,
  emptyState,
  total = data.length,
  budget,
  scope = 'rows',
  ariaLabel = 'Data table',
}: DataTableProps<TData>) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const table = useTable({
    features: dataTableFeatures,
    columns,
    data,
  })
  const rows = table.getRowModel().rows
  const shouldVirtualize = rows.length > VIRTUALIZATION_THRESHOLD
  // TanStack Virtual's imperative API is intentionally used for row measurement and scrolling.
  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ESTIMATED_ROW_HEIGHT,
    overscan: 8,
  })
  const virtualItems = virtualizer.getVirtualItems()
  const renderItems = shouldVirtualize
    ? virtualItems.length > 0
      ? virtualItems
      : rows.slice(0, FALLBACK_VISIBLE_ROWS).map((row, index) => ({
          index,
          key: row.id,
          start: index * ESTIMATED_ROW_HEIGHT,
          size: ESTIMATED_ROW_HEIGHT,
        }))
    : rows.map((row, index) => ({
        index,
        key: row.id,
        start: index * ESTIMATED_ROW_HEIGHT,
        size: ESTIMATED_ROW_HEIGHT,
      }))

  if (data.length === 0) {
    return <div role="status">{emptyState}</div>
  }

  return (
    <div className="space-y-3">
      <div className="flex justify-end">
        <details className="relative">
          <summary className="cursor-pointer rounded border px-3 py-1.5 text-sm">Columns</summary>
          <div className="bg-background absolute right-0 z-10 mt-1 min-w-48 space-y-2 rounded border p-3 shadow-md">
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

      <div ref={scrollRef} className="max-h-[30rem] overflow-auto rounded-md border">
        <table aria-label={ariaLabel} className="w-full border-collapse text-sm">
          <caption className="sr-only">{ariaLabel}</caption>
          <thead className="bg-muted/80 sticky top-0 z-[1]">
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id}>
                {headerGroup.headers.map((header) => {
                  const sort = header.column.getIsSorted()
                  return (
                    <th
                      key={header.id}
                      scope="col"
                      aria-sort={
                        sort === 'asc' ? 'ascending' : sort === 'desc' ? 'descending' : 'none'
                      }
                      className="px-3 py-2 text-left font-medium"
                    >
                      {header.isPlaceholder ? null : header.column.getCanSort() ? (
                        <button
                          type="button"
                          className="font-medium underline-offset-2 hover:underline"
                          onClick={header.column.getToggleSortingHandler()}
                        >
                          {flexRender(header.column.columnDef.header, header.getContext())}
                          <span aria-hidden="true" className="ml-1">
                            {sort === 'asc' ? '↑' : sort === 'desc' ? '↓' : '↕'}
                          </span>
                        </button>
                      ) : (
                        flexRender(header.column.columnDef.header, header.getContext())
                      )}
                    </th>
                  )
                })}
              </tr>
            ))}
          </thead>
          <tbody
            data-testid="data-table-rows"
            style={
              shouldVirtualize
                ? { height: `${virtualizer.getTotalSize()}px`, position: 'relative' }
                : undefined
            }
          >
            {renderItems.map((virtualRow) => {
              const row = rows[virtualRow.index]
              if (!row) return null
              return (
                <tr
                  key={row.id}
                  style={
                    shouldVirtualize
                      ? {
                          height: `${virtualRow.size}px`,
                          position: 'absolute',
                          transform: `translateY(${virtualRow.start}px)`,
                          width: '100%',
                        }
                      : undefined
                  }
                  className="border-t"
                >
                  {row.getVisibleCells().map((cell) => (
                    <td key={cell.id} className="px-3 py-2 tabular-nums">
                      {flexRender(cell.column.columnDef.cell, cell.getContext()) ??
                        renderPrimitiveValue(cell.getValue())}
                    </td>
                  ))}
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      <footer className="text-muted-foreground text-sm">
        {budget !== undefined ? (
          <Truncated shown={data.length} budget={budget} scope={scope} />
        ) : (
          <span>
            Showing {data.length} of {total} {scope}.
          </span>
        )}
      </footer>
    </div>
  )
}
