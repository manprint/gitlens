import { useMemo } from 'react'
import { Link } from 'react-router-dom'

import { useInstanceTables } from '@/api/queries'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState } from '@/components/state'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { formatBytes, formatCount, formatPercent, formatRate } from '@/lib/format'
import type { IndexRow } from '@/lib/indexAnalysis'
import { buildIndexTax, summariseIndexTax, type IndexTax } from '@/lib/indexTax'

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
      width: '6rem',
      cell: (tax) => formatCount(tax.indexCount),
    },
    {
      id: 'index-size',
      header: 'Index size',
      sortValue: (tax) => tax.indexBytes,
      className: 'whitespace-nowrap',
      width: '8rem',
      cell: (tax) => formatBytes(tax.indexBytes),
    },
    {
      id: 'table-size',
      header: 'Table size',
      sortValue: (tax) => tax.tableBytes,
      className: 'whitespace-nowrap',
      width: '8rem',
      cell: (tax) => (tax.tableBytes === null ? UNKNOWN : formatBytes(tax.tableBytes)),
    },
    {
      id: 'ratio',
      header: 'Index/table %',
      sortValue: (tax) => tax.indexToTableRatio,
      className: 'whitespace-nowrap',
      width: '9rem',
      cell: (tax) =>
        tax.tableBytes === null ? '—' : (formatPercent(tax.indexBytes, tax.tableBytes) ?? '—'),
    },
    {
      id: 'unused',
      header: 'Unused size',
      sortValue: (tax) => tax.unusedBytes,
      className: 'whitespace-nowrap',
      width: '11rem',
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
      sortValue: (tax) => tax.writeRate,
      className: 'whitespace-nowrap',
      width: '8rem',
      cell: (tax) => (tax.writeRate === null ? UNKNOWN : formatRate(tax.writeRate)),
    },
    {
      id: 'amplification',
      header: 'Write amplification',
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
}

const ESTIMATE =
  'Every figure on this tab is an estimate. Write amplification is the inserts, deletes and updates that are not heap-only per second, times the indexes of the table. Unused size counts indexes with no scan: the advisor stands behind it, or it rests on raw counters. Unique and primary key indexes are never counted.'

/**
 * What the indexes of each table cost, computed here from the index and table
 * statistics already loaded: pglens runs no statement.
 */
export function IndexTaxPanel({ instanceId, rows, database }: IndexTaxPanelProps) {
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
      description="What the indexes of each table cost: how many there are, how much space they hold, how much of it looks unused and how many index entries every write adds."
      notes={
        <>
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
        { label: 'Tables', value: formatCount(summary.tables) },
        {
          label: 'Index size',
          value: formatBytes(summary.indexBytes),
          detail: `${formatCount(summary.indexCount)} indexes`,
        },
        {
          label: 'Unused size',
          value: formatBytes(summary.unusedBytes),
          detail: `${formatBytes(summary.unusedConfirmedBytes)} advisor-confirmed`,
        },
        {
          label: 'Write amplification',
          value: formatRate(summary.writeAmplification),
          detail: 'index entries written',
        },
      ]}
      title="Index tax"
      titleId="tax-title"
    >
      {taxes.length === 0 ? (
        <EmptyState
          description="The agent has not reported index statistics for this server yet."
          title="No indexes to show"
        />
      ) : (
        <SimpleTable
          ariaLabel="Index tax"
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
