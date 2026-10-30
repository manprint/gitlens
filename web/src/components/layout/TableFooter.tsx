import type { TableStateControls } from '@/hooks/useTableState'
import { PAGE_SIZES, type Page } from '@/lib/tableControls'

import { Truncated } from '../state/Truncated'
import { TablePagination } from './TablePagination'

/** Shown instead of the rows when the search and filters leave none (I-9). */
export function NoRowsMatch({ loaded, scope }: { loaded: number; scope: string }) {
  return (
    <div role="status" className="bg-card text-muted-foreground rounded-lg border p-4 text-sm">
      <strong className="text-foreground font-medium">No rows match</strong> the search or filters.
      Clear filters to show all {loaded} {scope}.
    </div>
  )
}

export interface TableFooterProps {
  ariaLabel: string
  scope: string
  page: Page<unknown>
  /** Rows before search and filters. */
  loaded: number
  controls: Pick<TableStateControls, 'state' | 'set'>
  /** The server's full result size and row budget: a capped load shows the truncation note. */
  total?: number
  budget?: number
}

/**
 * The visible range over the filtered and loaded counts (I-9) and, above
 * one page of the smallest size, the pager (D23).
 */
export function TableFooter({
  ariaLabel,
  scope,
  page,
  loaded,
  controls,
  total = loaded,
  budget,
}: TableFooterProps) {
  const { state, set } = controls
  return (
    <footer className="text-muted-foreground flex flex-wrap items-center justify-between gap-3 text-xs">
      <div className="space-y-1">
        <span className="block">
          Showing {page.from}–{page.to} of {page.total} {scope}
          {page.total < loaded ? ` (filtered from ${loaded})` : null}.
        </span>
        {budget !== undefined && total > loaded ? (
          <Truncated shown={loaded} budget={budget} scope={scope} />
        ) : null}
      </div>
      {page.total > PAGE_SIZES[0] ? (
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
  )
}
