import { LayoutDashboard, Search } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'

import { useAlerts, useClusters, useReadinessSummary } from '@/api/queries'
import type { Cluster, Schemas } from '@/api/types'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { HelpLink } from '@/components/layout/HelpLink'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { AlertListTruncated, EmptyState, ErrorState } from '@/components/state'
import { Input } from '@/components/ui/input'
import { useT } from '@/i18n/react'
import { AGENT_STALE_AFTER_SECONDS, deriveAgentHealth } from '@/lib/fleet'
import { clusterLabel } from '@/lib/names'
import { clusterReadiness } from '@/lib/readiness'

import { AgentHealthStrip } from './AgentHealthStrip'
import { ClusterCard } from './ClusterCard'
import {
  FleetSummaryBar,
  type FleetSummary,
  type HealthFilter,
  type InstanceFilter,
} from './FleetSummaryBar'
import { Button } from '@/components/ui/button'
import { Callout } from '@/components/ui/callout'

const HEALTH_FILTERS: readonly HealthFilter[] = ['all', 'ok', 'degraded', 'critical']
const HEALTH_RANK: Record<Cluster['health'], number> = { critical: 0, degraded: 1, ok: 2 }
const EMPTY_CLUSTERS: Cluster[] = []
const EMPTY_ALERTS: Schemas['Alert'][] = []

function readHealthFilter(value: string | null): HealthFilter {
  return HEALTH_FILTERS.includes(value as HealthFilter) ? (value as HealthFilter) : 'all'
}

/**
 * An alert belongs to a cluster by its cluster_id, else by its instance. The
 * server never sets a `cluster` label (its samples carry `standby` or
 * `slot_name` only), and a cluster name is the operator's choice (D14), so a
 * name never decides it.
 */
function alertMatchesCluster(
  alert: { cluster_id?: string | null; instance_id?: string | null },
  cluster: Cluster,
): boolean {
  if (alert.cluster_id) return alert.cluster_id === cluster.cluster_id
  return (
    typeof alert.instance_id === 'string' &&
    cluster.instances.some((instance) => instance.instance_id === alert.instance_id)
  )
}

function summarize(clusters: readonly Cluster[], firingAlerts: number | null): FleetSummary {
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
  const t = useT()
  const clustersQuery = useClusters()
  const alertsQuery = useAlerts({ state: 'firing' })
  // One request for every card; a failed or pending summary shows no badge.
  const readinessSummary = useReadinessSummary().data?.instances
  const location = useLocation()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [instanceFilter, setInstanceFilter] = useState<InstanceFilter>('all')
  const [alertsOnly, setAlertsOnly] = useState(false)
  const redirectedForUnauthorized = useRef(false)

  const clusters = clustersQuery.data ?? EMPTY_CLUSTERS
  const firingAlerts = alertsQuery.data ?? EMPTY_ALERTS
  const alertsAvailable = alertsQuery.error === null && !alertsQuery.isPending
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
    () => summarize(clusters, alertsAvailable ? firingAlerts.length : null),
    [alertsAvailable, clusters, firingAlerts.length],
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
            cluster.instances.some(
              (instance) =>
                instance.addr.toLocaleLowerCase('en-US').includes(normalizedTextFilter) ||
                instance.display_name?.toLocaleLowerCase('en-US').includes(normalizedTextFilter) ===
                  true,
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
          return (
            alertsAvailable && firingAlerts.some((alert) => alertMatchesCluster(alert, cluster))
          )
        })
        .sort(
          (left, right) =>
            HEALTH_RANK[left.health] - HEALTH_RANK[right.health] ||
            clusterLabel(left, left.cluster_id).localeCompare(
              clusterLabel(right, right.cluster_id),
            ),
        ),
    [
      alertsOnly,
      alertsAvailable,
      clusters,
      firingAlerts,
      healthFilter,
      instanceFilter,
      normalizedTextFilter,
    ],
  )

  const unauthorized =
    clustersQuery.error?.kind === 'unauthorized' || alertsQuery.error?.kind === 'unauthorized'

  useEffect(() => {
    if (!unauthorized || redirectedForUnauthorized.current) return
    redirectedForUnauthorized.current = true
    const next = `${location.pathname}${location.search}`
    void navigate(`/login?next=${encodeURIComponent(next)}`, { replace: true })
  }, [location.pathname, location.search, navigate, unauthorized])

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

  if (clustersQuery.isPending || alertsQuery.isPending) {
    return (
      <section aria-busy="true" aria-label={t('fleet.page.loadingLabel')} role="status">
        {t('fleet.page.loading')}
      </section>
    )
  }

  return (
    <div className="space-y-8 min-[1600px]:space-y-4">
      <PageHeader
        help={<HelpLink anchor="fleet" label={t('fleet.page.helpLabel')} />}
        icon={LayoutDashboard}
        eyebrow={t('fleet.page.eyebrow')}
        title={t('fleet.page.title')}
        subtitle={t('fleet.page.subtitle')}
        freshness={<FreshnessBadge dataUpdatedAt={clustersQuery.dataUpdatedAt} policy="fleet" />}
      />

      {alertsQuery.error ? (
        <Callout variant="warning">
          <strong>{t('fleet.page.alertsUnavailable')}</strong>
          <p>{t('fleet.page.alertsUnavailableBody')}</p>
          <Button
            size="sm"
            variant="outline"
            onClick={() => void alertsQuery.refetch()}
            type="button"
          >
            {t('fleet.page.retryAlerts')}
          </Button>
        </Callout>
      ) : null}

      <AlertListTruncated truncated={alertsQuery.truncated} />
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

      <div className="max-w-xl space-y-1.5">
        <label className="text-sm font-medium" htmlFor="fleet-filter">
          {t('fleet.page.filterLabel')}
        </label>
        <div className="relative">
          <Search
            aria-hidden="true"
            className="text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2"
          />
          <Input
            aria-describedby="fleet-filter-help"
            className="bg-card h-10 pl-9"
            data-primary-filter
            id="fleet-filter"
            onChange={(event) => updateSearchParam('q', event.target.value)}
            placeholder={t('fleet.page.filterPlaceholder')}
            type="search"
            value={textFilter}
          />
        </div>
        <p className="text-muted-foreground text-xs" id="fleet-filter-help">
          {t('fleet.page.filterHelpBefore')}
          <kbd>/</kbd>
          {t('fleet.page.filterHelpAfter')}
        </p>
      </div>

      <Section
        title={t('fleet.page.clustersTitle')}
        captionId="fleet.clusters"
        actions={
          <>
            <span className="text-muted-foreground text-sm">
              {t('fleet.page.shown', { shown: visibleClusters.length, total: clusters.length })}
            </span>
            <HelpLink anchor="fleet#cluster-cards" label={t('fleet.page.clusterCards')} />
          </>
        }
      >
        {visibleClusters.length > 0 ? (
          <div
            aria-label={t('fleet.page.clusterCards')}
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
                    // react-query keeps the last good alerts after a failed refetch, so the
                    // counts are gated on the current query state, not on whether data exists.
                    alertsAvailable
                      ? (alertCounts.get(cluster.cluster_id) ??
                        firingAlerts.filter((alert) => alertMatchesCluster(alert, cluster)).length)
                      : null
                  }
                  cluster={cluster}
                  readiness={clusterReadiness(
                    cluster.instances.map((instance) => instance.instance_id),
                    readinessSummary,
                  )}
                />
              )
            })}
          </div>
        ) : clusters.length === 0 ? (
          <EmptyState
            description={t('fleet.page.noClustersDescription')}
            title={t('fleet.page.noClustersTitle')}
          />
        ) : (
          <EmptyState
            description={t('fleet.page.noMatchDescription')}
            title={t('fleet.page.noMatchTitle')}
          />
        )}
      </Section>

      <small className="text-muted-foreground block text-xs">
        {t('fleet.page.build')} {__PGLENS_BUILD__}
      </small>
    </div>
  )
}
