import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { useAlerts, useClusters } from '@/api/queries'
import type { Cluster, Schemas } from '@/api/types'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { Input } from '@/components/ui/input'
import { AGENT_STALE_AFTER_SECONDS, deriveAgentHealth } from '@/lib/fleet'

import { AgentHealthStrip } from './AgentHealthStrip'
import { ClusterCard } from './ClusterCard'
import {
  FleetSummaryBar,
  type FleetSummary,
  type HealthFilter,
  type InstanceFilter,
} from './FleetSummaryBar'

const HEALTH_FILTERS: readonly HealthFilter[] = ['all', 'ok', 'degraded', 'critical']
const HEALTH_RANK: Record<Cluster['health'], number> = { critical: 0, degraded: 1, ok: 2 }
const EMPTY_CLUSTERS: Cluster[] = []
const EMPTY_ALERTS: Schemas['Alert'][] = []

function readHealthFilter(value: string | null): HealthFilter {
  return HEALTH_FILTERS.includes(value as HealthFilter) ? (value as HealthFilter) : 'all'
}

function alertMatchesCluster(
  alert: { cluster_id?: string; labels: Record<string, unknown> },
  cluster: Cluster,
): boolean {
  if (alert.cluster_id === cluster.cluster_id) return true
  return typeof alert.labels.cluster === 'string' && alert.labels.cluster === cluster.name
}

function summarize(clusters: readonly Cluster[], firingAlerts: number): FleetSummary {
  const summary: FleetSummary = {
    health: { ok: 0, degraded: 0, critical: 0 },
    instancesDown: 0,
    instancesUp: 0,
    firingAlerts,
  }

  for (const cluster of clusters) {
    summary.health[cluster.health] += 1
    for (const instance of cluster.instances) {
      if (instance.up) summary.instancesUp += 1
      else summary.instancesDown += 1
    }
  }

  return summary
}

export function FleetPage() {
  const clustersQuery = useClusters()
  const alertsQuery = useAlerts({ state: 'firing' })
  const [searchParams, setSearchParams] = useSearchParams()
  const [instanceFilter, setInstanceFilter] = useState<InstanceFilter>('all')
  const [alertsOnly, setAlertsOnly] = useState(false)

  const clusters = clustersQuery.data ?? EMPTY_CLUSTERS
  const firingAlerts = alertsQuery.data ?? EMPTY_ALERTS
  const healthFilter = readHealthFilter(searchParams.get('health'))
  const textFilter = searchParams.get('q') ?? ''
  const normalizedTextFilter = textFilter.trim().toLocaleLowerCase('en-US')
  const alertCounts = useMemo(() => {
    const counts = new Map<string, number>()
    for (const alert of firingAlerts) {
      if (alert.cluster_id) counts.set(alert.cluster_id, (counts.get(alert.cluster_id) ?? 0) + 1)
    }
    return counts
  }, [firingAlerts])

  const summary = useMemo(
    () => summarize(clusters, firingAlerts.length),
    [clusters, firingAlerts.length],
  )
  const agentHealthIssues = deriveAgentHealth(clusters, firingAlerts)
  const staleAgeByCluster = new Map<string, number>()
  for (const issue of agentHealthIssues) {
    const staleAge = Math.max(issue.ageSeconds, AGENT_STALE_AFTER_SECONDS)
    staleAgeByCluster.set(
      issue.cluster.cluster_id,
      Math.max(staleAgeByCluster.get(issue.cluster.cluster_id) ?? 0, staleAge),
    )
  }

  const visibleClusters = useMemo(
    () =>
      [...clusters]
        .filter((cluster) => healthFilter === 'all' || cluster.health === healthFilter)
        .filter((cluster) => {
          if (!normalizedTextFilter) return true
          return (
            cluster.name?.toLocaleLowerCase('en-US').includes(normalizedTextFilter) === true ||
            cluster.instances.some((instance) =>
              instance.addr.toLocaleLowerCase('en-US').includes(normalizedTextFilter),
            )
          )
        })
        .filter((cluster) => {
          if (instanceFilter === 'all') return true
          return cluster.instances.some((instance) =>
            instanceFilter === 'up' ? instance.up : !instance.up,
          )
        })
        .filter((cluster) => {
          if (!alertsOnly) return true
          return firingAlerts.some((alert) => alertMatchesCluster(alert, cluster))
        })
        .sort(
          (left, right) =>
            HEALTH_RANK[left.health] - HEALTH_RANK[right.health] ||
            (left.name ?? left.cluster_id).localeCompare(right.name ?? right.cluster_id),
        ),
    [alertsOnly, clusters, firingAlerts, healthFilter, instanceFilter, normalizedTextFilter],
  )

  function updateSearchParam(key: 'q' | 'health', value: string | null) {
    const next = new URLSearchParams(searchParams)
    if (value) next.set(key, value)
    else next.delete(key)
    setSearchParams(next)
  }

  if (clustersQuery.error) {
    return (
      <ErrorState
        endpoint="clusters"
        failure={clustersQuery.error}
        onRetry={() => void clustersQuery.refetch()}
      />
    )
  }

  if (alertsQuery.error) {
    return (
      <ErrorState
        endpoint="alerts"
        failure={alertsQuery.error}
        onRetry={() => void alertsQuery.refetch()}
      />
    )
  }

  if (clustersQuery.isPending || alertsQuery.isPending) {
    return (
      <section aria-busy="true" aria-label="Loading fleet overview" role="status">
        Loading fleet overview…
      </section>
    )
  }

  return (
    <div className="space-y-8">
      <PageHeader
        title="Fleet overview"
        subtitle="Every monitored cluster, its instances, and the problems that need attention."
        freshness={<FreshnessBadge dataUpdatedAt={clustersQuery.dataUpdatedAt} policy="fleet" />}
      />

      <AgentHealthStrip issues={agentHealthIssues} />

      <FleetSummaryBar
        alertsOnly={alertsOnly}
        healthFilter={healthFilter}
        instanceFilter={instanceFilter}
        onAlertsOnlyChange={setAlertsOnly}
        onHealthFilterChange={(filter) =>
          updateSearchParam('health', filter === 'all' ? null : filter)
        }
        onInstanceFilterChange={setInstanceFilter}
        summary={summary}
      />

      <div className="max-w-xl">
        <label className="text-sm font-medium" htmlFor="fleet-filter">
          Filter clusters
        </label>
        <Input
          aria-describedby="fleet-filter-help"
          data-primary-filter
          id="fleet-filter"
          onChange={(event) => updateSearchParam('q', event.target.value)}
          placeholder="Search cluster name or instance address"
          type="search"
          value={textFilter}
        />
        <p className="text-muted-foreground mt-1 text-xs" id="fleet-filter-help">
          Search by cluster name or instance address.
        </p>
      </div>

      <Section
        title="Clusters"
        description={`${visibleClusters.length} of ${clusters.length} clusters shown`}
      >
        {visibleClusters.length > 0 ? (
          <div
            aria-label="Cluster cards"
            className="grid gap-4 md:grid-cols-2 xl:grid-cols-3"
            role="list"
          >
            {visibleClusters.map((cluster) => {
              const agentStaleAgeSeconds = staleAgeByCluster.get(cluster.cluster_id)
              return (
                <ClusterCard
                  {...(agentStaleAgeSeconds === undefined ? {} : { agentStaleAgeSeconds })}
                  key={cluster.cluster_id}
                  alertCount={
                    alertCounts.get(cluster.cluster_id) ??
                    firingAlerts.filter((alert) => alertMatchesCluster(alert, cluster)).length
                  }
                  cluster={cluster}
                />
              )
            })}
          </div>
        ) : (
          <EmptyState
            description="Try clearing the search or changing the active fleet filters."
            title="No clusters match these filters"
          />
        )}
      </Section>

      <small>Build {__PGLENS_BUILD__}</small>
    </div>
  )
}
