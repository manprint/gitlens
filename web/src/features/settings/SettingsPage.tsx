import { Settings } from 'lucide-react'
import { useAlerts, useAdvisorRules, useClusters, useInstances } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { AlertListTruncated, EmptyState, ErrorState } from '@/components/state'

import { CommandAudit } from './CommandAudit'
import { InventoryTables } from './InventoryTables'
import { NamesPanel } from './NamesPanel'
import { ServerInfo } from './ServerInfo'
import { Callout } from '@/components/ui/callout'

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
    <div className="space-y-8 min-[1600px]:space-y-4">
      <PageHeader
        icon={Settings}
        eyebrow="System"
        freshness={<FreshnessBadge dataUpdatedAt={instancesQuery.dataUpdatedAt} policy="fleet" />}
        subtitle="What pglens monitors and with which permissions: server build, agents and their telemetry, the instance inventory, cluster and instance names, and an audit of the commands sent to the agents."
        title="Settings and inventory"
      />
      {clustersQuery.error || alertsQuery.error || rulesQuery.error ? (
        <Callout role="status" variant="warning">
          <strong>Some inventory details are unavailable.</strong>
          <p>
            The instance list is current, but cluster links, firing alerts, or advisor rules may be
            incomplete. Retry the affected surface from its owning page.
          </p>
        </Callout>
      ) : null}
      {instances.length === 0 ? (
        <EmptyState
          description="Install and configure the pglens agent on a PostgreSQL host to populate the fleet inventory."
          title="The fleet is empty"
        />
      ) : null}
      <AlertListTruncated truncated={alertsQuery.truncated} />
      <InventoryTables alerts={alerts} clusters={clusters} instances={instances} rules={rules} />
      {/* Names stays on this page until 11.1 gives it the `names` tab. */}
      <NamesPanel clusters={clusters} instances={instances} />
      <CommandAudit instances={instances} />
      <ServerInfo />
    </div>
  )
}

export default SettingsPage
