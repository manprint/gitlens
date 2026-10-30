import { useMemo, type ReactNode } from 'react'

import { useInstanceSettings, useInstanceTableHistory, useInstanceTables } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { Sparkline } from '@/components/charts/Sparkline'
import { MetricTile } from '@/components/layout/MetricTile'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { useTimeRange } from '@/hooks/useTimeRange'
import {
  buildDeadRows,
  compareDeadRows,
  matchesDeadFilter,
  readAutovacuumSettings,
  summariseDeadRows,
  trendDirection,
  trendValues,
  WORST_FLOOR,
  type DeadFilter,
  type DeadRow,
  type DeadSortKey,
} from '@/lib/deadRows'
import { formatBytes, formatCount, formatPercent, formatRate, formatRelative } from '@/lib/format'
import type { SortDirection } from '@/lib/tableControls'
import { queryWindowOf } from '@/lib/timerange'
import { cn } from '@/lib/utils'

/** The most tables the API returns for one server. */
export const DEAD_ROWS_LIMIT = 500
/** How many tables get a trend line: the history query follows the worst ones. */
const TREND_TABLES = 10

type Finding = Schemas['Finding']

const FILTERS: readonly { value: DeadFilter; label: string; hint: string }[] = [
  { value: 'all', label: 'All', hint: 'Every table the agent reported.' },
  { value: 'dead', label: 'With dead rows', hint: 'Tables with at least one dead tuple.' },
  {
    value: 'due',
    label: 'Past autovacuum trigger',
    hint: 'Dead tuples at or above autovacuum_vacuum_threshold + scale_factor × live tuples.',
  },
  { value: 'never', label: 'Never vacuumed', hint: 'No manual or automatic vacuum recorded.' },
  { value: 'flagged', label: 'Flagged by advisor', hint: 'Tables with an open advisor finding.' },
]

function compareDead(a: DeadRow, b: DeadRow, key: string, direction: SortDirection): number {
  return compareDeadRows(a, b, key as DeadSortKey, direction === 'desc')
}

// The page's Database select scopes the rows already: no database filter (tables inventory).
const DEAD_TOOLBAR: TableToolbarSpec<DeadRow> = {
  searchLabel: 'Search tables',
  searchFields: [(row) => row.table, (row) => row.schema, (row) => row.datname],
  filters: [
    {
      id: 'show',
      label: 'Show',
      kind: 'select',
      options: FILTERS.filter((option) => option.value !== 'all').map(({ value, label }) => ({
        value,
        label,
      })),
      accessor: () => null,
      predicate: (row, value) => matchesDeadFilter(row, value as DeadFilter),
    },
  ],
}

// compareDeadRows orders the rows: a column's sortValue only marks it sortable.
const SORTED_BY_COMPARE = () => null

/** The table columns; names and the last vacuum read best A to Z or oldest first. */
function deadColumns(
  database: string,
  now: Date,
  trends: ReadonlyMap<string, number[]>,
): SimpleColumn<DeadRow>[] {
  const counted = (
    id: DeadSortKey,
    header: string,
    cell: (row: DeadRow) => ReactNode,
    className = 'whitespace-nowrap',
  ): SimpleColumn<DeadRow> => ({
    id,
    header,
    sortValue: SORTED_BY_COMPARE,
    firstDirection: 'desc',
    className,
    cell,
  })
  return [
    {
      id: 'table',
      header: 'Table',
      sortValue: SORTED_BY_COMPARE,
      className: 'break-all',
      cell: (row) => (
        <>
          <span className="text-muted-foreground">
            {database === '' ? `${row.datname} · ` : ''}
            {row.schema}.
          </span>
          <span className="font-medium">{row.table}</span>
          {row.findings.length > 0 ? (
            <Badge className="ml-2" variant="warning">
              flagged
            </Badge>
          ) : null}
        </>
      ),
    },
    counted('dead', 'Dead', (row) => (
      <>
        {formatCount(row.dead)}
        {row.deadBytes === null || row.deadBytes <= 0 ? null : (
          <span className="text-muted-foreground block text-xs">
            ≈ {formatBytes(row.deadBytes)}
          </span>
        )}
      </>
    )),
    counted('ratio', 'Dead / live', (row) => percent(row.deadRatio)),
    counted('trigger', 'Trigger', (row) => (
      <span
        className={cn(row.triggerShare >= 1 && 'font-medium')}
        title={`Autovacuum starts at about ${formatCount(Math.round(row.trigger))} dead tuples.`}
      >
        {triggerText(row)}
        {row.triggerShare >= 1 ? (
          <span className="text-muted-foreground block text-xs">due</span>
        ) : null}
      </span>
    )),
    counted('size', 'Size', (row) => (row.bytes === null ? 'Unknown' : formatBytes(row.bytes))),
    {
      id: 'vacuum',
      header: 'Last vacuum',
      sortValue: SORTED_BY_COMPARE,
      className: 'whitespace-nowrap',
      cell: (row) => vacuumText(row, now),
    },
    counted('hot', 'HOT', (row) => percent(row.hotRatio)),
    counted('writes', 'Writes', (row) =>
      row.writeRate === null ? 'Unknown' : formatRate(row.writeRate),
    ),
    {
      id: 'trend',
      header: 'Trend',
      className: 'whitespace-nowrap',
      cell: (row) => {
        const values = trends.get(row.key)
        if (values === undefined || values.length < 2) {
          return <span className="text-muted-foreground">—</span>
        }
        const direction = trendDirection(values)
        return (
          <span
            className={cn(
              'inline-flex items-center gap-2',
              direction === 'growing' ? 'text-warning-foreground' : '',
            )}
          >
            <Sparkline
              label={`Dead tuples of ${row.table} over the selected range`}
              values={values}
            />
            <span className="text-muted-foreground text-xs">{direction}</span>
          </span>
        )
      },
    },
  ]
}

function percent(value: number | null): string {
  return value === null ? 'Unknown' : (formatPercent(value, 1) ?? 'Unknown')
}

function vacuumText(row: DeadRow, now: Date): string {
  if (row.lastVacuumAt === null) return row.neverVacuumed ? 'Never' : 'Unknown'
  return `${formatRelative(row.lastVacuumAt, now)}${row.lastVacuumKind === 'auto' ? ' (auto)' : ''}`
}

function triggerText(row: DeadRow): string {
  return `${row.triggerShare.toFixed(row.triggerShare >= 10 ? 0 : 1)}×`
}

function Detail({ row, now }: { row: DeadRow; now: Date }) {
  return (
    <div className="space-y-4 px-4 py-4">
      <dl className="grid gap-x-8 gap-y-1.5 text-sm sm:grid-cols-[auto_1fr_auto_1fr]">
        <dt className="text-muted-foreground">Live tuples</dt>
        <dd className="tabular-nums">{formatCount(row.live)}</dd>
        <dt className="text-muted-foreground">Dead tuples</dt>
        <dd className="tabular-nums">{formatCount(row.dead)}</dd>
        <dt className="text-muted-foreground">Heap size</dt>
        <dd className="tabular-nums">
          {row.tableBytes === null ? 'Unknown' : formatBytes(row.tableBytes)}
        </dd>
        <dt className="text-muted-foreground">With indexes and TOAST</dt>
        <dd className="tabular-nums">{row.bytes === null ? 'Unknown' : formatBytes(row.bytes)}</dd>
        <dt className="text-muted-foreground">Autovacuum starts at</dt>
        <dd className="tabular-nums">{formatCount(Math.round(row.trigger))} dead tuples</dd>
        <dt className="text-muted-foreground">Autovacuum runs</dt>
        <dd className="tabular-nums">
          {row.autovacuumPerDay === null ? 'Unknown' : `${row.autovacuumPerDay.toFixed(1)} per day`}
        </dd>
        <dt className="text-muted-foreground">Changes since analyze</dt>
        <dd className="tabular-nums">
          {row.modSinceAnalyze === null ? 'Unknown' : formatCount(row.modSinceAnalyze)}
        </dd>
        <dt className="text-muted-foreground">Last analyze</dt>
        <dd>{row.lastAnalyzeAt === null ? 'Never' : formatRelative(row.lastAnalyzeAt, now)}</dd>
        <dt className="text-muted-foreground">Updates</dt>
        <dd className="tabular-nums">
          {row.updateRate === null ? 'Unknown' : formatRate(row.updateRate)}
        </dd>
        <dt className="text-muted-foreground">Sequential scans</dt>
        <dd className="tabular-nums">
          {row.seqScanRate === null ? 'Unknown' : formatRate(row.seqScanRate)}
        </dd>
        <dt className="text-muted-foreground">Transaction ID age</dt>
        <dd className="tabular-nums">
          {row.frozenAge === null ? 'Unknown' : formatCount(row.frozenAge)}
        </dd>
      </dl>

      {row.hotRatio !== null && row.hotRatio < 0.5 && (row.updateRate ?? 0) > 0 ? (
        <p className="text-sm">
          Only {percent(row.hotRatio)} of the updates are heap-only (HOT). The others write a new
          entry in every index of the table, and leave dead tuples that vacuum has to clean there
          too.
        </p>
      ) : null}

      {row.findings.length > 0 ? (
        <ul className="space-y-2">
          {row.findings.map((finding: Finding) => (
            <li
              key={finding.finding_id}
              className="border-warning/40 bg-warning/10 rounded-lg border px-3 py-2 text-sm"
            >
              <strong>{finding.title}.</strong> {finding.detail}
              {finding.remediation ? (
                <p className="text-muted-foreground mt-1">{finding.remediation}</p>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}

interface DeadRowsPanelProps {
  instanceId: string
  /** The database picked on the page; empty for all of them. */
  database: string
  findings: readonly Finding[]
}

/**
 * The tables the agent reports, ranked by how many dead tuples they hold,
 * with the vacuum state and the write pattern that explain them.
 */
export function DeadRowsPanel({ instanceId, database, findings }: DeadRowsPanelProps) {
  const { range } = useTimeRange()

  const tablesQuery = useInstanceTables(instanceId, {
    limit: DEAD_ROWS_LIMIT,
    order_by: 'dead_tup',
  })
  const settingsQuery = useInstanceSettings(instanceId)
  const historyQuery = useInstanceTableHistory(
    instanceId,
    { limit: TREND_TABLES, ...(database === '' ? {} : { database }) },
    queryWindowOf(range),
  )
  const response = tablesQuery.data

  const autovacuum = useMemo(
    () => readAutovacuumSettings(settingsQuery.data?.settings ?? []),
    [settingsQuery.data],
  )
  const allRows = useMemo(
    () => buildDeadRows(response?.items ?? [], findings, autovacuum),
    [response?.items, findings, autovacuum],
  )
  const scoped = useMemo(
    () => (database === '' ? allRows : allRows.filter((row) => row.datname === database)),
    [allRows, database],
  )
  const summary = useMemo(() => summariseDeadRows(scoped), [scoped])
  const trends = useMemo(() => {
    const byTable = new Map<string, number[]>()
    for (const series of historyQuery.data?.tables ?? []) {
      byTable.set(
        `${series.datname}.${series.schemaname}.${series.relname}`,
        trendValues(series.points),
      )
    }
    return byTable
  }, [historyQuery.data])
  // Relative times are measured from when the data was fetched, so a render stays pure.
  const now = new Date(tablesQuery.dataUpdatedAt)
  const dataUpdatedAt = tablesQuery.dataUpdatedAt
  const columns = useMemo(
    () => deadColumns(database, new Date(dataUpdatedAt), trends),
    [database, dataUpdatedAt, trends],
  )

  if (tablesQuery.isPending && response === undefined) {
    return (
      <div aria-busy="true" aria-label="Loading tables" role="status">
        Loading tables…
      </div>
    )
  }
  if (tablesQuery.error && response === undefined) {
    return (
      <ErrorState
        endpoint="tables"
        failure={tablesQuery.error}
        onRetry={() => void tablesQuery.refetch()}
      />
    )
  }

  return (
    <div className="space-y-6">
      {response?.truncated || allRows.length >= DEAD_ROWS_LIMIT ? (
        <div className="bg-muted/40 rounded-lg border px-4 py-3 text-sm" role="status">
          <strong>The list is incomplete.</strong> The agent reports the tables with the most dead
          tuples first, up to <code>checks.table_stats.top_n</code> per database (50 by default).
          The figures below cover the {formatCount(allRows.length)} tables reported, so the worst
          tables are here and the long tail is not.
        </div>
      ) : null}

      {autovacuum.source === 'default' ? (
        <div className="bg-muted/40 rounded-lg border px-4 py-3 text-sm" role="status">
          <strong>Autovacuum trigger is approximate.</strong> The instance&apos;s autovacuum
          settings are not reported yet, so the page assumes PostgreSQL&apos;s defaults (50 dead
          tuples plus 20% of the live ones). A table&apos;s own storage parameters can change it.
        </div>
      ) : null}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
        <MetricTile
          label="Tables with dead rows"
          value={formatCount(summary.withDead)}
          unit={`of ${formatCount(summary.tables)}`}
        />
        <MetricTile
          label="Dead tuples"
          value={formatCount(summary.totalDead)}
          unit={summary.deadBytes > 0 ? `≈ ${formatBytes(summary.deadBytes)}` : undefined}
        />
        <MetricTile
          label="Worst table"
          value={summary.worst === null ? '—' : percent(summary.worst.deadRatio)}
          unit={
            summary.worst === null
              ? `none past ${formatCount(WORST_FLOOR)} dead tuples`
              : summary.worst.table
          }
        />
        <MetricTile label="Past autovacuum trigger" value={formatCount(summary.overTrigger)} />
        <MetricTile label="Never vacuumed" value={formatCount(summary.neverVacuumed)} />
      </div>

      <section
        aria-labelledby="dead-rows-title"
        className="bg-card shadow-card overflow-hidden rounded-xl border"
      >
        <div className="space-y-3 border-b px-5 py-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="text-base font-semibold" id="dead-rows-title">
                Tables by dead rows
              </h2>
              <p className="text-muted-foreground mt-0.5 text-sm">
                {formatCount(scoped.length)} tables. Dead space is an estimate: the heap size times
                the dead share of its tuples. The trend follows the {TREND_TABLES} worst tables over
                the selected time range.
              </p>
            </div>
          </div>
        </div>

        <div className="p-5">
          {scoped.length === 0 ? (
            <EmptyState
              description="The agent has not reported table statistics for this server yet."
              title="No tables to show"
            />
          ) : (
            <SimpleTable
              ariaLabel="Tables"
              columns={columns}
              compare={compareDead}
              defaultSort={{ id: 'dead', desc: true }}
              detailLabel={(row, expanded) =>
                `${expanded ? 'Hide' : 'Show'} details of ${row.table}`
              }
              emptyState={null}
              renderDetail={(row) => <Detail now={now} row={row} />}
              rowKey={(row) => row.key}
              rows={scoped}
              scope="tables"
              tableId="dead-rows"
              toolbar={DEAD_TOOLBAR}
            />
          )}
        </div>
      </section>
    </div>
  )
}
