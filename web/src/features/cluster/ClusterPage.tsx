import { Network } from 'lucide-react'
import { useEffect, useMemo, useRef, type ReactNode } from 'react'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import {
  useClusterReplication,
  useClusterSettingsDrift,
  useClusterSlots,
  useClusterTopology,
  useClusters,
} from '@/api/queries'
import type { Cluster } from '@/api/types'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { EmptyState, ErrorState } from '@/components/state'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useTimeRange } from '@/hooks/useTimeRange'
import { clusterLabel } from '@/lib/names'
import { toSlotRows } from '@/lib/replication'
import { queryWindowOf } from '@/lib/timerange'

import {
  CLUSTER_SECTIONS,
  DEFAULT_CLUSTER_SECTION,
  readClusterSection,
  type ClusterSectionId,
} from './clusterSections'
import { DriftSection } from './DriftSection'
import { EventTimeline } from './EventTimeline'
import { LagSection } from './LagSection'
import { SlotsSection } from './SlotsSection'
import { TopologyGraph } from './TopologyGraph'
import { Callout } from '@/components/ui/callout'

interface ClusterPageProps {
  /** Optional override keeps the page easy to exercise without the lazy route. */
  clusterId?: string
}

const EMPTY_CLUSTER_INSTANCES: Cluster['instances'] = []

function ClusterNotFound({ clusterId }: { clusterId: string }) {
  return (
    <section aria-labelledby="cluster-not-found-title" className="space-y-3">
      <h1
        className="text-foreground text-2xl font-semibold tracking-tight"
        id="cluster-not-found-title"
      >
        Cluster not found
      </h1>
      <p className="text-muted-foreground">
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
  const replicationQuery = useClusterReplication(clusterId, queryWindowOf(range))
  const driftQuery = useClusterSettingsDrift(clusterId)
  const slotsQuery = useClusterSlots(clusterId)
  const redirectedForUnauthorized = useRef(false)
  const [searchParams, setSearchParams] = useSearchParams()
  const section = readClusterSection(searchParams.get('section'))

  function showSection(next: ClusterSectionId) {
    setSearchParams(
      (current) => {
        const params = new URLSearchParams(current)
        // The default tab is never written to the URL (D12).
        if (next === DEFAULT_CLUSTER_SECTION) params.delete('section')
        else params.set('section', next)
        return params
      },
      { replace: true },
    )
  }

  const cluster = clustersQuery.data?.find((candidate) => candidate.cluster_id === clusterId)
  const instances = cluster?.instances ?? EMPTY_CLUSTER_INSTANCES
  // Built from the fields, not the object: the memo below depends on the cluster's instances.
  const title = clusterLabel({ cluster_id: clusterId, name: cluster?.name ?? null })
  const slotsData = slotsQuery.data
  const slotsUpdatedAt = slotsQuery.dataUpdatedAt
  const slots = useMemo(
    () => (slotsData ? toSlotRows(slotsData.slots, instances, new Date(slotsUpdatedAt)) : []),
    [instances, slotsData, slotsUpdatedAt],
  )
  const topology = topologyQuery.data?.topology ?? []
  const events = topologyQuery.data?.events ?? []
  const unauthorized =
    clustersQuery.error?.kind === 'unauthorized' ||
    topologyQuery.error?.kind === 'unauthorized' ||
    replicationQuery.error?.kind === 'unauthorized' ||
    driftQuery.error?.kind === 'unauthorized' ||
    slotsQuery.error?.kind === 'unauthorized'

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

  // Every registry tab has its panel: a new tab without one fails the type check.
  const content: Record<ClusterSectionId, () => ReactNode> = {
    topology: () => (
      <>
        <TopologyGraph instances={instances} topology={topology} />
        {topology.length === 0 ? (
          <p role="status">No replication observed for this cluster.</p>
        ) : null}
      </>
    ),
    lag: () => (
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
        ) : replicationQuery.isPending ? (
          <EmptyState
            title="Loading replication lag"
            description="Reading the replication lag series for this range."
          />
        ) : (
          <LagSection edges={replicationQuery.data?.edges ?? []} instances={instances} />
        )}
      </section>
    ),
    // A pending query has no data yet, so its tab must not show the empty
    // state that a settled, empty response would show.
    slots: () =>
      slotsQuery.isPending ? (
        <EmptyState
          title="Loading replication slots"
          description="Reading the replication slots this cluster reports."
        />
      ) : (
        <SlotsSection
          slots={slots}
          onRetry={() => void slotsQuery.refetch()}
          {...(slotsQuery.error && !slotsQuery.data ? { failure: slotsQuery.error } : {})}
        />
      ),
    drift: () =>
      driftQuery.isPending ? (
        <EmptyState
          title="Loading configuration drift"
          description="Comparing observed settings across the cluster instances."
        />
      ) : (
        <DriftSection
          entries={driftQuery.data ?? []}
          instances={instances}
          onRetry={() => void driftQuery.refetch()}
          {...(driftQuery.error ? { failure: driftQuery.error } : {})}
        />
      ),
    events: () => <EventTimeline events={events} />,
  }

  return (
    <div className="space-y-8 min-[1600px]:space-y-4">
      <PageHeader
        icon={Network}
        eyebrow="Cluster"
        title={title}
        truncateTitle
        subtitle={`${cluster.instance_count} monitored instance${cluster.instance_count === 1 ? '' : 's'} · ${cluster.health}`}
        freshness={<FreshnessBadge dataUpdatedAt={topologyQuery.dataUpdatedAt} policy="cluster" />}
      />

      {topologyQuery.error ? (
        <Callout role="status" variant="warning">
          Cluster topology is showing the last successful snapshot while the latest refresh failed.
        </Callout>
      ) : null}

      {/* One view per tab (15.3); every query above stays in the page, as before. */}
      <Tabs onValueChange={(value) => showSection(readClusterSection(value))} value={section}>
        <TabsList aria-label="Cluster sections" className="h-auto flex-wrap">
          {CLUSTER_SECTIONS.map((entry) => (
            <TabsTrigger key={entry.id} value={entry.id}>
              {entry.label}
            </TabsTrigger>
          ))}
        </TabsList>
        <p className="text-muted-foreground text-xs">
          Cluster identity: <code>{cluster.cluster_id}</code>{' '}
          {cluster.id_source === 'system_identifier'
            ? '(the PostgreSQL system identifier, which pglens uses to tell clusters apart)'
            : cluster.id_source === 'manual'
              ? '(derived from a cluster name supplied by the user, not from the PostgreSQL system identifier)'
              : null}
        </p>
        {CLUSTER_SECTIONS.map((entry) => (
          <TabsContent className="space-y-6 min-[1600px]:space-y-4" key={entry.id} value={entry.id}>
            {content[entry.id]()}
          </TabsContent>
        ))}
      </Tabs>
    </div>
  )
}

export default ClusterPage
