import { useMemo } from 'react'

import type { ApiFailure } from '@/api/client'
import { useIndexHistory } from '@/api/queries'
import { ChartCard, type ChartCardState } from '@/components/charts/ChartCard'
import { buildMetricOption, type MetricUnit } from '@/components/charts/metric.options'
import { TimeSeriesChart } from '@/components/charts/TimeSeriesChart'
import { Badge } from '@/components/ui/badge'
import { useTimeRange } from '@/hooks/useTimeRange'
import { useT, type Translate } from '@/i18n/react'
import { SAMPLE_SECONDS, stepFor, stepParam } from '@/lib/chartStep'
import { formatRelative } from '@/lib/format'
import type { IndexRow } from '@/lib/indexAnalysis'
import { queryWindowOf } from '@/lib/timerange'

/** The index statistics are sampled every 5 minutes: a bucket is never narrower than two samples. */
const HISTORY_STEP_FLOOR = 2 * SAMPLE_SECONDS.indexes!

interface ChartSpec {
  titleKey: 'indexes.indexDetail.indexScans' | 'indexes.indexDetail.indexSize'
  unitHint: string
  unit: MetricUnit
  value: 'idx_scan_rate' | 'index_bytes'
}

const CHARTS: readonly ChartSpec[] = [
  {
    titleKey: 'indexes.indexDetail.indexScans',
    unitHint: 'per second',
    unit: 'count',
    value: 'idx_scan_rate',
  },
  {
    titleKey: 'indexes.indexDetail.indexSize',
    unitHint: 'bytes',
    unit: 'bytes',
    value: 'index_bytes',
  },
]

function lastScanText(row: IndexRow, t: Translate): string {
  if (row.lastScanStatus === 'scanned' && row.lastScanAt !== null) {
    return t('indexes.indexDetail.lastScan', { when: formatRelative(row.lastScanAt, new Date()) })
  }
  if (row.lastScanStatus === 'never_since_reset') {
    return t('indexes.indexDetail.noScanSinceReset')
  }
  return t('indexes.indexDetail.lastScanUnavailable')
}

/**
 * The history of one index and what the row alone does not say: whether it is
 * valid or unique, and when it was last scanned. The window is the page's time
 * range, which never exceeds the 30 days the endpoint answers.
 */
export function IndexDetail({ instanceId, row }: { instanceId: string; row: IndexRow }) {
  const t = useT()

  const { range } = useTimeRange()
  const window = useMemo(() => queryWindowOf(range), [range])
  const stepSeconds = Math.max(
    stepFor('rate', range.from, range.to, SAMPLE_SECONDS.indexes),
    HISTORY_STEP_FLOOR,
  )
  const query = useIndexHistory(
    instanceId,
    {
      database: row.datname,
      schema: row.schema,
      index: row.name,
      step: stepParam(stepSeconds),
    },
    window,
  )
  const points = query.data?.points
  const failure: ApiFailure | null = query.error
  const noHistory = query.data !== undefined && (points?.length ?? 0) === 0

  const seriesOf = (chart: ChartSpec) => [
    {
      name: t(chart.titleKey),
      points: (points ?? []).map((point) => ({ ts: point.ts, value: point[chart.value] })),
    },
  ]
  const cardProps = (chart: ChartSpec) => {
    const state: ChartCardState =
      failure !== null && query.data === undefined
        ? 'error'
        : query.isPending
          ? 'loading'
          : seriesOf(chart)[0]!.points.some((point) => point.value !== null)
            ? 'ready'
            : 'empty'
    return {
      range,
      state,
      stepSeconds: query.data?.step_seconds ?? stepSeconds,
      title: t('indexes.indexDetail.chartTitle', { title: t(chart.titleKey), name: row.name }),
      unitHint: chart.unitHint,
      ...(failure === null ? {} : { error: failure, onRetry: () => void query.refetch() }),
    }
  }
  const chartOf = (chart: ChartSpec) => {
    const named = seriesOf(chart)
    return (
      <TimeSeriesChart
        ariaLabel={t('indexes.indexDetail.chartTitle', {
          title: t(chart.titleKey),
          name: row.name,
        })}
        dataTableLabel={t('indexes.indexDetail.chartDataTitle', {
          title: t(chart.titleKey),
          name: row.name,
        })}
        height={220}
        option={buildMetricOption(named, {
          unit: chart.unit,
          stepSeconds,
        })}
        series={named}
      />
    )
  }

  return (
    <div className="space-y-3" data-testid="index-detail">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <Badge variant={row.valid ? 'outline' : 'destructive'}>
          {row.valid ? t('indexes.indexDetail.valid') : t('indexes.indexDetail.invalid')}
        </Badge>
        {row.primary ? (
          <Badge variant="outline">{t('indexes.indexDetail.primaryKey')}</Badge>
        ) : null}
        {row.unique && !row.primary ? (
          <Badge variant="outline">{t('indexes.indexDetail.unique')}</Badge>
        ) : null}
        <span className="text-muted-foreground">{lastScanText(row, t)}</span>
      </div>
      {noHistory ? (
        <p className="text-muted-foreground text-sm">{t('indexes.indexDetail.noHistory')}</p>
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          <ChartCard captionId="index.history-scans" {...cardProps(CHARTS[0]!)}>
            {chartOf(CHARTS[0]!)}
          </ChartCard>
          <ChartCard captionId="index.history-size" {...cardProps(CHARTS[1]!)}>
            {chartOf(CHARTS[1]!)}
          </ChartCard>
        </div>
      )}
    </div>
  )
}
