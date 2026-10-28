import type { Cluster } from '@/api/types'
import { cn } from '@/lib/utils'

export type HealthFilter = 'all' | Cluster['health']
export type InstanceFilter = 'all' | 'up' | 'down'

export interface FleetSummary {
  health: Record<Cluster['health'], number>
  instancesDown: number
  instancesUp: number
  firingAlerts: number | null
}

interface FleetSummaryBarProps {
  alertsOnly: boolean
  healthFilter: HealthFilter
  instanceFilter: InstanceFilter
  onAlertsOnlyChange: (enabled: boolean) => void
  onHealthFilterChange: (filter: HealthFilter) => void
  onInstanceFilterChange: (filter: InstanceFilter) => void
  summary: FleetSummary
}

type Tone = 'neutral' | 'ok' | 'warning' | 'critical'

const TONE_DOT: Record<Tone, string> = {
  critical: 'bg-destructive',
  neutral: 'bg-primary',
  ok: 'bg-success',
  warning: 'bg-warning',
}

function SummaryFilter({
  active,
  children,
  count,
  disabled = false,
  label,
  onClick,
  tone = 'neutral',
}: {
  active: boolean
  children: string
  count: number | string
  disabled?: boolean
  label: string
  onClick: () => void
  tone?: Tone
}) {
  return (
    <button
      aria-label={`${children} ${count}: ${label}`}
      aria-pressed={active}
      className={cn(
        'bg-card shadow-card group flex h-full flex-col justify-between gap-1.5 rounded-xl border px-4 py-3 text-left transition-all disabled:cursor-not-allowed disabled:opacity-60',
        active
          ? 'border-primary ring-primary/20 ring-2'
          : 'hover:border-primary/40 hover:shadow-raised',
      )}
      disabled={disabled}
      onClick={onClick}
      type="button"
    >
      <span className="text-muted-foreground flex items-center gap-2 text-xs font-medium tracking-wide whitespace-nowrap uppercase">
        <span aria-hidden="true" className={cn('size-2 rounded-full', TONE_DOT[tone])} />
        {children}
      </span>{' '}
      <strong className="text-foreground text-2xl font-semibold tracking-tight tabular-nums">
        {count}
      </strong>
    </button>
  )
}

export function FleetSummaryBar({
  alertsOnly,
  healthFilter,
  instanceFilter,
  onAlertsOnlyChange,
  onHealthFilterChange,
  onInstanceFilterChange,
  summary,
}: FleetSummaryBarProps) {
  const totalClusters = Object.values(summary.health).reduce((total, count) => total + count, 0)

  return (
    <section aria-label="Fleet summary filters" className="space-y-3" data-testid="fleet-summary">
      <h2 className="sr-only">Fleet summary</h2>
      <div className="grid grid-cols-[repeat(auto-fill,minmax(10.5rem,1fr))] gap-3">
        <SummaryFilter
          active={healthFilter === 'all'}
          count={totalClusters}
          label={`Filter all clusters (${totalClusters})`}
          onClick={() => onHealthFilterChange('all')}
        >
          All clusters
        </SummaryFilter>
        <SummaryFilter
          active={healthFilter === 'ok'}
          count={summary.health.ok}
          label={`Filter healthy clusters (${summary.health.ok})`}
          onClick={() => onHealthFilterChange(healthFilter === 'ok' ? 'all' : 'ok')}
          tone="ok"
        >
          Healthy
        </SummaryFilter>
        <SummaryFilter
          active={healthFilter === 'degraded'}
          count={summary.health.degraded}
          label={`Filter degraded clusters (${summary.health.degraded})`}
          onClick={() => onHealthFilterChange(healthFilter === 'degraded' ? 'all' : 'degraded')}
          tone="warning"
        >
          Degraded
        </SummaryFilter>
        <SummaryFilter
          active={healthFilter === 'critical'}
          count={summary.health.critical}
          label={`Filter critical clusters (${summary.health.critical})`}
          onClick={() => onHealthFilterChange(healthFilter === 'critical' ? 'all' : 'critical')}
          tone="critical"
        >
          Critical
        </SummaryFilter>
        <SummaryFilter
          active={instanceFilter === 'up'}
          count={summary.instancesUp}
          label={`Filter clusters with instances up (${summary.instancesUp})`}
          onClick={() => onInstanceFilterChange(instanceFilter === 'up' ? 'all' : 'up')}
          tone="ok"
        >
          Instances up
        </SummaryFilter>
        <SummaryFilter
          active={instanceFilter === 'down'}
          count={summary.instancesDown}
          label={`Filter clusters with instances down (${summary.instancesDown})`}
          onClick={() => onInstanceFilterChange(instanceFilter === 'down' ? 'all' : 'down')}
          tone="critical"
        >
          Instances down
        </SummaryFilter>
        <SummaryFilter
          active={alertsOnly}
          count={summary.firingAlerts ?? 'Unavailable'}
          disabled={summary.firingAlerts === null}
          label={`Filter clusters with firing alerts (${summary.firingAlerts ?? 'unavailable'})`}
          onClick={() => onAlertsOnlyChange(!alertsOnly)}
          tone="warning"
        >
          Firing alerts
        </SummaryFilter>
      </div>
    </section>
  )
}
