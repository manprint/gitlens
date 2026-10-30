import { useMemo, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { Database, Gauge, Hash, Server, Timer } from 'lucide-react'

import { useClusters, useInstanceDatabases, useStatements } from '@/api/queries'
import type { Cluster } from '@/api/types'
import { CopyButton } from '@/components/layout/CopyButton'
import {
  DataTable,
  type DataTableProps,
  type DataTableToolbar,
} from '@/components/layout/DataTable'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { MetricTile } from '@/components/layout/MetricTile'
import { PageHeader } from '@/components/layout/PageHeader'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { useTimeRange } from '@/hooks/useTimeRange'
import { formatCount, formatMilliseconds, formatPercent } from '@/lib/format'
import {
  normaliseQueryText,
  shareOfTotal,
  summariseStatement,
  type StatementRow,
} from '@/lib/statements'
import { chooseInstance, readLastInstance } from '@/lib/lastInstance'
import { queryWindowOf } from '@/lib/timerange'
import { cn } from '@/lib/utils'
import { NativeSelect } from '@/components/ui/native-select'

/**
 * How many statements the page loads and ranks; the table shows 20 per page
 * (D23). The server caps a request at 1000.
 */
export const STATEMENT_FETCH_LIMIT = 200

type Ranking = 'mean' | 'total'

const RANKINGS: readonly { value: Ranking; label: string; hint: string }[] = [
  {
    value: 'mean',
    label: 'Mean time per call',
    hint: 'The statements that take longest each time they run.',
  },
  {
    value: 'total',
    label: 'Total time',
    hint: 'The statements that consume the most database time overall (mean × calls).',
  },
]

const ORDER_BY = { mean: 'mean_exec_time', total: 'total_exec_time' } as const

function readRanking(value: string | null): Ranking {
  return value === 'total' ? 'total' : 'mean'
}

interface InstanceOption {
  id: string
  label: string
  up: boolean
}

function instanceOptions(clusters: readonly Cluster[]): InstanceOption[] {
  return clusters.flatMap((cluster) =>
    cluster.instances.map((instance) => ({
      id: instance.instance_id,
      label: `${instance.addr}:${instance.port}${cluster.name ? ` · ${cluster.name}` : ''}`,
      up: instance.up,
    })),
  )
}

function queryPath(instanceId: string, queryid: number | string, datname?: string | null): string {
  const path = `/instances/${encodeURIComponent(instanceId)}/queries/${encodeURIComponent(String(queryid))}`
  return datname ? `${path}?db=${encodeURIComponent(datname)}` : path
}

interface StatementsPayload {
  disabled?: boolean
  disabled_reason?: string
  statements: StatementRow[]
  truncated: boolean
}

interface SlowRow {
  rank: number
  row: StatementRow
  mean: number | null
  total: number | null
  calls: number | null
  share: number | null
}

// The search acts on the loaded statements until the server search arrives (D20).
const SLOW_TOOLBAR: DataTableToolbar<SlowRow> = {
  searchFields: [
    (slow) => slow.row.query_text,
    (slow) => String(slow.row.queryid),
    (slow) => slow.row.datname,
  ],
}

function slowColumns(instanceId: string): DataTableProps<SlowRow>['columns'] {
  return [
    {
      id: 'rank',
      header: 'Rank',
      accessorFn: (slow) => slow.rank,
      cell: ({ row }) => (
        <span
          className={cn(
            'flex size-7 items-center justify-center rounded-md text-xs font-semibold tabular-nums',
            row.original.rank <= 3
              ? 'bg-destructive/10 text-destructive ring-destructive/20 ring-1'
              : 'bg-muted text-muted-foreground',
          )}
        >
          {row.original.rank}
        </span>
      ),
    },
    {
      id: 'query',
      header: 'Query',
      enableSorting: false,
      cell: ({ row }) => {
        const { row: statement } = row.original
        const text = statement.query_text ? normaliseQueryText(statement.query_text) : ''
        return (
          <div className="flex max-w-[36rem] min-w-0 items-center gap-2">
            <Link
              aria-label={`Query ${String(statement.queryid)}`}
              className="text-primary block min-w-0 truncate font-mono text-xs hover:underline"
              title={statement.query_text ?? `Query ${String(statement.queryid)}`}
              to={queryPath(instanceId, statement.queryid, statement.datname)}
            >
              {text || `Query ${String(statement.queryid)}`}
            </Link>
            {statement.truncated ? <Badge variant="warning">text truncated</Badge> : null}
            {statement.query_text ? <CopyButton text={statement.query_text} /> : null}
          </div>
        )
      },
    },
    {
      id: 'database',
      header: 'Database',
      accessorFn: (slow) => slow.row.datname ?? null,
      cell: ({ row }) => row.original.row.datname ?? '—',
    },
    {
      id: 'calls',
      header: 'Calls',
      accessorFn: (slow) => slow.calls,
      cell: ({ row }) => (row.original.calls === null ? '—' : formatCount(row.original.calls)),
    },
    {
      id: 'mean',
      header: 'Mean time',
      accessorFn: (slow) => slow.mean,
      cell: ({ row }) => (
        <span className="whitespace-nowrap">{formatMilliseconds(row.original.mean)}</span>
      ),
    },
    {
      id: 'total',
      header: 'Total time',
      accessorFn: (slow) => slow.total,
      cell: ({ row }) => (
        <span className="whitespace-nowrap">{formatMilliseconds(row.original.total)}</span>
      ),
    },
    {
      id: 'share',
      header: 'Share of listed time',
      accessorFn: (slow) => slow.share,
      cell: ({ row }) => {
        const { share } = row.original
        return (
          <div className="flex min-w-24 items-center gap-2">
            <span className="w-12 text-right tabular-nums">
              {share === null ? '—' : (formatPercent(share, 1) ?? '—')}
            </span>
            <div className="bg-muted h-1.5 w-16 overflow-hidden rounded-full" aria-hidden="true">
              <div
                className="from-primary/70 to-primary h-full rounded-full bg-gradient-to-r"
                style={{ width: `${Math.max(2, Math.round((share ?? 0) * 100))}%` }}
              />
            </div>
          </div>
        )
      },
    },
  ]
}

interface InstanceStatementsProps {
  /** The server and ranking controls, shown in one filter bar with the database. */
  controls: ReactNode
  instanceId: string
  ranking: Ranking
}

function InstanceStatements({ controls, instanceId, ranking }: InstanceStatementsProps) {
  const [searchParams, setSearchParams] = useSearchParams()
  const { range } = useTimeRange()
  const database = searchParams.get('db') ?? undefined
  const databasesQuery = useInstanceDatabases(instanceId)
  const statementsQuery = useStatements(
    {
      instance_id: instanceId,
      limit: STATEMENT_FETCH_LIMIT,
      order_by: ORDER_BY[ranking],
      ...(database === undefined ? {} : { database }),
    },
    queryWindowOf(range),
  )
  const response = statementsQuery.data as StatementsPayload | undefined
  const rows = useMemo(() => response?.statements ?? [], [response?.statements])
  const shares = useMemo(() => shareOfTotal(rows), [rows])
  const summaries = useMemo(() => rows.map((row) => summariseStatement(row)), [rows])
  const monitored = (databasesQuery.data?.databases ?? []).filter((entry) => entry.monitored)

  const slowRows = useMemo<SlowRow[]>(
    () =>
      rows.map((row, index) => ({
        rank: index + 1,
        row,
        mean: summaries[index]?.mean ?? null,
        total: summaries[index]?.total ?? null,
        calls: summaries[index]?.calls ?? null,
        share: shares[index] ?? null,
      })),
    [rows, shares, summaries],
  )
  const columns = useMemo(() => slowColumns(instanceId), [instanceId])
  const totalTime = summaries.reduce((sum, summary) => sum + (summary.total ?? 0), 0)
  const totalCalls = summaries.reduce((sum, summary) => sum + (summary.calls ?? 0), 0)
  const slowestMean = Math.max(0, ...summaries.map((summary) => summary.mean ?? 0))

  function setDatabase(value: string) {
    const next = new URLSearchParams(searchParams)
    if (value) next.set('db', value)
    else next.delete('db')
    // Another database is a new ranking: back to the first page (D23).
    next.delete('slow.page')
    setSearchParams(next, { replace: true })
  }

  return (
    <div className="space-y-6">
      <div className="bg-card shadow-card flex flex-wrap items-end gap-x-6 gap-y-4 rounded-xl border p-4">
        {controls}
        <div className="space-y-1.5">
          <label className="text-muted-foreground text-xs font-medium" htmlFor="slow-db">
            Database
          </label>
          <div className="relative">
            <Database
              aria-hidden="true"
              className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2"
            />
            <NativeSelect
              className="w-56 max-w-full pr-3 pl-8"
              id="slow-db"
              onChange={(event) => setDatabase(event.target.value)}
              value={database ?? ''}
            >
              <option value="">All databases</option>
              {monitored.map((entry) => (
                <option key={entry.datname} value={entry.datname}>
                  {entry.datname}
                </option>
              ))}
              {database !== undefined && !monitored.some((entry) => entry.datname === database) ? (
                <option value={database}>{database}</option>
              ) : null}
            </NativeSelect>
          </div>
        </div>
        <div className="ml-auto self-center">
          <FreshnessBadge dataUpdatedAt={statementsQuery.dataUpdatedAt} policy="statements" />
        </div>
      </div>

      {statementsQuery.isPending && response === undefined ? (
        <div aria-busy="true" aria-label="Loading slowest queries" role="status">
          Loading slowest queries…
        </div>
      ) : statementsQuery.error && response === undefined ? (
        statementsQuery.error.kind === 'not_found' ? (
          <EmptyState
            description="This instance does not report pg_stat_statements. Add the module to shared_preload_libraries and create the extension, then restart the agent."
            title="pg_stat_statements is not available"
          />
        ) : (
          <ErrorState
            endpoint="statements"
            failure={statementsQuery.error}
            onRetry={() => void statementsQuery.refetch()}
          />
        )
      ) : (
        <>
          {response?.disabled ? (
            <div
              className="border-warning/40 bg-warning/10 rounded-lg border px-4 py-3 text-sm"
              role="status"
            >
              <strong>pg_stat_statements is not collected.</strong>{' '}
              {response.disabled_reason
                ? `The agent skips the check: ${response.disabled_reason}.`
                : 'The agent skips the check on this instance.'}{' '}
              Statements collected earlier are still shown.
            </div>
          ) : null}

          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <MetricTile
              label="Slowest mean time"
              value={rows.length === 0 ? null : formatMilliseconds(slowestMean)}
            />
            <MetricTile
              label="Time spent by these queries"
              value={rows.length === 0 ? null : formatMilliseconds(totalTime)}
            />
            <MetricTile label="Calls" value={rows.length === 0 ? null : formatCount(totalCalls)} />
            <MetricTile
              label="Statements ranked"
              value={String(rows.length)}
              unit={`of ${STATEMENT_FETCH_LIMIT} max`}
            />
          </div>

          <section
            aria-labelledby="slowest-list-title"
            className="bg-card shadow-card overflow-hidden rounded-xl border"
          >
            <div className="flex flex-wrap items-center justify-between gap-2 border-b px-5 py-4">
              <div>
                <h2 className="text-base font-semibold" id="slowest-list-title">
                  Ranked by {ranking === 'mean' ? 'mean time per call' : 'total time'}
                </h2>
                <p className="text-muted-foreground mt-0.5 text-sm">
                  {RANKINGS.find((option) => option.value === ranking)?.hint} Open a query to run
                  EXPLAIN and see its plan history.
                </p>
              </div>
            </div>
            {rows.length === 0 ? (
              <div className="p-5">
                <EmptyState
                  description="No pg_stat_statements entries match this server, database and time range. Try a wider range."
                  title="No statements yet"
                />
              </div>
            ) : (
              <div className="p-5">
                <DataTable
                  ariaLabel="Slowest queries"
                  columns={columns}
                  data={slowRows}
                  emptyState={null}
                  scope="statements"
                  tableId="slow"
                  toolbar={SLOW_TOOLBAR}
                />
              </div>
            )}
            {response?.truncated ? (
              <p className="text-muted-foreground border-t px-5 py-3 text-xs">
                Some statements may be missing: the agent kept only its top-N budget (
                <code>checks.stat_statements.top_n</code>) in this window.
              </p>
            ) : null}
          </section>
        </>
      )}
    </div>
  )
}

export function SlowQueriesPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const clustersQuery = useClusters()
  const options = useMemo(() => instanceOptions(clustersQuery.data ?? []), [clustersQuery.data])
  const instanceId = chooseInstance(
    clustersQuery.data ?? [],
    searchParams.get('instance'),
    readLastInstance(),
  )
  const ranking = readRanking(searchParams.get('rank'))

  function update(key: 'instance' | 'rank', value: string) {
    const next = new URLSearchParams(searchParams)
    next.set(key, value)
    // A database belongs to one instance, so switching server clears it.
    if (key === 'instance') next.delete('db')
    // A new server or ranking starts on the first page (D23).
    next.delete('slow.page')
    setSearchParams(next, { replace: true })
  }

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow="Query performance"
        icon={Gauge}
        subtitle={
          <>
            The statements with the longest execution time on a server, measured by{' '}
            <code>pg_stat_statements</code> over the selected time range. Use it to find the queries
            worth optimising first.
          </>
        }
        title="Slowest queries"
      />

      {clustersQuery.error && clustersQuery.data === undefined ? (
        <ErrorState
          endpoint="clusters"
          failure={clustersQuery.error}
          onRetry={() => void clustersQuery.refetch()}
        />
      ) : clustersQuery.isPending ? (
        <div aria-busy="true" aria-label="Loading servers" role="status">
          Loading servers…
        </div>
      ) : instanceId === null ? (
        <EmptyState
          description="Set up the pglens agent on a PostgreSQL host to start collecting statements."
          title="No monitored servers yet"
        />
      ) : (
        <>
          <InstanceStatements
            key={instanceId}
            controls={
              <>
                <div className="space-y-1.5">
                  <label
                    className="text-muted-foreground text-xs font-medium"
                    htmlFor="slow-instance"
                  >
                    Server
                  </label>
                  <div className="relative">
                    <Server
                      aria-hidden="true"
                      className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2"
                    />
                    <NativeSelect
                      className="w-72 max-w-full pr-3 pl-8"
                      id="slow-instance"
                      onChange={(event) => update('instance', event.target.value)}
                      value={instanceId}
                    >
                      {options.map((option) => (
                        <option key={option.id} value={option.id}>
                          {option.label}
                          {option.up ? '' : ' (down)'}
                        </option>
                      ))}
                    </NativeSelect>
                  </div>
                </div>
                <fieldset>
                  <legend className="text-muted-foreground mb-1.5 text-xs font-medium">
                    Rank by
                  </legend>
                  <div className="bg-muted inline-flex rounded-lg p-1">
                    {RANKINGS.map((option) => {
                      const Icon = option.value === 'mean' ? Timer : Hash
                      const active = option.value === ranking
                      return (
                        <label
                          key={option.value}
                          className={cn(
                            'has-[:focus-visible]:ring-ring inline-flex h-8 cursor-pointer items-center gap-1.5 rounded-md px-3 text-sm font-medium transition-colors has-[:focus-visible]:ring-2',
                            active
                              ? 'bg-card text-foreground shadow-sm'
                              : 'text-muted-foreground hover:text-foreground',
                          )}
                          title={option.hint}
                        >
                          <input
                            checked={active}
                            className="sr-only"
                            name="slow-rank"
                            onChange={() => update('rank', option.value)}
                            type="radio"
                            value={option.value}
                          />
                          <Icon aria-hidden="true" className="size-4" />
                          {option.label}
                        </label>
                      )
                    })}
                  </div>
                </fieldset>
              </>
            }
            instanceId={instanceId}
            ranking={ranking}
          />
        </>
      )}
    </div>
  )
}

export default SlowQueriesPage
