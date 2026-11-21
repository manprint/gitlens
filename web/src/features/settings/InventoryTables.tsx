import { useMemo } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { useInstanceDatabases, useInstanceHost } from '@/api/queries'
import { REFRESH } from '@/api/policy'
import type { Schemas } from '@/api/types'
import { DataTable, type DataTableToolbar } from '@/components/layout/DataTable'
import { HelpLink } from '@/components/layout/HelpLink'
import { Section } from '@/components/layout/Section'
import { withTerms } from '@/components/layout/withTerms'
import { EmptyState, ErrorState } from '@/components/state'
import { Button } from '@/components/ui/button'
import { Callout } from '@/components/ui/callout'
import { Tabs, TabsContent } from '@/components/ui/tabs'
import type { MessageKey } from '@/i18n/messages/en'
import { useT, type Translate } from '@/i18n/react'
import { formatCountOf, formatPostgresVersion, formatTimestamp } from '@/lib/format'
import { clusterLabel, hasDisplayName, instanceAddress, instanceLabel } from '@/lib/names'
import { ruleText } from '@/lib/ruleText'
import { matchesSearch, type SearchField } from '@/lib/tableControls'

import { InstanceViewTabs } from '@/features/instance/InstancePanel'
import { useInstanceView } from '@/features/instance/instanceViews'
import { InstancePicker } from './InstancePicker'
import { usePickedInstance } from './usePickedInstance'
import { INVENTORY_VIEWS } from './settingsTabs'

type Alert = Schemas['Alert']
type AdvisorRule = Schemas['AdvisorRule']
type Cluster = Schemas['Cluster']
type Database = Schemas['Database']
type InstanceSummary = Schemas['InstanceSummary']
type PermTier = Schemas['PermTier']

const permissionTiers: readonly PermTier[] = ['T0', 'T1', 'T2']

/** What each tier lets pglens do, in the words of the glossary (internal/pgtype/role.go). */
const TIER_MEANING: Record<PermTier, MessageKey> = {
  T0: 'settings.inventory.tierMeaningT0',
  T1: 'settings.inventory.tierMeaningT1',
  T2: 'settings.inventory.tierMeaningT2',
}

/** What each tier unlocks, one key per list item. */
const TIER_ACTIONS: Record<PermTier, readonly MessageKey[]> = {
  T0: ['settings.inventory.actionT0View'],
  T1: ['settings.inventory.actionT1Explain', 'settings.inventory.actionT1ExplainAnalyze'],
  T2: ['settings.inventory.actionT2Cancel', 'settings.inventory.actionT2Signals'],
}

function isAgentDownAlert(alert: Alert): boolean {
  if (alert.state !== 'firing') return false
  return (
    alert.rule_id === 'agent_down' ||
    alert.rule_id === 'instance_unreachable' ||
    alert.alert_key.startsWith('agent_down/') ||
    alert.alert_key.startsWith('instance_unreachable/')
  )
}

/**
 * Why an agent row reads Down or Reporting, in plain words. The row is Down when the
 * instance is not up (no report within the last 60 seconds) or an agent-down alert is
 * open; an alert can stay open for a moment after the instance reports again, until
 * the alert rules next run.
 */
function agentReason(up: boolean, alert: Alert | undefined, t: Translate): string {
  if (alert !== undefined) {
    const summary = t('settings.inventory.alertSummary', { summary: alert.summary })
    return up
      ? t('settings.inventory.reasonBackOpen', { summary })
      : t('settings.inventory.reasonDownOpen', { summary })
  }
  return up ? t('settings.inventory.reasonRecent') : t('settings.inventory.reasonNoReport')
}

interface InstanceRow extends InstanceSummary {
  cluster_id: string | null
  cluster_name: string | null
}

interface DatabaseRow extends Database {
  skip_reason: string
}

interface AgentRow {
  agent_id: string
  instance_id: string
  address: string
  last_seen: string
  state: 'Down' | 'Reporting'
  reason: string
}

export interface InventoryTablesProps {
  alerts: readonly Alert[]
  clusters: readonly Cluster[]
  instances: readonly InstanceSummary[]
  rules: readonly AdvisorRule[]
}

/** The URL parameter of the Monitored instances search; the database views follow it. */
const INSTANCE_SEARCH_PARAM = 'monitored-instances.q'

/** The fields the Monitored instances search matches; "Databases by instance" follows it. */
const MONITORED_INSTANCE_SEARCH: readonly SearchField<InstanceRow>[] = [
  (instance) => instance.instance_id,
  (instance) => instance.display_name,
  (instance) => instance.addr,
  (instance) => instance.role,
  (instance) => instance.cluster_id,
  (instance) => instance.cluster_name,
  (instance) => instance.perm_tier,
]

function instanceToolbar(t: Translate): DataTableToolbar<InstanceRow> {
  return {
    searchFields: MONITORED_INSTANCE_SEARCH,
    filters: [
      {
        id: 'role',
        label: t('settings.inventory.colRole'),
        kind: 'select',
        accessor: (instance) => instance.role,
      },
      {
        id: 'state',
        label: t('settings.inventory.colState'),
        kind: 'select',
        options: [
          { value: 'up', label: t('settings.inventory.up') },
          { value: 'down', label: t('settings.inventory.down') },
        ],
        accessor: (instance) => (instance.up ? 'up' : 'down'),
      },
    ],
  }
}

function databaseToolbar(t: Translate): DataTableToolbar<DatabaseRow> {
  return {
    searchFields: [(database) => database.datname, (database) => database.skip_reason],
    filters: [
      {
        id: 'monitored',
        label: t('settings.inventory.colMonitored'),
        kind: 'select',
        options: [
          { value: 'yes', label: t('settings.inventory.yes') },
          { value: 'no', label: t('settings.inventory.no') },
        ],
        accessor: (database) => (database.monitored ? 'yes' : 'no'),
      },
    ],
  }
}

function agentToolbar(t: Translate): DataTableToolbar<AgentRow> {
  return {
    searchFields: [(agent) => agent.agent_id, (agent) => agent.address, (agent) => agent.reason],
    filters: [
      {
        id: 'state',
        label: t('settings.inventory.colState'),
        kind: 'select',
        options: [
          { value: 'Down', label: t('settings.inventory.down') },
          { value: 'Reporting', label: t('settings.inventory.reporting') },
        ],
        accessor: (agent) => agent.state,
      },
    ],
  }
}

function HostMetricsCell({ instanceId }: { instanceId: string }) {
  const t = useT()
  const query = useInstanceHost(instanceId, REFRESH.inventory)

  if (query.error && !query.data) {
    return (
      <span title={t('settings.inventory.hostUnavailableTitle')}>
        {t('settings.inventory.hostUnavailable')}
      </span>
    )
  }
  if (query.isPending || query.data === undefined) {
    return <span>{t('settings.inventory.loading')}</span>
  }
  if (!query.data.available) {
    return (
      <span title={query.data.reason ?? t('settings.inventory.hostUnavailableFallback')}>
        {t('settings.inventory.hostUnavailable')}
      </span>
    )
  }
  return (
    <span>
      {t(
        query.data.stale
          ? 'settings.inventory.hostAvailableStale'
          : 'settings.inventory.hostAvailable',
      )}
    </span>
  )
}

function instanceColumns(t: Translate) {
  return [
    {
      accessorKey: 'instance_id',
      header: t('settings.inventory.colInstance'),
      // A named instance shows its name; the id stays in the tooltip and the search.
      cell: ({ row }: { row: { original: InstanceRow } }) =>
        hasDisplayName(row.original) ? (
          <span title={row.original.instance_id}>{instanceLabel(row.original)}</span>
        ) : (
          <code className="text-xs">{row.original.instance_id}</code>
        ),
    },
    {
      accessorKey: 'addr',
      header: t('settings.inventory.colAddress'),
      cell: ({ row }: { row: { original: InstanceRow } }) => (
        <span>{instanceAddress(row.original)}</span>
      ),
    },
    {
      accessorKey: 'cluster_name',
      header: t('settings.inventory.colCluster'),
      cell: ({ row }: { row: { original: InstanceRow } }) =>
        row.original.cluster_id ? (
          <Link to={`/clusters/${row.original.cluster_id}`}>
            {clusterLabel(
              { cluster_id: row.original.cluster_id, name: row.original.cluster_name },
              row.original.cluster_id,
            )}
          </Link>
        ) : (
          <span>{t('settings.inventory.notAssigned')}</span>
        ),
    },
    { accessorKey: 'role', header: t('settings.inventory.colRole') },
    {
      accessorKey: 'pg_version',
      header: t('settings.inventory.colPostgres'),
      cell: ({ row }: { row: { original: InstanceRow } }) =>
        formatPostgresVersion(row.original.pg_version),
    },
    {
      accessorKey: 'perm_tier',
      header: t('settings.inventory.colPermTier'),
      meta: { help: 'settings.permission-tier' as const },
    },
    {
      accessorKey: 'last_seen',
      header: t('settings.inventory.colLastSeen'),
      cell: ({ row }: { row: { original: InstanceRow } }) => (
        <time dateTime={row.original.last_seen}>{formatTimestamp(row.original.last_seen)}</time>
      ),
    },
    {
      accessorKey: 'up',
      header: t('settings.inventory.colState'),
      meta: { help: 'settings.instance-state' as const },
      cell: ({ row }: { row: { original: InstanceRow } }) =>
        t(row.original.up ? 'settings.inventory.up' : 'settings.inventory.down'),
    },
    {
      accessorKey: 'host_metrics',
      header: t('settings.inventory.colHostMetrics'),
      enableSorting: false,
      cell: ({ row }: { row: { original: InstanceRow } }) => (
        <HostMetricsCell instanceId={row.original.instance_id} />
      ),
    },
  ]
}

function databaseColumns(t: Translate) {
  return [
    { accessorKey: 'datname', header: t('settings.inventory.colDatabase') },
    {
      accessorKey: 'monitored',
      header: t('settings.inventory.colMonitored'),
      cell: ({ row }: { row: { original: DatabaseRow } }) =>
        t(row.original.monitored ? 'settings.inventory.yes' : 'settings.inventory.no'),
    },
    {
      accessorKey: 'skip_reason',
      header: t('settings.inventory.colSkipReason'),
      meta: { help: 'settings.skip-reason' as const },
    },
  ]
}

function DatabaseInventory({ instance }: { instance: InstanceRow }) {
  const t = useT()
  const query = useInstanceDatabases(instance.instance_id, REFRESH.inventory)
  const toolbar = useMemo(() => databaseToolbar(t), [t])
  const label = instanceLabel(instance)
  const title = t('settings.inventory.databasesTitle', { label })

  if (query.error && !query.data) {
    return (
      <Section captionId="settings.inventory-databases-instance" title={title}>
        <ErrorState
          endpoint={t('settings.inventory.endpointDatabases', { label })}
          failure={query.error}
          onRetry={() => void query.refetch()}
        />
      </Section>
    )
  }
  if (query.isPending || query.data === undefined) {
    return (
      <Section captionId="settings.inventory-databases-instance" title={title}>
        <section
          aria-busy="true"
          aria-label={t('settings.inventory.loadingDatabasesFor', { label })}
          role="status"
        >
          {t('settings.inventory.loadingDatabases')}
        </section>
      </Section>
    )
  }

  const rows: DatabaseRow[] = [...query.data.databases]
    .sort((left, right) => {
      if (left.monitored === right.monitored) return 0
      return left.monitored ? 1 : -1
    })
    .map((database) => ({ ...database, skip_reason: database.skip_reason ?? '—' }))

  return (
    <Section captionId="settings.inventory-databases-instance" title={title}>
      <DataTable
        ariaLabel={t('settings.inventory.databasesFor', { label })}
        captionId="settings.inventory-databases-table"
        toolbarExtra={
          <span className="text-muted-foreground text-sm">
            {t('settings.inventory.notMonitoredCount', {
              count: query.data.not_monitored_count,
            })}
          </span>
        }
        tableId={`instance-databases-${instance.instance_id}`}
        toolbar={toolbar}
        columns={databaseColumns(t)}
        data={rows}
        emptyState={
          <EmptyState
            description={t('settings.inventory.noDatabasesDescription')}
            title={t('settings.inventory.noDatabasesTitle')}
          />
        }
        scope="databases"
        total={rows.length}
      />
    </Section>
  )
}

function TierSummary({ instances, rules }: Pick<InventoryTablesProps, 'instances' | 'rules'>) {
  const t = useT()
  return (
    <Section
      actions={<HelpLink anchor="settings#permission-tiers" label={t('settings.views.tiers')} />}
      captionId="settings.permission-tiers"
      title={t('settings.views.tiers')}
    >
      <div className="grid gap-4 md:grid-cols-3">
        {permissionTiers.map((tier) => {
          // A rule's minimum tier is the lowest tier at which it can run (internal/advisor); a
          // higher tier runs it too, so each card lists only the rules that first need this tier.
          const tierRules = rules.filter((rule) => rule.min_tier === tier)
          const instanceCount = instances.filter((instance) => instance.perm_tier === tier).length

          return (
            // From 1600 px the cards are tighter and each label runs in before its list,
            // which wraps inline, so the three cards fit one Full HD screen (D26).
            <article
              key={tier}
              className="rounded-md border p-4 min-[1600px]:px-3 min-[1600px]:py-2.5"
            >
              <div className="min-[1600px]:flex min-[1600px]:flex-wrap min-[1600px]:items-baseline min-[1600px]:gap-x-3">
                <h3 className="font-medium">{t('settings.inventory.tierHeading', { tier })}</h3>
                <p className="mt-1 text-sm min-[1600px]:mt-0">
                  <strong>{t('settings.inventory.instancesLabel')}</strong> {instanceCount}
                </p>
                <p className="text-muted-foreground mt-1 text-sm min-[1600px]:mt-0 min-[1600px]:text-xs">
                  {withTerms(t(TIER_MEANING[tier]))}
                </p>
              </div>
              <div className="mt-3 text-sm min-[1600px]:mt-1.5 min-[1600px]:text-xs min-[1600px]:leading-4">
                <strong className="min-[1600px]:mr-2 min-[1600px]:whitespace-nowrap min-[1600px]:after:content-[':']">
                  {t('settings.inventory.rulesNeedTier', { tier })}
                </strong>
                {tierRules.length > 0 ? (
                  <>
                    <span className="min-[1600px]:mr-2">
                      {formatCountOf(tierRules.length, 'rule')}
                    </span>
                    <details className="mt-1 min-[1600px]:inline">
                      <summary className="text-primary cursor-pointer min-[1600px]:inline">
                        {t('settings.inventory.showList')}
                      </summary>
                      <ul
                        className="mt-1 list-disc space-y-0.5 pl-5"
                        // `list-none` drops the list role in Safari; keep it explicit.
                        role="list"
                      >
                        {tierRules.map((rule) => {
                          const text = ruleText(rule.id)
                          return (
                            <li key={rule.id}>
                              {text === undefined ? null : `${text} `}
                              <span
                                className={`font-mono text-xs ${text === undefined ? '' : 'text-muted-foreground'}`}
                              >
                                {rule.id}
                              </span>
                            </li>
                          )
                        })}
                      </ul>
                    </details>
                  </>
                ) : (
                  <span className="text-muted-foreground mt-1">
                    {tier === 'T0'
                      ? t('settings.inventory.noRulesT0')
                      : t('settings.inventory.noRulesWithout', { tier })}
                  </span>
                )}
              </div>
              <div className="mt-3 text-sm min-[1600px]:mt-1.5 min-[1600px]:text-xs min-[1600px]:leading-4">
                <strong className="min-[1600px]:mr-2 min-[1600px]:whitespace-nowrap min-[1600px]:after:content-[':']">
                  {t('settings.inventory.actionsUnlocked')}
                </strong>
                <ul
                  className="mt-1 list-disc pl-5 min-[1600px]:inline min-[1600px]:list-none min-[1600px]:pl-0"
                  role="list"
                >
                  {TIER_ACTIONS[tier].map((key) => (
                    <li className="min-[1600px]:mr-2 min-[1600px]:inline" key={key}>
                      {t(key)}
                    </li>
                  ))}
                </ul>
              </div>
            </article>
          )
        })}
      </div>
    </Section>
  )
}

function AgentInventory({ instances, alerts }: Pick<InventoryTablesProps, 'instances' | 'alerts'>) {
  const t = useT()
  const toolbar = useMemo(() => agentToolbar(t), [t])
  const rows: AgentRow[] = instances.map((instance) => {
    const alert = alerts.find(
      (candidate) => candidate.instance_id === instance.instance_id && isAgentDownAlert(candidate),
    )
    const isDown = !instance.up || alert !== undefined
    return {
      agent_id: `agent-${instance.instance_id}`,
      instance_id: instance.instance_id,
      address: `${instance.addr}:${instance.port}`,
      last_seen: instance.last_seen,
      state: isDown ? 'Down' : 'Reporting',
      reason: agentReason(instance.up, alert, t),
    }
  })

  return (
    <Section
      actions={<HelpLink anchor="settings#agents" label={t('settings.views.agents')} />}
      captionId="settings.inventory-agents"
      title={t('settings.inventory.agentsTitle')}
    >
      <p className="text-muted-foreground mb-3 text-sm">{t('settings.inventory.agentsIntro')}</p>
      <DataTable
        ariaLabel={t('settings.inventory.agentsTable')}
        captionId="settings.inventory-agents-table"
        tableId="derived-agent-inventory"
        toolbar={toolbar}
        columns={[
          { accessorKey: 'agent_id', header: t('settings.inventory.colAgent') },
          { accessorKey: 'address', header: t('settings.inventory.colInstanceAddress') },
          {
            accessorKey: 'last_seen',
            header: t('settings.inventory.colLastSeen'),
            cell: ({ row }: { row: { original: AgentRow } }) => (
              <time dateTime={row.original.last_seen}>
                {formatTimestamp(row.original.last_seen)}
              </time>
            ),
          },
          {
            accessorKey: 'state',
            header: t('settings.inventory.colState'),
            meta: { help: 'settings.agent-state' as const },
            cell: ({ row }: { row: { original: AgentRow } }) =>
              t(
                row.original.state === 'Down'
                  ? 'settings.inventory.down'
                  : 'settings.inventory.reporting',
              ),
          },
          { accessorKey: 'reason', header: t('settings.inventory.colReason') },
        ]}
        data={rows}
        emptyState={
          <EmptyState
            description={t('settings.inventory.emptyAgentsDescription')}
            title={t('settings.inventory.emptyAgentsTitle')}
          />
        }
        scope="agents"
        total={rows.length}
      />
    </Section>
  )
}

export function InventoryTables({ alerts, clusters, instances, rules }: InventoryTablesProps) {
  const t = useT()
  const [searchParams, setSearchParams] = useSearchParams()
  const query = searchParams.get(INSTANCE_SEARCH_PARAM) ?? ''
  // Built once. Fresh column definitions on each render give the host cell a
  // new component type, so every render remounts it and refetches its metrics.
  const instanceTableColumns = useMemo(() => instanceColumns(t), [t])
  const instanceTableToolbar = useMemo(() => instanceToolbar(t), [t])
  const clusterByInstance = useMemo(() => {
    const result = new Map<string, Cluster>()
    for (const cluster of clusters) {
      for (const instance of cluster.instances) {
        result.set(instance.instance_id, cluster)
      }
    }
    return result
  }, [clusters])
  const rows = useMemo(
    () =>
      instances.map<InstanceRow>((instance) => {
        const cluster = clusterByInstance.get(instance.instance_id)
        return {
          ...instance,
          cluster_id: cluster?.cluster_id ?? null,
          cluster_name: cluster?.name ?? null,
        }
      }),
    [clusterByInstance, instances],
  )
  // The table searches its own rows; the database sections follow its search only.
  const matching = useMemo(
    () => rows.filter((instance) => matchesSearch(instance, query, MONITORED_INSTANCE_SEARCH)),
    [query, rows],
  )
  const [view = 'instances', showView] = useInstanceView(INVENTORY_VIEWS)
  // One instance's databases at a time (GAP-9), among those the search leaves.
  const [pickedDatabases, pickDatabases] = usePickedInstance('dbInstance', matching)

  function clearInstanceSearch() {
    setSearchParams(
      (current) => {
        const params = new URLSearchParams(current)
        params.delete(INSTANCE_SEARCH_PARAM)
        return params
      },
      { replace: true },
    )
  }

  return (
    // One table per view, in the URL (11.1); Instances is the default.
    <Tabs className="gap-4" onValueChange={showView} value={view}>
      <InstanceViewTabs
        label={t('settings.page.inventoryViews')}
        views={INVENTORY_VIEWS.map((entry) => ({ id: entry.id, label: t(entry.labelKey) }))}
      />
      <TabsContent value="instances">
        <Section
          actions={<HelpLink anchor="settings#instances" label={t('settings.views.instances')} />}
          captionId="settings.inventory-instances"
          title={t('settings.views.instances')}
        >
          <DataTable
            ariaLabel={t('settings.inventory.instancesTable')}
            captionId="settings.inventory-instances-table"
            tableId="monitored-instances"
            toolbar={instanceTableToolbar}
            columns={instanceTableColumns}
            data={rows}
            emptyState={
              <EmptyState
                description={t('settings.inventory.noInstancesDescription')}
                title={t('settings.inventory.noInstancesTitle')}
              />
            }
            scope="instances"
            total={instances.length}
          />
        </Section>
      </TabsContent>

      <TabsContent value="tiers">
        <TierSummary instances={instances} rules={rules} />
      </TabsContent>
      <TabsContent value="agents">
        <AgentInventory alerts={alerts} instances={instances} />
      </TabsContent>

      <TabsContent value="databases">
        <Section
          actions={
            <>
              <InstancePicker
                instances={matching}
                label={t('settings.inventory.databasesInstance')}
                onPick={pickDatabases}
                picked={pickedDatabases}
              />
              <HelpLink anchor="settings#databases" label={t('settings.views.databases')} />
            </>
          }
          captionId="settings.inventory-databases"
          title={t('settings.views.databases')}
        >
          {query !== '' ? (
            // The search box is in the Instances view; say so here, where it still filters.
            <Callout className="mb-4" role="status">
              <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
                <span>{t('settings.inventory.filteredBy', { query })}</span>
                <Button onClick={clearInstanceSearch} size="xs" type="button" variant="outline">
                  {t('settings.inventory.clearFilter')}
                </Button>
              </div>
            </Callout>
          ) : null}
          {rows.length > 0 && matching.length === 0 ? (
            <EmptyState
              description={t('settings.inventory.noMatchDescription')}
              title={t('settings.inventory.noMatchTitle')}
            />
          ) : pickedDatabases ? (
            <DatabaseInventory instance={pickedDatabases} key={pickedDatabases.instance_id} />
          ) : null}
        </Section>
      </TabsContent>
    </Tabs>
  )
}
