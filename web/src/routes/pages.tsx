import { FleetPage } from '@/features/fleet/FleetPage'
import { ClusterPage } from '@/features/cluster/ClusterPage'
import AshPageComponent from '@/features/ash/AshPage'
import InstancePage from '@/features/instance/InstancePage'
import QueryListPageComponent from '@/features/queries/QueryListPage'

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
  return <PlaceholderPage surface="Query detail and plan history" />
}

export function LocksPage() {
  return <PlaceholderPage surface="Locks and activity" />
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
