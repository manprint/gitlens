import { useMemo, type ReactNode } from 'react'

import { useInstanceSettings, useInstanceTableHistory, useInstanceTables } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { Sparkline } from '@/components/charts/Sparkline'
import { HelpLink } from '@/components/layout/HelpLink'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { useFocusMissing } from '@/hooks/useSearch'
import { useTimeRange } from '@/hooks/useTimeRange'
import { useT, type Translate } from '@/i18n/react'
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
import {
  formatBytes,
  formatCount,
  formatDecimal,
  formatPercent,
  formatRate,
  formatRelative,
} from '@/lib/format'
import type { SortDirection } from '@/lib/tableControls'
import { queryWindowOf } from '@/lib/timerange'
import { cn } from '@/lib/utils'

import { CELL_BADGE, IndexPanel, PanelNote } from './IndexPanel'

/** The most tables the API returns for one server. */
export const DEAD_ROWS_LIMIT = 500
/** How many tables get a trend line: the history query follows the worst ones. */
const TREND_TABLES = 10

type Finding = Schemas['Finding']

function deadFilters(t: Translate): readonly { value: DeadFilter; label: string; hint: string }[] {
  return [
    {
      value: 'all',
      label: t('indexes.deadRows.filterAll'),
      hint: t('indexes.deadRows.filterAllHint'),
    },
    {
      value: 'dead',
      label: t('indexes.deadRows.filterDead'),
      hint: t('indexes.deadRows.filterDeadHint'),
    },
    {
      value: 'due',
      label: t('indexes.deadRows.dueAutovacuum'),
      hint: t('indexes.deadRows.filterDueHint'),
    },
    {
      value: 'never',
      label: t('indexes.deadRows.neverVacuumed'),
      hint: t('indexes.deadRows.filterNeverHint'),
    },
    {
      value: 'flagged',
      label: t('indexes.deadRows.filterFlagged'),
      hint: t('indexes.deadRows.filterFlaggedHint'),
    },
  ]
}

function compareDead(a: DeadRow, b: DeadRow, key: string, direction: SortDirection): number {
  return compareDeadRows(a, b, key as DeadSortKey, direction === 'desc')
}

// The page's Database select scopes the rows already: no database filter (tables inventory).
function deadToolbar(t: Translate): TableToolbarSpec<DeadRow> {
  return {
    searchLabel: t('indexes.deadRows.searchTables'),
    searchFields: [(row) => row.table, (row) => row.schema, (row) => row.datname],
    filters: [
      {
        id: 'show',
        label: t('indexes.deadRows.show'),
        kind: 'select',
        options: deadFilters(t)
          .filter((option) => option.value !== 'all')
          .map(({ value, label }) => ({
            value,
            label,
          })),
        accessor: () => null,
        predicate: (row, value) => matchesDeadFilter(row, value as DeadFilter),
      },
    ],
  }
}

// compareDeadRows orders the rows: a column's sortValue only marks it sortable.
const SORTED_BY_COMPARE = () => null

/** The table columns; names and the last vacuum read best A to Z or oldest first. */
function deadColumns(
  database: string,
  now: Date,
  trends: ReadonlyMap<string, number[]>,
  t: Translate,
): SimpleColumn<DeadRow>[] {
  const counted = (
    id: DeadSortKey,
    header: string,
    cell: (row: DeadRow) => ReactNode,
    width: string,
    help?: SimpleColumn<DeadRow>['help'],
    truncate?: (row: DeadRow) => string,
  ): SimpleColumn<DeadRow> => ({
    id,
    header,
    ...(help === undefined ? {} : { help }),
    ...(truncate === undefined ? {} : { truncate }),
    sortValue: SORTED_BY_COMPARE,
    firstDirection: 'desc',
    className: 'whitespace-nowrap',
    width,
    cell,
  })
  return [
    {
      id: 'table',
      header: t('indexes.deadRows.table'),
      sortValue: SORTED_BY_COMPARE,
      className: 'break-all max-[1599px]:min-w-48',
      truncate: (row) =>
        `${database === '' ? `${row.datname} · ` : ''}${row.schema}.${row.table}${row.findings.length > 0 ? t('indexes.deadRows.flaggedSuffix') : ''}`,
      cell: (row) => (
        <>
          <span className="text-muted-foreground">
            {database === '' ? `${row.datname} · ` : ''}
            {row.schema}.
          </span>
          <span className="font-medium">{row.table}</span>
          {row.findings.length > 0 ? (
            <Badge className={cn('ml-2', CELL_BADGE)} variant="warning">
              {t('indexes.deadRows.flagged')}
            </Badge>
          ) : null}
        </>
      ),
    },
    // From 1600 px the second lines (dead space, "due") sit beside the figure (D26).
    counted(
      'dead',
      t('indexes.deadRows.deadRows'),
      (row) => (
        <>
          {formatCount(row.dead)}
          {row.deadBytes === null || row.deadBytes <= 0 ? null : (
            <span className="text-muted-foreground block text-xs min-[1600px]:ml-1.5 min-[1600px]:inline">
              ≈ {formatBytes(row.deadBytes)}
            </span>
          )}
        </>
      ),
      '10rem',
      'indexes.dead-rows-column',
      (row) =>
        row.deadBytes === null || row.deadBytes <= 0
          ? formatCount(row.dead)
          : t('indexes.deadRows.deadRowsTitle', {
              count: formatCount(row.dead),
              size: formatBytes(row.deadBytes),
            }),
    ),
    counted(
      'ratio',
      t('indexes.deadRows.deadLive'),
      (row) => percent(row.deadRatio, t),
      '8.5rem',
      'indexes.dead-live',
    ),
    counted(
      'trigger',
      t('indexes.deadRows.vacuumDue'),
      (row) => (
        <span className={cn(row.triggerShare >= 1 && 'font-medium')}>
          {triggerText(row)}
          {row.triggerShare >= 1 ? (
            <span className="text-muted-foreground block text-xs min-[1600px]:ml-1.5 min-[1600px]:inline">
              {t('indexes.deadRows.due')}
            </span>
          ) : null}
        </span>
      ),
      '7rem',
      'indexes.vacuum-due',
      (row) =>
        row.triggerShare >= 1
          ? t('indexes.deadRows.vacuumDueTitleDue', {
              share: triggerText(row),
              count: formatCount(Math.round(row.trigger)),
            })
          : t('indexes.deadRows.vacuumDueTitle', {
              share: triggerText(row),
              count: formatCount(Math.round(row.trigger)),
            }),
    ),
    counted(
      'size',
      t('indexes.deadRows.size'),
      (row) => (row.bytes === null ? t('indexes.deadRows.unknown') : formatBytes(row.bytes)),
      '6.5rem',
    ),
    {
      id: 'vacuum',
      header: t('indexes.deadRows.lastVacuum'),
      help: 'indexes.dead-last-vacuum',
      sortValue: SORTED_BY_COMPARE,
      className: 'whitespace-nowrap',
      width: '12.5rem',
      truncate: (row) => vacuumText(row, now, t),
      cell: (row) => vacuumText(row, now, t),
    },
    counted('hot', 'HOT', (row) => percent(row.hotRatio, t), '6rem', 'indexes.hot-share'),
    counted(
      'writes',
      t('indexes.deadRows.writes'),
      (row) => (row.writeRate === null ? t('indexes.deadRows.unknown') : formatRate(row.writeRate)),
      '7rem',
      'indexes.dead-writes',
    ),
    {
      id: 'trend',
      header: t('indexes.deadRows.trend'),
      help: 'indexes.dead-trend',
      className: 'whitespace-nowrap',
      width: '10rem',
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
              className="min-[1600px]:h-4 min-[1600px]:w-16"
              label={t('indexes.deadRows.trendLabel', { table: row.table })}
              values={values}
            />
            <span className="text-muted-foreground text-xs">{directionText(t, direction)}</span>
          </span>
        )
      },
    },
  ]
}

function percent(value: number | null, t: Translate): string {
  return value === null
    ? t('indexes.deadRows.unknown')
    : (formatPercent(value, 1) ?? t('indexes.deadRows.unknown'))
}

function directionText(t: Translate, direction: ReturnType<typeof trendDirection>): string {
  if (direction === 'growing') return t('indexes.deadRows.trendGrowing')
  return direction === 'shrinking'
    ? t('indexes.deadRows.trendShrinking')
    : t('indexes.deadRows.trendFlat')
}

function vacuumText(row: DeadRow, now: Date, t: Translate): string {
  if (row.lastVacuumAt === null)
    return row.neverVacuumed ? t('indexes.deadRows.never') : t('indexes.deadRows.unknown')
  const when = formatRelative(row.lastVacuumAt, now)
  return row.lastVacuumKind === 'auto' ? t('indexes.deadRows.vacuumAuto', { when }) : when
}

function triggerText(row: DeadRow): string {
  return `${formatDecimal(row.triggerShare, row.triggerShare >= 10 ? 0 : 1)}×`
}

function Detail({ row, now }: { row: DeadRow; now: Date }) {
  const t = useT()

  return (
    <div className="space-y-4 px-4 py-4">
      <dl className="grid gap-x-8 gap-y-1.5 text-sm sm:grid-cols-[auto_1fr_auto_1fr]">
        <dt className="text-muted-foreground">{t('indexes.deadRows.liveRows')}</dt>
        <dd className="tabular-nums">{formatCount(row.live)}</dd>
        <dt className="text-muted-foreground">{t('indexes.deadRows.deadRows')}</dt>
        <dd className="tabular-nums">{formatCount(row.dead)}</dd>
        <dt className="text-muted-foreground">{t('indexes.deadRows.heapSize')}</dt>
        <dd className="tabular-nums">
          {row.tableBytes === null ? t('indexes.deadRows.unknown') : formatBytes(row.tableBytes)}
        </dd>
        <dt className="text-muted-foreground">{t('indexes.deadRows.withIndexesToast')}</dt>
        <dd className="tabular-nums">
          {row.bytes === null ? t('indexes.deadRows.unknown') : formatBytes(row.bytes)}
        </dd>
        <dt className="text-muted-foreground">{t('indexes.deadRows.autovacuumStartsAt')}</dt>
        <dd className="tabular-nums">
          {formatCount(Math.round(row.trigger))} {t('indexes.deadRows.deadRowsUnit')}
        </dd>
        <dt className="text-muted-foreground">{t('indexes.deadRows.autovacuumRuns')}</dt>
        <dd className="tabular-nums">
          {row.autovacuumPerDay === null
            ? t('indexes.deadRows.unknown')
            : t('indexes.deadRows.perDay', { rate: formatDecimal(row.autovacuumPerDay, 1) })}
        </dd>
        <dt className="text-muted-foreground">{t('indexes.deadRows.changesSinceAnalyze')}</dt>
        <dd className="tabular-nums">
          {row.modSinceAnalyze === null
            ? t('indexes.deadRows.unknown')
            : formatCount(row.modSinceAnalyze)}
        </dd>
        <dt className="text-muted-foreground">{t('indexes.deadRows.lastAnalyze')}</dt>
        <dd>
          {row.lastAnalyzeAt === null
            ? t('indexes.deadRows.never')
            : formatRelative(row.lastAnalyzeAt, now)}
        </dd>
        <dt className="text-muted-foreground">{t('indexes.deadRows.updates')}</dt>
        <dd className="tabular-nums">
          {row.updateRate === null ? t('indexes.deadRows.unknown') : formatRate(row.updateRate)}
        </dd>
        <dt className="text-muted-foreground">{t('indexes.deadRows.seqScans')}</dt>
        <dd className="tabular-nums">
          {row.seqScanRate === null ? t('indexes.deadRows.unknown') : formatRate(row.seqScanRate)}
        </dd>
        <dt className="text-muted-foreground">{t('indexes.deadRows.transactionIdAge')}</dt>
        <dd className="tabular-nums">
          {row.frozenAge === null ? t('indexes.deadRows.unknown') : formatCount(row.frozenAge)}
        </dd>
      </dl>

      {row.hotRatio !== null && row.hotRatio < 0.5 && (row.updateRate ?? 0) > 0 ? (
        <p className="text-sm">
          {t('indexes.deadRows.hotNote', { share: percent(row.hotRatio, t) })}
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
  /** The key of a row to expand and give focus to (a search hit, D20). */
  focusKey?: string | undefined
  /** Called once the row is shown (true) or when it is not in the list (false). */
  onFocusDone?: (found: boolean) => void
}

/**
 * The tables the agent reports, ranked by how many dead rows they hold,
 * with the vacuum state and the write pattern that explain them.
 */
export function DeadRowsPanel({
  instanceId,
  database,
  findings,
  focusKey,
  onFocusDone,
}: DeadRowsPanelProps) {
  const t = useT()

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
  // Without tables there is no row to show for a search hit (D20).
  useFocusMissing(focusKey, onFocusDone, response !== undefined && scoped.length === 0)
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
    () => deadColumns(database, new Date(dataUpdatedAt), trends, t),
    [database, dataUpdatedAt, trends, t],
  )
  const toolbar = useMemo(() => deadToolbar(t), [t])

  if (tablesQuery.isPending && response === undefined) {
    return (
      <div aria-busy="true" aria-label={t('indexes.deadRows.loadingTablesLabel')} role="status">
        {t('indexes.deadRows.loadingTables')}
      </div>
    )
  }
  if (tablesQuery.error && response === undefined) {
    return (
      <ErrorState
        endpoint={t('indexes.deadRows.endpointTables')}
        failure={tablesQuery.error}
        onRetry={() => void tablesQuery.refetch()}
      />
    )
  }

  const incomplete = response?.truncated === true || allRows.length >= DEAD_ROWS_LIMIT
  const incompleteText = t('indexes.deadRows.incompleteTitle', {
    count: formatCount(allRows.length),
  })
  const approximateText = t('indexes.deadRows.approximateTitle')
  const notes =
    incomplete || autovacuum.source === 'default' ? (
      <>
        {incomplete ? (
          <PanelNote title={incompleteText}>
            <strong>{t('indexes.deadRows.incompleteLead')}</strong>{' '}
            {t('indexes.deadRows.incompleteBefore')} <code>checks.table_stats.top_n</code>{' '}
            {t('indexes.deadRows.incompleteAfter', { count: formatCount(allRows.length) })}
          </PanelNote>
        ) : null}
        {autovacuum.source === 'default' ? (
          <PanelNote title={approximateText}>
            <strong>{t('indexes.deadRows.approximateLead')}</strong>{' '}
            {t('indexes.deadRows.approximateRest')}
          </PanelNote>
        ) : null}
      </>
    ) : null

  return (
    <IndexPanel
      actions={<HelpLink anchor="indexes#dead-rows" label={t('indexes.deadRows.deadRowsTab')} />}
      captionId="indexes.dead-rows"
      description={t('indexes.deadRows.description', { count: formatCount(scoped.length) })}
      notes={notes}
      stats={[
        {
          label: t('indexes.deadRows.statTablesWithDead'),
          value: formatCount(summary.withDead),
          unit: 'tables',
          detail: t('indexes.deadRows.statOfTables', {
            total: formatCount(summary.tables),
            count: summary.tables,
          }),
        },
        {
          label: t('indexes.deadRows.deadRows'),
          value: formatCount(summary.totalDead),
          unit: 'rows',
          ...(summary.deadBytes > 0 ? { detail: `≈ ${formatBytes(summary.deadBytes)}` } : {}),
        },
        {
          label: t('indexes.deadRows.worstTable'),
          value: summary.worst === null ? '—' : percent(summary.worst.deadRatio, t),
          detail:
            summary.worst === null
              ? t('indexes.deadRows.statNonePast', { limit: formatCount(WORST_FLOOR) })
              : summary.worst.table,
        },
        {
          label: t('indexes.deadRows.dueAutovacuum'),
          value: formatCount(summary.overTrigger),
          unit: 'tables',
        },
        {
          label: t('indexes.deadRows.neverVacuumed'),
          value: formatCount(summary.neverVacuumed),
          unit: 'tables',
        },
      ]}
      title={t('indexes.deadRows.tablesByDeadRows')}
      titleId="dead-rows-title"
    >
      {scoped.length === 0 ? (
        <EmptyState
          description={t('indexes.deadRows.noTableStats')}
          title={t('indexes.deadRows.noTablesToShow')}
        />
      ) : (
        <SimpleTable
          ariaLabel={t('indexes.deadRows.tables')}
          captionId="indexes.dead-rows-table"
          columns={columns}
          compare={compareDead}
          defaultSort={{ id: 'dead', desc: true }}
          detailLabel={(row, expanded) =>
            expanded
              ? t('indexes.deadRows.hideDetails', { table: row.table })
              : t('indexes.deadRows.showDetails', { table: row.table })
          }
          emptyState={null}
          renderDetail={(row) => <Detail now={now} row={row} />}
          rowKey={(row) => row.key}
          rows={scoped}
          scope="tables"
          tableId="dead-rows"
          toolbar={toolbar}
          focusKey={focusKey}
          {...(onFocusDone === undefined ? {} : { onFocusDone })}
        />
      )}
    </IndexPanel>
  )
}
