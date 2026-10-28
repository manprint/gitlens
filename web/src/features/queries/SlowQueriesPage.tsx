import { useMemo, useState, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { ArrowUpRight, ChevronDown, Database, Gauge, Hash, Server, Timer } from 'lucide-react'

import { useClusters, useInstanceDatabases, useStatements } from '@/api/queries'
import type { Cluster } from '@/api/types'
import { CopyButton } from '@/components/layout/CopyButton'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { MetricTile } from '@/components/layout/MetricTile'
import { PageHeader } from '@/components/layout/PageHeader'
import { SqlText } from '@/components/layout/SqlText'
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

/** How many statements the page ranks: the operator asked for the top 20. */
export const SLOWEST_LIMIT = 20

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

interface RankedRowProps {
  instanceId: string
  rank: number
  ranking: Ranking
  row: StatementRow
  share: number | null
  /** The row's ranked value relative to the first row, for the bar length. */
  relative: number
}

function RankedStatement({ instanceId, rank, ranking, row, share, relative }: RankedRowProps) {
  const summary = summariseStatement(row)
  const text = row.query_text ?? ''
  const [expanded, setExpanded] = useState(false)

  return (
    <li className="group hover:bg-muted/30 grid gap-4 px-5 py-4 transition-colors md:grid-cols-[2.5rem_minmax(0,1fr)_auto]">
      <div className="flex items-start">
        <span
          className={cn(
            'flex size-8 items-center justify-center rounded-lg text-sm font-semibold tabular-nums',
            rank <= 3
              ? 'bg-destructive/10 text-destructive ring-destructive/20 ring-1'
              : 'bg-muted text-muted-foreground',
          )}
        >
          {rank}
        </span>
      </div>

      <div className="min-w-0 space-y-2">
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <Link
            className="text-primary inline-flex items-center gap-1 font-semibold hover:underline"
            to={queryPath(instanceId, row.queryid, row.datname)}
          >
            Query {String(row.queryid)}
            <ArrowUpRight aria-hidden="true" className="size-3.5" />
          </Link>
          {row.datname ? (
            <Badge className="font-normal" variant="outline">
              <Database aria-hidden="true" />
              {row.datname}
            </Badge>
          ) : null}
          {row.truncated ? <Badge variant="warning">text truncated</Badge> : null}
        </div>
        {text ? (
          <div className="bg-muted/40 rounded-lg border">
            <div
              className={cn(
                'overflow-hidden px-3 py-2 break-words whitespace-pre-wrap',
                !expanded && 'line-clamp-2',
              )}
            >
              <SqlText sql={expanded ? text : normaliseQueryText(text)} />
            </div>
            <div className="flex items-center justify-between border-t px-1.5 py-0.5">
              <button
                aria-expanded={expanded}
                className="text-muted-foreground hover:text-foreground inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs font-medium transition-colors"
                onClick={() => setExpanded((current) => !current)}
                type="button"
              >
                <ChevronDown
                  aria-hidden="true"
                  className={cn('size-3.5 transition-transform', expanded && 'rotate-180')}
                />
                {expanded ? 'Collapse' : 'Show full query'}
              </button>
              <CopyButton text={text} />
            </div>
          </div>
        ) : (
          <p className="text-muted-foreground text-sm italic">Query text not available.</p>
        )}
        <div className="flex items-center gap-3" title="Relative to the slowest statement listed">
          <div className="bg-muted h-1.5 flex-1 overflow-hidden rounded-full">
            <div
              className="from-primary/70 to-primary h-full rounded-full bg-gradient-to-r"
              style={{ width: `${Math.max(2, Math.round(relative * 100))}%` }}
            />
          </div>
        </div>
      </div>

      <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-4 md:w-[26rem]">
        <div>
          <dt className="text-muted-foreground text-xs">Mean</dt>
          <dd
            className={cn(
              'font-semibold tabular-nums',
              ranking === 'mean' ? 'text-foreground text-base' : 'text-foreground/80',
            )}
          >
            {formatMilliseconds(summary.mean)}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground text-xs">Total</dt>
          <dd
            className={cn(
              'font-semibold tabular-nums',
              ranking === 'total' ? 'text-foreground text-base' : 'text-foreground/80',
            )}
          >
            {formatMilliseconds(summary.total)}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground text-xs">Calls</dt>
          <dd className="tabular-nums">
            {summary.calls === null ? '—' : formatCount(summary.calls)}
          </dd>
        </div>
        <div>
          <dt
            className="text-muted-foreground text-xs"
            title="Share of the total time of the statements listed here"
          >
            Share of listed time
          </dt>
          <dd className="tabular-nums">
            {share === null ? '—' : (formatPercent(share, 1) ?? '—')}
          </dd>
        </div>
      </dl>
    </li>
  )
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
      limit: SLOWEST_LIMIT,
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

  const rankedValues = summaries.map((summary) =>
    ranking === 'mean' ? summary.mean : summary.total,
  )
  const topValue = Math.max(0, ...rankedValues.map((value) => value ?? 0))
  const totalTime = summaries.reduce((sum, summary) => sum + (summary.total ?? 0), 0)
  const totalCalls = summaries.reduce((sum, summary) => sum + (summary.calls ?? 0), 0)
  const slowestMean = Math.max(0, ...summaries.map((summary) => summary.mean ?? 0))

  function setDatabase(value: string) {
    const next = new URLSearchParams(searchParams)
    if (value) next.set('db', value)
    else next.delete('db')
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
            <select
              className="border-input bg-card h-9 w-56 max-w-full rounded-md border pr-3 pl-8 text-sm shadow-xs"
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
            </select>
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
              unit={`of ${SLOWEST_LIMIT}`}
            />
          </div>

          <section
            aria-labelledby="slowest-list-title"
            className="bg-card shadow-card overflow-hidden rounded-xl border"
          >
            <div className="flex flex-wrap items-center justify-between gap-2 border-b px-5 py-4">
              <div>
                <h2 className="text-base font-semibold" id="slowest-list-title">
                  Top {SLOWEST_LIMIT} by {ranking === 'mean' ? 'mean time per call' : 'total time'}
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
              <ol aria-label="Slowest queries" className="divide-y">
                {rows.map((row, index) => (
                  <RankedStatement
                    key={`${row.datname ?? ''}-${String(row.queryid)}`}
                    instanceId={instanceId}
                    rank={index + 1}
                    ranking={ranking}
                    relative={topValue > 0 ? (rankedValues[index] ?? 0) / topValue : 0}
                    row={row}
                    share={shares[index] ?? null}
                  />
                ))}
              </ol>
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
    setSearchParams(next, { replace: true })
  }

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow="Query performance"
        icon={Gauge}
        subtitle={
          <>
            The {SLOWEST_LIMIT} statements with the longest execution time on a server, measured by{' '}
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
                    <select
                      className="border-input bg-card h-9 w-72 max-w-full rounded-md border pr-3 pl-8 text-sm shadow-xs"
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
                    </select>
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
