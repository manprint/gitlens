import { ArrowLeft, Zap } from 'lucide-react'
import { Link } from 'react-router-dom'

import { useInstance } from '@/api/queries'
import { CopyButton } from '@/components/layout/CopyButton'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { MetricTile } from '@/components/layout/MetricTile'
import { HelpLink } from '@/components/layout/HelpLink'
import { PageHeader } from '@/components/layout/PageHeader'
import { SqlText } from '@/components/layout/SqlText'
import { Badge } from '@/components/ui/badge'
import { useT } from '@/i18n/react'
import { formatBytes, formatCount, formatMilliseconds, formatPercent } from '@/lib/format'
import { instanceLabel } from '@/lib/names'

import { useQueryStatement, type Statement } from './useQueryStatement'

interface QueryOverviewProps {
  instanceId: string
  queryid: string
  datname?: string
}

function StatementMetrics({ statement }: { statement: Statement }) {
  const t = useT()
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
        captionId="queries.detail-mean-time"
        label={t('queries.common.meanTime')}
        value={
          statement.mean_exec_time === null || statement.mean_exec_time === undefined
            ? null
            : formatMilliseconds(statement.mean_exec_time)
        }
      />
      <MetricTile
        captionId="queries.detail-total-time"
        label={t('queries.common.totalTime')}
        value={
          statement.total_exec_time_ms === null || statement.total_exec_time_ms === undefined
            ? null
            : formatMilliseconds(statement.total_exec_time_ms)
        }
      />
      <MetricTile
        captionId="queries.detail-calls"
        label={t('queries.common.calls')}
        value={calls === null ? null : formatCount(calls)}
      />
      <MetricTile
        captionId="queries.detail-rows-per-call"
        label={t('queries.overview.rowsPerCall')}
        unit="rows"
        value={rowsPerCall === null ? null : formatCount(Math.round(rowsPerCall * 10) / 10)}
      />
      <MetricTile
        captionId="queries.detail-cache-hit-ratio"
        label={t('queries.overview.cacheHitRatio')}
        value={cacheHit}
      />
      <MetricTile
        captionId="queries.detail-wal"
        label={t('queries.overview.walGenerated')}
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
  const t = useT()
  const instanceQuery = useInstance(instanceId)
  const { statementsQuery, statement } = useQueryStatement(instanceId, queryid, datname)
  const instance = instanceQuery.data
  const server = instance ? instanceLabel(instance) : null
  const backLink = `/instances/${encodeURIComponent(instanceId)}/queries`

  return (
    <div className="space-y-6 min-[1600px]:space-y-4">
      <PageHeader
        help={<HelpLink anchor="query-performance" label={t('queries.overview.detailHelp')} />}
        icon={Zap}
        eyebrow={server ?? t('queries.overview.eyebrow')}
        title={t('queries.overview.title')}
        subtitle={
          <span className="flex flex-wrap items-center gap-2">
            <span>
              {t('queries.overview.query')} <code className="font-mono text-xs">{queryid}</code>
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
            {t('queries.overview.allQueries')}
          </Link>
        }
      />

      {statementsQuery.isPending ? (
        <div aria-busy="true" aria-label={t('queries.overview.loadingStatsLabel')} role="status">
          {t('queries.overview.loadingStats')}
        </div>
      ) : statement ? (
        <>
          <section
            aria-label={t('queries.overview.statementText')}
            className="bg-card shadow-card overflow-hidden rounded-xl border"
          >
            <div className="flex items-center justify-between gap-3 border-b px-5 py-2.5">
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="text-sm font-semibold">{t('queries.overview.statement')}</h2>
                <HelpLink
                  anchor="query-performance#statement-text"
                  label={t('queries.overview.statementText')}
                />
              </div>
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
          {t('queries.overview.noActivity')}
        </p>
      )}
    </div>
  )
}
