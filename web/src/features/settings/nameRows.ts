import type { Schemas } from '@/api/types'
import { clusterLabel, instanceAddress, instanceLabel } from '@/lib/names'

type Cluster = Schemas['Cluster']
type InstanceSummary = Schemas['InstanceSummary']

/**
 * One row of the Names table: a cluster, or one of its instances (with its
 * cluster's values, so a sort or search by cluster keeps the group together).
 */
export interface NameRow {
  key: string
  kind: 'cluster' | 'instance'
  cluster_id: string | null
  cluster_name: string | null
  /** What the console shows for the cluster: its name or its default label. */
  cluster_label: string
  instance_id: string | null
  instance_name: string | null
  instance_label: string | null
  address: string | null
  role: string | null
}

function instanceRow(instance: InstanceSummary, cluster: Cluster | null): NameRow {
  return {
    key: `instance:${instance.instance_id}`,
    kind: 'instance',
    cluster_id: cluster?.cluster_id ?? null,
    cluster_name: cluster?.name ?? null,
    cluster_label: cluster ? clusterLabel(cluster) : 'Not assigned',
    instance_id: instance.instance_id,
    instance_name: instance.display_name ?? null,
    instance_label: instanceLabel(instance),
    address: instanceAddress(instance),
    role: instance.role,
  }
}

/** Clusters, each followed by its instances; instances outside every cluster come last. */
export function nameRows(
  clusters: readonly Cluster[],
  instances: readonly InstanceSummary[],
): NameRow[] {
  // The instance list is the source of an instance's own fields; the cluster list groups them.
  const byId = new Map(instances.map((instance) => [instance.instance_id, instance]))
  const grouped = new Set<string>()
  const rows: NameRow[] = []
  for (const cluster of clusters) {
    rows.push({
      key: `cluster:${cluster.cluster_id}`,
      kind: 'cluster',
      cluster_id: cluster.cluster_id,
      cluster_name: cluster.name,
      cluster_label: clusterLabel(cluster),
      instance_id: null,
      instance_name: null,
      instance_label: null,
      address: null,
      role: null,
    })
    for (const member of cluster.instances) {
      grouped.add(member.instance_id)
      rows.push(instanceRow(byId.get(member.instance_id) ?? member, cluster))
    }
  }
  for (const instance of instances) {
    if (!grouped.has(instance.instance_id)) rows.push(instanceRow(instance, null))
  }
  return rows
}
