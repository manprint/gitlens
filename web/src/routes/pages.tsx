import { FleetPage } from '@/features/fleet/FleetPage'
import { ClusterPage } from '@/features/cluster/ClusterPage'
import AshPageComponent from '@/features/ash/AshPage'
import InstancePage from '@/features/instance/InstancePage'
import { useInstance } from '@/api/queries'
import { ErrorState } from '@/components/state'
import { isMonitoredDatabase } from '@/lib/databases'
import ExplainPanel from '@/features/queries/ExplainPanel'
import PlanHistory from '@/features/queries/PlanHistory'
import { QueryOverview } from '@/features/queries/QueryOverview'
import QueryListPageComponent from '@/features/queries/QueryListPage'
import SlowQueriesPageComponent from '@/features/queries/SlowQueriesPage'
import LocksPageComponent from '@/features/locks/BlockingTree'
import { FindingsPage as FindingsPageComponent } from '@/features/findings/FindingsPage'
import { AlertsPage as AlertsPageComponent } from '@/features/alerts/AlertsPage'
import { EventsPage as EventsPageComponent } from '@/features/alerts/EventsPage'
import { RulesPage as RulesPageComponent } from '@/features/alerts/RulesPage'
import { SilencesPage as SilencesPageComponent } from '@/features/alerts/SilencesPage'
import { SettingsPage as SettingsPageComponent } from '@/features/settings/SettingsPage'
import { useParams, useSearchParams } from 'react-router-dom'

interface PlaceholderPageProps {
  surface: string
}

function PlaceholderPage({ surface }: PlaceholderPageProps) {
  return (
    <section aria-labelledby="page-title" className="space-y-3">
      <h1 className="text-foreground text-2xl font-semibold tracking-tight" id="page-title">
        {surface}
      </h1>
      <p className="text-muted-foreground">{surface} is not implemented in this build.</p>
    </section>
  )
}

export function FleetOverviewPage() {
  return <FleetPage />
}

export function SlowQueriesPage() {
  return <SlowQueriesPageComponent />
}

export function ClusterDetailPage() {
  return <ClusterPage />
}

export function InstanceDetailPage() {
  return <InstancePage />
}

export function AshPage() {
  return <AshPageComponent />
}

export function QueryInspectorPage() {
  return <QueryListPageComponent />
}

export function QueryDetailPage() {
  const { instanceId, queryid } = useParams<{ instanceId: string; queryid: string }>()
  const [searchParams] = useSearchParams()
  const instanceQuery = useInstance(instanceId ?? '')
  // Set by the list link to the statement's database. Without it EXPLAIN runs
  // in the agent's own DSN database and plan history lists every database.
  const requestedDatabase = searchParams.get('db')

  if (!instanceId || !queryid) {
    return <PlaceholderPage surface="Query detail and plan history" />
  }

  // The URL is editable, so the database is checked against the instance's
  // monitored list before any command or plan request names it.
  if (requestedDatabase !== null) {
    if (instanceQuery.data === undefined) {
      return instanceQuery.error ? (
        <ErrorState
          endpoint="instance"
          failure={instanceQuery.error}
          onRetry={() => void instanceQuery.refetch()}
        />
      ) : (
        <section aria-busy="true" aria-label="Loading instance" role="status">
          Loading instance…
        </section>
      )
    }
    if (!isMonitoredDatabase(instanceQuery.data.databases, requestedDatabase)) {
      return (
        <section
          role="alert"
          className="border-destructive/40 bg-destructive/10 rounded-xl border text-sm"
        >
          <strong>Database not monitored.</strong> {requestedDatabase} is not monitored on this
          instance, so this statement is not explained or planned here.
        </section>
      )
    }
  }

  const datname = requestedDatabase ?? undefined
  return (
    <div className="space-y-6">
      <QueryOverview
        {...(datname === undefined ? {} : { datname })}
        instanceId={instanceId}
        queryid={queryid}
      />
      <ExplainPanel
        currentTier={instanceQuery.data?.perm_tier ?? 'T0'}
        {...(datname === undefined ? {} : { datname })}
        instanceId={instanceId}
        queryid={queryid}
      />
      <PlanHistory
        {...(datname === undefined ? {} : { datname })}
        instanceId={instanceId}
        queryid={queryid}
      />
    </div>
  )
}

export function LocksPage() {
  return <LocksPageComponent />
}

export function FindingsPage() {
  return <FindingsPageComponent />
}

export function AlertsPage() {
  return <AlertsPageComponent />
}

export function EventsPage() {
  return <EventsPageComponent />
}

export function RulesPage() {
  return <RulesPageComponent />
}

export function SilencesPage() {
  return <SilencesPageComponent />
}

export function SettingsPage() {
  return <SettingsPageComponent />
}
