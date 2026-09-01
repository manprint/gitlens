import { useMemo } from 'react'

import { Link, useParams, useSearchParams } from 'react-router-dom'

import { useStatements } from '@/api/queries'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { Disabled } from '@/components/state/Disabled'
import { EmptyState } from '@/components/state/EmptyState'
import { ErrorState } from '@/components/state/ErrorState'
import { Truncated } from '@/components/state/Truncated'
import { Unknown } from '@/components/state/Unknown'
import { formatBytes, formatCount, formatPercent } from '@/lib/format'
import {
  normaliseQueryText,
  shareOfTotal,
  summariseStatement,
  type StatementOrderBy,
  type StatementRow,
} from '@/lib/statements'
import { useTimeRange } from '@/hooks/useTimeRange'

const STATEMENT_LIMIT = 50
type StatementSort = Extract<
  StatementOrderBy,
  'total_exec_time' | 'mean_exec_time' | 'calls' | 'rows' | 'shared_blks_read'
>

const DEFAULT_SORT: StatementSort = 'total_exec_time'

const SORT_OPTIONS: readonly { value: StatementSort; label: string }[] = [
  { value: 'total_exec_time', label: 'Total time' },
  { value: 'mean_exec_time', label: 'Mean time' },
  { value: 'calls', label: 'Calls' },
  { value: 'rows', label: 'Rows' },
  { value: 'shared_blks_read', label: 'Shared blocks read' },
]

interface QueryListPageProps {
  instanceId?: string
}

interface StatementDisplayRow extends StatementRow {
  totalShare: number | null
}

interface StatementApiResponse {
  disabled?: boolean
  enabled?: boolean
  statements: StatementRow[]
  truncated: boolean
}

function readSort(value: string | null): StatementSort {
  return SORT_OPTIONS.some((option) => option.value === value)
    ? (value as StatementSort)
    : DEFAULT_SORT
}

function queryPath(instanceId: string, queryid: number | string): string {
  return `/instances/${encodeURIComponent(instanceId)}/queries/${encodeURIComponent(String(queryid))}`
}

function formatNumber(value: number | null | undefined): string {
  return value == null
    ? '—'
    : new Intl.NumberFormat('en-US', { maximumFractionDigits: 2 }).format(value)
}

function formatMilliseconds(value: number | null | undefined): string {
  return value == null ? '—' : `${formatNumber(value)} ms`
}

function queryTextCell(row: StatementDisplayRow, instanceId: string) {
  const queryText = row.query_text ?? ''
  const normalised = normaliseQueryText(queryText)

  return (
    <div className="min-w-64 space-y-1">
      <Link
        className="text-primary text-xs underline-offset-4 hover:underline"
        to={queryPath(instanceId, row.queryid)}
      >
        Query {String(row.queryid)}
      </Link>
      {queryText ? (
        <details className="max-w-xl">
          <summary className="cursor-pointer truncate font-mono text-sm" title={normalised}>
            {normalised}
          </summary>
          <pre className="bg-muted mt-2 max-h-48 overflow-auto rounded p-2 font-mono text-xs whitespace-pre-wrap">
            {queryText}
          </pre>
        </details>
      ) : (
        <Unknown />
      )}
    </div>
  )
}

function statementColumns(instanceId: string): DataTableProps<StatementDisplayRow>['columns'] {
  return [
    {
      id: 'query',
      header: 'Query',
      enableSorting: false,
      cell: ({ row }) => queryTextCell(row.original, instanceId),
    },
    {
      id: 'calls',
      header: 'Calls',
      enableSorting: false,
      cell: ({ row }) => {
        const summary = summariseStatement(row.original)
        return (
          <span className="tabular-nums">
            {summary.calls == null ? '—' : formatCount(summary.calls)}
          </span>
        )
      },
    },
    {
      id: 'total-time',
      header: 'Total time',
      enableSorting: false,
      cell: ({ row }) => {
        const summary = summariseStatement(row.original)
        return <span className="tabular-nums">{formatMilliseconds(summary.total)}</span>
      },
    },
    {
      id: 'mean-time',
      header: 'Mean time',
      enableSorting: false,
      cell: ({ row }) => {
        const summary = summariseStatement(row.original)
        return <span className="tabular-nums">{formatMilliseconds(summary.mean)}</span>
      },
    },
    {
      id: 'share-total',
      header: 'Share of total',
      enableSorting: false,
      cell: ({ row }) => {
        const share = row.original.totalShare
        return (
          <span className="tabular-nums">
            {share == null ? '—' : (formatPercent(share, 1) ?? '—')}
          </span>
        )
      },
    },
    {
      id: 'rows',
      header: 'Rows',
      enableSorting: false,
      cell: ({ row }) => <span className="tabular-nums">{formatNumber(row.original.rows)}</span>,
    },
    {
      id: 'cache-hit-ratio',
      header: 'Cache hit ratio',
      enableSorting: false,
      cell: ({ row }) => {
        const summary = summariseStatement(row.original)
        return (
          <span className="tabular-nums">
            {summary.cacheHitRatio == null ? '—' : (formatPercent(summary.cacheHitRatio, 1) ?? '—')}
          </span>
        )
      },
    },
    {
      id: 'temporary-bytes',
      header: 'Temporary bytes/call',
      enableSorting: false,
      cell: ({ row }) => {
        const summary = summariseStatement(row.original)
        return (
          <span className="tabular-nums">
            {summary.tempBytesPerCall == null ? '—' : formatBytes(summary.tempBytesPerCall)}
          </span>
        )
      },
    },
  ]
}

function isDisabledResponse(data: StatementApiResponse): boolean {
  return data.disabled === true || data.enabled === false
}

function DisabledStatementsState() {
  return (
    <div className="space-y-4">
      <PageHeader
        title="Query inspector"
        subtitle="Inspect the statements collected for the selected instance."
      />
      <Disabled feature="pg_stat_statements" configKey="pg_stat_statements" />
      <p className="text-muted-foreground text-sm">
        <a className="underline" href="/README.md#agent-configuration">
          See the README extension guidance.
        </a>
      </p>
    </div>
  )
}

export function QueryListPage({ instanceId: instanceIdOverride }: QueryListPageProps = {}) {
  const { instanceId: routeInstanceId } = useParams<{ instanceId: string }>()
  const [searchParams, setSearchParams] = useSearchParams()
  const { range } = useTimeRange()
  const instanceId = instanceIdOverride ?? routeInstanceId ?? ''
  const sort = readSort(searchParams.get('sort'))
  const database = searchParams.get('db') ?? undefined
  const rangeSearchKey = [
    searchParams.get('range'),
    searchParams.get('from'),
    searchParams.get('to'),
  ].join('|')
  // Relative ranges are resolved from the clock; keep their request window
  // stable between renders so a completed query does not change its key.
  const requestRange = useMemo(
    () => ({ from: range.from.toISOString(), to: range.to.toISOString() }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [rangeSearchKey],
  )
  const params = {
    from: requestRange.from,
    instance_id: instanceId,
    limit: STATEMENT_LIMIT,
    order_by: sort,
    to: requestRange.to,
    ...(database === undefined ? {} : { database }),
  }
  const statementsQuery = useStatements(params)

  const response = statementsQuery.data as StatementApiResponse | undefined
  const rows = useMemo<StatementDisplayRow[]>(() => {
    const statementRows = response?.statements ?? []
    const shares = shareOfTotal(statementRows)
    return statementRows.map((row, index) => ({
      ...row,
      totalShare: shares[index] ?? null,
    }))
  }, [response?.statements])

  const onSortChange = (value: string) => {
    const next = new URLSearchParams(searchParams)
    next.set('sort', readSort(value))
    setSearchParams(next, { replace: true })
  }

  if (statementsQuery.isPending && response === undefined) {
    return <div role="status">Loading statements…</div>
  }

  if (statementsQuery.error?.kind === 'not_found') {
    return <DisabledStatementsState />
  }

  if (statementsQuery.error && response === undefined) {
    return (
      <ErrorState
        endpoint="statements"
        failure={statementsQuery.error}
        onRetry={() => void statementsQuery.refetch()}
      />
    )
  }

  if (response && isDisabledResponse(response)) {
    return <DisabledStatementsState />
  }

  const truncated = response?.truncated === true

  return (
    <div className="space-y-8">
      <PageHeader
        title="Query inspector"
        subtitle="Inspect the statements collected for the selected instance."
        freshness={
          <FreshnessBadge dataUpdatedAt={statementsQuery.dataUpdatedAt} policy="statements" />
        }
      />
      <Section
        id="statement-list"
        title="Statements"
        description="Statement statistics are scoped to the selected database and time range."
      >
        <div className="mb-4 flex items-center gap-2">
          <label className="text-sm font-medium" htmlFor="statement-sort">
            Sort statements
          </label>
          <select
            id="statement-sort"
            className="bg-background rounded border px-2 py-1 text-sm"
            value={sort}
            onChange={(event) => onSortChange(event.target.value)}
          >
            {SORT_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </div>
        <DataTable
          ariaLabel="Statement statistics"
          {...(truncated ? { budget: STATEMENT_LIMIT } : {})}
          columns={statementColumns(instanceId)}
          data={rows}
          emptyState={
            <EmptyState
              title="No statements"
              description="No pg_stat_statements entries match the selected instance, database, and time range."
            />
          }
          scope="statements"
        />
        {truncated ? (
          <div className="text-muted-foreground mt-3 text-sm">
            <Truncated shown={rows.length} budget={STATEMENT_LIMIT} scope="statements" />{' '}
            <span>
              The top-N budget is configured by <code>checks.stat_statements.top_n</code>.
            </span>
          </div>
        ) : null}
        <p className="text-muted-foreground mt-4 text-sm">
          Entries beyond <code>pg_stat_statements.max</code> are evicted and may appear and
          disappear. <code>queryid</code> values are comparable only within this cluster.
        </p>
      </Section>
    </div>
  )
}

export default QueryListPage
