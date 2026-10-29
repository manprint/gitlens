import type { EChartsOption } from 'echarts'

import type { ApiFailure } from '@/api/client'
import { useQueryMetricsList, useWorkloadSeries, type QueryMetricsResult } from '@/api/queries'
import { ChartCard, type ChartCardState } from '@/components/charts/ChartCard'
import { ChartGroup } from '@/components/charts/ChartGroup'
import {
  buildMetricOption,
  MAX_RESET_LINES,
  withSecondaryAxis,
  type MetricUnit,
} from '@/components/charts/metric.options'
import { TimeSeriesChart, type TimeSeriesTableSeries } from '@/components/charts/TimeSeriesChart'
import { useTimeRange } from '@/hooks/useTimeRange'
import { stepParam } from '@/lib/chartStep'
import { countResets } from '@/lib/chartTime'
import { cacheHitRatioSeries, scaleSeries, type MetricSeriesPoint } from '@/lib/metrics'
import { queryWindowOf, type QueryWindow } from '@/lib/timerange'

import { ChartNav } from './ChartNav'
import {
  cardStepSeconds,
  OVERVIEW_CHART_GROUP,
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
  const resets = annotateResets ? Math.max(0, ...series.map((item) => countResets(item.points))) : 0
  return (
    <ChartCard
      range={range}
      state={state}
      stepSeconds={stepSeconds}
      title={title}
      {...(unitHint === undefined ? {} : { unitHint })}
      {...(wide ? { wide } : {})}
      {...(resets > MAX_RESET_LINES ? { subtitle: `${resets} counter resets` } : {})}
      {...(failure ? { error: failure.error, onRetry: failure.retry } : {})}
    >
      <TimeSeriesChart
        ariaLabel={title}
        dataTableLabel={`${title} data`}
        group={OVERVIEW_CHART_GROUP}
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
  annotateResets: boolean,
): EChartsOption {
  const option = buildMetricOption(series, {
    annotateResets,
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
    name: line.label,
    points: queries[index] ? pointsOf(queries[index], line.scale) : [],
    unit: line.unit ?? card.unit,
  }))
  const derived =
    card.derive === 'cache-hit-ratio'
      ? [
          {
            name: card.title,
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
      failure={firstFailure(queries)}
      option={chartOption(
        series,
        derived.map(({ unit }) => unit),
        stepSeconds,
        annotateResets,
      )}
      range={range}
      series={series}
      state={cardState(queries, series)}
      stepSeconds={stepSeconds}
      title={card.title}
      unitHint={card.unitHint}
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
  const series = card.series.map(({ series: name, label }) => ({
    name: label,
    points: (query.data?.series.find((item) => item.name === name)?.points ?? []).map(
      ({ ts, value }) => ({ ts, value: value ?? null }),
    ),
  }))
  const annotateResets = card.annotateResets ?? true

  return (
    <CardFrame
      annotateResets={annotateResets}
      failure={
        query.error && !query.data
          ? { error: query.error, retry: () => void query.refetch() }
          : undefined
      }
      option={chartOption(
        series,
        card.series.map(({ unit }) => unit ?? card.unit),
        stepSeconds,
        annotateResets,
      )}
      range={range}
      series={series}
      state={cardState([query], series)}
      stepSeconds={stepSeconds}
      title={card.title}
      unitHint={card.unitHint}
      wide={card.wide}
    />
  )
}

/**
 * The Overview charts below the summary tiles: a separator, the sticky
 * anchor nav, then one group per subject (D4).
 */
export function OverviewCharts({ database, instanceId }: OverviewChartsProps) {
  const { range } = useTimeRange()
  const window = queryWindowOf(range)

  return (
    <div className="mt-12" data-testid="overview-charts">
      <hr className="border-border mb-6" />
      <ChartNav links={OVERVIEW_NAV_LINKS} />
      {OVERVIEW_GROUPS.map((group, index) => (
        <ChartGroup
          className={index === 0 ? 'mt-6 scroll-mt-32' : 'mt-10 scroll-mt-32'}
          description={group.description}
          id={group.id}
          key={group.id}
          title={group.title}
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
