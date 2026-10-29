import { Fragment, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { ChevronDown, ChevronRight } from 'lucide-react'

import { useInstanceTables } from '@/api/queries'
import { CopyButton } from '@/components/layout/CopyButton'
import { MetricTile } from '@/components/layout/MetricTile'
import { EmptyState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { formatBytes, formatCount, formatPercent, formatRate } from '@/lib/format'
import type { IndexRow } from '@/lib/indexAnalysis'
import {
  buildCleanup,
  cleanupScript,
  indexWriteRates,
  summariseCleanup,
  type CleanupCandidate,
  type CleanupReason,
} from '@/lib/indexCleanup'
import { summariseColumns } from '@/lib/indexDefinition'

import { DEAD_ROWS_LIMIT } from './DeadRowsPanel'

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
}

/**
 * The indexes that look removable, with what a drop gives back. pglens never
 * runs a drop: it writes the statement, and the operator decides.
 */
export function CleanupPanel({ instanceId, rows, siblings }: CleanupPanelProps) {
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set())
  const tablesQuery = useInstanceTables(instanceId, {
    limit: DEAD_ROWS_LIMIT,
    order_by: 'dead_tup',
  })
  const writeRates = useMemo(
    () => indexWriteRates(tablesQuery.data?.items ?? []),
    [tablesQuery.data],
  )
  const candidates = useMemo(() => buildCleanup(rows, writeRates), [rows, writeRates])
  const totalBytes = useMemo(() => rows.reduce((sum, row) => sum + (row.bytes ?? 0), 0), [rows])
  const summary = useMemo(() => summariseCleanup(candidates, totalBytes), [candidates, totalBytes])
  const script = useMemo(() => cleanupScript(candidates), [candidates])
  const statistical = candidates.filter((candidate) => candidate.basis === 'statistics').length

  function toggleOpen(key: string) {
    setOpen((current) => {
      const next = new Set(current)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  return (
    <div className="space-y-6">
      {siblings.length > 0 ? (
        <div className="bg-muted/40 rounded-lg border px-4 py-3 text-sm" role="status">
          <strong>This server is part of a cluster.</strong> Scans are counted on the server that
          runs them, so an index with no scan here may be in use on a standby. Check{' '}
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
          before you drop anything.
        </div>
      ) : null}

      {statistical > 0 ? (
        <div className="bg-muted/40 rounded-lg border px-4 py-3 text-sm" role="status">
          <strong>
            {formatCount(statistical)} {statistical === 1 ? 'candidate rests' : 'candidates rest'}{' '}
            on raw counters.
          </strong>{' '}
          The advisor calls an index unused only after 7 days of usage history. Until then
          &quot;never scanned&quot; means no scan since the statistics were last reset.
        </div>
      ) : null}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <MetricTile
          label="Candidates"
          value={formatCount(summary.count)}
          unit={`${formatCount(summary.confirmed.count)} confirmed · ${formatCount(summary.provisional.count)} raw counters`}
        />
        <MetricTile
          label="Space to give back"
          value={formatBytes(summary.confirmed.reclaimBytes)}
          unit={[
            summary.confirmedShare === null
              ? null
              : `${formatPercent(summary.confirmed.reclaimBytes, totalBytes) ?? ''} of the indexes`,
            summary.provisional.count > 0
              ? `+ ${formatBytes(summary.provisional.reclaimBytes)} on raw counters`
              : null,
          ]
            .filter((part) => part !== null)
            .join(' · ')}
        />
        <MetricTile
          label="Index writes saved"
          value={formatRate(summary.confirmed.writeRate)}
          unit={[
            summary.confirmed.writeRateUnknown > 0
              ? `${formatCount(summary.confirmed.writeRateUnknown)} not measured`
              : null,
            summary.provisional.count > 0
              ? `+ ${formatRate(summary.provisional.writeRate)} on raw counters`
              : null,
          ]
            .filter((part) => part !== null)
            .join(' · ')}
        />
        <MetricTile
          label="By reason"
          value={`${formatCount(summary.byReason.unused + summary.byReason['never-scanned'])} unused`}
          unit={`${formatCount(summary.byReason.duplicate)} duplicate · ${formatCount(summary.byReason.redundant)} redundant · ${formatCount(summary.byReason.invalid)} invalid`}
        />
      </div>

      <section
        aria-labelledby="cleanup-title"
        className="bg-card shadow-card overflow-hidden rounded-xl border"
      >
        <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-4">
          <div>
            <h2 className="text-base font-semibold" id="cleanup-title">
              Indexes that look removable
            </h2>
            <p className="text-muted-foreground mt-0.5 text-sm">
              Primary keys and unique indexes are never listed. Index writes are the entries every
              insert and every non-HOT update add to each index of the table. pglens does not run
              any statement: copy it, review it, run it yourself. The script holds the confirmed
              drops; those that rest on raw counters are in it as comments.
            </p>
          </div>
          {candidates.length > 0 ? <CopyButton subject="cleanup script" text={script} /> : null}
        </div>

        {candidates.length === 0 ? (
          <div className="p-5">
            <EmptyState
              description={
                rows.length === 0
                  ? 'The agent has not reported index statistics for this server yet.'
                  : 'No index is invalid, duplicated, redundant or unused.'
              }
              title="Nothing to clean up"
            />
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table aria-label="Cleanup candidates" className="w-full text-sm">
              <thead className="text-muted-foreground bg-muted/40 text-xs uppercase">
                <tr>
                  <th className="w-8 px-3 py-2" scope="col">
                    <span className="sr-only">Details</span>
                  </th>
                  <th className="px-3 py-2 text-left font-medium" scope="col">
                    Index
                  </th>
                  <th className="px-3 py-2 text-left font-medium" scope="col">
                    Table
                  </th>
                  <th className="px-3 py-2 text-left font-medium" scope="col">
                    Reason
                  </th>
                  <th className="px-3 py-2 text-left font-medium" scope="col">
                    Size
                  </th>
                  <th className="px-3 py-2 text-left font-medium" scope="col">
                    Index writes
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {candidates.map((candidate) => {
                  const { row } = candidate
                  const expanded = open.has(row.key)
                  const Chevron = expanded ? ChevronDown : ChevronRight
                  const reason = REASONS[candidate.reason]
                  return (
                    <Fragment key={row.key}>
                      <tr className="hover:bg-muted/30 align-top">
                        <td className="px-3 py-2">
                          <button
                            aria-expanded={expanded}
                            aria-label={`${expanded ? 'Hide' : 'Show'} the drop of ${row.name}`}
                            className="text-muted-foreground hover:text-foreground"
                            onClick={() => toggleOpen(row.key)}
                            type="button"
                          >
                            <Chevron aria-hidden="true" className="size-4" />
                          </button>
                        </td>
                        <td className="px-3 py-2 font-medium break-all">{row.name}</td>
                        <td className="px-3 py-2 break-all">
                          <span className="text-muted-foreground">
                            {row.datname} · {row.schema}.
                          </span>
                          {row.table}
                        </td>
                        <td className="px-3 py-2">
                          <Badge variant={reason.variant}>{reason.label}</Badge>
                        </td>
                        <td className="px-3 py-2 whitespace-nowrap tabular-nums">
                          {row.bytes === null ? 'Unknown' : formatBytes(row.bytes)}
                        </td>
                        <td className="px-3 py-2 whitespace-nowrap tabular-nums">
                          {candidate.writeRate === null
                            ? 'Not measured'
                            : formatRate(candidate.writeRate)}
                        </td>
                      </tr>
                      {expanded ? (
                        <tr className="bg-muted/20">
                          <td colSpan={6}>
                            <Detail candidate={candidate} siblings={siblings} />
                          </td>
                        </tr>
                      ) : null}
                    </Fragment>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  )
}
