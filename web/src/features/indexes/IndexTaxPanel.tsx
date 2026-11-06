import { useMemo } from 'react'
import { Link } from 'react-router-dom'

import { useInstanceTables } from '@/api/queries'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState } from '@/components/state'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { formatBytes, formatCount, formatCountOf, formatPercent, formatRate } from '@/lib/format'
import type { IndexRow } from '@/lib/indexAnalysis'
import { buildIndexTax, summariseIndexTax, type IndexTax } from '@/lib/indexTax'

import { NO_INDEX_STATS } from './emptyText'
import { IndexPanel, PanelNote } from './IndexPanel'

const TAX_TOOLBAR: TableToolbarSpec<IndexTax> = {
  searchFields: [(tax) => `${tax.schema}.${tax.table}`, (tax) => tax.datname],
  filters: [],
}

/** The most tables the API returns for one server. */
const TABLE_LIMIT = 500

const UNKNOWN = 'Not measured'

/** The Cleanup tab narrowed to one table: its search matches `schema.table`. */
function cleanupLink(instanceId: string, database: string, tax: IndexTax): string {
  const params = new URLSearchParams({ instance: instanceId, tab: 'cleanup' })
  if (database !== '') params.set('db', database)
  params.set('cleanup.q', `${tax.schema}.${tax.table}`)
  return `/indexes?${params.toString()}`
}

function columns(instanceId: string, database: string): readonly SimpleColumn<IndexTax>[] {
  return [
    {
      id: 'table',
      header: 'Table',
      sortValue: (tax) => `${tax.schema}.${tax.table}`,
      className: 'font-medium break-all',
      truncate: (tax) => `${tax.datname} · ${tax.schema}.${tax.table}`,
      cell: (tax) => (
        <>
          <span className="text-muted-foreground">
            {tax.datname} · {tax.schema}.
          </span>
          {tax.table}
        </>
      ),
    },
    {
      id: 'indexes',
      header: 'Indexes',
      sortValue: (tax) => tax.indexCount,
      className: 'whitespace-nowrap',
      width: '7rem',
      cell: (tax) => formatCount(tax.indexCount),
    },
    {
      id: 'index-size',
      header: 'Index size',
      help: 'indexes.tax-index-size',
      sortValue: (tax) => tax.indexBytes,
      className: 'whitespace-nowrap',
      width: '8rem',
      cell: (tax) => formatBytes(tax.indexBytes),
    },
    {
      id: 'table-size',
      header: 'Table size',
      help: 'indexes.tax-table-size',
      sortValue: (tax) => tax.tableBytes,
      className: 'whitespace-nowrap',
      width: '8rem',
      cell: (tax) => (tax.tableBytes === null ? UNKNOWN : formatBytes(tax.tableBytes)),
    },
    {
      id: 'ratio',
      header: 'Index/table %',
      help: 'indexes.tax-ratio',
      sortValue: (tax) => tax.indexToTableRatio,
      className: 'whitespace-nowrap',
      width: '10rem',
      cell: (tax) =>
        tax.tableBytes === null ? '—' : (formatPercent(tax.indexBytes, tax.tableBytes) ?? '—'),
    },
    {
      id: 'unused',
      header: 'Unused size',
      help: 'indexes.tax-unused',
      sortValue: (tax) => tax.unusedBytes,
      className: 'whitespace-nowrap',
      width: '13rem',
      cell: (tax) =>
        tax.unusedCount === 0 ? (
          '0 B'
        ) : (
          <>
            {formatBytes(tax.unusedBytes)}{' '}
            <span className="text-muted-foreground text-xs">
              {tax.unusedBasis === 'advisor' ? 'advisor' : 'incl. raw counters'}
            </span>
          </>
        ),
    },
    {
      id: 'writes',
      header: 'Writes/s',
      help: 'indexes.tax-writes',
      sortValue: (tax) => tax.writeRate,
      className: 'whitespace-nowrap',
      width: '8rem',
      cell: (tax) => (tax.writeRate === null ? UNKNOWN : formatRate(tax.writeRate)),
    },
    {
      id: 'amplification',
      header: 'Write amplification',
      help: 'indexes.tax-amplification',
      sortValue: (tax) => tax.writeAmplification,
      className: 'whitespace-nowrap',
      width: '10rem',
      cell: (tax) =>
        tax.writeAmplification === null ? UNKNOWN : `${formatRate(tax.writeAmplification)}`,
    },
    {
      id: 'cleanup',
      header: 'Cleanup',
      srOnlyHeader: true,
      className: 'whitespace-nowrap',
      width: '7rem',
      cell: (tax) => (
        <Link
          aria-label={`Show cleanup candidates of ${tax.schema}.${tax.table}`}
          className="text-primary font-medium hover:underline"
          to={cleanupLink(instanceId, database, tax)}
        >
          Cleanup
        </Link>
      ),
    },
  ]
}

interface IndexTaxPanelProps {
  instanceId: string
  /** The indexes in scope: the page's database filter already applied. */
  rows: readonly IndexRow[]
  database: string
  /** The agent or the API cut the index list: every figure covers the reported indexes only. */
  incomplete?: boolean
  /** Why the list is empty, when it is. */
  emptyDescription?: string
}

const INCOMPLETE_DETAIL =
  'The agent reports only its busiest and largest indexes per database (checks.index_stats.top_n, 50 by default). Each table shows the indexes reported, so its index size and write amplification can be lower than the real ones, and tables with no reported index are missing. Raise top_n in the agent configuration to see more.'
const INCOMPLETE = `The list is incomplete. ${INCOMPLETE_DETAIL}`

const ESTIMATE =
  'Every figure on this tab is an estimate. Write amplification is the inserts, deletes and updates that are not heap-only per second, times the indexes of the table. Unused size counts indexes with no scan: the advisor stands behind it, or it rests on raw counters. Unique and primary key indexes are never counted. Table size is the heap, without TOAST.'

/**
 * What the indexes of each table cost, computed here from the index and table
 * statistics already loaded: pglens runs no statement.
 */
export function IndexTaxPanel({
  instanceId,
  rows,
  database,
  incomplete = false,
  emptyDescription = NO_INDEX_STATS,
}: IndexTaxPanelProps) {
  // The largest tables first: the ones whose indexes cost the most are never cut.
  const tablesQuery = useInstanceTables(instanceId, { limit: TABLE_LIMIT, order_by: 'size' })
  const taxes = useMemo(
    () => buildIndexTax(rows, tablesQuery.data?.items ?? []),
    [rows, tablesQuery.data],
  )
  const summary = useMemo(() => summariseIndexTax(taxes), [taxes])
  const tableColumns = useMemo(() => columns(instanceId, database), [instanceId, database])
  const unmeasured = taxes.filter((tax) => tax.tableBytes === null).length

  return (
    <IndexPanel
      captionId="indexes.tax"
      description="What each table's indexes cost: how many, how big, how much looks unused, entries added per write."
      notes={
        <>
          {incomplete ? (
            <PanelNote title={INCOMPLETE} tone="warning">
              <strong>The list is incomplete.</strong> {INCOMPLETE_DETAIL}
            </PanelNote>
          ) : null}
          <PanelNote title={ESTIMATE}>
            <strong>Estimates.</strong> {ESTIMATE}
          </PanelNote>
          {unmeasured > 0 ? (
            <PanelNote
              title={`${unmeasured} tables have no table statistics, so they show no ratio and no write amplification.`}
            >
              {formatCount(unmeasured)} {unmeasured === 1 ? 'table has' : 'tables have'} no table
              statistics: no ratio, no write amplification.
            </PanelNote>
          ) : null}
        </>
      }
      stats={[
        { label: 'Tables', value: formatCount(summary.tables), unit: 'tables' },
        {
          label: 'Index size',
          value: formatBytes(summary.indexBytes),
          detail: `${formatCountOf(summary.indexCount, 'index', 'indexes')} reported`,
        },
        {
          label: 'Unused size',
          value: formatBytes(summary.unusedBytes),
          detail: `${formatBytes(summary.unusedConfirmedBytes)} advisor-confirmed`,
        },
        {
          label: 'Write amplification',
          value:
            summary.writeAmplification === null
              ? UNKNOWN
              : formatRate(summary.writeAmplification).replace(/\/s$/, ''),
          ...(summary.writeAmplification === null ? {} : { unit: 'per second' as const }),
          detail: `Index entries added per second by writes; ${formatCount(summary.measuredTables)} of ${formatCount(summary.tables)} tables measured`,
        },
      ]}
      title="Index tax"
      titleId="tax-title"
    >
      {taxes.length === 0 ? (
        <EmptyState description={emptyDescription} title="No indexes to show" />
      ) : (
        <SimpleTable
          ariaLabel="Index tax by table"
          captionId="indexes.tax-table"
          columns={tableColumns}
          defaultSort={{ id: 'index-size', desc: true }}
          emptyState={null}
          rowKey={(tax) => tax.key}
          rows={taxes}
          scope="tables"
          tableId="tax"
          toolbar={TAX_TOOLBAR}
        />
      )}
    </IndexPanel>
  )
}
