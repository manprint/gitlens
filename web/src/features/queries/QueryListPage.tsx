import { Zap } from 'lucide-react'
import { useMemo } from 'react'

import { Link, useParams, useSearchParams } from 'react-router-dom'

import { useStatements } from '@/api/queries'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { InfoHint } from '@/components/layout/InfoHint'
import { PageHeader } from '@/components/layout/PageHeader'
import { FULL_SEARCH_HINT, SearchBox } from '@/components/layout/SearchBox'
import { Section } from '@/components/layout/Section'
import { SqlText } from '@/components/layout/SqlText'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/state/EmptyState'
import { ErrorState } from '@/components/state/ErrorState'
import { Truncated } from '@/components/state/Truncated'
import { Unknown } from '@/components/state/Unknown'
import { formatBytes, formatCount, formatMilliseconds, formatPercent } from '@/lib/format'
import { matchCountNote, searchTermOf } from '@/lib/highlight'
import { useKeepFocus } from '@/hooks/useKeepFocus'
import { useLegacySearchParam, useSearchDraft, useShownTerm } from '@/hooks/useSearch'
import {
  normaliseQueryText,
  shareOfTotal,
  summariseStatement,
  type StatementOrderBy,
  type StatementRow,
} from '@/lib/statements'
import { useTimeRange } from '@/hooks/useTimeRange'
import { queryWindowOf } from '@/lib/timerange'
import { NativeSelect } from '@/components/ui/native-select'

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
  /** Sent with `q` only: more statements match than the limit returned (D20). */
  has_more?: boolean
}

function readSort(value: string | null): StatementSort {
  return SORT_OPTIONS.some((option) => option.value === value)
    ? (value as StatementSort)
    : DEFAULT_SORT
}

// The same queryid can name different statements in different databases, and
// EXPLAIN runs in the database it is given, so the link carries the database.
function queryPath(instanceId: string, queryid: number | string, datname?: string | null): string {
  const path = `/instances/${encodeURIComponent(instanceId)}/queries/${encodeURIComponent(String(queryid))}`
  return datname ? `${path}?db=${encodeURIComponent(datname)}` : path
}

function formatNumber(value: number | null | undefined): string {
  return value == null
    ? '—'
    : new Intl.NumberFormat('en-US', { maximumFractionDigits: 2 }).format(value)
}

function queryTextCell(row: StatementDisplayRow, instanceId: string, term: string) {
  const queryText = row.query_text ?? ''
  const normalised = normaliseQueryText(queryText)

  return (
    // From 1600 px the link and the statement share one line (D26).
    <div className="min-w-64 space-y-1 min-[1600px]:flex min-[1600px]:items-center min-[1600px]:gap-2 min-[1600px]:space-y-0">
      <Link
        className="text-primary text-xs font-semibold underline-offset-4 hover:underline min-[1600px]:shrink-0 min-[1600px]:whitespace-nowrap"
        to={queryPath(instanceId, row.queryid, row.datname)}
      >
        Query {String(row.queryid)}
      </Link>
      {queryText ? (
        <details className="max-w-xl min-[1600px]:min-w-0">
          <summary
            className="cursor-pointer truncate text-sm min-[1600px]:leading-5 min-[1600px]:[&_code]:leading-5"
            title={normalised}
          >
            <SqlText highlight={term} sql={normalised} />
          </summary>
          <pre className="bg-muted/60 mt-2 max-h-48 overflow-auto rounded-lg border p-3 text-xs whitespace-pre-wrap">
            <SqlText highlight={term} sql={queryText} />
          </pre>
        </details>
      ) : (
        <Unknown />
      )}
    </div>
  )
}

// The `Sort statements` select ranks on the server and the search box filters
// there, over the whole window (D20); the column sort buttons reorder the
// loaded statements.
function statementColumns(
  instanceId: string,
  term: string,
): DataTableProps<StatementDisplayRow>['columns'] {
  return [
    {
      id: 'query',
      header: 'Query',
      // Takes the width the fixed-width figures leave from 1600 px (D26).
      enableSorting: false,
      cell: ({ row }) => queryTextCell(row.original, instanceId, term),
    },
    {
      id: 'calls',
      meta: { width: '7rem' },
      header: 'Calls',
      accessorFn: (row) => summariseStatement(row).calls,
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
      meta: { width: '8rem', help: 'statements.total-time' },
      header: 'Total time',
      accessorFn: (row) => summariseStatement(row).total,
      cell: ({ row }) => {
        const summary = summariseStatement(row.original)
        return (
          <span className="whitespace-nowrap tabular-nums">
            {formatMilliseconds(summary.total)}
          </span>
        )
      },
    },
    {
      id: 'mean-time',
      meta: { width: '8rem', help: 'statements.mean-time' },
      header: 'Mean time',
      accessorFn: (row) => summariseStatement(row).mean,
      cell: ({ row }) => {
        const summary = summariseStatement(row.original)
        return (
          <span className="whitespace-nowrap tabular-nums">{formatMilliseconds(summary.mean)}</span>
        )
      },
    },
    {
      id: 'share-total',
      meta: { width: '10rem' },
      header: 'Share of listed rows',
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
      meta: { width: '7rem' },
      header: 'Rows',
      accessorFn: (row) =>
        typeof row.rows === 'number' && Number.isFinite(row.rows) ? row.rows : null,
      cell: ({ row }) => <span className="tabular-nums">{formatNumber(row.original.rows)}</span>,
    },
    {
      id: 'cache-hit-ratio',
      meta: { width: '10rem' },
      header: 'Cache hit ratio',
      accessorFn: (row) => summariseStatement(row).cacheHitRatio,
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
      meta: { width: '6.5rem' },
      header: 'WAL',
      accessorFn: (row) =>
        typeof row.wal_bytes === 'number' && Number.isFinite(row.wal_bytes) ? row.wal_bytes : null,
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
      meta: { width: '15rem' },
      header: 'Temporary bytes/call',
      accessorFn: (row) => summariseStatement(row).tempBytesPerCall,
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
        icon={Zap}
        eyebrow="Query performance"
        title="Query inspector"
        subtitle="Every statement pg_stat_statements recorded on this instance in the selected time range. Change the ranking to find the slowest, most frequent or most I/O-heavy queries, and open one to see its plan."
      />
      <div role="status" className="bg-muted/50 rounded-lg border px-4 py-3 text-sm">
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
  const q = searchParams.get('q') ?? ''
  const term = searchTermOf(q)
  // An older link searched the table (`statement-statistics.q`): it now searches the server.
  useLegacySearchParam('statement-statistics.q')
  // The box moves between the toolbar and a bar: its text lives here.
  const searchDraft = useSearchDraft(q)
  const params = {
    instance_id: instanceId,
    limit: STATEMENT_LIMIT,
    order_by: sort,
    ...(database === undefined ? {} : { database }),
    ...(term === '' ? {} : { q: term }),
  }
  const statementsQuery = useStatements(params, queryWindowOf(range))

  const response = statementsQuery.data as StatementApiResponse | undefined
  // The term the listed statements answer: the previous one while a new search loads.
  const shownTerm = useShownTerm(term, statementsQuery.isPlaceholderData, statementsQuery.isSuccess)
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
    // A new server ranking starts on the first page (D23).
    next.delete('statement-statistics.page')
    setSearchParams(next, { replace: true })
  }

  function setSearch(value: string) {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        if (value.trim() === '') next.delete('q')
        else next.set('q', value)
        // Another search is another result: back to the first page (D23); the
        // table's own search is replaced by this box.
        next.delete('statement-statistics.page')
        next.delete('statement-statistics.q')
        return next
      },
      { replace: true },
    )
  }

  // The no-match action: the search ends and the box keeps the keyboard.
  function clearSearch() {
    setSearch('')
    document.getElementById('statement-search')?.focus()
  }

  // The box sits in the table's toolbar line, or above the empty state without rows.
  const keepFocus = useKeepFocus(rows.length === 0 ? 'bar' : 'table')
  const searchBox = (
    // `contents`: the wrapper only keeps focus as the box moves, it adds no box.
    <div className="contents" {...keepFocus}>
      <SearchBox
        busy={statementsQuery.isFetching && term !== ''}
        draft={searchDraft}
        hint={FULL_SEARCH_HINT}
        id="statement-search"
        label="Search statements"
        onChange={setSearch}
        placeholder="Query text, queryid or database"
        value={q}
      />
    </div>
  )

  // Loading and failing draw the box in the same place, so it keeps its text
  // and focus while a corrected search loads after a failed one.
  if (statementsQuery.isPending && response === undefined) {
    return (
      <div className="space-y-4">
        <div className="flex">{searchBox}</div>
        <div role="status">Loading statements…</div>
      </div>
    )
  }

  if (statementsQuery.error?.kind === 'not_found') {
    return <DisabledStatementsState reason={undefined} />
  }

  if (statementsQuery.error && response === undefined) {
    // The box stays, so a search the server refused can be changed or cleared.
    return (
      <div className="space-y-4">
        <div className="flex">{searchBox}</div>
        <ErrorState
          endpoint="statements"
          failure={statementsQuery.error}
          onRetry={() => void statementsQuery.refetch()}
        />
      </div>
    )
  }

  const disabled = response !== undefined && isDisabledResponse(response)
  // Statements collected before the check was skipped are still shown.
  if (disabled && rows.length === 0 && term === '') {
    return <DisabledStatementsState reason={response.disabled_reason} />
  }

  const truncated = response?.truncated === true
  // The budget and eviction notes share the table's toolbar line (15.3, D26).
  const statementNotes = (
    <div
      className="text-muted-foreground flex min-w-0 flex-1 items-center gap-2 self-center overflow-hidden text-xs whitespace-nowrap"
      title={`${truncated ? `Showing ${rows.length} of ${STATEMENT_LIMIT} statements (truncated; budget ${STATEMENT_LIMIT}). The top-N budget is configured by checks.stat_statements.top_n. ` : ''}Entries beyond pg_stat_statements.max are evicted and may appear and disappear. queryid values are comparable only within this cluster.`}
    >
      {truncated ? (
        <>
          <Truncated shown={rows.length} budget={STATEMENT_LIMIT} scope="statements" />
          <span>
            The top-N budget is configured by <code>checks.stat_statements.top_n</code>.
          </span>
        </>
      ) : null}
      <span className="truncate">
        Entries beyond <code>pg_stat_statements.max</code> are evicted and may appear and disappear.{' '}
        <code>queryid</code> values are comparable only within this cluster.
      </span>
    </div>
  )

  return (
    <div className="space-y-8 min-[1600px]:space-y-4">
      <PageHeader
        icon={Zap}
        eyebrow="Query performance"
        title="Query inspector"
        subtitle="Every statement pg_stat_statements recorded on this instance in the selected time range. Change the ranking to find the slowest, most frequent or most I/O-heavy queries, and open one to see its plan."
        freshness={
          <FreshnessBadge dataUpdatedAt={statementsQuery.dataUpdatedAt} policy="statements" />
        }
      />
      <Section
        id="statement-list"
        title="Statements"
        captionId="queries.statements-section"
        actions={
          <>
            <label className="text-muted-foreground text-sm font-medium" htmlFor="statement-sort">
              Sort statements
            </label>
            <NativeSelect
              className="min-[1600px]:h-8"
              id="statement-sort"
              value={sort}
              onChange={(event) => onSortChange(event.target.value)}
            >
              {SORT_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </NativeSelect>
            <InfoHint id="queries.ranking" label="Sort statements" />
          </>
        }
      >
        {disabled ? (
          <p
            role="status"
            className="border-warning bg-warning/10 mb-4 rounded-lg border p-3 text-sm"
          >
            {skipSentence(response.disabled_reason)} The statements below were collected earlier.
          </p>
        ) : null}
        {rows.length === 0 ? <div className="mb-3 flex">{searchBox}</div> : null}
        <DataTable
          ariaLabel="Statement statistics"
          captionId="queries.statements-table"
          tableId="statement-statistics"
          columns={statementColumns(instanceId, shownTerm)}
          data={rows}
          emptyState={
            shownTerm === '' ? (
              <EmptyState
                title="No statements"
                description="No pg_stat_statements entries match the selected instance, database, and time range."
              />
            ) : (
              <EmptyState
                action={
                  <Button onClick={() => clearSearch()} size="sm" type="button" variant="outline">
                    Show all statements
                  </Button>
                }
                description="The search covers every statement collected on this instance in the time range, not only the listed ones. Try another term or a wider range."
                title={`No statement matches “${shownTerm}” in this range`}
              />
            )
          }
          {...(shownTerm === ''
            ? {}
            : { footerNote: matchCountNote(rows.length, response?.has_more === true) })}
          scope="statements"
          toolbarExtra={
            <>
              {searchBox}
              {statementNotes}
            </>
          }
        />
        {/* Without rows the table has no toolbar line: the notes follow the empty state. */}
        {rows.length === 0 ? <div className="mt-3 flex">{statementNotes}</div> : null}
      </Section>
    </div>
  )
}

export default QueryListPage
