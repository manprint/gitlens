import type { Cluster } from '@/api/types'
import { InfoHint } from '@/components/layout/InfoHint'
import { useT } from '@/i18n/react'
import type { CaptionId } from '@/lib/captions'
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

const UNIT_KEY = {
  alerts: 'fleet.unit.alerts',
  clusters: 'fleet.unit.clusters',
  instances: 'fleet.unit.instances',
} as const

function SummaryFilter({
  active,
  captionId,
  children,
  count,
  disabled = false,
  label,
  onClick,
  tone = 'neutral',
  unit,
}: {
  active: boolean
  captionId: CaptionId
  children: string
  count: number | string
  disabled?: boolean
  label: string
  onClick: () => void
  tone?: Tone
  unit?: 'clusters' | 'instances' | 'alerts'
}) {
  const t = useT()
  return (
    <div className="relative h-full" data-caption-id={captionId}>
      <button
        aria-label={
          unit !== undefined && typeof count === 'number'
            ? t('fleet.summary.ariaWithUnit', {
                name: children,
                count,
                unit: t(UNIT_KEY[unit]),
                label,
              })
            : t('fleet.summary.ariaNoUnit', { name: children, count, label })
        }
        aria-pressed={active}
        className={cn(
          'bg-card shadow-card group flex h-full w-full flex-col justify-between gap-1.5 rounded-xl border py-3 pr-8 pl-4 text-left transition-all disabled:cursor-not-allowed disabled:opacity-60',
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
        <span className="flex items-baseline gap-1.5">
          <strong className="text-foreground text-2xl font-semibold tracking-tight tabular-nums">
            {count}
          </strong>{' '}
          {unit !== undefined && typeof count === 'number' ? (
            <span className="text-muted-foreground text-xs">{t(UNIT_KEY[unit])}</span>
          ) : null}
        </span>
      </button>
      <InfoHint className="absolute top-2 right-2" id={captionId} label={children} />
    </div>
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
  const t = useT()
  const totalClusters = Object.values(summary.health).reduce((total, count) => total + count, 0)

  return (
    <section
      aria-label={t('fleet.summary.filters')}
      className="space-y-3"
      data-testid="fleet-summary"
    >
      <h2 className="sr-only">{t('fleet.summary.heading')}</h2>
      <div className="grid grid-cols-[repeat(auto-fill,minmax(11.5rem,1fr))] gap-3">
        <SummaryFilter
          active={healthFilter === 'all'}
          captionId="fleet.summary-all"
          count={totalClusters}
          label={t('fleet.summary.filterAll', { count: totalClusters })}
          onClick={() => onHealthFilterChange('all')}
          unit="clusters"
        >
          {t('fleet.summary.all')}
        </SummaryFilter>
        <SummaryFilter
          active={healthFilter === 'ok'}
          captionId="fleet.summary-healthy"
          count={summary.health.ok}
          label={t('fleet.summary.filterHealthy', { count: summary.health.ok })}
          onClick={() => onHealthFilterChange(healthFilter === 'ok' ? 'all' : 'ok')}
          tone="ok"
          unit="clusters"
        >
          {t('fleet.summary.healthy')}
        </SummaryFilter>
        <SummaryFilter
          active={healthFilter === 'degraded'}
          captionId="fleet.summary-degraded"
          count={summary.health.degraded}
          label={t('fleet.summary.filterDegraded', { count: summary.health.degraded })}
          onClick={() => onHealthFilterChange(healthFilter === 'degraded' ? 'all' : 'degraded')}
          tone="warning"
          unit="clusters"
        >
          {t('fleet.summary.degraded')}
        </SummaryFilter>
        <SummaryFilter
          active={healthFilter === 'critical'}
          captionId="fleet.summary-critical"
          count={summary.health.critical}
          label={t('fleet.summary.filterCritical', { count: summary.health.critical })}
          onClick={() => onHealthFilterChange(healthFilter === 'critical' ? 'all' : 'critical')}
          tone="critical"
          unit="clusters"
        >
          {t('fleet.summary.critical')}
        </SummaryFilter>
        <SummaryFilter
          active={instanceFilter === 'up'}
          captionId="fleet.summary-instances-up"
          count={summary.instancesUp}
          label={t('fleet.summary.filterInstancesUp', { count: summary.instancesUp })}
          onClick={() => onInstanceFilterChange(instanceFilter === 'up' ? 'all' : 'up')}
          tone="ok"
          unit="instances"
        >
          {t('fleet.summary.instancesUp')}
        </SummaryFilter>
        <SummaryFilter
          active={instanceFilter === 'down'}
          captionId="fleet.summary-instances-down"
          count={summary.instancesDown}
          label={t('fleet.summary.filterInstancesDown', { count: summary.instancesDown })}
          onClick={() => onInstanceFilterChange(instanceFilter === 'down' ? 'all' : 'down')}
          tone="critical"
          unit="instances"
        >
          {t('fleet.summary.instancesDown')}
        </SummaryFilter>
        <SummaryFilter
          active={alertsOnly}
          captionId="fleet.summary-firing-alerts"
          count={summary.firingAlerts ?? t('fleet.summary.unavailable')}
          disabled={summary.firingAlerts === null}
          label={t('fleet.summary.filterFiringAlerts', {
            count: summary.firingAlerts ?? t('fleet.summary.unavailableLower'),
          })}
          onClick={() => onAlertsOnlyChange(!alertsOnly)}
          tone="warning"
          unit="alerts"
        >
          {t('fleet.summary.firingAlerts')}
        </SummaryFilter>
      </div>
    </section>
  )
}
