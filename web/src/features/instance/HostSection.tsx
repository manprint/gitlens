import { useInstanceHost } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { MetricTile } from '@/components/layout/MetricTile'
import { Section } from '@/components/layout/Section'
import { Degraded, ErrorState, Unknown } from '@/components/state'
import { formatBytes, formatCount, formatPercent } from '@/lib/format'

interface HostSectionProps {
  instanceId: string
}

type HostMetricKey =
  | 'host_cpu_used_ratio'
  | 'host_cpu_count'
  | 'host_mem_total_bytes'
  | 'host_mem_available_bytes'
  | 'host_swap_total_bytes'
  | 'host_swap_used_bytes'
  | 'host_load1'
  | 'host_load5'
  | 'host_load15'
  | 'host_disk_total_bytes'
  | 'host_disk_free_bytes'
  | 'host_disk_free_ratio'

interface HostMetricDefinition {
  key: HostMetricKey
  label: string
  format: (value: number | null) => string | null
}

function nullableNumber(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function metricValues(data: Schemas['HostResponse']): Record<string, unknown> {
  if (data.metrics !== undefined) {
    return data.metrics
  }

  return Object.fromEntries(
    Object.entries(data as Record<string, unknown>).filter(
      ([key, value]) => key.startsWith('host_') && typeof value === 'number',
    ),
  )
}

function formatRatio(value: number | null): string | null {
  return value === null ? null : formatPercent(value, 1)
}

function formatLoad(value: number | null): string | null {
  return value === null ? null : value.toFixed(2)
}

function formatHostCount(value: number | null): string | null {
  return value === null ? null : formatCount(value)
}

const METRICS: readonly HostMetricDefinition[] = [
  { key: 'host_cpu_used_ratio', label: 'CPU used', format: formatRatio },
  { key: 'host_cpu_count', label: 'CPU count', format: formatHostCount },
  { key: 'host_mem_total_bytes', label: 'Memory total', format: formatBytes },
  { key: 'host_mem_available_bytes', label: 'Memory available', format: formatBytes },
  { key: 'host_swap_total_bytes', label: 'Swap total', format: formatBytes },
  { key: 'host_swap_used_bytes', label: 'Swap used', format: formatBytes },
  { key: 'host_load1', label: 'Load average (1m)', format: formatLoad },
  { key: 'host_load5', label: 'Load average (5m)', format: formatLoad },
  { key: 'host_load15', label: 'Load average (15m)', format: formatLoad },
  { key: 'host_disk_total_bytes', label: 'Filesystem total', format: formatBytes },
  { key: 'host_disk_free_bytes', label: 'Filesystem free', format: formatBytes },
  { key: 'host_disk_free_ratio', label: 'Filesystem free ratio', format: formatRatio },
]

function HostMetricTiles({ data }: { data: Schemas['HostResponse'] }) {
  const values = metricValues(data)

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {METRICS.map(({ format, key, label }) => {
        const value = nullableNumber(values[key])
        return <MetricTile key={key} label={label} value={format(value)} />
      })}
    </div>
  )
}

export function HostSection({ instanceId }: HostSectionProps) {
  const query = useInstanceHost(instanceId)

  return (
    <Section
      id="host-metrics"
      title="Host metrics"
      description="CPU, memory, load average, and filesystem readings. Per-device IOPS, disk latency, and network metrics are not collected."
    >
      {query.error && !query.data ? (
        <ErrorState
          endpoint="instance host metrics"
          failure={query.error}
          onRetry={() => void query.refetch()}
        />
      ) : query.isPending || query.data === undefined ? (
        <section aria-busy="true" aria-label="Loading host metrics" role="status">
          Loading host metrics…
        </section>
      ) : query.data.available === false ? (
        <div className="space-y-3">
          <Degraded reason={query.data.reason ?? 'Host metrics are unavailable.'} />
          <p className="text-muted-foreground text-sm">
            Host collection is local-only. Configure <code>host_local</code> in the{' '}
            <a href="/README.md#agent-configuration">agent configuration documentation</a>.
          </p>
        </div>
      ) : (
        <div className="space-y-4">
          <p className="text-sm">
            <strong>Source:</strong>{' '}
            {query.data.source ?? <Unknown reason="Source was not reported." />}
          </p>
          <HostMetricTiles data={query.data} />
        </div>
      )}
    </Section>
  )
}
