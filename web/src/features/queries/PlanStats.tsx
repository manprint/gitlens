import type { Schemas } from '@/api/types'
import { InfoHint } from '@/components/layout/InfoHint'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { useT } from '@/i18n/react'
import { formatCount, formatMilliseconds, formatTimestamp } from '@/lib/format'

type PlanStat = Schemas['PlanStat']

export interface PlanStatsProps {
  stats: Schemas['PlanStats']
  selectedHashes: readonly string[]
  /** Whether a stored plan of this hash is loaded and can be compared. */
  canTick: (hash: string) => boolean
  onToggle: (hash: string) => void
}

function shortHash(hash: string): string {
  return hash.slice(0, 8)
}

export function PlanStats({ stats, selectedHashes, canTick, onToggle }: PlanStatsProps) {
  const t = useT()
  if (stats.plans.length === 0) return null
  const columns: SimpleColumn<PlanStat>[] = [
    {
      id: 'select',
      header: t('queries.common.select'),
      srOnlyHeader: true,
      cell: (row) => (
        <input
          type="checkbox"
          aria-label={t('queries.planStats.select', { shape: shortHash(row.plan_hash) })}
          checked={selectedHashes.includes(row.plan_hash)}
          disabled={!row.has_plan || !canTick(row.plan_hash)}
          onChange={() => onToggle(row.plan_hash)}
        />
      ),
    },
    {
      id: 'shape',
      header: t('queries.planHistory.shape'),
      rowHeader: true,
      sortValue: (row) => row.plan_hash,
      cell: (row) => (
        <span className="flex flex-col">
          <code className="font-mono text-xs" title={row.plan_hash}>
            {shortHash(row.plan_hash)}
          </code>
          {!row.has_plan && (
            <span className="text-muted-foreground text-xs">{t('queries.planStats.noBody')}</span>
          )}
        </span>
      ),
    },
    {
      id: 'samples',
      header: t('queries.planStats.samples'),
      sortValue: (row) => row.samples,
      cell: (row) => formatCount(row.samples),
    },
    {
      id: 'avg',
      header: t('queries.planStats.avg'),
      sortValue: (row) => row.avg_ms,
      cell: (row) => formatMilliseconds(row.avg_ms),
    },
    {
      id: 'p95',
      header: t('queries.planStats.p95'),
      sortValue: (row) => row.p95_ms,
      cell: (row) => formatMilliseconds(row.p95_ms),
    },
    {
      id: 'first',
      header: t('queries.planStats.firstSeen'),
      sortValue: (row) => Date.parse(row.first_ts),
      cell: (row) => formatTimestamp(row.first_ts),
    },
    {
      id: 'last',
      header: t('queries.planStats.lastSeen'),
      sortValue: (row) => Date.parse(row.last_ts),
      cell: (row) => formatTimestamp(row.last_ts),
    },
  ]
  return (
    <section
      aria-labelledby="plan-stats-title"
      className="space-y-3"
      data-caption-id="queries.plan-stats"
    >
      <div>
        <div className="flex items-center gap-1">
          <h3 id="plan-stats-title" className="text-sm font-semibold">
            {t('queries.planStats.title')}
          </h3>
          <InfoHint id="queries.plan-stats" label={t('queries.planStats.title')} />
        </div>
        <p className="text-muted-foreground mt-1 text-sm">{t('queries.planStats.intro')}</p>
      </div>
      <SimpleTable
        ariaLabel={t('queries.planStats.entriesLabel')}
        captionId="queries.plan-stats-rows"
        caption={t('queries.planStats.caption')}
        columns={columns}
        emptyState={null}
        rowKey={(row) => row.plan_hash}
        rows={stats.plans}
        scope={t('queries.planStats.scope')}
        tableId="plan-stats"
      />
      {stats.changes.length > 0 && (
        <ul className="space-y-1 text-sm" aria-label={t('queries.planStats.changesLabel')}>
          {stats.changes.map((change) => (
            <li key={`${change.ts}:${change.to_plan_hash}`}>
              {t('queries.planStats.changed', {
                time: formatTimestamp(change.ts),
                from: shortHash(change.from_plan_hash),
                to: shortHash(change.to_plan_hash),
              })}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

export default PlanStats
