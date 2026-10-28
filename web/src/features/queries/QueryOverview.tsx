import { ArrowLeft, Zap } from 'lucide-react'
import { Link } from 'react-router-dom'

import { useInstance, useStatements } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { CopyButton } from '@/components/layout/CopyButton'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { MetricTile } from '@/components/layout/MetricTile'
import { PageHeader } from '@/components/layout/PageHeader'
import { SqlText } from '@/components/layout/SqlText'
import { Badge } from '@/components/ui/badge'
import { useTimeRange } from '@/hooks/useTimeRange'
import { formatBytes, formatCount, formatMilliseconds, formatPercent } from '@/lib/format'
import { queryWindowOf } from '@/lib/timerange'

type Statement = Schemas['Statement']

// pg_stat_statements has no per-query endpoint, so the statement is looked up
// in the instance's statements for the selected window, as wide as the API allows.
const LOOKUP_LIMIT = 1000

interface QueryOverviewProps {
  instanceId: string
  queryid: string
  datname?: string
}

function findStatement(
  statements: readonly Statement[],
  queryid: string,
  datname: string | undefined,
): Statement | undefined {
  return statements.find(
    (statement) =>
      statement.queryid === queryid && (datname === undefined || statement.datname === datname),
  )
}

function StatementMetrics({ statement }: { statement: Statement }) {
  const calls = statement.calls ?? null
  const rowsPerCall =
    calls !== null && calls > 0 && statement.rows !== null && statement.rows !== undefined
      ? statement.rows / calls
      : null
  const hit = statement.shared_blks_hit ?? null
  const read = statement.shared_blks_read ?? null
  const cacheHit = hit !== null && read !== null ? formatPercent(hit, hit + read) : null

  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
      <MetricTile
        label="Mean time"
        value={
          statement.mean_exec_time === null || statement.mean_exec_time === undefined
            ? null
            : formatMilliseconds(statement.mean_exec_time)
        }
      />
      <MetricTile
        label="Total time"
        value={
          statement.total_exec_time_ms === null || statement.total_exec_time_ms === undefined
            ? null
            : formatMilliseconds(statement.total_exec_time_ms)
        }
      />
      <MetricTile label="Calls" value={calls === null ? null : formatCount(calls)} />
      <MetricTile
        label="Rows per call"
        value={rowsPerCall === null ? null : formatCount(Math.round(rowsPerCall * 10) / 10)}
      />
      <MetricTile label="Cache hit ratio" value={cacheHit} />
      <MetricTile
        label="WAL generated"
        value={
          statement.wal_bytes === null || statement.wal_bytes === undefined
            ? null
            : formatBytes(statement.wal_bytes)
        }
      />
    </div>
  )
}

/**
 * Page header of the query detail: which statement this is, where it runs and
 * how it performed in the selected time range.
 */
export function QueryOverview({ instanceId, queryid, datname }: QueryOverviewProps) {
  const { range } = useTimeRange()
  const instanceQuery = useInstance(instanceId)
  const statementsQuery = useStatements(
    {
      instance_id: instanceId,
      limit: LOOKUP_LIMIT,
      order_by: 'total_exec_time',
      ...(datname === undefined ? {} : { database: datname }),
    },
    queryWindowOf(range),
  )
  const response = statementsQuery.data as Schemas['StatementsResponse'] | undefined
  const statement = findStatement(response?.statements ?? [], queryid, datname)
  const instance = instanceQuery.data
  const server = instance ? `${instance.addr}:${instance.port}` : null
  const backLink = `/instances/${encodeURIComponent(instanceId)}/queries`

  return (
    <div className="space-y-6">
      <PageHeader
        icon={Zap}
        eyebrow={server ?? 'Query performance'}
        title="Query detail and plan history"
        subtitle={
          <span className="flex flex-wrap items-center gap-2">
            <span>
              Query <code className="font-mono text-xs">{queryid}</code>
            </span>
            {statement?.datname || datname ? (
              <Badge variant="outline">{statement?.datname ?? datname}</Badge>
            ) : null}
          </span>
        }
        freshness={
          statementsQuery.dataUpdatedAt > 0 ? (
            <FreshnessBadge dataUpdatedAt={statementsQuery.dataUpdatedAt} policy="statements" />
          ) : null
        }
        actions={
          <Link
            className="hover:bg-muted inline-flex h-9 items-center gap-1.5 rounded-md border px-3 text-sm font-medium transition-colors"
            to={backLink}
          >
            <ArrowLeft aria-hidden="true" className="size-4" />
            All queries
          </Link>
        }
      />

      {statementsQuery.isPending ? (
        <div aria-busy="true" aria-label="Loading statement statistics" role="status">
          Loading statement statistics…
        </div>
      ) : statement ? (
        <>
          <section
            aria-label="Statement text"
            className="bg-card shadow-card overflow-hidden rounded-xl border"
          >
            <div className="flex items-center justify-between gap-3 border-b px-5 py-2.5">
              <h2 className="text-sm font-semibold">Statement</h2>
              <CopyButton text={statement.query_text} />
            </div>
            <pre className="max-h-80 overflow-auto px-5 py-4 whitespace-pre-wrap" tabIndex={0}>
              <SqlText sql={statement.query_text} />
            </pre>
          </section>
          <StatementMetrics statement={statement} />
        </>
      ) : (
        <p className="text-muted-foreground bg-card rounded-xl border border-dashed px-5 py-4 text-sm">
          No pg_stat_statements activity was recorded for this query in the selected time range, so
          only its plans are shown.
        </p>
      )}
    </div>
  )
}
