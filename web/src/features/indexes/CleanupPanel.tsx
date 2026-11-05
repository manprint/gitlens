import { Fragment, useMemo } from 'react'
import { Link } from 'react-router-dom'

import { useAdvisorWindows, useInstanceTables } from '@/api/queries'
import { CopyButton } from '@/components/layout/CopyButton'
import { InfoHint } from '@/components/layout/InfoHint'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { formatBytes, formatCount, formatPercent, formatRate } from '@/lib/format'
import type { IndexRow } from '@/lib/indexAnalysis'
import {
  buildCleanup,
  cleanupScript,
  indexWriteRates,
  summariseCleanup,
  UNUSED_INDEX_MIN_BYTES,
  unusedWindowPhrase,
  type CleanupCandidate,
  type CleanupReason,
} from '@/lib/indexCleanup'
import { summariseColumns } from '@/lib/indexDefinition'

import { DEAD_ROWS_LIMIT } from './DeadRowsPanel'
import { NO_INDEX_STATS } from './emptyText'
import { CELL_BADGE, IndexPanel, PanelNote } from './IndexPanel'

const REASONS: Record<
  CleanupReason,
  { label: string; variant: 'destructive' | 'warning' | 'outline' }
> = {
  invalid: { label: 'Invalid', variant: 'destructive' },
  duplicate: { label: 'Duplicate', variant: 'warning' },
  redundant: { label: 'Redundant prefix', variant: 'warning' },
  unused: { label: 'Unused', variant: 'warning' },
  'never-scanned': { label: 'Never scanned', variant: 'outline' },
}

const CLEANUP_TOOLBAR: TableToolbarSpec<CleanupCandidate> = {
  searchFields: [
    (candidate) => candidate.row.name,
    (candidate) => `${candidate.row.schema}.${candidate.row.table}`,
  ],
  filters: [
    {
      id: 'reason',
      label: 'Reason',
      kind: 'select',
      options: Object.entries(REASONS).map(([value, { label }]) => ({ value, label })),
      accessor: (candidate) => candidate.reason,
    },
    {
      id: 'basis',
      label: 'Basis',
      kind: 'select',
      options: [
        { value: 'advisor', label: 'Advisor finding' },
        { value: 'statistics', label: 'Raw counters' },
      ],
      accessor: (candidate) => candidate.basis,
    },
  ],
}

const CLEANUP_COLUMNS: readonly SimpleColumn<CleanupCandidate>[] = [
  {
    id: 'index',
    header: 'Index',
    sortValue: (candidate) => candidate.row.name,
    className: 'font-medium break-all',
    width: '22rem',
    truncate: (candidate) => candidate.row.name,
    cell: (candidate) => candidate.row.name,
  },
  {
    id: 'table',
    header: 'Table',
    sortValue: (candidate) => `${candidate.row.schema}.${candidate.row.table}`,
    className: 'break-all',
    truncate: ({ row }) => `${row.datname} · ${row.schema}.${row.table}`,
    cell: ({ row }) => (
      <>
        <span className="text-muted-foreground">
          {row.datname} · {row.schema}.
        </span>
        {row.table}
      </>
    ),
  },
  {
    id: 'reason',
    header: 'Reason',
    help: 'indexes.cleanup-reason-column',
    width: '10rem',
    cell: (candidate) => {
      const reason = REASONS[candidate.reason]
      return (
        <Badge className={CELL_BADGE} variant={reason.variant}>
          {reason.label}
        </Badge>
      )
    },
  },
  {
    id: 'size',
    header: 'Size',
    sortValue: (candidate) => candidate.row.bytes,
    className: 'whitespace-nowrap',
    width: '7rem',
    cell: ({ row }) => (row.bytes === null ? 'Unknown' : formatBytes(row.bytes)),
  },
  {
    id: 'writes',
    header: 'Index writes',
    help: 'indexes.cleanup-writes',
    sortValue: (candidate) => candidate.writeRate,
    className: 'whitespace-nowrap',
    width: '10.5rem',
    cell: (candidate) =>
      candidate.writeRate === null ? 'Not measured' : formatRate(candidate.writeRate),
  },
]

/** The servers that share a cluster with this one: they may use an index this one never scans. */
export interface SiblingServer {
  id: string
  label: string
}

function Statement({ label, subject, sql }: { label: string; subject: string; sql: string }) {
  return (
    <div className="space-y-1">
      <h4 className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
        {label}
      </h4>
      <div className="bg-muted/40 rounded-lg border">
        <pre className="overflow-x-auto px-3 py-2 font-mono text-xs break-words whitespace-pre-wrap">
          {sql}
        </pre>
        <div className="flex justify-end border-t px-1.5 py-0.5">
          <CopyButton subject={subject} text={sql} />
        </div>
      </div>
    </div>
  )
}

function Detail({
  candidate,
  siblings,
}: {
  candidate: CleanupCandidate
  siblings: readonly SiblingServer[]
}) {
  const { row } = candidate
  const rarelyUsed = candidate.reason === 'unused' || candidate.reason === 'never-scanned'
  return (
    <div className="space-y-4 px-4 py-4">
      <p className="text-sm">{candidate.why}</p>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
        <dt className="text-muted-foreground">Columns</dt>
        <dd>
          <code className="font-mono">{summariseColumns(row.parsed) || '—'}</code>
        </dd>
        {candidate.coveredBy === null ? null : (
          <>
            <dt className="text-muted-foreground">Stays</dt>
            <dd>
              <code className="font-mono">{candidate.coveredBy}</code>
            </dd>
          </>
        )}
      </dl>

      <Statement
        label="Drop"
        sql={candidate.dropStatement}
        subject={`drop statement of ${row.name}`}
      />
      {candidate.recreateStatement === null ? (
        <p className="text-muted-foreground text-sm">
          The agent did not report the definition, so there is nothing to rebuild it from. Take it
          from the schema before you drop.
        </p>
      ) : (
        <Statement
          label="Rebuild, to undo the drop"
          sql={candidate.recreateStatement}
          subject={`rebuild statement of ${row.name}`}
        />
      )}

      <div className="space-y-1 text-sm">
        <h4 className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
          Before you drop
        </h4>
        <ul className="list-disc space-y-1 pl-5">
          {rarelyUsed ? (
            <>
              <li>
                Check that it does not back a foreign key: without it, deletes on the referenced
                table scan this one. The page cannot see that.
              </li>
              <li>
                A report that runs once a month or a year leaves no scan in a short history. Ask who
                owns the table.
              </li>
            </>
          ) : null}
          {candidate.reason === 'invalid' ? (
            <li>
              If it came from a failed <code>CREATE INDEX CONCURRENTLY</code>, drop it and build it
              again once the cause is gone.
            </li>
          ) : null}
          {siblings.length > 0 ? (
            <li>
              Scans on a standby are counted there, not here. Look at{' '}
              {siblings.map((sibling, position) => (
                <Fragment key={sibling.id}>
                  {position > 0 ? ', ' : ''}
                  <Link
                    className="text-primary font-medium hover:underline"
                    to={`/indexes?instance=${encodeURIComponent(sibling.id)}&tab=cleanup`}
                  >
                    {sibling.label}
                  </Link>
                </Fragment>
              ))}{' '}
              first.
            </li>
          ) : null}
          <li>Run the drop on a quiet moment and keep the rebuild statement at hand.</li>
        </ul>
      </div>
    </div>
  )
}

interface CleanupPanelProps {
  instanceId: string
  /** The indexes in scope: the page's database filter already applied. */
  rows: readonly IndexRow[]
  siblings: readonly SiblingServer[]
  /** Why the list is empty when no index was reported. */
  emptyDescription?: string
}

/**
 * The indexes that look removable, with what a drop gives back. pglens never
 * runs a drop: it writes the statement, and the operator decides.
 */
export function CleanupPanel({
  instanceId,
  rows,
  siblings,
  emptyDescription = NO_INDEX_STATS,
}: CleanupPanelProps) {
  const tablesQuery = useInstanceTables(instanceId, {
    limit: DEAD_ROWS_LIMIT,
    order_by: 'dead_tup',
  })
  const writeRates = useMemo(
    () => indexWriteRates(tablesQuery.data?.items ?? []),
    [tablesQuery.data],
  )
  // The configured window names the length when no finding carries it; any
  // console session may read it. Until it loads, the texts name no length.
  const windowsQuery = useAdvisorWindows()
  const configuredWindow =
    windowsQuery.data?.find((window) => window.key === 'window.unused_index')?.seconds ?? null
  const candidates = useMemo(
    () => buildCleanup(rows, writeRates, configuredWindow),
    [rows, writeRates, configuredWindow],
  )
  const totalBytes = useMemo(() => rows.reduce((sum, row) => sum + (row.bytes ?? 0), 0), [rows])
  const summary = useMemo(() => summariseCleanup(candidates, totalBytes), [candidates, totalBytes])
  const script = useMemo(
    () => cleanupScript(candidates, configuredWindow),
    [candidates, configuredWindow],
  )
  const statistical = candidates.filter((candidate) => candidate.basis === 'statistics').length

  const clusterText = `This server is part of a cluster. Check ${siblings.map((sibling) => sibling.label).join(', ')} before you drop anything: scans are counted on the server that runs them, so an index with no scan here may be in use on a standby.`
  const statisticalHeadline = `${formatCount(statistical)} ${statistical === 1 ? 'candidate rests' : 'candidates rest'} on raw counters.`
  const statisticalText = `"Never scanned" means no scan since the statistics were last reset. The advisor calls an index unused only after ${unusedWindowPhrase(rows, configuredWindow)} with no scan, and never one of ${formatBytes(UNUSED_INDEX_MIN_BYTES)} or less.`
  const notes =
    siblings.length > 0 || statistical > 0 ? (
      <>
        {siblings.length > 0 ? (
          <PanelNote title={clusterText}>
            <strong>This server is part of a cluster.</strong> Check{' '}
            {siblings.map((sibling, position) => (
              <Fragment key={sibling.id}>
                {position > 0 ? ', ' : ''}
                <Link
                  className="text-primary font-medium hover:underline"
                  to={`/indexes?instance=${encodeURIComponent(sibling.id)}&tab=cleanup`}
                >
                  {sibling.label}
                </Link>
              </Fragment>
            ))}{' '}
            before you drop anything: scans are counted on the server that runs them, so an index
            with no scan here may be in use on a standby.
          </PanelNote>
        ) : null}
        {statistical > 0 ? (
          <PanelNote title={`${statisticalHeadline} ${statisticalText}`}>
            <strong>{statisticalHeadline}</strong> {statisticalText}
          </PanelNote>
        ) : null}
      </>
    ) : null

  return (
    <IndexPanel
      actions={
        <>
          <InfoHint id="indexes.cleanup-reason" label="cleanup reason" />
          {candidates.length > 0 ? <CopyButton subject="cleanup script" text={script} /> : null}
        </>
      }
      captionId="indexes.cleanup"
      description="Indexes that look unused, repeat another index or are invalid, with the disk space and index writes that dropping them would save. Primary keys and unique indexes are never listed; pglens drops nothing: copy the script, review it and run it yourself."
      notes={notes}
      stats={[
        {
          label: 'Candidates',
          value: formatCount(summary.count),
          unit: 'indexes',
          detail: `${formatCount(summary.confirmed.count)} confirmed · ${formatCount(summary.provisional.count)} raw counters`,
        },
        {
          label: 'Space to give back',
          value: formatBytes(summary.confirmed.reclaimBytes),
          detail: [
            summary.confirmedShare === null
              ? null
              : `${formatPercent(summary.confirmed.reclaimBytes, totalBytes) ?? ''} of the indexes`,
            summary.provisional.count > 0
              ? `+ ${formatBytes(summary.provisional.reclaimBytes)} on raw counters`
              : null,
          ]
            .filter((part) => part !== null)
            .join(' · '),
        },
        {
          label: 'Index writes saved',
          value: formatRate(summary.confirmed.writeRate).replace(/\/s$/, ''),
          unit: 'per second',
          detail: [
            summary.confirmed.writeRateUnknown > 0
              ? `${formatCount(summary.confirmed.writeRateUnknown)} not measured`
              : null,
            summary.provisional.count > 0
              ? `+ ${formatRate(summary.provisional.writeRate).replace('/s', ' per second')} on raw counters`
              : null,
          ]
            .filter((part) => part !== null)
            .join(' · '),
        },
        {
          label: 'Unused indexes',
          value: formatCount(summary.byReason.unused),
          unit: 'indexes',
          detail: 'judged unused by the advisor',
        },
        {
          label: 'Duplicate or redundant indexes',
          value: formatCount(summary.byReason.duplicate + summary.byReason.redundant),
          unit: 'indexes',
        },
        {
          label: 'Invalid indexes',
          value: formatCount(summary.byReason.invalid),
          unit: 'indexes',
        },
      ]}
      title="Indexes that look removable"
      titleId="cleanup-title"
    >
      {candidates.length === 0 ? (
        <EmptyState
          description={
            rows.length === 0
              ? emptyDescription
              : 'No index is invalid, duplicated, redundant or unused.'
          }
          title="Nothing to clean up"
        />
      ) : (
        <SimpleTable
          ariaLabel="Cleanup candidates"
          captionId="indexes.cleanup-table"
          columns={CLEANUP_COLUMNS}
          detailLabel={(candidate, expanded) =>
            `${expanded ? 'Hide' : 'Show'} the drop of ${candidate.row.name}`
          }
          emptyState={null}
          renderDetail={(candidate) => <Detail candidate={candidate} siblings={siblings} />}
          rowKey={(candidate) => candidate.row.key}
          rows={candidates}
          scope="candidates"
          tableId="cleanup"
          toolbar={CLEANUP_TOOLBAR}
        />
      )}
    </IndexPanel>
  )
}
