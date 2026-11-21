import { Zap } from 'lucide-react'
import { useMemo } from 'react'

import { Link, useParams, useSearchParams } from 'react-router-dom'

import { useStatements } from '@/api/queries'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { InfoHint } from '@/components/layout/InfoHint'
import { HelpLink } from '@/components/layout/HelpLink'
import { PageHeader } from '@/components/layout/PageHeader'
import { SearchBox } from '@/components/layout/SearchBox'
import { Section } from '@/components/layout/Section'
import { SqlText } from '@/components/layout/SqlText'
import { Button } from '@/components/ui/button'
import type { MessageKey } from '@/i18n/messages/en'
import { type Translate, useT } from '@/i18n/react'
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
const SORT_OPTIONS: readonly { value: StatementSort; labelKey: MessageKey }[] = [
  { value: 'total_exec_time', labelKey: 'queries.common.totalTime' },
  { value: 'mean_exec_time', labelKey: 'queries.common.meanTime' },
  { value: 'calls', labelKey: 'queries.common.calls' },
  { value: 'rows', labelKey: 'queries.common.rows' },
  { value: 'shared_blks_read', labelKey: 'queries.list.sortSharedRead' },
  { value: 'temp_blks_written', labelKey: 'queries.list.sortTempWritten' },
  { value: 'wal_bytes', labelKey: 'queries.list.sortWal' },
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

function queryTextCell(row: StatementDisplayRow, instanceId: string, term: string, t: Translate) {
  const queryText = row.query_text ?? ''
  const normalised = normaliseQueryText(queryText)

  return (
    // From 1600 px the link and the statement share one line (D26).
    <div className="min-w-64 space-y-1 min-[1600px]:flex min-[1600px]:min-w-0 min-[1600px]:items-center min-[1600px]:gap-2 min-[1600px]:space-y-0 min-[1600px]:overflow-hidden">
      <Link
        className="text-primary text-xs font-semibold underline-offset-4 hover:underline min-[1600px]:shrink-0 min-[1600px]:whitespace-nowrap"
        to={queryPath(instanceId, row.queryid, row.datname)}
      >
        {t('queries.list.queryLink', { queryid: String(row.queryid) })}
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
  t: Translate,
): DataTableProps<StatementDisplayRow>['columns'] {
  return [
    {
      id: 'query',
      header: t('queries.common.query'),
      // Takes the width the fixed-width figures leave from 1600 px (D26).
      enableSorting: false,
      cell: ({ row }) => queryTextCell(row.original, instanceId, term, t),
    },
    {
      id: 'calls',
      meta: { width: '5.5rem' },
      header: t('queries.common.calls'),
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
      header: t('queries.common.totalTime'),
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
      header: t('queries.common.meanTime'),
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
      meta: { width: '9.5rem', help: 'statements.share-of-listed' },
      header: t('queries.list.shareOfListed'),
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
      meta: { width: '5.5rem' },
      header: t('queries.common.rows'),
      accessorFn: (row) =>
        typeof row.rows === 'number' && Number.isFinite(row.rows) ? row.rows : null,
      cell: ({ row }) => <span className="tabular-nums">{formatNumber(row.original.rows)}</span>,
    },
    {
      id: 'cache-hit-ratio',
      meta: { width: '8rem', help: 'statements.cache-hit-ratio' },
      header: t('queries.overview.cacheHitRatio'),
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
      meta: { width: '6.5rem', help: 'statements.wal' },
      header: t('queries.list.wal'),
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
      meta: { width: '11rem', help: 'statements.temp-bytes-per-call' },
      header: t('queries.list.tempBytesPerCall'),
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

function skipSentence(reason: string | undefined, t: Translate): string {
  return reason ? t('queries.list.skipWithReason', { reason }) : t('queries.list.skipNone')
}

// There is no configuration key to turn back on: the agent skips the check
// when, at startup, it finds no pg_stat_statements view it can read.
function DisabledStatementsState({ reason }: { reason: string | undefined }) {
  const t = useT()
  return (
    <div className="space-y-4">
      <PageHeader
        help={<HelpLink anchor="query-performance" label={t('queries.list.inspectorHelp')} />}
        icon={Zap}
        eyebrow={t('queries.overview.eyebrow')}
        title={t('queries.list.inspectorTitle')}
        subtitle={t('queries.list.inspectorSubtitle')}
      />
      <div role="status" className="bg-muted/50 rounded-lg border px-4 py-3 text-sm">
        <strong>{t('queries.list.disabledTitle')}</strong> {skipSentence(reason, t)}
      </div>
      <p className="text-muted-foreground text-sm">
        {t('queries.list.extensionBefore')} <code>shared_preload_libraries</code>.{' '}
        <a className="underline" href="/README.md#agent-configuration">
          {t('queries.list.readmeLink')}
        </a>
      </p>
    </div>
  )
}

export function QueryListPage({ instanceId: instanceIdOverride }: QueryListPageProps = {}) {
  const t = useT()
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
        fullSearch
        id="statement-search"
        label={t('queries.list.searchLabel')}
        onChange={setSearch}
        placeholder={t('queries.list.searchPlaceholder')}
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
        <div role="status">{t('queries.list.loading')}</div>
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
          endpoint={t('queries.list.statements')}
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
      title={
        truncated
          ? `${t('queries.list.notesTruncated', { shown: rows.length, limit: STATEMENT_LIMIT })} ${t('queries.list.notesEvicted')}`
          : t('queries.list.notesEvicted')
      }
    >
      {truncated ? (
        <>
          <Truncated
            shown={rows.length}
            budget={STATEMENT_LIMIT}
            scope={t('queries.list.statements')}
          />
          <span>
            {t('queries.list.topNBefore')} <code>checks.stat_statements.top_n</code>.
          </span>
        </>
      ) : null}
      <span className="truncate">
        {t('queries.list.evictedBefore')} <code>pg_stat_statements.max</code>{' '}
        {t('queries.list.evictedAfter')} <code>queryid</code> {t('queries.list.queryidAfter')}
      </span>
    </div>
  )

  return (
    <div className="space-y-8 min-[1600px]:space-y-2">
      <PageHeader
        help={<HelpLink anchor="query-performance" label={t('queries.list.inspectorHelp')} />}
        icon={Zap}
        eyebrow={t('queries.overview.eyebrow')}
        title={t('queries.list.inspectorTitle')}
        subtitle={t('queries.list.inspectorSubtitleRange')}
        freshness={
          <FreshnessBadge dataUpdatedAt={statementsQuery.dataUpdatedAt} policy="statements" />
        }
      />
      <Section
        id="statement-list"
        title={t('queries.list.statementsTitle')}
        captionId="queries.statements-section"
        actions={
          <>
            <label className="text-muted-foreground text-sm font-medium" htmlFor="statement-sort">
              {t('queries.list.sortLabel')}
            </label>
            <NativeSelect
              className="min-[1600px]:h-8"
              id="statement-sort"
              value={sort}
              onChange={(event) => onSortChange(event.target.value)}
            >
              {SORT_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {t(option.labelKey)}
                </option>
              ))}
            </NativeSelect>
            <InfoHint id="queries.ranking" label={t('queries.list.sortLabel')} />
            <HelpLink
              anchor="query-performance#query-inspector"
              label={t('queries.list.tableHelp')}
            />
          </>
        }
      >
        {disabled ? (
          <p
            role="status"
            className="border-warning bg-warning/10 mb-4 rounded-lg border p-3 text-sm"
          >
            {skipSentence(response.disabled_reason, t)} {t('queries.list.collectedEarlier')}
          </p>
        ) : null}
        {rows.length === 0 ? <div className="mb-3 flex">{searchBox}</div> : null}
        <DataTable
          ariaLabel={t('queries.list.ariaLabel')}
          captionId="queries.statements-table"
          tableId="statement-statistics"
          columns={statementColumns(instanceId, shownTerm, t)}
          data={rows}
          emptyState={
            shownTerm === '' ? (
              <EmptyState
                title={t('queries.list.noStatementsTitle')}
                description={t('queries.list.noStatementsText')}
              />
            ) : (
              <EmptyState
                action={
                  <Button onClick={() => clearSearch()} size="sm" type="button" variant="outline">
                    {t('queries.list.showAll')}
                  </Button>
                }
                description={t('queries.list.noMatchText')}
                title={t('queries.list.noMatchTitle', { term: shownTerm })}
              />
            )
          }
          {...(shownTerm === ''
            ? {}
            : { footerNote: matchCountNote(rows.length, response?.has_more === true) })}
          scope={t('queries.list.statements')}
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
