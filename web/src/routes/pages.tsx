import { FleetPage } from '@/features/fleet/FleetPage'
import { ClusterPage } from '@/features/cluster/ClusterPage'

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
  return <PlaceholderPage surface="Instance detail" />
}

export function AshPage() {
  return <PlaceholderPage surface="ASH and wait analysis" />
}

export function QueryInspectorPage() {
  return <PlaceholderPage surface="Query inspector" />
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
