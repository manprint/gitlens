/**
 * Every route of the console and how it must fit a Full HD screen (D26, I-13).
 * Data only: vitest imports it (FHD-ROUTES-001), so no Playwright runtime here.
 *
 * - `fit`: toolbar, the default 20-row table and the pager without a page
 *   scrollbar at 1920×1080 and 1920×950, and never a horizontal scrollbar.
 * - `charts`: chart pages; exempt from the vertical rule only.
 * - `exempt`: no data page (only `/login` and `*`).
 *
 * `path` is a router pattern plus an optional query string; `:instanceId`,
 * `:clusterId` and `:queryid` are resolved on the live stack. `pendingUnit`
 * names the unit that restructures the page and removes the field when its own
 * fit check passes.
 */

import { ALERTS_SECTIONS, EVENTS_SECTIONS } from '../src/features/alerts/alertingSections'
import { CLUSTER_SECTIONS } from '../src/features/cluster/clusterSections'
import { INDEX_VIEWS } from '../src/features/indexes/indexViews'
import { FINDINGS_VIEWS } from '../src/features/findings/findingsViews'
import {
  ADVISOR_VIEWS,
  AUDIT_VIEWS,
  INVENTORY_VIEWS,
  SETTINGS_TABS,
} from '../src/features/settings/settingsTabs'
import {
  CATALOG_VIEW_IDS,
  INSTANCE_SECTIONS,
  INTERNALS_VIEW_IDS,
  RELATION_VIEW_IDS,
} from '../src/features/instance/instanceSections'

/**
 * Section and view of every non-default view of an instance tab (15.3). The
 * data-dependent views are left out: `replay` exists on a standby only (the
 * live check picks a primary) and the logical-replication views only with
 * subscriptions or slots; the tab entry measures whichever view opens first.
 * `slru` is kept but also depends on data: without SLRU rows the URL falls back
 * to the first Internals view.
 */
export const INSTANCE_SUBVIEWS: readonly (readonly [string, string])[] = [
  ...RELATION_VIEW_IDS.slice(1).map((view) => ['relations', view] as const),
  ...CATALOG_VIEW_IDS.slice(1).map((view) => ['catalog', view] as const),
  ...INTERNALS_VIEW_IDS.filter((view) => view === 'slru' || view === 'temp').map(
    (view) => ['internals', view] as const,
  ),
]

export type FullHdKind = 'fit' | 'charts' | 'exempt'

export interface FullHdRoute {
  path: string
  name: string
  kind: FullHdKind
  reason?: string
  pendingUnit?: string
}

export const FULLHD_ROUTES: readonly FullHdRoute[] = [
  { path: '/login', name: 'Sign in', kind: 'exempt', reason: 'no data page' },
  { path: '/', name: 'Fleet overview', kind: 'fit' },
  { path: '/queries', name: 'Slowest queries', kind: 'fit' },
  { path: '/indexes?tab=indexes', name: 'Index analysis: indexes', kind: 'fit' },
  // The other views of the Indexes tab (15.3); the inventory is the default.
  ...INDEX_VIEWS.slice(1).map(({ id, label }): FullHdRoute => ({
    path: `/indexes?tab=indexes&view=${id}`,
    name: `Index analysis: indexes, ${label}`,
    kind: 'fit',
  })),
  { path: '/indexes?tab=cleanup', name: 'Index analysis: cleanup', kind: 'fit' },
  { path: '/indexes?tab=dead-rows', name: 'Index analysis: dead rows', kind: 'fit' },
  // One entry per cluster-page tab (15.3), kind from the tab registry.
  ...CLUSTER_SECTIONS.map(({ id, label, kind }): FullHdRoute => ({
    path: `/clusters/:clusterId?section=${id}`,
    name: `Cluster detail: ${label}`,
    kind: kind === 'charts' ? 'charts' : 'fit',
  })),
  // One entry per instance-page tab (15.2), kind from the section registry.
  ...INSTANCE_SECTIONS.map(({ id, label, kind }): FullHdRoute => ({
    path: `/instances/:instanceId?section=${id}`,
    name: `Instance detail: ${label}`,
    kind: kind === 'charts' ? 'charts' : 'fit',
  })),
  // The views inside instance tabs (15.3), one table each; the first view of a tab
  // is its default. Views that only exist with data (logical replication, a
  // standby's recovery conflicts) are measured through the tab entry.
  ...INSTANCE_SUBVIEWS.map(([section, view]): FullHdRoute => ({
    path: `/instances/:instanceId?section=${section}&view=${view}`,
    name: `Instance detail: ${section}, ${view}`,
    kind: 'fit',
  })),
  { path: '/instances/:instanceId/ash', name: 'ASH and wait analysis', kind: 'charts' },
  { path: '/instances/:instanceId/queries', name: 'Query inspector', kind: 'fit' },
  {
    path: '/instances/:instanceId/queries/:queryid',
    name: 'Query detail and plan history',
    kind: 'fit',
  },
  {
    path: '/instances/:instanceId/locks',
    name: 'Locks and activity',
    kind: 'fit',
    pendingUnit: '16.3',
  },
  // One entry per findings tab (15.3); the default tab has no `view` in its URL.
  ...FINDINGS_VIEWS.map(({ id, label }, index): FullHdRoute => ({
    path: index === 0 ? '/findings' : `/findings?view=${id}`,
    name: `Advisor findings: ${label}`,
    kind: 'fit',
  })),
  // One entry per alerting tab (15.3); the default tab has no `section` in its URL.
  ...ALERTS_SECTIONS.map(({ id, label }, index): FullHdRoute => ({
    path: index === 0 ? '/alerts' : `/alerts?section=${id}`,
    name: `Alerts: ${label}`,
    kind: 'fit',
  })),
  ...EVENTS_SECTIONS.map(({ id, label }, index): FullHdRoute => ({
    path: index === 0 ? '/events' : `/events?section=${id}`,
    name: `Fleet events: ${label}`,
    kind: 'fit',
  })),
  { path: '/alerts/rules', name: 'Alert rules', kind: 'fit' },
  { path: '/alerts/silences', name: 'Alert silences', kind: 'fit' },
  // One entry per Settings tab (11.1); the default tab has no `tab` in its URL.
  ...SETTINGS_TABS.map(({ id, label }, index): FullHdRoute => ({
    path: index === 0 ? '/settings' : `/settings?tab=${id}`,
    name: `Settings: ${label}`,
    kind: 'fit',
  })),
  // The other views of the Inventory tab, one table each; Instances is the default.
  ...INVENTORY_VIEWS.slice(1).map(({ id, label }): FullHdRoute => ({
    path: `/settings?view=${id}`,
    name: `Settings: Inventory, ${label}`,
    kind: 'fit',
  })),
  // The other view of the Advisor rules tab; Rules is the default.
  ...ADVISOR_VIEWS.slice(1).map(({ id, label }): FullHdRoute => ({
    path: `/settings?tab=advisor&view=${id}`,
    name: `Settings: Advisor rules, ${label}`,
    kind: 'fit',
  })),
  // The other view of the Audit & server tab; Command audit is the default.
  ...AUDIT_VIEWS.slice(1).map(({ id, label }): FullHdRoute => ({
    path: `/settings?tab=audit&view=${id}`,
    name: `Settings: Audit & server, ${label}`,
    kind: 'fit',
  })),
  { path: '*', name: 'Not found', kind: 'exempt', reason: 'no data page' },
]

/** The pathname of an inventory entry, without its query string. */
export function routePathname(path: string): string {
  return path.split('?')[0] ?? path
}

/** The Full HD reference viewports (A2): full screen and maximised window. */
export const FULLHD_VIEWPORTS = [
  { width: 1920, height: 1080 },
  { width: 1920, height: 950 },
] as const
