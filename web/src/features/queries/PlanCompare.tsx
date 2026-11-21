import type { Schemas } from '@/api/types'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { useT } from '@/i18n/react'
import { formatCount, formatMilliseconds, formatTimestamp } from '@/lib/format'

type PlanStat = Schemas['PlanStat']

const shortHash = (hash: string) => hash.slice(0, 8)

interface Row {
  id: string
  label: string
  older: string
  newer: string
}

/** The statistics of the two compared plans, side by side. */
export function PlanStatsCompare({ older, newer }: { older: PlanStat; newer: PlanStat }) {
  const t = useT()
  const line = (id: string, label: string, value: (stat: PlanStat) => string): Row => ({
    id,
    label,
    older: value(older),
    newer: value(newer),
  })
  const rows: Row[] = [
    line('samples', t('queries.planStats.samples'), (s) => formatCount(s.samples)),
    line('avg', t('queries.planStats.avg'), (s) => formatMilliseconds(s.avg_ms)),
    line('p95', t('queries.planStats.p95'), (s) => formatMilliseconds(s.p95_ms)),
    line('min', t('queries.planStats.min'), (s) => formatMilliseconds(s.min_ms)),
    line('max', t('queries.planStats.max'), (s) => formatMilliseconds(s.max_ms)),
    line('first', t('queries.planStats.firstSeen'), (s) => formatTimestamp(s.first_ts)),
    line('last', t('queries.planStats.lastSeen'), (s) => formatTimestamp(s.last_ts)),
  ]
  const columns: SimpleColumn<Row>[] = [
    { id: 'figure', header: t('queries.planStats.figure'), rowHeader: true, cell: (r) => r.label },
    {
      id: 'older',
      header: t('queries.planStats.olderPlan', { shape: shortHash(older.plan_hash) }),
      cell: (r) => r.older,
    },
    {
      id: 'newer',
      header: t('queries.planStats.newerPlan', { shape: shortHash(newer.plan_hash) }),
      cell: (r) => r.newer,
    },
  ]
  return (
    <SimpleTable
      ariaLabel={t('queries.planStats.compareLabel')}
      captionId="queries.plan-stats-compare"
      caption={t('queries.planStats.compareCaption')}
      columns={columns}
      emptyState={null}
      rowKey={(r) => r.id}
      rows={rows}
      scope={t('queries.planStats.compareScope')}
      tableId="plan-stats-compare"
    />
  )
}

export default PlanStatsCompare
