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
import { HelpLink } from '@/components/layout/HelpLink'
import { PageHeader } from '@/components/layout/PageHeader'
import { HighlightedText, SearchBox } from '@/components/layout/SearchBox'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import type { MessageKey } from '@/i18n/messages/en'
import { type Translate, useT } from '@/i18n/react'
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
import { queryWindowOf, rangeLabelKey } from '@/lib/timerange'
import { cn } from '@/lib/utils'
import { NativeSelect } from '@/components/ui/native-select'

/**
 * How many statements the page loads and ranks; the table shows 20 per page
 * (D23). The server caps a request at 1000.
 */
export const STATEMENT_FETCH_LIMIT = 200

type Ranking = 'mean' | 'total'

const RANKINGS: readonly { value: Ranking; labelKey: MessageKey; hintKey: MessageKey }[] = [
  { value: 'mean', labelKey: 'queries.slow.rankMean', hintKey: 'queries.slow.rankMeanHint' },
  { value: 'total', labelKey: 'queries.common.totalTime', hintKey: 'queries.slow.rankTotalHint' },
]

const ORDER_BY = { mean: 'mean_exec_time', total: 'total_exec_time' } as const

// The tiles share the list header, so from 1600 px they are one short line each (D26).
const HEADER_TILE = 'min-[1600px]:shrink-0 min-[1600px]:py-1'
const HEADER_TILE_FLEX = 'min-[1600px]:min-w-0 min-[1600px]:flex-1 min-[1600px]:py-1'

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

function slowColumns(
  instanceId: string,
  term: string,
  t: Translate,
): DataTableProps<SlowRow>['columns'] {
  return [
    {
      id: 'rank',
      header: t('queries.slow.rank'),
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
      header: t('queries.common.query'),
      enableSorting: false,
      cell: ({ row }) => {
        const { row: statement } = row.original
        const text = statement.query_text ? normaliseQueryText(statement.query_text) : ''
        return (
          <div className="flex max-w-[36rem] min-w-0 items-center gap-2">
            {/* The name starts with `Query <id>` and still holds the visible text (label in name). */}
            <Link
              className="text-primary block min-w-0 truncate font-mono text-xs hover:underline"
              title={
                statement.query_text ??
                t('queries.list.queryLink', { queryid: String(statement.queryid) })
              }
              to={queryPath(instanceId, statement.queryid, statement.datname)}
            >
              {text ? (
                <>
                  <span className="sr-only">
                    {t('queries.list.queryLink', { queryid: String(statement.queryid) })}:{' '}
                  </span>
                  <HighlightedText q={term} text={text} />
                </>
              ) : (
                t('queries.list.queryLink', { queryid: String(statement.queryid) })
              )}
            </Link>
            {statement.truncated ? (
              <Badge variant="warning">{t('queries.slow.textTruncated')}</Badge>
            ) : null}
            {statement.query_text ? <CopyButton text={statement.query_text} /> : null}
          </div>
        )
      },
    },
    {
      id: 'database',
      header: t('queries.common.database'),
      accessorFn: (slow) => slow.row.datname ?? null,
      cell: ({ row }) => row.original.row.datname ?? '—',
    },
    {
      id: 'calls',
      header: t('queries.common.calls'),
      accessorFn: (slow) => slow.calls,
      cell: ({ row }) => (row.original.calls === null ? '—' : formatCount(row.original.calls)),
    },
    {
      id: 'mean',
      meta: { help: 'statements.mean-time' },
      header: t('queries.common.meanTime'),
      accessorFn: (slow) => slow.mean,
      cell: ({ row }) => (
        <span className="whitespace-nowrap">{formatMilliseconds(row.original.mean)}</span>
      ),
    },
    {
      id: 'total',
      meta: { help: 'statements.total-time' },
      header: t('queries.common.totalTime'),
      accessorFn: (slow) => slow.total,
      cell: ({ row }) => (
        <span className="whitespace-nowrap">{formatMilliseconds(row.original.total)}</span>
      ),
    },
    {
      id: 'share',
      meta: { help: 'statements.share-of-listed' },
      header: t('queries.list.shareOfListed'),
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
  /** The name of the server whose statements are listed, for the empty state. */
  serverLabel: string
  ranking: Ranking
}

function InstanceStatements({
  controls,
  instanceId,
  ranking,
  serverLabel,
}: InstanceStatementsProps) {
  const t = useT()
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
  const columns = useMemo(() => slowColumns(instanceId, shownTerm, t), [instanceId, shownTerm, t])
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
        fullSearch
        id="slow-search"
        label={t('queries.list.searchLabel')}
        onChange={setSearch}
        placeholder={t('queries.list.searchPlaceholder')}
        value={q}
      />
      {controls}
      <div className="space-y-1.5">
        <label
          className="text-muted-foreground text-xs font-medium min-[1600px]:sr-only"
          htmlFor="slow-db"
        >
          {t('queries.common.database')}
        </label>
        <div className="relative">
          <Database
            aria-hidden="true"
            className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2"
          />
          <NativeSelect
            className="w-56 max-w-full pr-3 pl-8 min-[1600px]:h-7 min-[1600px]:w-48"
            id="slow-db"
            onChange={(event) => setDatabase(event.target.value)}
            value={database ?? ''}
          >
            <option value="">{t('queries.slow.allDatabases')}</option>
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
  const rankingOption = RANKINGS.find((option) => option.value === ranking)
  const hint = rankingOption ? t(rankingOption.hintKey) : undefined

  if (statementsQuery.isPending && response === undefined) {
    return (
      <div className="space-y-6 min-[1600px]:space-y-4">
        {standaloneBar}
        <div aria-busy="true" aria-label={t('queries.slow.loadingLabel')} role="status">
          {t('queries.slow.loading')}
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
            description={t('queries.slow.notAvailableText')}
            title={t('queries.slow.notAvailableTitle')}
          />
        ) : (
          <ErrorState
            endpoint={t('queries.list.statements')}
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
          <strong>{t('queries.slow.disabledTitle')}</strong>{' '}
          {response.disabled_reason
            ? t('queries.slow.skipWithReason', { reason: response.disabled_reason })
            : t('queries.slow.skipNone')}{' '}
          {t('queries.slow.collectedEarlier')}
        </div>
      ) : null}

      <section
        aria-labelledby="slowest-list-title"
        className="bg-card shadow-card overflow-hidden rounded-xl border"
      >
        {/* The summary tiles share the list header; from 1600 px on one line (D26). */}
        <div className="flex flex-col gap-4 border-b px-5 py-4 min-[1600px]:flex-row min-[1600px]:items-center min-[1600px]:justify-between min-[1600px]:py-1">
          <div className="min-w-0 min-[1600px]:w-80 min-[1600px]:shrink-0">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-base font-semibold" id="slowest-list-title">
                {ranking === 'mean'
                  ? t('queries.slow.rankedByMean')
                  : t('queries.slow.rankedByTotal')}
              </h2>
              <HelpLink anchor="slowest-queries#table" label={t('queries.slow.tableHelp')} />
            </div>
            <p
              className="text-muted-foreground mt-0.5 text-sm min-[1600px]:line-clamp-2 min-[1600px]:text-xs min-[1600px]:leading-snug"
              title={hint}
            >
              {hint}
            </p>
          </div>
          <div className="grid gap-4 min-[1600px]:flex min-[1600px]:min-w-0 min-[1600px]:gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <MetricTile
              captionId="slow.slowest-mean"
              className={HEADER_TILE}
              label={t('queries.slow.slowestMean')}
              value={rows.length === 0 ? null : formatMilliseconds(slowestMean)}
            />
            <MetricTile
              captionId="slow.total-time"
              className={HEADER_TILE}
              label={t('queries.slow.totalTimeTile')}
              value={rows.length === 0 ? null : formatMilliseconds(totalTime)}
            />
            <MetricTile
              captionId="slow.calls"
              className={HEADER_TILE}
              label={t('queries.common.calls')}
              value={rows.length === 0 ? null : formatCount(totalCalls)}
            />
            <MetricTile
              captionId="slow.statements-ranked"
              className={HEADER_TILE_FLEX}
              detail={t('queries.slow.rankedDetail', {
                limit: STATEMENT_FETCH_LIMIT,
                missing: response?.truncated ? t('queries.slow.someMissing') : '',
              })}
              label={t('queries.slow.statementsRanked')}
              unit="statements"
              value={String(rows.length)}
            />
          </div>
        </div>
        {rows.length === 0 ? (
          <div className="p-5">
            {shownTerm === '' ? (
              <EmptyState
                description={t('queries.slow.noEntriesText', {
                  server: serverLabel,
                  scope:
                    database === undefined ? '' : t('queries.slow.noEntriesDatabase', { database }),
                  range: t(rangeLabelKey(range)),
                  field: t('queries.slow.serverLabel'),
                })}
                title={t('queries.slow.noStatementsYet')}
              />
            ) : (
              <EmptyState
                action={
                  <Button onClick={() => clearSearch()} size="sm" type="button" variant="outline">
                    {t('queries.list.showAll')}
                  </Button>
                }
                description={t('queries.slow.noMatchText')}
                title={t('queries.list.noMatchTitle', { term: shownTerm })}
              />
            )}
          </div>
        ) : (
          <div className="p-5 min-[1600px]:p-3">
            <DataTable
              ariaLabel={t('queries.slow.ariaLabel')}
              captionId="slow.table"
              columns={columns}
              data={slowRows}
              emptyState={null}
              {...(shownTerm === ''
                ? {}
                : { footerNote: matchCountNote(rows.length, response?.has_more === true) })}
              scope={t('queries.list.statements')}
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
  const t = useT()
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
        help={<HelpLink anchor="slowest-queries" label={t('queries.slow.pageHelp')} />}
        eyebrow={t('queries.overview.eyebrow')}
        icon={Gauge}
        subtitle={
          <>
            {t('queries.slow.subtitleBefore')} <code>pg_stat_statements</code>{' '}
            {t('queries.slow.subtitleAfter')}
          </>
        }
        title={t('queries.slow.title')}
      />

      {clustersQuery.error && clustersQuery.data === undefined ? (
        <ErrorState
          endpoint={t('queries.slow.clusters')}
          failure={clustersQuery.error}
          onRetry={() => void clustersQuery.refetch()}
        />
      ) : clustersQuery.isPending ? (
        <div aria-busy="true" aria-label={t('queries.slow.loadingServersLabel')} role="status">
          {t('queries.slow.loadingServers')}
        </div>
      ) : instanceId === null ? (
        <EmptyState
          description={t('queries.slow.noServersText')}
          title={t('queries.slow.noServersTitle')}
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
                    {t('queries.slow.serverLabel')}
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
                          {option.up ? '' : t('queries.slow.optionDown')}
                        </option>
                      ))}
                    </NativeSelect>
                  </div>
                </div>
                <fieldset>
                  <legend className="text-muted-foreground mb-1.5 text-xs font-medium min-[1600px]:sr-only">
                    {t('queries.slow.rankBy')}
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
                            title={t(option.hintKey)}
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
                            {t(option.labelKey)}
                          </label>
                        )
                      })}
                    </div>
                    <InfoHint id="slow.rank-by" label={t('queries.slow.rankBy')} />
                  </div>
                </fieldset>
              </>
            }
            instanceId={instanceId}
            ranking={ranking}
            serverLabel={options.find((option) => option.id === instanceId)?.label ?? instanceId}
          />
        </>
      )}
    </div>
  )
}

export default SlowQueriesPage
