import { useEffect, type ReactNode } from 'react'
import { Settings } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'

import { useAlerts, useAdvisorRules, useClusters, useInstances } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { AlertListTruncated, EmptyState, ErrorState } from '@/components/state'
import { Callout } from '@/components/ui/callout'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { InstanceViewTabs } from '@/features/instance/InstancePanel'
import { INSTANCE_VIEW_PARAM, useInstanceView } from '@/features/instance/instanceViews'

import { CommandAudit } from './CommandAudit'
import { InventoryTables } from './InventoryTables'
import { NamesPanel } from './NamesPanel'
import { ServerInfo } from './ServerInfo'
import {
  AUDIT_VIEWS,
  DEFAULT_SETTINGS_TAB,
  INVENTORY_VIEWS,
  readSettingsTab,
  SETTINGS_TAB_PARAM,
  SETTINGS_TABS,
  type SettingsTabId,
} from './settingsTabs'

/**
 * What the tabs that 11.2–11.5 fill will configure (I-16). Until then they
 * state it and offer no control.
 */
const PLANNED_TABS: Record<'preferences' | 'retention' | 'advisor' | 'notifications', string> = {
  preferences:
    'Console preferences kept in this browser: the theme, the default time range, the refresh interval and the table page size.',
  retention:
    'How many days of history each metrics family keeps, with a preview of what a shorter retention would delete before it is applied.',
  advisor:
    'Which advisor rules run, and the observation windows of the unused-index and backup-history checks.',
  notifications:
    'Whether each notification channel (Slack, webhook, Google Chat) is configured, and a test message per channel.',
}

function PlannedTab({ id, label }: { id: keyof typeof PLANNED_TABS; label: string }) {
  return (
    <section
      aria-labelledby={`settings-${id}-title`}
      className="bg-card shadow-card rounded-xl border px-5 py-4"
    >
      <h2 id={`settings-${id}-title`} className="text-foreground text-base font-semibold">
        {label}
      </h2>
      <p className="text-muted-foreground mt-1 text-sm leading-relaxed">{PLANNED_TABS[id]}</p>
      <p className="text-muted-foreground mt-1 text-sm leading-relaxed">
        Nothing can be changed here yet.
      </p>
    </section>
  )
}

/** The tabs with their own `?view=` views; the other tabs have none. */
const TAB_VIEWS: Partial<Record<SettingsTabId, readonly { id: string }[]>> = {
  inventory: INVENTORY_VIEWS,
  audit: AUDIT_VIEWS,
}

/** The Audit & server tab: one view each for the command audit and the server information. */
function AuditTab({ instances }: { instances: Schemas['InstanceSummary'][] }) {
  const [view = 'commands', showView] = useInstanceView(AUDIT_VIEWS)

  return (
    // Command audit is the default view and is never written to the URL.
    <Tabs className="gap-4" onValueChange={showView} value={view}>
      <InstanceViewTabs label="Audit views" views={AUDIT_VIEWS} />
      <TabsContent value="commands">
        <CommandAudit instances={instances} />
      </TabsContent>
      <TabsContent value="server">
        <ServerInfo />
      </TabsContent>
    </Tabs>
  )
}

export function SettingsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const tab = readSettingsTab(searchParams.get(SETTINGS_TAB_PARAM))
  const rawTab = searchParams.get(SETTINGS_TAB_PARAM)
  const rawView = searchParams.get(INSTANCE_VIEW_PARAM)

  // An unknown `tab`, or a `view` the open tab does not have, is removed from the URL.
  useEffect(() => {
    const views = TAB_VIEWS[tab] ?? []
    const badTab = rawTab !== null && rawTab !== tab
    const badView = rawView !== null && !views.some((entry) => entry.id === rawView)
    if (!badTab && !badView) return
    setSearchParams(
      (current) => {
        const params = new URLSearchParams(current)
        if (badTab) params.delete(SETTINGS_TAB_PARAM)
        if (badView) params.delete(INSTANCE_VIEW_PARAM)
        return params
      },
      { replace: true },
    )
  }, [rawTab, rawView, setSearchParams, tab])
  const instancesQuery = useInstances()
  const clustersQuery = useClusters()
  const alertsQuery = useAlerts({ state: 'firing' })
  const rulesQuery = useAdvisorRules()

  function showTab(next: SettingsTabId) {
    setSearchParams(
      (current) => {
        const params = new URLSearchParams(current)
        // The default tab is never written to the URL (D12).
        if (next === DEFAULT_SETTINGS_TAB) params.delete(SETTINGS_TAB_PARAM)
        else params.set(SETTINGS_TAB_PARAM, next)
        // A view belongs to the tab that wrote it.
        if (next !== tab) params.delete(INSTANCE_VIEW_PARAM)
        return params
      },
      { replace: true },
    )
  }

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

  // Only the active tab mounts its content (Radix), so a tab's queries start when it opens.
  const content: Record<SettingsTabId, () => ReactNode> = {
    inventory: () => (
      <>
        {instances.length === 0 ? (
          <EmptyState
            description="Install and configure the pglens agent on a PostgreSQL host to populate the fleet inventory."
            title="The fleet is empty"
          />
        ) : null}
        <AlertListTruncated truncated={alertsQuery.truncated} />
        <InventoryTables alerts={alerts} clusters={clusters} instances={instances} rules={rules} />
      </>
    ),
    preferences: () => <PlannedTab id="preferences" label="Preferences" />,
    retention: () => <PlannedTab id="retention" label="Data retention" />,
    advisor: () => <PlannedTab id="advisor" label="Advisor rules" />,
    notifications: () => <PlannedTab id="notifications" label="Notifications" />,
    names: () => <NamesPanel clusters={clusters} instances={instances} />,
    audit: () => <AuditTab instances={instances} />,
  }

  return (
    <div className="space-y-8 min-[1600px]:space-y-4">
      <PageHeader
        icon={Settings}
        eyebrow="System"
        freshness={<FreshnessBadge dataUpdatedAt={instancesQuery.dataUpdatedAt} policy="fleet" />}
        subtitle="What pglens monitors and with which permissions, one tab per area: the instance inventory, console preferences, data retention, advisor rules, notification channels, cluster and instance names, and the command audit with the server build."
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
      {/* One tab per area (11.1); every query above stays in the page. */}
      <Tabs onValueChange={(value) => showTab(readSettingsTab(value))} value={tab}>
        <TabsList aria-label="Settings sections" className="h-auto flex-wrap">
          {SETTINGS_TABS.map((entry) => (
            <TabsTrigger key={entry.id} value={entry.id}>
              {entry.label}
            </TabsTrigger>
          ))}
        </TabsList>
        {SETTINGS_TABS.map((entry) => (
          <TabsContent className="space-y-6 min-[1600px]:space-y-4" key={entry.id} value={entry.id}>
            {content[entry.id]()}
          </TabsContent>
        ))}
      </Tabs>
    </div>
  )
}

export default SettingsPage
