import { useMemo } from 'react'

import { useAutovacuumRuns, useAutovacuumSummary } from '@/api/queries'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { ErrorState, Unknown } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { useFormat, useT, type Translate } from '@/i18n/react'
import {
  AUTOVACUUM_SETTINGS,
  runBadges,
  runKey,
  runsQuery,
  summaryKey,
  type AutovacuumRun,
  type AutovacuumTableSummary,
} from '@/lib/autovacuum'
import {
  formatBytes,
  formatCount,
  formatDuration,
  formatTimestamp,
  TIMESTAMP_COLUMN_WIDTH,
} from '@/lib/format'

import { CaptionedBlock } from './InstancePanel'

function tableName(row: Pick<AutovacuumTableSummary, 'datname' | 'schemaname' | 'relname'>) {
  return `${row.datname} · ${row.schemaname}.${row.relname}`
}

/** The setup the history needs; the names stay English. */
export function AutovacuumSetup() {
  const t = useT()
  return (
    <div className="text-muted-foreground space-y-1 text-sm" data-testid="autovacuum-setup">
      <p>{t('instance.autovacuum.empty')}</p>
      <ul className="list-disc pl-5">
        {AUTOVACUUM_SETTINGS.map((setting) => (
          <li key={setting}>
            <code className="font-mono text-xs">{setting}</code>
          </li>
        ))}
      </ul>
      <p>{t('instance.autovacuum.setupHint')}</p>
    </div>
  )
}

function badgeText(t: Translate, badge: ReturnType<typeof runBadges>[number]): string {
  return badge === 'wraparound'
    ? t('instance.autovacuum.badge.wraparound')
    : t('instance.autovacuum.badge.aggressive')
}

/** The runs of one table, newest first: the table history of the summary row. */
export function AutovacuumRunsTable({
  instanceId,
  table,
}: {
  instanceId: string
  table: Pick<AutovacuumTableSummary, 'datname' | 'schemaname' | 'relname'>
}) {
  const t = useT()
  const fmt = useFormat()
  const query = useAutovacuumRuns(runsQuery(instanceId, table))
  const runs = query.data?.runs
  const columns = useMemo<SimpleColumn<AutovacuumRun>[]>(() => {
    const count = (value: number | null) =>
      value === null ? <Unknown /> : formatCount(value, fmt.intl)
    return [
      {
        id: 'ts',
        header: t('instance.autovacuum.col.time'),
        rowHeader: true,
        width: TIMESTAMP_COLUMN_WIDTH,
        sortValue: (run) => Date.parse(run.ts),
        cell: (run) => formatTimestamp(run.ts),
      },
      {
        id: 'kind',
        header: t('instance.autovacuum.col.kind'),
        sortValue: (run) => run.kind,
        cell: (run) =>
          run.kind === 'analyze'
            ? t('instance.autovacuum.kind.analyze')
            : t('instance.autovacuum.kind.vacuum'),
      },
      {
        id: 'flags',
        header: t('instance.autovacuum.col.flags'),
        cell: (run) => (
          <span className="flex flex-wrap gap-1">
            {runBadges(run).map((badge) => (
              <Badge key={badge} variant={badge === 'wraparound' ? 'warning' : 'outline'}>
                {badgeText(t, badge)}
              </Badge>
            ))}
          </span>
        ),
      },
      {
        id: 'elapsed',
        header: t('instance.autovacuum.col.elapsed'),
        sortValue: (run) => run.elapsed_seconds,
        cell: (run) => formatDuration(run.elapsed_seconds, fmt.intl),
      },
      {
        id: 'tuples_removed',
        header: t('instance.autovacuum.col.tuplesRemoved'),
        sortValue: (run) => run.tuples_removed,
        cell: (run) => count(run.tuples_removed),
      },
      {
        id: 'dead_not_removable',
        header: t('instance.autovacuum.col.deadNotRemovable'),
        sortValue: (run) => run.tuples_dead_not_removable,
        cell: (run) => count(run.tuples_dead_not_removable),
      },
      {
        id: 'pages_scanned',
        header: t('instance.autovacuum.col.pagesScanned'),
        sortValue: (run) => run.pages_scanned,
        cell: (run) => count(run.pages_scanned),
      },
      {
        id: 'wal_bytes',
        header: t('instance.autovacuum.col.walBytes'),
        sortValue: (run) => run.wal_bytes,
        cell: (run) => formatBytes(run.wal_bytes, fmt.intl),
      },
      {
        id: 'buffer_reads',
        header: t('instance.autovacuum.col.bufferReads'),
        sortValue: (run) => run.buffer_reads,
        cell: (run) => count(run.buffer_reads),
      },
    ]
  }, [t, fmt])
  if (query.error && !runs) {
    return (
      <ErrorState
        endpoint={t('instance.autovacuum.runsEndpoint')}
        failure={query.error}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (!runs) {
    return <div role="status">{t('instance.autovacuum.loadingRuns')}</div>
  }
  const caption = t('instance.autovacuum.runsCaption', { table: tableName(table) })
  return (
    <SimpleTable
      ariaLabel={caption}
      captionId="relations.autovacuum-runs"
      columns={columns}
      emptyState={
        <p className="text-muted-foreground text-sm">{t('instance.autovacuum.noRuns')}</p>
      }
      rowKey={runKey}
      rows={runs}
      scope={t('instance.autovacuum.runsNoun')}
      tableId="autovacuum-runs"
      defaultSort={{ id: 'ts', desc: true }}
    />
  )
}

/** Autovacuum activity: the tables with the most autovacuum time (plan 007 8.3). */
export function AutovacuumActivity({ instanceId }: { instanceId: string }) {
  const t = useT()
  const fmt = useFormat()
  const query = useAutovacuumSummary({ instance_id: instanceId })
  const tables = query.data?.tables
  const title = t('instance.autovacuum.title')
  const columns = useMemo<SimpleColumn<AutovacuumTableSummary>[]>(
    () => [
      {
        id: 'table',
        header: t('instance.autovacuum.col.table'),
        rowHeader: true,
        className: 'font-normal',
        sortValue: (row) => tableName(row),
        cell: (row) => tableName(row),
      },
      {
        id: 'runs',
        header: t('instance.autovacuum.col.runs'),
        sortValue: (row) => row.runs,
        cell: (row) => formatCount(row.runs, fmt.intl),
      },
      {
        id: 'total',
        header: t('instance.autovacuum.col.totalElapsed'),
        sortValue: (row) => row.total_elapsed_seconds,
        cell: (row) => formatDuration(row.total_elapsed_seconds, fmt.intl),
      },
      {
        id: 'max',
        header: t('instance.autovacuum.col.maxElapsed'),
        sortValue: (row) => row.max_elapsed_seconds,
        cell: (row) => formatDuration(row.max_elapsed_seconds, fmt.intl),
      },
      {
        id: 'tuples_removed',
        header: t('instance.autovacuum.col.tuplesRemoved'),
        sortValue: (row) => row.tuples_removed,
        cell: (row) => formatCount(row.tuples_removed, fmt.intl),
      },
      {
        id: 'wraparound',
        header: t('instance.autovacuum.col.wraparoundRuns'),
        sortValue: (row) => row.wraparound_runs,
        cell: (row) =>
          row.wraparound_runs > 0 ? (
            <Badge variant="warning">{formatCount(row.wraparound_runs, fmt.intl)}</Badge>
          ) : (
            formatCount(0, fmt.intl)
          ),
      },
      {
        id: 'last',
        header: t('instance.autovacuum.col.lastRun'),
        width: TIMESTAMP_COLUMN_WIDTH,
        sortValue: (row) => Date.parse(row.last_ts),
        cell: (row) => formatTimestamp(row.last_ts),
      },
    ],
    [t, fmt],
  )
  if (query.error && !tables) {
    return (
      <ErrorState
        endpoint={t('instance.autovacuum.endpoint')}
        failure={query.error}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (!tables) {
    return <div role="status">{t('instance.autovacuum.loading')}</div>
  }
  if (tables.length === 0) {
    return (
      <CaptionedBlock ariaLabel={title} captionId="relations.autovacuum">
        <AutovacuumSetup />
      </CaptionedBlock>
    )
  }
  return (
    <SimpleTable
      ariaLabel={title}
      captionId="relations.autovacuum"
      columns={columns}
      emptyState={null}
      rowKey={summaryKey}
      rows={tables}
      scope={t('instance.autovacuum.noun')}
      tableId="autovacuum-summary"
      defaultSort={{ id: 'total', desc: true }}
      headerToggleName={(column) =>
        t('instance.relations.sortBy', { list: title, column: column.header })
      }
      renderDetail={(row) => <AutovacuumRunsTable instanceId={instanceId} table={row} />}
      detailLabel={(row, expanded) =>
        t(expanded ? 'instance.autovacuum.hideRuns' : 'instance.autovacuum.showRuns', {
          table: tableName(row),
        })
      }
    />
  )
}
