import { useInstanceHost } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { HelpLink } from '@/components/layout/HelpLink'
import { MetricTile } from '@/components/layout/MetricTile'
import { Section } from '@/components/layout/Section'
import { Degraded, ErrorState, Unknown } from '@/components/state'
import type { CaptionId } from '@/lib/captions'
import type { TileUnit } from '@/lib/units'
import type { MessageKey } from '@/i18n/messages/en'
import { useT, type Translate } from '@/i18n/react'
import { formatBytes, formatCount, formatDecimal, formatPercent } from '@/lib/format'

import { HostCharts } from './HostCharts'
import { ProcessesPanel } from './ProcessesPanel'

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
  labelKey: MessageKey
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
  return value === null ? null : formatDecimal(value, 2)
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

const METRICS: readonly HostMetricDefinition[] = [
  {
    key: 'host_cpu_used_ratio',
    labelKey: 'instance.host.metric.cpuUsed',
    captionId: 'host.cpu-used',
    format: formatRatio,
  },
  {
    key: 'host_cpu_count',
    labelKey: 'instance.host.metric.cpuCount',
    captionId: 'host.cpu-count',
    unit: 'CPUs',
    format: formatHostCount,
  },
  {
    key: 'host_mem_total_bytes',
    labelKey: 'instance.host.metric.memTotal',
    captionId: 'host.mem-total',
    format: formatBytes,
  },
  {
    key: 'host_mem_available_bytes',
    labelKey: 'instance.host.metric.memAvailable',
    captionId: 'host.mem-available',
    format: formatBytes,
  },
  {
    key: 'host_swap_total_bytes',
    labelKey: 'instance.host.metric.swapTotal',
    captionId: 'host.swap-total',
    format: formatBytes,
  },
  {
    key: 'host_swap_used_bytes',
    labelKey: 'instance.host.metric.swapUsed',
    captionId: 'host.swap-used',
    format: formatBytes,
  },
  {
    key: 'host_load1',
    labelKey: 'instance.host.metric.load1',
    captionId: 'host.load-average',
    format: formatLoad,
  },
  {
    key: 'host_load5',
    labelKey: 'instance.host.metric.load5',
    captionId: 'host.load-average',
    format: formatLoad,
  },
  {
    key: 'host_load15',
    labelKey: 'instance.host.metric.load15',
    captionId: 'host.load-average',
    format: formatLoad,
  },
  {
    key: 'host_disk_total_bytes',
    labelKey: 'instance.host.metric.diskTotal',
    captionId: 'host.disk-total',
    format: formatBytes,
  },
  {
    key: 'host_disk_free_bytes',
    labelKey: 'instance.host.metric.diskFree',
    captionId: 'host.disk-free',
    format: formatBytes,
  },
  {
    key: 'host_disk_free_ratio',
    labelKey: 'instance.host.metric.diskFreeRatio',
    captionId: 'host.disk-free-ratio',
    format: formatRatio,
  },
]

// The CPU ratio comes from the container's CPU limit when the agent found one,
// and from the whole host otherwise. A cgroup source does not settle which: a
// memory limit alone also makes the source a cgroup, and the ratio then stays
// host-wide. So the cgroup label names both cases.
function cpuUsedLabel(source: string | undefined, t: Translate): string {
  if (source === 'host') return t('instance.host.cpuHost')
  if (source === 'cgroup_v1' || source === 'cgroup_v2') {
    return t('instance.host.cpuCgroup')
  }
  return t('instance.host.metric.cpuUsed')
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
  const t = useT()
  const values = metricValues(data)

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {METRICS.map(({ captionId, format, key, labelKey, unit }) => {
        const value = nullableNumber(values[key])
        const tileLabel = key === 'host_cpu_used_ratio' ? cpuUsedLabel(data.source, t) : t(labelKey)
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
                  {t('instance.host.notVisible')}
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
  const t = useT()
  const query = useInstanceHost(instanceId)
  const host = query.data?.available === false ? undefined : query.data

  return (
    <div className="space-y-8 min-[1600px]:space-y-4">
      <Section
        actions={<HelpLink anchor="instance-overview#host" label={t('instance.host.section')} />}
        id="host-metrics"
        title={t('instance.host.section')}
        captionId="host.section"
      >
        {query.error && !query.data ? (
          <ErrorState
            endpoint="instance host metrics"
            failure={query.error}
            onRetry={() => void query.refetch()}
          />
        ) : query.isPending || query.data === undefined ? (
          <section aria-busy="true" aria-label={t('instance.host.loadingAria')} role="status">
            {t('instance.host.loading')}
          </section>
        ) : query.data.available === false ? (
          <div className="space-y-3">
            <Degraded reason={query.data.reason ?? t('instance.host.unavailable')} />
            <p className="text-muted-foreground text-sm">
              {t('instance.host.localOnlyBefore')} <code>host_local</code>{' '}
              {t('instance.host.localOnlyMiddle')}{' '}
              <a href="/README.md#agent-configuration">{t('instance.host.localOnlyLink')}</a>.
            </p>
          </div>
        ) : (
          <div className="space-y-4">
            <p className="text-sm">
              <strong>{t('instance.host.source')}</strong>{' '}
              {query.data.source ?? <Unknown reason={t('instance.host.sourceNotReported')} />}
            </p>
            <HostMetricTiles data={query.data} />
          </div>
        )}
      </Section>
      {host === undefined ? null : <HostCharts instanceId={instanceId} {...hostChartFlags(host)} />}
      {host === undefined ? null : <ProcessesPanel instanceId={instanceId} />}
    </div>
  )
}
