import { useAlerts, useAdvisorRules, useClusters, useInstances } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { EmptyState, ErrorState } from '@/components/state'

import { CommandAudit } from './CommandAudit'
import { InventoryTables } from './InventoryTables'
import { ServerInfo } from './ServerInfo'

export function SettingsPage() {
  const instancesQuery = useInstances()
  const clustersQuery = useClusters()
  const alertsQuery = useAlerts({ state: 'firing' })
  const rulesQuery = useAdvisorRules()

  if (instancesQuery.error) {
    return (
      <ErrorState
        endpoint="instances"
        failure={instancesQuery.error}
        onRetry={() => void instancesQuery.refetch()}
      />
    )
  }

  if (
    instancesQuery.isPending ||
    clustersQuery.isPending ||
    alertsQuery.isPending ||
    rulesQuery.isPending
  ) {
    return (
      <section aria-busy="true" aria-label="Loading settings and inventory" role="status">
        Loading settings and inventory…
      </section>
    )
  }

  const instances = instancesQuery.data ?? []
  const clusters = clustersQuery.data ?? []
  const alerts = alertsQuery.data ?? []
  const rules = rulesQuery.data ?? []

  return (
    <div className="space-y-8">
      <PageHeader
        freshness={<FreshnessBadge dataUpdatedAt={instancesQuery.dataUpdatedAt} policy="fleet" />}
        subtitle="Inventory, permission capabilities, and agent telemetry for the monitored fleet."
        title="Settings and inventory"
      />
      {clustersQuery.error || alertsQuery.error || rulesQuery.error ? (
        <div className="border-warning/40 bg-warning/10 text-sm" role="status">
          <strong>Some inventory details are unavailable.</strong>
          <p>
            The instance list is current, but cluster links, firing alerts, or advisor rules may be
            incomplete. Retry the affected surface from its owning page.
          </p>
        </div>
      ) : null}
      {instances.length === 0 ? (
        <EmptyState
          description="Install and configure the pglens agent on a PostgreSQL host to populate the fleet inventory."
          title="The fleet is empty"
        />
      ) : null}
      <InventoryTables alerts={alerts} clusters={clusters} instances={instances} rules={rules} />
      <CommandAudit instances={instances} />
      <ServerInfo />
    </div>
  )
}

export default SettingsPage
