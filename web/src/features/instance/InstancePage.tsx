import { Link, useParams, useSearchParams } from 'react-router-dom'

import { useClusters, useInstance, useInstanceDatabases } from '@/api/queries'
import { ErrorState } from '@/components/state'
import { selectedDatabase } from '@/lib/databases'

import { InstanceHeader } from './InstanceHeader'
import { DatabaseSelector } from './DatabaseSelector'
import { HostSection } from './HostSection'
import { OverviewSection } from './OverviewSection'

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
  const clustersQuery = useClusters()
  const databasesQuery = useInstanceDatabases(instanceId)

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

  const cluster = clustersQuery.data?.find(
    (candidate) => candidate.cluster_id === instanceQuery.data.cluster_id,
  )

  return (
    <div className="space-y-8">
      <InstanceHeader
        dataUpdatedAt={instanceQuery.dataUpdatedAt}
        instance={instanceQuery.data}
        replayLagSeconds={cluster?.max_replay_lag_seconds}
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
    </div>
  )
}

export default InstancePage
