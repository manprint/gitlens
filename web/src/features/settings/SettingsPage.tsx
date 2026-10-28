import { Settings } from 'lucide-react'
import { useAlerts, useAdvisorRules, useClusters, useInstances } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { AlertListTruncated, EmptyState, ErrorState } from '@/components/state'

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
        icon={Settings}
        eyebrow="System"
        freshness={<FreshnessBadge dataUpdatedAt={instancesQuery.dataUpdatedAt} policy="fleet" />}
        subtitle="What pglens monitors and with which permissions: server build, agents and their telemetry, the instance inventory, and an audit of the commands sent to the agents."
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
      <AlertListTruncated truncated={alertsQuery.truncated} />
      <InventoryTables alerts={alerts} clusters={clusters} instances={instances} rules={rules} />
      <CommandAudit instances={instances} />
      <ServerInfo />
    </div>
  )
}

export default SettingsPage
