import { useEffect, useRef, type ReactNode } from 'react'
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom'

import { useClusterReplication, useInstance, useInstanceDatabases } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { ErrorState } from '@/components/state'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { selectedDatabase } from '@/lib/databases'

import { InstanceHeader, type ReplayLag } from './InstanceHeader'
import { CatalogSection } from './CatalogSection'
import { DatabaseSelector } from './DatabaseSelector'
import { HostSection } from './HostSection'
import { InternalsSection } from './InternalsSection'
import { LogicalReplicationSection } from './LogicalReplicationSection'
import { OverviewSection } from './OverviewSection'
import { RelationsSection } from './RelationsSection'
import {
  DEFAULT_INSTANCE_SECTION,
  INSTANCE_SECTIONS,
  legacyHashToSection,
  readInstanceSection,
  type InstanceSectionId,
} from './instanceSections'
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
    <section aria-labelledby="instance-not-found-title" className="space-y-3">
      <h1
        className="text-foreground text-2xl font-semibold tracking-tight"
        id="instance-not-found-title"
      >
        Instance not found
      </h1>
      <p className="text-muted-foreground">
        No monitored instance matches <code>{instanceId}</code>.
      </p>
      <Link to="/">Back to fleet overview</Link>
    </section>
  )
}

export function InstancePage({ instanceId: instanceIdOverride }: InstancePageProps = {}) {
  const { instanceId: routeInstanceId } = useParams<{ instanceId: string }>()
  const [searchParams, setSearchParams] = useSearchParams()
  const location = useLocation()
  const { hash } = location
  const tabList = useRef<HTMLDivElement>(null)
  const section = readInstanceSection(searchParams.get('section'))
  const instanceId = instanceIdOverride ?? routeInstanceId ?? ''
  const instanceQuery = useInstance(instanceId)
  const databasesQuery = useInstanceDatabases(instanceId)
  const replicationQuery = useClusterReplication(
    instanceQuery.data?.cluster_id ?? '',
    REPLAY_LAG_WINDOW,
    { enabled: instanceQuery.data?.role === 'standby' },
  )

  function showSection(next: InstanceSectionId) {
    setSearchParams(
      (current) => {
        const params = new URLSearchParams(current)
        // The default section is never written to the URL (D12).
        if (next === DEFAULT_INSTANCE_SECTION) params.delete('section')
        else params.set('section', next)
        return params
      },
      { replace: true },
    )
  }

  // An old in-page anchor (`#catalog`, `#host`, …) opens its section's tab.
  const legacySection = legacyHashToSection(hash)
  useEffect(() => {
    if (legacySection !== null) showSection(legacySection)
    // showSection only reads the current URL; the hash is the trigger.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [legacySection])

  // A link that switched the tab (the Overview's Host link) unmounts itself: focus the tab.
  const focusSection = (location.state as { focusSection?: boolean } | null)?.focusSection
  useEffect(() => {
    if (!focusSection) return
    tabList.current?.querySelector<HTMLElement>('[role="tab"][data-state="active"]')?.focus()
  }, [focusSection, location.key])

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

  const instance = instanceQuery.data
  // Every registry section has its panel: a new section without one fails the type check.
  const content: Record<InstanceSectionId, () => ReactNode> = {
    overview: () => {
      if (!databasesQuery.data) return null
      const selected = selectedDatabase(databasesQuery.data.databases, searchParams.get('db'))
      return selected ? (
        <OverviewSection database={selected.datname} instanceId={instanceId} />
      ) : null
    },
    host: () => <HostSection instanceId={instanceId} />,
    settings: () => <SettingsSection instanceId={instanceId} />,
    relations: () => <RelationsSection currentTier={instance.perm_tier} instanceId={instanceId} />,
    catalog: () => <CatalogSection instanceId={instanceId} />,
    logical: () => <LogicalReplicationSection instanceId={instanceId} />,
    internals: () => <InternalsSection instanceId={instanceId} />,
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
        <DatabaseSelector
          databases={databasesQuery.data.databases}
          notMonitoredCount={databasesQuery.data.not_monitored_count}
        />
      )}
      {/* One section at a time (15.2): only the active tab is mounted and fetched. */}
      <Tabs onValueChange={(value) => showSection(readInstanceSection(value))} value={section}>
        <TabsList aria-label="Instance sections" className="h-auto flex-wrap" ref={tabList}>
          {INSTANCE_SECTIONS.map((entry) => (
            <TabsTrigger key={entry.id} value={entry.id}>
              {entry.label}
            </TabsTrigger>
          ))}
        </TabsList>
        {INSTANCE_SECTIONS.map((entry) => (
          <TabsContent key={entry.id} value={entry.id}>
            {content[entry.id]()}
          </TabsContent>
        ))}
      </Tabs>
    </div>
  )
}

export default InstancePage
