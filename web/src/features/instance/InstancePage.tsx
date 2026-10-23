import { Link, useParams, useSearchParams } from 'react-router-dom'

import { useClusterReplication, useInstance, useInstanceDatabases } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { ErrorState } from '@/components/state'
import { selectedDatabase } from '@/lib/databases'

import { InstanceHeader, type ReplayLag } from './InstanceHeader'
import { CatalogSection } from './CatalogSection'
import { DatabaseSelector } from './DatabaseSelector'
import { HostSection } from './HostSection'
import { InternalsSection } from './InternalsSection'
import { LogicalReplicationSection } from './LogicalReplicationSection'
import { OverviewSection } from './OverviewSection'
import { RelationsSection } from './RelationsSection'
import { SettingsSection } from './SettingsSection'

// The header shows the newest replay-lag reading, so a short window holds it.
const REPLAY_LAG_WINDOW: { durationMs: number } = { durationMs: 5 * 60 * 1_000 }
// The server bounds a cluster's replay lag to the last 60 seconds, so an older
// reading is a blip that has aged out, not the lag now.
const REPLAY_LAG_FRESH_MS = 60 * 1_000

// A standby's own lag is the replay series of the edge it reports from. Only
// the newest point counts: a null newest point is not measured, and an older
// reading is not a measurement now. The cluster maximum is never used here,
// because it can belong to another standby.
function ownReplayLag(
  edges: readonly Schemas['ReplicationEdge'][],
  instanceId: string,
  now: number,
): number | null {
  let newest: Schemas['SeriesPoint'] | undefined
  for (const edge of edges) {
    if (edge.from !== instanceId || edge.metric !== 'replay_lag_sec') continue
    for (const point of edge.series) {
      if (newest === undefined || Date.parse(point.ts) > Date.parse(newest.ts)) newest = point
    }
  }
  if (newest === undefined || newest.value === null) return null
  return now - Date.parse(newest.ts) > REPLAY_LAG_FRESH_MS ? null : newest.value
}

function replayLagOf(
  query: ReturnType<typeof useClusterReplication>,
  instanceId: string,
): ReplayLag {
  if (query.data) {
    return { state: 'reading', seconds: ownReplayLag(query.data.edges, instanceId, Date.now()) }
  }
  if (query.error) return { state: 'unavailable' }
  return { state: 'loading' }
}

interface InstancePageProps {
  /** Optional override keeps the page easy to exercise without the lazy route. */
  instanceId?: string
}

function LoadingInstance() {
  return (
    <section aria-busy="true" aria-label="Loading instance detail" role="status">
      Loading instance detail…
    </section>
  )
}

function InstanceNotFound({ instanceId }: { instanceId: string }) {
  return (
    <section aria-labelledby="instance-not-found-title">
      <h1 id="instance-not-found-title">Instance not found</h1>
      <p>
        No monitored instance matches <code>{instanceId}</code>.
      </p>
      <Link to="/">Back to fleet overview</Link>
    </section>
  )
}

export function InstancePage({ instanceId: instanceIdOverride }: InstancePageProps = {}) {
  const { instanceId: routeInstanceId } = useParams<{ instanceId: string }>()
  const [searchParams] = useSearchParams()
  const instanceId = instanceIdOverride ?? routeInstanceId ?? ''
  const instanceQuery = useInstance(instanceId)
  const databasesQuery = useInstanceDatabases(instanceId)
  const replicationQuery = useClusterReplication(
    instanceQuery.data?.cluster_id ?? '',
    REPLAY_LAG_WINDOW,
    { enabled: instanceQuery.data?.role === 'standby' },
  )

  if (instanceQuery.error?.kind === 'not_found') {
    return <InstanceNotFound instanceId={instanceId} />
  }

  if (instanceQuery.error && !instanceQuery.data) {
    return (
      <ErrorState
        endpoint="instance"
        failure={instanceQuery.error}
        onRetry={() => void instanceQuery.refetch()}
      />
    )
  }

  if (instanceQuery.isPending || instanceQuery.data === undefined) {
    return <LoadingInstance />
  }

  return (
    <div className="space-y-8">
      <InstanceHeader
        dataUpdatedAt={instanceQuery.dataUpdatedAt}
        instance={instanceQuery.data}
        replayLag={replayLagOf(replicationQuery, instanceId)}
      />
      {databasesQuery.error && !databasesQuery.data ? (
        <ErrorState
          endpoint="instance databases"
          failure={databasesQuery.error}
          onRetry={() => void databasesQuery.refetch()}
        />
      ) : databasesQuery.isPending || databasesQuery.data === undefined ? (
        <section aria-busy="true" aria-label="Loading instance databases" role="status">
          Loading instance databases…
        </section>
      ) : (
        <>
          <DatabaseSelector
            databases={databasesQuery.data.databases}
            notMonitoredCount={databasesQuery.data.not_monitored_count}
          />
          {(() => {
            const selected = selectedDatabase(databasesQuery.data.databases, searchParams.get('db'))
            return selected ? (
              <OverviewSection database={selected.datname} instanceId={instanceId} />
            ) : null
          })()}
        </>
      )}
      <HostSection instanceId={instanceId} />
      <SettingsSection instanceId={instanceId} />
      <RelationsSection instanceId={instanceId} currentTier={instanceQuery.data.perm_tier} />
      <CatalogSection instanceId={instanceId} />
      <LogicalReplicationSection instanceId={instanceId} />
      <InternalsSection instanceId={instanceId} />
    </div>
  )
}

export default InstancePage
