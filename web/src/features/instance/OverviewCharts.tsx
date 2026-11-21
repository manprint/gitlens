import type { EChartsOption } from 'echarts'

import type { ApiFailure } from '@/api/client'
import { useQueryMetricsList, useWorkloadSeries, type QueryMetricsResult } from '@/api/queries'
import { ChartCard, type ChartCardState } from '@/components/charts/ChartCard'
import { ChartGroup } from '@/components/charts/ChartGroup'
import {
  buildMetricOption,
  withSecondaryAxis,
  type MetricUnit,
} from '@/components/charts/metric.options'
import { TimeSeriesChart, type TimeSeriesTableSeries } from '@/components/charts/TimeSeriesChart'
import { useTimeRange } from '@/hooks/useTimeRange'
import { useT } from '@/i18n/react'
import { stepParam } from '@/lib/chartStep'
import type { CaptionId } from '@/lib/captions'
import { countGaps } from '@/lib/chartTime'
import { cacheHitRatioSeries, scaleSeries, type MetricSeriesPoint } from '@/lib/metrics'
import { queryWindowOf, type QueryWindow } from '@/lib/timerange'

import { ChartNav } from './ChartNav'
import {
  cardStepSeconds,
  OVERVIEW_GROUPS,
  OVERVIEW_NAV_LINKS,
  type MetricCardSpec,
  type WorkloadCardSpec,
} from './overviewCharts'

interface OverviewChartsProps {
  instanceId: string
  /** Database of the per-database `pg_*` metrics. */
  database: string
}

interface Range {
  from: Date
  to: Date
}

interface CardFrameProps {
  title: string
  captionId: CaptionId
  unitHint: string | undefined
  wide: boolean | undefined
  range: Range
  stepSeconds: number
  state: ChartCardState
  failure: { error: ApiFailure; retry: () => void } | undefined
  series: readonly TimeSeriesTableSeries[]
  option: EChartsOption
  annotateResets: boolean
}

/** Error if a query failed with nothing to show, loading until all answered, empty without a value. */
function cardState(
  queries: readonly { error: ApiFailure | null; data: unknown; isPending: boolean }[],
  series: readonly TimeSeriesTableSeries[],
): ChartCardState {
  if (queries.some((query) => query.error && !query.data)) return 'error'
  if (queries.some((query) => query.isPending)) return 'loading'
  const measured = series.some((item) => item.points.some((point) => point.value !== null))
  return measured ? 'ready' : 'empty'
}

function CardFrame({
  title,
  captionId,
  unitHint,
  wide,
  range,
  stepSeconds,
  state,
  failure,
  series,
  option,
  annotateResets,
}: CardFrameProps) {
  const t = useT()
  // The chart draws no marker: the range line names the runs, a long outage is one gap.
  const gaps = annotateResets ? Math.max(0, ...series.map((item) => countGaps(item.points))) : 0
  return (
    <ChartCard
      captionId={captionId}
      range={range}
      state={state}
      stepSeconds={stepSeconds}
      title={title}
      {...(unitHint === undefined ? {} : { unitHint })}
      {...(wide ? { wide } : {})}
      {...(gaps > 0 ? { subtitle: t('instance.overviewCharts.gaps', { count: gaps }) } : {})}
      {...(failure ? { error: failure.error, onRetry: failure.retry } : {})}
    >
      <TimeSeriesChart
        ariaLabel={title}
        dataTableLabel={`${title} data`}
        option={option}
        series={series}
      />
    </ChartCard>
  )
}

function chartOption(
  series: readonly TimeSeriesTableSeries[],
  units: readonly MetricUnit[],
  stepSeconds: number,
): EChartsOption {
  const option = buildMetricOption(series, {
    stepSeconds,
    ...(units[0] === undefined ? {} : { unit: units[0] }),
  })
  return withSecondaryAxis(option, units)
}

function pointsOf(query: QueryMetricsResult, scale: number | undefined): MetricSeriesPoint[] {
  const points = (query.data?.series ?? []).map(({ ts, value }) => ({ ts, value }))
  return scale === undefined ? points : scaleSeries(points, scale)
}

function firstFailure(queries: readonly QueryMetricsResult[]) {
  const failed = queries.find((query) => query.error && !query.data)
  return failed?.error ? { error: failed.error, retry: () => void failed.refetch() } : undefined
}

function MetricChartCard({
  card,
  instanceId,
  database,
  range,
  window,
}: {
  card: MetricCardSpec
  instanceId: string
  database: string
  range: Range
  window: QueryWindow
}) {
  const t = useT()
  const stepSeconds = cardStepSeconds(card, range.from, range.to)
  const step = stepParam(stepSeconds)
  const queries = useQueryMetricsList(
    card.metrics.map(({ metric, scope }) => ({
      ...(scope === 'database' ? { database } : {}),
      instance_id: instanceId,
      metric,
      step,
    })),
    window,
  )
  const lines = card.metrics.map((line, index) => ({
    name: t(line.labelKey),
    points: queries[index] ? pointsOf(queries[index], card.perStep ? stepSeconds : undefined) : [],
    unit: line.unit ?? card.unit,
  }))
  const derived =
    card.derive === 'cache-hit-ratio'
      ? [
          {
            name: t(card.titleKey),
            points: cacheHitRatioSeries(lines[0]?.points ?? [], lines[1]?.points ?? []),
            unit: card.unit,
          },
        ]
      : lines
  const series = derived.map(({ name, points }) => ({ name, points }))
  const annotateResets = card.annotateResets ?? true

  return (
    <CardFrame
      annotateResets={annotateResets}
      captionId={card.captionId}
      failure={firstFailure(queries)}
      option={chartOption(
        series,
        derived.map(({ unit }) => unit),
        stepSeconds,
      )}
      range={range}
      series={series}
      state={cardState(queries, series)}
      stepSeconds={stepSeconds}
      title={t(card.titleKey)}
      unitHint={card.unitHintKey === undefined ? undefined : t(card.unitHintKey)}
      wide={card.wide}
    />
  )
}

function WorkloadChartCard({
  card,
  instanceId,
  range,
  window,
}: {
  card: WorkloadCardSpec
  instanceId: string
  range: Range
  window: QueryWindow
}) {
  const t = useT()
  const requestedStep = cardStepSeconds(card, range.from, range.to)
  const query = useWorkloadSeries(
    instanceId,
    {
      series: card.series.map(({ series }) => series).join(','),
      source: card.workloadSource,
      step: stepParam(requestedStep),
    },
    window,
  )
  // The server may raise the step (source sampling, 1000-bucket cap).
  const stepSeconds = query.data?.step_seconds ?? requestedStep
  const series = card.series.map(({ series: name, labelKey }) => ({
    name: t(labelKey),
    points: (query.data?.series.find((item) => item.name === name)?.points ?? []).map(
      ({ ts, value }) => ({ ts, value: value ?? null }),
    ),
  }))
  const annotateResets = card.annotateResets ?? true

  return (
    <CardFrame
      annotateResets={annotateResets}
      captionId={card.captionId}
      failure={
        query.error && !query.data
          ? { error: query.error, retry: () => void query.refetch() }
          : undefined
      }
      option={chartOption(
        series,
        card.series.map(({ unit }) => unit ?? card.unit),
        stepSeconds,
      )}
      range={range}
      series={series}
      state={cardState([query], series)}
      stepSeconds={stepSeconds}
      title={t(card.titleKey)}
      unitHint={card.unitHintKey === undefined ? undefined : t(card.unitHintKey)}
      wide={card.wide}
    />
  )
}

/**
 * The Overview charts below the summary tiles card: the sticky anchor nav,
 * then one card per subject (D4), spaced like the other cards of the page.
 */
export function OverviewCharts({ database, instanceId }: OverviewChartsProps) {
  const t = useT()
  const { range } = useTimeRange()
  const window = queryWindowOf(range)

  return (
    <div className="space-y-8 min-[1600px]:space-y-4" data-testid="overview-charts">
      <ChartNav links={OVERVIEW_NAV_LINKS} />
      {OVERVIEW_GROUPS.map((group) => (
        <ChartGroup
          className="scroll-mt-32"
          captionId={group.captionId}
          id={group.id}
          key={group.id}
          title={t(group.titleKey)}
        >
          {group.cards.map((card) =>
            card.source === 'metrics' ? (
              <MetricChartCard
                card={card}
                database={database}
                instanceId={instanceId}
                key={card.id}
                range={range}
                window={window}
              />
            ) : (
              <WorkloadChartCard
                card={card}
                instanceId={instanceId}
                key={card.id}
                range={range}
                window={window}
              />
            ),
          )}
        </ChartGroup>
      ))}
    </div>
  )
}
