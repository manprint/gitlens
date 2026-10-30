/**
 * The tabs of the cluster page (15.3, D26): one view per tab so each holds
 * one primary table. The registry is used by the page, the Full HD route
 * inventory and the tests; `charts` tabs are exempt from the vertical fit only.
 */

export const CLUSTER_SECTIONS = [
  { id: 'topology', label: 'Topology', kind: 'table' },
  { id: 'lag', label: 'Replication lag', kind: 'charts' },
  { id: 'slots', label: 'Replication slots', kind: 'table' },
  { id: 'drift', label: 'Configuration drift', kind: 'table' },
  { id: 'events', label: 'Events', kind: 'table' },
] as const

export type ClusterSectionId = (typeof CLUSTER_SECTIONS)[number]['id']

export const DEFAULT_CLUSTER_SECTION: ClusterSectionId = 'topology'

/** The tab a `?section=` value names; absent or unknown values mean Topology. */
export function readClusterSection(value: string | null): ClusterSectionId {
  return CLUSTER_SECTIONS.find((section) => section.id === value)?.id ?? DEFAULT_CLUSTER_SECTION
}
