import { useInstanceHost } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { MetricTile } from '@/components/layout/MetricTile'
import { Section } from '@/components/layout/Section'
import { Degraded, ErrorState, Unknown } from '@/components/state'
import type { CaptionId } from '@/lib/captions'
import type { TileUnit } from '@/lib/units'
import { formatBytes, formatCount, formatPercent } from '@/lib/format'

import { HostCharts } from './HostCharts'

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
  captionId: CaptionId
  unit?: TileUnit
  format: (value: number | null) => string | null
}

function nullableNumber(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

// The server flattens every host metric to a top-level key, so the values are
// read from the response itself.
function metricValues(data: Schemas['HostResponse']): Record<string, unknown> {
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

/** The filesystem tiles depend on the agent seeing the data directory. */
const DISK_KEYS: ReadonlySet<HostMetricKey> = new Set([
  'host_disk_total_bytes',
  'host_disk_free_bytes',
  'host_disk_free_ratio',
])

export const DISK_NOT_VISIBLE = 'Not visible to the agent'

const METRICS: readonly HostMetricDefinition[] = [
  {
    key: 'host_cpu_used_ratio',
    label: 'CPU used',
    captionId: 'host.cpu-used',
    format: formatRatio,
  },
  {
    key: 'host_cpu_count',
    label: 'CPU count',
    captionId: 'host.cpu-count',
    unit: 'CPUs',
    format: formatHostCount,
  },
  {
    key: 'host_mem_total_bytes',
    label: 'Memory total',
    captionId: 'host.mem-total',
    format: formatBytes,
  },
  {
    key: 'host_mem_available_bytes',
    label: 'Memory available',
    captionId: 'host.mem-available',
    format: formatBytes,
  },
  {
    key: 'host_swap_total_bytes',
    label: 'Swap total',
    captionId: 'host.swap-total',
    format: formatBytes,
  },
  {
    key: 'host_swap_used_bytes',
    label: 'Swap used',
    captionId: 'host.swap-used',
    format: formatBytes,
  },
  {
    key: 'host_load1',
    label: 'Load average (1 min)',
    captionId: 'host.load-average',
    format: formatLoad,
  },
  {
    key: 'host_load5',
    label: 'Load average (5 min)',
    captionId: 'host.load-average',
    format: formatLoad,
  },
  {
    key: 'host_load15',
    label: 'Load average (15 min)',
    captionId: 'host.load-average',
    format: formatLoad,
  },
  {
    key: 'host_disk_total_bytes',
    label: 'Filesystem total',
    captionId: 'host.disk-total',
    format: formatBytes,
  },
  {
    key: 'host_disk_free_bytes',
    label: 'Filesystem free',
    captionId: 'host.disk-free',
    format: formatBytes,
  },
  {
    key: 'host_disk_free_ratio',
    label: 'Filesystem free ratio',
    captionId: 'host.disk-free-ratio',
    format: formatRatio,
  },
]

// The CPU ratio comes from the container's CPU limit when the agent found one,
// and from the whole host otherwise. A cgroup source does not settle which: a
// memory limit alone also makes the source a cgroup, and the ratio then stays
// host-wide. So the cgroup label names both cases.
function cpuUsedLabel(source: string | undefined): string {
  if (source === 'host') return 'CPU used (host-wide)'
  if (source === 'cgroup_v1' || source === 'cgroup_v2') {
    return 'CPU used (of container CPU limit, or host if none set)'
  }
  return 'CPU used'
}

/** What the latest reading says about the filesystem and swap charts. */
function hostChartFlags(data: Schemas['HostResponse']) {
  const values = metricValues(data)
  return {
    diskMeasured: typeof values.host_disk_total_bytes === 'number',
    swapAbsent: values.host_swap_total_bytes === 0,
  }
}

function HostMetricTiles({ data }: { data: Schemas['HostResponse'] }) {
  const values = metricValues(data)

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {METRICS.map(({ captionId, format, key, label, unit }) => {
        const value = nullableNumber(values[key])
        const tileLabel = key === 'host_cpu_used_ratio' ? cpuUsedLabel(data.source) : label
        const shown = format(value)
        return (
          <MetricTile
            key={key}
            captionId={captionId}
            label={tileLabel}
            {...(unit === undefined ? {} : { unit })}
            value={
              value === null && DISK_KEYS.has(key) ? (
                <span className="text-muted-foreground text-base font-normal">
                  {DISK_NOT_VISIBLE}
                </span>
              ) : (
                shown
              )
            }
          />
        )
      })}
    </div>
  )
}

export function HostSection({ instanceId }: HostSectionProps) {
  const query = useInstanceHost(instanceId)

  return (
    <Section id="host-metrics" title="Host metrics" captionId="host.section">
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
          <hr className="border-border my-8" />
          <HostCharts instanceId={instanceId} {...hostChartFlags(query.data)} />
        </div>
      )}
    </Section>
  )
}
