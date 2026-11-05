import { useMemo, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { Database, Gauge, Hash, Server, Timer } from 'lucide-react'

import { useClusters, useInstanceDatabases, useStatements } from '@/api/queries'
import type { Cluster } from '@/api/types'
import { CopyButton } from '@/components/layout/CopyButton'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { InfoHint } from '@/components/layout/InfoHint'
import { MetricTile } from '@/components/layout/MetricTile'
import { PageHeader } from '@/components/layout/PageHeader'
import { FULL_SEARCH_HINT, HighlightedText, SearchBox } from '@/components/layout/SearchBox'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useKeepFocus } from '@/hooks/useKeepFocus'
import { useLegacySearchParam, useSearchDraft, useShownTerm } from '@/hooks/useSearch'
import { useTimeRange } from '@/hooks/useTimeRange'
import { formatCount, formatMilliseconds, formatPercent } from '@/lib/format'
import { matchCountNote, searchTermOf } from '@/lib/highlight'
import {
  normaliseQueryText,
  shareOfTotal,
  summariseStatement,
  type StatementRow,
} from '@/lib/statements'
import { chooseInstance, readLastInstance } from '@/lib/lastInstance'
import { clusterLabel, instanceLabel } from '@/lib/names'
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

// The tiles share the list header, so from 1600 px they are one short line each (D26).
const HEADER_TILE = 'min-[1600px]:py-1.5'

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
      label: `${instanceLabel(instance)}${cluster.name ? ` · ${clusterLabel(cluster)}` : ''}`,
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
  /** Sent with `q` only: more statements match than the limit returned (D20). */
  has_more?: boolean
}

interface SlowRow {
  rank: number
  row: StatementRow
  mean: number | null
  total: number | null
  calls: number | null
  share: number | null
}

function slowColumns(instanceId: string, term: string): DataTableProps<SlowRow>['columns'] {
  return [
    {
      id: 'rank',
      header: 'Rank',
      accessorFn: (slow) => slow.rank,
      cell: ({ row }) => (
        <span
          className={cn(
            'flex size-7 items-center justify-center rounded-md text-xs font-semibold tabular-nums min-[1600px]:size-5',
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
            {/* The name starts with `Query <id>` and still holds the visible text (label in name). */}
            <Link
              className="text-primary block min-w-0 truncate font-mono text-xs hover:underline"
              title={statement.query_text ?? `Query ${String(statement.queryid)}`}
              to={queryPath(instanceId, statement.queryid, statement.datname)}
            >
              {text ? (
                <>
                  <span className="sr-only">Query {String(statement.queryid)}: </span>
                  <HighlightedText q={term} text={text} />
                </>
              ) : (
                `Query ${String(statement.queryid)}`
              )}
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
      meta: { help: 'statements.mean-time' },
      header: 'Mean time',
      accessorFn: (slow) => slow.mean,
      cell: ({ row }) => (
        <span className="whitespace-nowrap">{formatMilliseconds(row.original.mean)}</span>
      ),
    },
    {
      id: 'total',
      meta: { help: 'statements.total-time' },
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
  // The search runs on the server over the whole window (D20); one box, `?q=`.
  const q = searchParams.get('q') ?? ''
  const term = searchTermOf(q)
  // An older link searched the table (`slow.q`): it now searches the server.
  useLegacySearchParam('slow.q')
  // The box moves between the toolbar and a bar: its text lives here.
  const searchDraft = useSearchDraft(q)
  const databasesQuery = useInstanceDatabases(instanceId)
  const statementsQuery = useStatements(
    {
      instance_id: instanceId,
      limit: STATEMENT_FETCH_LIMIT,
      order_by: ORDER_BY[ranking],
      ...(database === undefined ? {} : { database }),
      ...(term === '' ? {} : { q: term }),
    },
    queryWindowOf(range),
  )
  const response = statementsQuery.data as StatementsPayload | undefined
  // The term the listed statements answer: the previous one while a new search loads.
  const shownTerm = useShownTerm(term, statementsQuery.isPlaceholderData, statementsQuery.isSuccess)
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
  const columns = useMemo(() => slowColumns(instanceId, shownTerm), [instanceId, shownTerm])
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

  function setSearch(value: string) {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        if (value.trim() === '') next.delete('q')
        else next.set('q', value)
        // Another search is another result: back to the first page (D23).
        next.delete('slow.page')
        // The table's own search, which this box replaces (D20).
        next.delete('slow.q')
        return next
      },
      { replace: true },
    )
  }

  // The no-match action: the search ends and the box keeps the keyboard.
  function clearSearch() {
    setSearch('')
    document.getElementById('slow-search')?.focus()
  }

  // The server, ranking and database controls. With statements to list they sit in
  // the table's toolbar line beside the search box (15.3, D26); otherwise in a bar.
  const controlBar = (
    <>
      <SearchBox
        busy={statementsQuery.isFetching && term !== ''}
        draft={searchDraft}
        hint={FULL_SEARCH_HINT}
        id="slow-search"
        label="Search statements"
        onChange={setSearch}
        placeholder="Query text, queryid or database"
        value={q}
      />
      {controls}
      <div className="space-y-1.5">
        <label
          className="text-muted-foreground text-xs font-medium min-[1600px]:sr-only"
          htmlFor="slow-db"
        >
          Database
        </label>
        <div className="relative">
          <Database
            aria-hidden="true"
            className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2"
          />
          <NativeSelect
            className="w-56 max-w-full pr-3 pl-8 min-[1600px]:h-8 min-[1600px]:w-44"
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
    </>
  )
  const keepFocus = useKeepFocus(rows.length === 0 ? 'bar' : 'table')
  // `contents`: the wrapper only keeps focus as the controls move, it adds no box.
  const movableControls = (
    <div className="contents" {...keepFocus}>
      {controlBar}
    </div>
  )
  const standaloneBar = (
    <div className="bg-card shadow-card flex flex-wrap items-end gap-x-6 gap-y-4 rounded-xl border p-4">
      {movableControls}
    </div>
  )
  const hint = RANKINGS.find((option) => option.value === ranking)?.hint

  if (statementsQuery.isPending && response === undefined) {
    return (
      <div className="space-y-6 min-[1600px]:space-y-4">
        {standaloneBar}
        <div aria-busy="true" aria-label="Loading slowest queries" role="status">
          Loading slowest queries…
        </div>
      </div>
    )
  }

  if (statementsQuery.error && response === undefined) {
    return (
      <div className="space-y-6 min-[1600px]:space-y-4">
        {standaloneBar}
        {statementsQuery.error.kind === 'not_found' ? (
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
        )}
      </div>
    )
  }

  return (
    <div className="space-y-6 min-[1600px]:space-y-4">
      {rows.length === 0 ? standaloneBar : null}
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

      <section
        aria-labelledby="slowest-list-title"
        className="bg-card shadow-card overflow-hidden rounded-xl border"
      >
        {/* The summary tiles share the list header; from 1600 px on one line (D26). */}
        <div className="flex flex-col gap-4 border-b px-5 py-4 min-[1600px]:flex-row min-[1600px]:items-center min-[1600px]:justify-between min-[1600px]:py-2">
          <div className="min-w-0">
            <h2 className="text-base font-semibold" id="slowest-list-title">
              Ranked by {ranking === 'mean' ? 'mean time per call' : 'total time'}
            </h2>
            <p
              className="text-muted-foreground mt-0.5 text-sm min-[1600px]:truncate"
              title={`${hint ?? ''} Open a query to run EXPLAIN and see its plan history.${response?.truncated ? ' Some statements may be missing: the agent kept only its top-N budget (checks.stat_statements.top_n) in this window.' : ''}`}
            >
              {hint} Open a query to run EXPLAIN and see its plan history.
              {response?.truncated ? (
                <>
                  {' '}
                  Some statements may be missing: the agent kept only its top-N budget (
                  <code>checks.stat_statements.top_n</code>) in this window.
                </>
              ) : null}
            </p>
          </div>
          <div className="grid gap-4 min-[1600px]:flex min-[1600px]:shrink-0 min-[1600px]:gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <MetricTile
              captionId="slow.slowest-mean"
              className={HEADER_TILE}
              label="Slowest mean time"
              value={rows.length === 0 ? null : formatMilliseconds(slowestMean)}
            />
            <MetricTile
              captionId="slow.total-time"
              className={HEADER_TILE}
              label="Time spent by these queries"
              value={rows.length === 0 ? null : formatMilliseconds(totalTime)}
            />
            <MetricTile
              captionId="slow.calls"
              className={HEADER_TILE}
              label="Calls"
              value={rows.length === 0 ? null : formatCount(totalCalls)}
            />
            <MetricTile
              captionId="slow.statements-ranked"
              className={HEADER_TILE}
              detail={`The page ranks at most ${STATEMENT_FETCH_LIMIT} statements.`}
              label="Statements ranked"
              unit="statements"
              value={String(rows.length)}
            />
          </div>
        </div>
        {rows.length === 0 ? (
          <div className="p-5">
            {shownTerm === '' ? (
              <EmptyState
                description="No pg_stat_statements entries match this server, database and time range. Try a wider range."
                title="No statements yet"
              />
            ) : (
              <EmptyState
                action={
                  <Button onClick={() => clearSearch()} size="sm" type="button" variant="outline">
                    Show all statements
                  </Button>
                }
                description="The search covers every statement collected for this server and database in the time range, not only the ranked ones. Try another term or a wider range."
                title={`No statement matches “${shownTerm}” in this range`}
              />
            )}
          </div>
        ) : (
          <div className="p-5 min-[1600px]:p-3">
            <DataTable
              ariaLabel="Slowest queries"
              captionId="slow.table"
              columns={columns}
              data={slowRows}
              emptyState={null}
              {...(shownTerm === ''
                ? {}
                : { footerNote: matchCountNote(rows.length, response?.has_more === true) })}
              scope="statements"
              tableId="slow"
              toolbarExtra={movableControls}
            />
          </div>
        )}
      </section>
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
    <div className="space-y-6 min-[1600px]:space-y-4">
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
                    className="text-muted-foreground text-xs font-medium min-[1600px]:sr-only"
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
                      className="w-72 max-w-full pr-3 pl-8 min-[1600px]:h-8 min-[1600px]:w-56"
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
                  <legend className="text-muted-foreground mb-1.5 text-xs font-medium min-[1600px]:sr-only">
                    Rank by
                  </legend>
                  <div className="flex items-center gap-1">
                    <div className="bg-muted inline-flex rounded-lg p-1 min-[1600px]:p-0.5">
                      {RANKINGS.map((option) => {
                        const Icon = option.value === 'mean' ? Timer : Hash
                        const active = option.value === ranking
                        return (
                          <label
                            key={option.value}
                            className={cn(
                              'has-[:focus-visible]:ring-ring inline-flex h-8 cursor-pointer items-center gap-1.5 rounded-md px-3 text-sm font-medium transition-colors has-[:focus-visible]:ring-2 min-[1600px]:h-7',
                              active
                                ? 'bg-card text-foreground shadow-sm'
                                : 'text-muted-foreground hover:text-foreground',
                            )}
                            title={option.hint}
                          >
                            <input
                              checked={active}
                              id={`slow-rank-${option.value}`}
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
                    <InfoHint id="slow.rank-by" label="Rank by" />
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
