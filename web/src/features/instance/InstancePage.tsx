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
import { INSTANCE_VIEW_PARAM } from './instanceViews'
import { ReadinessSection } from '@/features/readiness/ReadinessSection'

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
        // A view belongs to the section that wrote it.
        if (next !== section) params.delete(INSTANCE_VIEW_PARAM)
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
    // The database selector scopes the Overview only, so it lives in that tab (15.3).
    overview: () => {
      if (databasesQuery.error && !databasesQuery.data) {
        return (
          <ErrorState
            endpoint="instance databases"
            failure={databasesQuery.error}
            onRetry={() => void databasesQuery.refetch()}
          />
        )
      }
      if (databasesQuery.isPending || databasesQuery.data === undefined) {
        return (
          <section aria-busy="true" aria-label="Loading instance databases" role="status">
            Loading instance databases…
          </section>
        )
      }
      const selected = selectedDatabase(databasesQuery.data.databases, searchParams.get('db'))
      return (
        <div className="space-y-8 min-[1600px]:space-y-4">
          <DatabaseSelector
            databases={databasesQuery.data.databases}
            notMonitoredCount={databasesQuery.data.not_monitored_count}
          />
          {selected ? (
            <OverviewSection database={selected.datname} instanceId={instanceId} />
          ) : null}
        </div>
      )
    },
    host: () => <HostSection instanceId={instanceId} />,
    settings: () => <SettingsSection instanceId={instanceId} />,
    relations: () => <RelationsSection currentTier={instance.perm_tier} instanceId={instanceId} />,
    catalog: () => <CatalogSection instanceId={instanceId} />,
    logical: () => <LogicalReplicationSection instanceId={instanceId} />,
    internals: () => <InternalsSection instanceId={instanceId} />,
    readiness: () => <ReadinessSection instanceId={instanceId} />,
  }

  return (
    // One section at a time (15.2): only the active tab is mounted and fetched.
    // The tab list sits in the header, beside the metrics from 1600 px (15.3).
    <Tabs onValueChange={(value) => showSection(readInstanceSection(value))} value={section}>
      <InstanceHeader
        dataUpdatedAt={instanceQuery.dataUpdatedAt}
        instance={instanceQuery.data}
        replayLag={replayLagOf(replicationQuery, instanceId)}
        sectionTabs={
          <TabsList
            aria-label="Instance sections"
            className="h-auto flex-wrap min-[1600px]:flex-nowrap min-[1600px]:max-[1919px]:[&_[role=tab]]:px-1.5 min-[1600px]:max-[1919px]:[&_[role=tab]]:text-[13px]"
            ref={tabList}
          >
            {INSTANCE_SECTIONS.map((entry) => (
              <TabsTrigger key={entry.id} value={entry.id}>
                {entry.label}
              </TabsTrigger>
            ))}
          </TabsList>
        }
      />
      {INSTANCE_SECTIONS.map((entry) => (
        <TabsContent key={entry.id} value={entry.id}>
          {content[entry.id]()}
        </TabsContent>
      ))}
    </Tabs>
  )
}

export default InstancePage
