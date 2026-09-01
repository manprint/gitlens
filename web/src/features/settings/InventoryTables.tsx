import { useMemo } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { useInstanceDatabases, useInstanceHost } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { DataTable } from '@/components/layout/DataTable'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { Input } from '@/components/ui/input'
import { formatPostgresVersion } from '@/lib/format'

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

function instanceMatchesFilter(instance: InstanceRow, filter: string): boolean {
  if (!filter) return true
  const haystack = [
    instance.instance_id,
    instance.addr,
    instance.role,
    instance.cluster_id ?? '',
    instance.cluster_name ?? '',
    instance.perm_tier,
  ]
    .join(' ')
    .toLocaleLowerCase()
  return haystack.includes(filter.toLocaleLowerCase())
}

function HostMetricsCell({ instanceId }: { instanceId: string }) {
  const query = useInstanceHost(instanceId)

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
      cell: ({ row }: { row: { original: InstanceRow } }) => (
        <code className="text-xs">{row.original.instance_id}</code>
      ),
    },
    {
      accessorKey: 'addr',
      header: 'Address / port',
      cell: ({ row }: { row: { original: InstanceRow } }) => (
        <span>
          {row.original.addr}:{row.original.port}
        </span>
      ),
    },
    {
      accessorKey: 'cluster_name',
      header: 'Cluster',
      cell: ({ row }: { row: { original: InstanceRow } }) =>
        row.original.cluster_id ? (
          <Link to={`/clusters/${row.original.cluster_id}`}>
            {row.original.cluster_name ?? row.original.cluster_id}
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
        <time dateTime={row.original.last_seen}>{row.original.last_seen}</time>
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
  const query = useInstanceDatabases(instance.instance_id)
  const title = `Databases — ${instance.addr}:${instance.port}`

  if (query.error && !query.data) {
    return (
      <Section title={title} description="Database inventory for this monitored instance.">
        <ErrorState
          endpoint={`databases for ${instance.addr}`}
          failure={query.error}
          onRetry={() => void query.refetch()}
        />
      </Section>
    )
  }
  if (query.isPending || query.data === undefined) {
    return (
      <Section title={title} description="Database inventory for this monitored instance.">
        <section
          aria-busy="true"
          aria-label={`Loading databases for ${instance.addr}`}
          role="status"
        >
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
    <Section
      title={title}
      description={`Database inventory for this monitored instance. Databases not monitored: ${query.data.not_monitored_count}.`}
    >
      <DataTable
        ariaLabel={`Databases for ${instance.addr}:${instance.port}`}
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
    <Section
      title="Permission tiers"
      description="Counts and capabilities are based on the live advisor rule catalogue and the permission contract."
    >
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
                    'ANALYZE remains subject to allow_explain_analyze on the target.',
                  ]
                : [
                    'Cancel or terminate client backends on eligible targets.',
                    'Signals remain subject to allow_signal on the target.',
                  ]

          return (
            <article key={tier} className="rounded-md border p-4">
              <h3 className="font-medium">Tier {tier}</h3>
              <p className="mt-1 text-sm">
                <strong>Instances:</strong> {instanceCount}
              </p>
              <div className="mt-3 text-sm">
                <strong>Advisor rules unlocked</strong>
                {unlockedRules.length > 0 ? (
                  <ul className="mt-1 list-disc pl-5">
                    {unlockedRules.map((rule) => (
                      <li key={rule.id}>
                        {rule.id} ({rule.min_tier})
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-muted-foreground mt-1">No rules returned by the catalogue.</p>
                )}
              </div>
              <div className="mt-3 text-sm">
                <strong>Actions unlocked</strong>
                <ul className="mt-1 list-disc pl-5">
                  {actions.map((action) => (
                    <li key={action}>{action}</li>
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
    <Section
      title="Agents (derived from instance telemetry)"
      description="The API has no separate agent listing endpoint. This view derives agent state from instance last_seen/up data and firing agent_down alerts."
    >
      <DataTable
        ariaLabel="Derived agent inventory"
        columns={[
          { accessorKey: 'agent_id', header: 'Agent' },
          { accessorKey: 'address', header: 'Instance address' },
          {
            accessorKey: 'last_seen',
            header: 'Last seen',
            cell: ({ row }: { row: { original: AgentRow } }) => (
              <time dateTime={row.original.last_seen}>{row.original.last_seen}</time>
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
  const filter = searchParams.get('q') ?? ''
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
      instances
        .map<InstanceRow>((instance) => {
          const cluster = clusterByInstance.get(instance.instance_id)
          return {
            ...instance,
            cluster_id: cluster?.cluster_id ?? null,
            cluster_name: cluster?.name ?? null,
          }
        })
        .filter((instance) => instanceMatchesFilter(instance, filter)),
    [clusterByInstance, filter, instances],
  )

  function updateFilter(value: string) {
    const next = new URLSearchParams(searchParams)
    if (value) next.set('q', value)
    else next.delete('q')
    setSearchParams(next, { replace: true })
  }

  return (
    <div className="space-y-8">
      <Section
        title="Instances"
        description="Every monitored PostgreSQL instance, its cluster relationship, permission tier, and telemetry availability."
      >
        <label className="mb-4 block max-w-xl space-y-1 text-sm" htmlFor="instance-filter">
          <span className="font-medium">Filter instances</span>
          <Input
            id="instance-filter"
            onChange={(event) => updateFilter(event.target.value)}
            placeholder="Address, cluster, role, tier, or instance ID"
            type="search"
            value={filter}
          />
        </label>
        <DataTable
          ariaLabel="Monitored instances"
          columns={instanceColumns()}
          data={rows}
          emptyState={
            instances.length === 0 ? (
              <EmptyState
                description="Install and configure the pglens agent on a PostgreSQL host to add the first instance."
                title="No monitored instances"
              />
            ) : (
              <EmptyState
                description="Try a different address, cluster, role, tier, or instance ID."
                title="No instances match this filter"
              />
            )
          }
          scope="instances"
          total={instances.length}
        />
      </Section>

      <TierSummary instances={instances} rules={rules} />
      <AgentInventory alerts={alerts} instances={instances} />

      <Section
        title="Databases by instance"
        description="Unmonitored databases appear first because their skip reason needs operator attention."
      >
        <div className="space-y-6">
          {rows.map((instance) => (
            <DatabaseInventory instance={instance} key={instance.instance_id} />
          ))}
        </div>
      </Section>
    </div>
  )
}
