import type { Cluster } from '@/api/types'

export type HealthFilter = 'all' | Cluster['health']
export type InstanceFilter = 'all' | 'up' | 'down'

export interface FleetSummary {
  health: Record<Cluster['health'], number>
  instancesDown: number
  instancesUp: number
  firingAlerts: number
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

function SummaryFilter({
  active,
  children,
  count,
  label,
  onClick,
}: {
  active: boolean
  children: string
  count: number
  label: string
  onClick: () => void
}) {
  return (
    <button
      aria-label={label}
      aria-pressed={active}
      className="border-input bg-background hover:bg-accent flex min-w-28 flex-col rounded-md border px-3 py-2 text-left text-sm transition-colors"
      onClick={onClick}
      type="button"
    >
      <span className="text-muted-foreground">{children}</span>
      <strong className="text-lg tabular-nums">{count}</strong>
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
      <h2 className="text-lg font-medium">Fleet summary</h2>
      <div className="flex flex-wrap gap-2">
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
        >
          Healthy
        </SummaryFilter>
        <SummaryFilter
          active={healthFilter === 'degraded'}
          count={summary.health.degraded}
          label={`Filter degraded clusters (${summary.health.degraded})`}
          onClick={() => onHealthFilterChange(healthFilter === 'degraded' ? 'all' : 'degraded')}
        >
          Degraded
        </SummaryFilter>
        <SummaryFilter
          active={healthFilter === 'critical'}
          count={summary.health.critical}
          label={`Filter critical clusters (${summary.health.critical})`}
          onClick={() => onHealthFilterChange(healthFilter === 'critical' ? 'all' : 'critical')}
        >
          Critical
        </SummaryFilter>
        <SummaryFilter
          active={instanceFilter === 'up'}
          count={summary.instancesUp}
          label={`Filter clusters with instances up (${summary.instancesUp})`}
          onClick={() => onInstanceFilterChange(instanceFilter === 'up' ? 'all' : 'up')}
        >
          Instances up
        </SummaryFilter>
        <SummaryFilter
          active={instanceFilter === 'down'}
          count={summary.instancesDown}
          label={`Filter clusters with instances down (${summary.instancesDown})`}
          onClick={() => onInstanceFilterChange(instanceFilter === 'down' ? 'all' : 'down')}
        >
          Instances down
        </SummaryFilter>
        <SummaryFilter
          active={alertsOnly}
          count={summary.firingAlerts}
          label={`Filter clusters with firing alerts (${summary.firingAlerts})`}
          onClick={() => onAlertsOnlyChange(!alertsOnly)}
        >
          Firing alerts
        </SummaryFilter>
      </div>
    </section>
  )
}
