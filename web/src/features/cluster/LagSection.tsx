import { useMemo } from 'react'

import { Degraded } from '@/components/state/Degraded'
import { EmptyState } from '@/components/state/EmptyState'
import { Unknown } from '@/components/state/Unknown'
import { ChartCard } from '@/components/charts/ChartCard'
import { TimeSeriesChart, type TimeSeriesTableSeries } from '@/components/charts/TimeSeriesChart'
import { buildLagOption } from '@/components/charts/lag.options'
import { useTimeRange } from '@/hooks/useTimeRange'
import { EmptyCaptioned } from './EmptyCaptioned'
import {
  alignSeries,
  hasGaps,
  replicationInstanceLabel,
  type AlignedReplicationSeries,
  type ReplicationInstance,
  type ReplicationMetricEdge,
} from '@/lib/replication'
import type { TimeRangeResult } from '@/lib/timerange'

/**
 * The server returns raw replication rows (no buckets), written by the
 * replication_streaming check every 10 s (its DefaultInterval in
 * internal/check/replication_streaming.go), so the chart's step is that interval.
 */
const LAG_SAMPLE_SECONDS = 10

export interface LagSectionProps {
  edges: readonly ReplicationMetricEdge[]
  /** The cluster's instances, to name the ends of each edge. */
  instances?: readonly ReplicationInstance[]
  /** A controlled range keeps retention and empty-state behaviour easy to verify. */
  range?: TimeRangeResult
}

interface EdgeGroup {
  from: string
  to: string
  metrics: ReplicationMetricEdge[]
}

const NO_INSTANCES: readonly ReplicationInstance[] = []

function groupEdges(edges: readonly ReplicationMetricEdge[]): EdgeGroup[] {
  const groups = new Map<string, EdgeGroup>()
  for (const edge of edges) {
    const key = `${edge.from}\u0000${edge.to}`
    const group = groups.get(key)
    if (group) {
      group.metrics.push(edge)
    } else {
      groups.set(key, { from: edge.from, metrics: [edge], to: edge.to })
    }
  }
  return [...groups.values()].sort((a, b) =>
    `${a.from}\u0000${a.to}`.localeCompare(`${b.from}\u0000${b.to}`),
  )
}

function tableSeries(aligned: AlignedReplicationSeries): TimeSeriesTableSeries[] {
  return ['write_lag_sec', 'flush_lag_sec', 'replay_lag_sec'].map((name) => ({
    name,
    points: aligned.timestamps.map((ts, index) => ({
      ts,
      value: aligned[name as keyof AlignedReplicationSeries][index] as number | null,
    })),
  }))
}

function edgeHasGaps(aligned: AlignedReplicationSeries): boolean {
  return (
    hasGaps(aligned.write_lag_sec) ||
    hasGaps(aligned.flush_lag_sec) ||
    hasGaps(aligned.replay_lag_sec)
  )
}

// The ends used to be printed as raw instance IDs. The standby end is an
// instance ID only when it resolves to a monitored instance; otherwise it is
// the application_name the primary reports, shown as is.
function edgeHeading(group: EdgeGroup, instances: readonly ReplicationInstance[]): string {
  const sync = group.metrics.find((edge) => edge.sync_state !== undefined)?.sync_state ?? 'Unknown'
  return `${replicationInstanceLabel(instances, group.from)} → ${replicationInstanceLabel(instances, group.to)} (${sync})`
}

export function LagSection({
  edges,
  instances = NO_INSTANCES,
  range: controlledRange,
}: LagSectionProps) {
  const { range: urlRange } = useTimeRange()
  const range = controlledRange ?? urlRange
  const groups = useMemo(() => groupEdges(edges), [edges])
  const exceedsRetention = range.to.getTime() - range.from.getTime() > 30 * 24 * 60 * 60 * 1_000

  if (exceedsRetention) {
    return <Degraded reason="Selected range exceeds the 30-day raw retention window." />
  }

  return (
    <section aria-labelledby="replication-lag-heading">
      <h2 id="replication-lag-heading">Replication lag</h2>
      {groups.length === 0 ? (
        <EmptyCaptioned ariaLabel="Replication lag" captionId="lag.edge">
          <EmptyState
            title="No replication lag"
            description="No replication lag data in this range, so there is nothing to chart. Lag is recorded for each standby that copies changes from a primary of this cluster."
          />
        </EmptyCaptioned>
      ) : (
        <div className="space-y-6">
          {groups.map((group) => {
            const aligned = alignSeries(group.metrics)
            const heading = edgeHeading(group, instances)
            const hasData = aligned.timestamps.length > 0
            return (
              <article key={`${group.from}\u0000${group.to}`} aria-label={heading}>
                <ChartCard
                  captionId="lag.edge"
                  range={range}
                  state="ready"
                  stepSeconds={LAG_SAMPLE_SECONDS}
                  title={heading}
                >
                  {hasData ? (
                    <>
                      <TimeSeriesChart
                        ariaLabel={`Replication lag for ${heading}`}
                        option={buildLagOption(aligned)}
                        series={tableSeries(aligned)}
                      />
                      {edgeHasGaps(aligned) ? (
                        <p role="note">Gaps in this range indicate missing, unmeasured samples.</p>
                      ) : null}
                    </>
                  ) : (
                    <p role="status">
                      <Unknown reason="No replication lag sample was returned for this edge." /> No
                      data in this range.
                    </p>
                  )}
                </ChartCard>
              </article>
            )
          })}
        </div>
      )}
    </section>
  )
}
