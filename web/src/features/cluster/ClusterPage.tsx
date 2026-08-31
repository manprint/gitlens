import { useEffect, useRef } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'

import {
  useClusterReplication,
  useClusterSettingsDrift,
  useClusterTopology,
  useClusters,
} from '@/api/queries'
import type { Cluster } from '@/api/types'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { EmptyState, ErrorState } from '@/components/state'
import { useTimeRange } from '@/hooks/useTimeRange'

import { DriftSection } from './DriftSection'
import { EventTimeline } from './EventTimeline'
import { LagSection } from './LagSection'
import { SlotsSection } from './SlotsSection'
import { TopologyGraph } from './TopologyGraph'

interface ClusterPageProps {
  /** Optional override keeps the page easy to exercise without the lazy route. */
  clusterId?: string
}

const EMPTY_CLUSTER_INSTANCES: Cluster['instances'] = []

function ClusterNotFound({ clusterId }: { clusterId: string }) {
  return (
    <section aria-labelledby="cluster-not-found-title">
      <h1 id="cluster-not-found-title">Cluster not found</h1>
      <p>
        No monitored cluster matches <code>{clusterId}</code>.
      </p>
      <Link to="/">Back to fleet overview</Link>
    </section>
  )
}

function LoadingCluster() {
  return (
    <section aria-busy="true" aria-label="Loading cluster detail" role="status">
      Loading cluster detail…
    </section>
  )
}

export function ClusterPage({ clusterId: clusterIdOverride }: ClusterPageProps = {}) {
  const { clusterId: routeClusterId } = useParams<{ clusterId: string }>()
  const clusterId = clusterIdOverride ?? routeClusterId ?? ''
  const location = useLocation()
  const navigate = useNavigate()
  const { range } = useTimeRange()
  const clustersQuery = useClusters()
  const topologyQuery = useClusterTopology(clusterId)
  const replicationQuery = useClusterReplication(clusterId, {
    from: range.from.toISOString(),
    to: range.to.toISOString(),
  })
  const driftQuery = useClusterSettingsDrift(clusterId)
  const redirectedForUnauthorized = useRef(false)

  const cluster = clustersQuery.data?.find((candidate) => candidate.cluster_id === clusterId)
  const topology = topologyQuery.data?.topology ?? []
  const events = topologyQuery.data?.events ?? []
  const unauthorized =
    clustersQuery.error?.kind === 'unauthorized' ||
    topologyQuery.error?.kind === 'unauthorized' ||
    replicationQuery.error?.kind === 'unauthorized' ||
    driftQuery.error?.kind === 'unauthorized'

  useEffect(() => {
    if (!unauthorized || redirectedForUnauthorized.current) return
    redirectedForUnauthorized.current = true
    const next = `${location.pathname}${location.search}`
    void navigate(`/login?next=${encodeURIComponent(next)}`, { replace: true })
  }, [location.pathname, location.search, navigate, unauthorized])

  const clusterNotFound =
    topologyQuery.error?.kind === 'not_found' ||
    (clustersQuery.isSuccess && topologyQuery.isSuccess && cluster === undefined)

  if (clusterNotFound) return <ClusterNotFound clusterId={clusterId} />

  if (clustersQuery.error && !clustersQuery.data) {
    return (
      <ErrorState
        endpoint="clusters"
        failure={clustersQuery.error}
        onRetry={() => void clustersQuery.refetch()}
      />
    )
  }

  if (topologyQuery.error && !topologyQuery.data) {
    return (
      <ErrorState
        endpoint="cluster topology"
        failure={topologyQuery.error}
        onRetry={() => void topologyQuery.refetch()}
      />
    )
  }

  if (clustersQuery.isPending || topologyQuery.isPending || cluster === undefined) {
    return <LoadingCluster />
  }

  return (
    <div className="space-y-8">
      <PageHeader
        title={cluster.name ?? `Cluster ${cluster.cluster_id}`}
        subtitle={`${cluster.instance_count} monitored instance${cluster.instance_count === 1 ? '' : 's'} · ${cluster.health}`}
        freshness={<FreshnessBadge dataUpdatedAt={topologyQuery.dataUpdatedAt} policy="cluster" />}
      />

      {topologyQuery.error ? (
        <div role="status" className="border-warning/40 bg-warning/10 text-sm">
          Cluster topology is showing the last successful snapshot while the latest refresh failed.
        </div>
      ) : null}

      <TopologyGraph instances={cluster.instances ?? EMPTY_CLUSTER_INSTANCES} topology={topology} />
      {topology.length === 0 ? (
        <p role="status">No replication observed for this cluster.</p>
      ) : null}

      <section aria-labelledby="cluster-lag-heading">
        <h2 id="cluster-lag-heading" className="sr-only">
          Replication lag charts
        </h2>
        {replicationQuery.error ? (
          <ErrorState
            endpoint="replication lag charts"
            failure={replicationQuery.error}
            onRetry={() => void replicationQuery.refetch()}
          />
        ) : (
          <LagSection edges={replicationQuery.data?.edges ?? []} />
        )}
      </section>

      <SlotsSection />

      <DriftSection
        entries={driftQuery.data ?? []}
        onRetry={() => void driftQuery.refetch()}
        {...(driftQuery.error ? { failure: driftQuery.error } : {})}
      />

      <EventTimeline events={events} />

      {driftQuery.isPending ? (
        <EmptyState
          title="Loading configuration drift"
          description="Comparing observed settings across the cluster instances."
        />
      ) : null}

      <p className="text-muted-foreground text-xs">
        Cluster identity: <code>{cluster.cluster_id}</code>
      </p>
    </div>
  )
}

export default ClusterPage
