import { useMemo } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { useInstanceDatabases, useInstanceHost } from '@/api/queries'
import { REFRESH } from '@/api/policy'
import type { Schemas } from '@/api/types'
import { DataTable, type DataTableToolbar } from '@/components/layout/DataTable'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { Button } from '@/components/ui/button'
import { Callout } from '@/components/ui/callout'
import { Tabs, TabsContent } from '@/components/ui/tabs'
import { formatPostgresVersion, formatTimestamp } from '@/lib/format'
import { clusterLabel, hasDisplayName, instanceAddress, instanceLabel } from '@/lib/names'
import { matchesSearch, type SearchField } from '@/lib/tableControls'

import { InstanceViewTabs } from '@/features/instance/InstancePanel'
import { useInstanceView } from '@/features/instance/instanceViews'
import { INVENTORY_VIEWS } from './settingsTabs'

type Alert = Schemas['Alert']
type AdvisorRule = Schemas['AdvisorRule']
type Cluster = Schemas['Cluster']
type Database = Schemas['Database']
type InstanceSummary = Schemas['InstanceSummary']
type PermTier = Schemas['PermTier']

const permissionTiers: readonly PermTier[] = ['T0', 'T1', 'T2']
const tierRank: Record<PermTier, number> = { T0: 0, T1: 1, T2: 2 }

function isAgentDownAlert(alert: Alert): boolean {
  if (alert.state !== 'firing') return false
  return (
    alert.rule_id === 'agent_down' ||
    alert.rule_id === 'instance_unreachable' ||
    alert.alert_key.startsWith('agent_down/') ||
    alert.alert_key.startsWith('instance_unreachable/')
  )
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
  state: string
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

const INSTANCE_TOOLBAR: DataTableToolbar<InstanceRow> = {
  searchFields: MONITORED_INSTANCE_SEARCH,
  filters: [
    { id: 'role', label: 'Role', kind: 'select', accessor: (instance) => instance.role },
    {
      id: 'state',
      label: 'State',
      kind: 'select',
      options: [
        { value: 'up', label: 'Up' },
        { value: 'down', label: 'Down' },
      ],
      accessor: (instance) => (instance.up ? 'up' : 'down'),
    },
  ],
}

const DATABASE_TOOLBAR: DataTableToolbar<DatabaseRow> = {
  searchFields: [(database) => database.datname, (database) => database.skip_reason],
  filters: [
    {
      id: 'monitored',
      label: 'Monitored',
      kind: 'select',
      options: [
        { value: 'yes', label: 'Yes' },
        { value: 'no', label: 'No' },
      ],
      accessor: (database) => (database.monitored ? 'yes' : 'no'),
    },
  ],
}

const AGENT_TOOLBAR: DataTableToolbar<AgentRow> = {
  searchFields: [(agent) => agent.agent_id, (agent) => agent.address, (agent) => agent.reason],
  filters: [{ id: 'state', label: 'State', kind: 'select', accessor: (agent) => agent.state }],
}

function HostMetricsCell({ instanceId }: { instanceId: string }) {
  const query = useInstanceHost(instanceId, REFRESH.inventory)

  if (query.error && !query.data) {
    return <span title="The host metrics endpoint could not be loaded">Unavailable</span>
  }
  if (query.isPending || query.data === undefined) {
    return <span>Loading…</span>
  }
  if (!query.data.available) {
    return <span title={query.data.reason ?? 'Host metrics are unavailable'}>Unavailable</span>
  }
  return <span>{query.data.stale ? 'Available (stale)' : 'Available'}</span>
}

function instanceColumns() {
  return [
    {
      accessorKey: 'instance_id',
      header: 'Instance',
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
      header: 'Address / port',
      cell: ({ row }: { row: { original: InstanceRow } }) => (
        <span>{instanceAddress(row.original)}</span>
      ),
    },
    {
      accessorKey: 'cluster_name',
      header: 'Cluster',
      cell: ({ row }: { row: { original: InstanceRow } }) =>
        row.original.cluster_id ? (
          <Link to={`/clusters/${row.original.cluster_id}`}>
            {clusterLabel(
              { cluster_id: row.original.cluster_id, name: row.original.cluster_name },
              row.original.cluster_id,
            )}
          </Link>
        ) : (
          <span>Not assigned</span>
        ),
    },
    { accessorKey: 'role', header: 'Role' },
    {
      accessorKey: 'pg_version',
      header: 'PostgreSQL',
      cell: ({ row }: { row: { original: InstanceRow } }) =>
        formatPostgresVersion(row.original.pg_version),
    },
    { accessorKey: 'perm_tier', header: 'Permission tier' },
    {
      accessorKey: 'last_seen',
      header: 'Last seen',
      cell: ({ row }: { row: { original: InstanceRow } }) => (
        <time dateTime={row.original.last_seen}>
          {formatTimestamp(row.original.last_seen, 'UTC')}
        </time>
      ),
    },
    {
      accessorKey: 'up',
      header: 'State',
      cell: ({ row }: { row: { original: InstanceRow } }) => (row.original.up ? 'Up' : 'Down'),
    },
    {
      accessorKey: 'host_metrics',
      header: 'Host metrics',
      enableSorting: false,
      cell: ({ row }: { row: { original: InstanceRow } }) => (
        <HostMetricsCell instanceId={row.original.instance_id} />
      ),
    },
  ]
}

function databaseColumns() {
  return [
    { accessorKey: 'datname', header: 'Database' },
    {
      accessorKey: 'monitored',
      header: 'Monitored',
      cell: ({ row }: { row: { original: DatabaseRow } }) =>
        row.original.monitored ? 'Yes' : 'No',
    },
    { accessorKey: 'skip_reason', header: 'Skip reason' },
  ]
}

function DatabaseInventory({ instance }: { instance: InstanceRow }) {
  const query = useInstanceDatabases(instance.instance_id, REFRESH.inventory)
  const label = instanceLabel(instance)
  const title = `Databases — ${label}`

  if (query.error && !query.data) {
    return (
      <Section captionId="settings.inventory-databases-instance" title={title}>
        <ErrorState
          endpoint={`databases for ${label}`}
          failure={query.error}
          onRetry={() => void query.refetch()}
        />
      </Section>
    )
  }
  if (query.isPending || query.data === undefined) {
    return (
      <Section captionId="settings.inventory-databases-instance" title={title}>
        <section aria-busy="true" aria-label={`Loading databases for ${label}`} role="status">
          Loading databases…
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
        ariaLabel={`Databases for ${label}`}
        captionId="settings.inventory-databases-table"
        toolbarExtra={
          <span className="text-muted-foreground text-sm">
            Databases not monitored: {query.data.not_monitored_count}.
          </span>
        }
        tableId={`instance-databases-${instance.instance_id}`}
        toolbar={DATABASE_TOOLBAR}
        columns={databaseColumns()}
        data={rows}
        emptyState={
          <EmptyState
            description="The instance reported no databases in this inventory sample."
            title="No databases reported"
          />
        }
        scope="databases"
        total={rows.length}
      />
    </Section>
  )
}

function TierSummary({ instances, rules }: Pick<InventoryTablesProps, 'instances' | 'rules'>) {
  return (
    <Section captionId="settings.permission-tiers" title="Permission tiers">
      <div className="grid gap-4 md:grid-cols-3">
        {permissionTiers.map((tier) => {
          const unlockedRules = rules.filter((rule) => tierRank[rule.min_tier] <= tierRank[tier])
          const instanceCount = instances.filter((instance) => instance.perm_tier === tier).length
          const actions =
            tier === 'T0'
              ? ['View inventory, findings, and the rule catalogue.']
              : tier === 'T1'
                ? [
                    'Run plan-only EXPLAIN on eligible targets.',
                    'EXPLAIN ANALYZE is accepted when the target sets allow_explain_analyze; the interface offers it at T2 only.',
                  ]
                : [
                    'Cancel or terminate client backends on eligible targets.',
                    'Signals remain subject to allow_signal on the target.',
                  ]

          return (
            // From 1600 px the cards are tighter and each label runs in before its list,
            // which wraps inline, so the three cards fit one Full HD screen (D26).
            <article
              key={tier}
              className="rounded-md border p-4 min-[1600px]:px-3 min-[1600px]:py-2.5"
            >
              <div className="min-[1600px]:flex min-[1600px]:items-baseline min-[1600px]:gap-3">
                <h3 className="font-medium">Tier {tier}</h3>
                <p className="mt-1 text-sm min-[1600px]:mt-0">
                  <strong>Instances:</strong> {instanceCount}
                </p>
              </div>
              <div className="mt-3 text-sm min-[1600px]:mt-1.5 min-[1600px]:text-xs min-[1600px]:leading-4">
                <strong className="min-[1600px]:mr-2 min-[1600px]:whitespace-nowrap min-[1600px]:after:content-[':']">
                  Advisor rules unlocked
                </strong>
                {unlockedRules.length > 0 ? (
                  <ul
                    className="mt-1 list-disc pl-5 min-[1600px]:inline min-[1600px]:list-none min-[1600px]:pl-0"
                    // `list-none` drops the list role in Safari; keep it explicit.
                    role="list"
                  >
                    {unlockedRules.map((rule) => (
                      <li
                        className="min-[1600px]:mr-2 min-[1600px]:inline-block min-[1600px]:whitespace-nowrap"
                        key={rule.id}
                      >
                        {rule.id} ({rule.min_tier})
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-muted-foreground mt-1">No rules returned by the catalogue.</p>
                )}
              </div>
              <div className="mt-3 text-sm min-[1600px]:mt-1.5 min-[1600px]:text-xs min-[1600px]:leading-4">
                <strong className="min-[1600px]:mr-2 min-[1600px]:whitespace-nowrap min-[1600px]:after:content-[':']">
                  Actions unlocked
                </strong>
                <ul
                  className="mt-1 list-disc pl-5 min-[1600px]:inline min-[1600px]:list-none min-[1600px]:pl-0"
                  role="list"
                >
                  {actions.map((action) => (
                    <li className="min-[1600px]:mr-2 min-[1600px]:inline" key={action}>
                      {action}
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
      reason:
        alert?.summary ?? (isDown ? 'The instance is not up.' : 'No firing agent_down alert.'),
    }
  })

  return (
    <Section captionId="settings.inventory-agents" title="Agents (derived from instance telemetry)">
      <p className="text-muted-foreground mb-3 text-sm">
        The API has no separate agent listing endpoint. This view derives agent state from instance
        last_seen/up data and firing agent_down alerts.
      </p>
      <DataTable
        ariaLabel="Derived agent inventory"
        captionId="settings.inventory-agents-table"
        tableId="derived-agent-inventory"
        toolbar={AGENT_TOOLBAR}
        columns={[
          { accessorKey: 'agent_id', header: 'Agent' },
          { accessorKey: 'address', header: 'Instance address' },
          {
            accessorKey: 'last_seen',
            header: 'Last seen',
            cell: ({ row }: { row: { original: AgentRow } }) => (
              <time dateTime={row.original.last_seen}>
                {formatTimestamp(row.original.last_seen, 'UTC')}
              </time>
            ),
          },
          { accessorKey: 'state', header: 'State' },
          { accessorKey: 'reason', header: 'Reason' },
        ]}
        data={rows}
        emptyState={
          <EmptyState
            description="No monitored instances are available to derive an agent view from."
            title="No agent telemetry"
          />
        }
        scope="agents"
        total={rows.length}
      />
    </Section>
  )
}

export function InventoryTables({ alerts, clusters, instances, rules }: InventoryTablesProps) {
  const [searchParams, setSearchParams] = useSearchParams()
  const query = searchParams.get(INSTANCE_SEARCH_PARAM) ?? ''
  // Built once. Fresh column definitions on each render give the host cell a
  // new component type, so every render remounts it and refetches its metrics.
  const instanceTableColumns = useMemo(() => instanceColumns(), [])
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
      <InstanceViewTabs label="Inventory views" views={INVENTORY_VIEWS} />
      <TabsContent value="instances">
        <Section captionId="settings.inventory-instances" title="Instances">
          <DataTable
            ariaLabel="Monitored instances"
            captionId="settings.inventory-instances-table"
            tableId="monitored-instances"
            toolbar={INSTANCE_TOOLBAR}
            columns={instanceTableColumns}
            data={rows}
            emptyState={
              <EmptyState
                description="Install and configure the pglens agent on a PostgreSQL host to add the first instance."
                title="No monitored instances"
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
        <Section captionId="settings.inventory-databases" title="Databases by instance">
          {query !== '' ? (
            // The search box is in the Instances view; say so here, where it still filters.
            <Callout className="mb-4" role="status">
              <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
                <span>Filtered by the Instances search “{query}”.</span>
                <Button onClick={clearInstanceSearch} size="xs" type="button" variant="outline">
                  Clear filter
                </Button>
              </div>
            </Callout>
          ) : null}
          {rows.length > 0 && matching.length === 0 ? (
            <EmptyState
              description="Try a different address, cluster, role, tier, or instance ID."
              title="No instances match this filter"
            />
          ) : (
            <div className="space-y-6 min-[1600px]:space-y-4">
              {matching.map((instance) => (
                <DatabaseInventory instance={instance} key={instance.instance_id} />
              ))}
            </div>
          )}
        </Section>
      </TabsContent>
    </Tabs>
  )
}
