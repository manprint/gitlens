import { useMemo } from 'react'

import { Link, useParams, useSearchParams } from 'react-router-dom'

import { useStatements } from '@/api/queries'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
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
import { queryWindowOf } from '@/lib/timerange'

const STATEMENT_LIMIT = 50
type StatementSort = StatementOrderBy

const DEFAULT_SORT: StatementSort = 'total_exec_time'

// The server has always ranked by temporary blocks and WAL too; the list
// offered neither, so the statements spilling to disk or writing the most
// WAL could only be found by paging through another ranking.
const SORT_OPTIONS: readonly { value: StatementSort; label: string }[] = [
  { value: 'total_exec_time', label: 'Total time' },
  { value: 'mean_exec_time', label: 'Mean time' },
  { value: 'calls', label: 'Calls' },
  { value: 'rows', label: 'Rows' },
  { value: 'shared_blks_read', label: 'Shared blocks read' },
  { value: 'temp_blks_written', label: 'Temporary blocks written' },
  { value: 'wal_bytes', label: 'WAL bytes' },
]

interface QueryListPageProps {
  instanceId?: string
}

interface StatementDisplayRow extends StatementRow {
  totalShare: number | null
}

interface StatementApiResponse {
  disabled?: boolean
  disabled_reason?: string
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
      id: 'wal-bytes',
      header: 'WAL',
      enableSorting: false,
      cell: ({ row }) => {
        const walBytes = row.original.wal_bytes
        return (
          <span className="tabular-nums">
            {typeof walBytes === 'number' && Number.isFinite(walBytes)
              ? formatBytes(walBytes)
              : '—'}
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

function skipSentence(reason: string | undefined): string {
  return reason
    ? `The agent skips the stat_statements check: ${reason}.`
    : 'The agent does not collect statements for this instance.'
}

// There is no configuration key to turn back on: the agent skips the check
// when, at startup, it finds no pg_stat_statements view it can read.
function DisabledStatementsState({ reason }: { reason: string | undefined }) {
  return (
    <div className="space-y-4">
      <PageHeader
        title="Query inspector"
        subtitle="Inspect the statements collected for the selected instance."
      />
      <div role="status" className="border-muted bg-muted/30 text-sm">
        <strong>pg_stat_statements disabled.</strong> {skipSentence(reason)}
      </div>
      <p className="text-muted-foreground text-sm">
        The agent looks for the view when it starts, in the database its DSN names and then in the
        monitored databases; the module must also be in <code>shared_preload_libraries</code>.{' '}
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
  const params = {
    instance_id: instanceId,
    limit: STATEMENT_LIMIT,
    order_by: sort,
    ...(database === undefined ? {} : { database }),
  }
  const statementsQuery = useStatements(params, queryWindowOf(range))

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
    return <DisabledStatementsState reason={undefined} />
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

  const disabled = response !== undefined && isDisabledResponse(response)
  // Statements collected before the check was skipped are still shown.
  if (disabled && rows.length === 0) {
    return <DisabledStatementsState reason={response.disabled_reason} />
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
        {disabled ? (
          <p
            role="status"
            className="border-warning bg-warning/10 mb-4 rounded-lg border p-3 text-sm"
          >
            {skipSentence(response.disabled_reason)} The statements below were collected earlier.
          </p>
        ) : null}
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
