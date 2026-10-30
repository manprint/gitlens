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

import { INSTANCE_SECTIONS } from '../src/features/instance/instanceSections'

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
  { path: '/indexes?tab=cleanup', name: 'Index analysis: cleanup', kind: 'fit' },
  { path: '/indexes?tab=dead-rows', name: 'Index analysis: dead rows', kind: 'fit' },
  { path: '/clusters/:clusterId', name: 'Cluster detail', kind: 'fit' },
  // One entry per instance-page tab (15.2), kind from the section registry.
  ...INSTANCE_SECTIONS.map(({ id, label, kind }): FullHdRoute => ({
    path: `/instances/:instanceId?section=${id}`,
    name: `Instance detail: ${label}`,
    kind: kind === 'charts' ? 'charts' : 'fit',
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
  { path: '/findings', name: 'Advisor findings', kind: 'fit' },
  { path: '/alerts', name: 'Alerts and events', kind: 'fit' },
  { path: '/events', name: 'Fleet event timeline', kind: 'fit' },
  { path: '/alerts/rules', name: 'Alert rules', kind: 'fit' },
  { path: '/alerts/silences', name: 'Alert silences', kind: 'fit' },
  { path: '/settings', name: 'Settings and inventory', kind: 'fit', pendingUnit: '11.1' },
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
