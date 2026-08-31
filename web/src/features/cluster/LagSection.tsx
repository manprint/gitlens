import { useMemo } from 'react'

import { Degraded } from '@/components/state/Degraded'
import { Unknown } from '@/components/state/Unknown'
import { TimeSeriesChart, type TimeSeriesTableSeries } from '@/components/charts/TimeSeriesChart'
import { buildLagOption } from '@/components/charts/lag.options'
import { useTimeRange } from '@/hooks/useTimeRange'
import {
  alignSeries,
  hasGaps,
  type AlignedReplicationSeries,
  type ReplicationMetricEdge,
} from '@/lib/replication'
import type { TimeRangeResult } from '@/lib/timerange'

export interface LagSectionProps {
  edges: readonly ReplicationMetricEdge[]
  /** A controlled range keeps retention and empty-state behaviour easy to verify. */
  range?: TimeRangeResult
}

interface EdgeGroup {
  from: string
  to: string
  metrics: ReplicationMetricEdge[]
}

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

function edgeHeading(group: EdgeGroup): string {
  const sync = group.metrics.find((edge) => edge.sync_state !== undefined)?.sync_state ?? 'Unknown'
  return `${group.from} → ${group.to} (${sync})`
}

export function LagSection({ edges, range: controlledRange }: LagSectionProps) {
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
        <p role="status">
          <Unknown reason="No replication edges were returned for this range." /> No replication lag
          data in this range.
        </p>
      ) : (
        <div className="space-y-6">
          {groups.map((group) => {
            const aligned = alignSeries(group.metrics)
            const heading = edgeHeading(group)
            const hasData = aligned.timestamps.length > 0
            return (
              <article
                key={`${group.from}-${group.to}`}
                aria-labelledby={`lag-${group.from}-${group.to}`}
              >
                <h3 id={`lag-${group.from}-${group.to}`}>{heading}</h3>
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
              </article>
            )
          })}
        </div>
      )}
    </section>
  )
}
