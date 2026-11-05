/**
 * The sections of the instance page, one tab each (15.2, D26): the single
 * registry used by the page, the Full HD route inventory and the tests.
 * `charts` sections are exempt from the Full HD vertical rule only.
 * `captionId` is the section's ⓘ caption; null for the sections of other route groups.
 */

export const INSTANCE_SECTIONS = [
  {
    id: 'overview',
    label: 'Overview',
    kind: 'charts',
    legacyHash: 'instance-overview',
    captionId: 'overview.section',
  },
  {
    id: 'host',
    label: 'Host',
    kind: 'charts',
    legacyHash: 'host-metrics',
    captionId: 'host.section',
  },
  {
    id: 'settings',
    label: 'Settings',
    kind: 'table',
    legacyHash: 'settings',
    captionId: 'instance.settings',
  },
  {
    id: 'relations',
    label: 'Tables and indexes',
    kind: 'table',
    legacyHash: 'instance-relations',
    captionId: 'relations.section',
  },
  {
    id: 'catalog',
    label: 'Schema objects',
    kind: 'table',
    legacyHash: 'catalog',
    captionId: 'catalog.section',
  },
  {
    id: 'logical',
    label: 'Logical replication',
    kind: 'table',
    legacyHash: 'logical-replication',
    captionId: 'logical.section',
  },
  {
    id: 'internals',
    label: 'Internals',
    kind: 'table',
    legacyHash: 'internals',
    captionId: 'internals.section',
  },
  {
    id: 'readiness',
    label: 'Readiness',
    kind: 'table',
    legacyHash: 'readiness',
    captionId: 'instance.readiness',
  },
] as const

/**
 * The views inside instance tabs (15.3), one table each, in tab order: the
 * first is the default and never written to `?view=`. Internals and logical
 * replication offer a view only when the instance has its data (`replay` on a
 * standby, `subscriptions`/`slots` when present).
 */
export const RELATION_VIEW_IDS = ['tables', 'indexes', 'table-bloat', 'index-bloat'] as const
export const CATALOG_VIEW_IDS = [
  'triggers',
  'sequences',
  'partitions',
  'functions',
  'security-definer',
  'foreign-keys',
  'extensions',
  'postgis',
] as const
export const INTERNALS_VIEW_IDS = ['databases', 'slru', 'temp', 'replay'] as const
export const LOGICAL_VIEW_IDS = ['subscriptions', 'slots'] as const

export type InstanceSectionId = (typeof INSTANCE_SECTIONS)[number]['id']

export const DEFAULT_INSTANCE_SECTION: InstanceSectionId = 'overview'

/** The section a `?section=` value names; absent or unknown values mean Overview. */
export function readInstanceSection(value: string | null): InstanceSectionId {
  return INSTANCE_SECTIONS.find((section) => section.id === value)?.id ?? DEFAULT_INSTANCE_SECTION
}

/**
 * The section a legacy in-page anchor pointed at, so old links
 * (`/instances/:id#catalog`, the Overview's `#host`) open the right tab.
 */
export function legacyHashToSection(hash: string): InstanceSectionId | null {
  const id = hash.replace(/^#/, '')
  if (id === 'host') return 'host'
  return INSTANCE_SECTIONS.find((section) => section.legacyHash === id)?.id ?? null
}
