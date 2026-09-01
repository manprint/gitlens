import { FleetPage } from '@/features/fleet/FleetPage'
import { ClusterPage } from '@/features/cluster/ClusterPage'
import AshPageComponent from '@/features/ash/AshPage'
import InstancePage from '@/features/instance/InstancePage'
import { useInstance } from '@/api/queries'
import ExplainPanel from '@/features/queries/ExplainPanel'
import PlanHistory from '@/features/queries/PlanHistory'
import QueryListPageComponent from '@/features/queries/QueryListPage'
import LocksPageComponent from '@/features/locks/BlockingTree'
import { useParams } from 'react-router-dom'

interface PlaceholderPageProps {
  surface: string
}

function PlaceholderPage({ surface }: PlaceholderPageProps) {
  return (
    <section aria-labelledby="page-title">
      <h1 id="page-title">{surface}</h1>
      <p>{surface} is not implemented in this build.</p>
    </section>
  )
}

export function FleetOverviewPage() {
  return <FleetPage />
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
  const instanceQuery = useInstance(instanceId ?? '')

  if (!instanceId || !queryid) {
    return <PlaceholderPage surface="Query detail and plan history" />
  }

  return (
    <div className="space-y-8">
      <ExplainPanel
        currentTier={instanceQuery.data?.perm_tier ?? 'T0'}
        instanceId={instanceId}
        queryid={queryid}
      />
      <PlanHistory instanceId={instanceId} queryid={queryid} />
    </div>
  )
}

export function LocksPage() {
  return <LocksPageComponent />
}

export function FindingsPage() {
  return <PlaceholderPage surface="Advisor findings" />
}

export function AlertsPage() {
  return <PlaceholderPage surface="Alerts and events" />
}

export function SettingsPage() {
  return <PlaceholderPage surface="Settings and inventory" />
}
