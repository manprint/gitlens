import { describe, expect, it } from 'vitest'

import { ALERTS_SECTIONS, EVENTS_SECTIONS } from '@/features/alerts/alertingSections'
import { CLUSTER_SECTIONS } from '@/features/cluster/clusterSections'
import { TABS } from '@/features/indexes/IndexAnalysisPage'
import { FINDINGS_VIEWS } from '@/features/findings/findingsViews'
import { INDEX_VIEWS } from '@/features/indexes/indexViews'
import {
  ADVISOR_VIEWS,
  AUDIT_VIEWS,
  INVENTORY_VIEWS,
  SETTINGS_TABS,
} from '@/features/settings/settingsTabs'
import {
  CATALOG_VIEW_IDS,
  INSTANCE_SECTIONS,
  RELATION_VIEW_IDS,
} from '@/features/instance/instanceSections'

import { FULLHD_ROUTES, INSTANCE_SUBVIEWS, routePathname } from '../../e2e/fullhd_routes'
import { routeTable } from './index'

/** The routes whose vertical fit may be waived: chart pages only (D26). */
const CHART_ROUTES = [
  '/instances/:instanceId/ash',
  '/instances/:instanceId?section=overview',
  '/instances/:instanceId?section=host',
  '/clusters/:clusterId?section=lag',
]

describe('Full HD route inventory', () => {
  it('FHD-ROUTES-001 lists every router path, no unknown path and every index tab', () => {
    const routerPaths = new Set(routeTable.map((route) => route.path))
    const inventoryPaths = new Set(FULLHD_ROUTES.map((route) => routePathname(route.path)))

    for (const path of routerPaths) expect(inventoryPaths, `missing ${path}`).toContain(path)
    for (const path of inventoryPaths) expect(routerPaths, `unknown ${path}`).toContain(path)
    for (const { id } of CLUSTER_SECTIONS) {
      expect(FULLHD_ROUTES.map((route) => route.path)).toContain(
        `/clusters/:clusterId?section=${id}`,
      )
    }
    for (const { id } of INSTANCE_SECTIONS) {
      expect(FULLHD_ROUTES.map((route) => route.path)).toContain(
        `/instances/:instanceId?section=${id}`,
      )
    }
    for (const [page, sections] of [
      ['/alerts', ALERTS_SECTIONS],
      ['/events', EVENTS_SECTIONS],
    ] as const) {
      for (const [index, { id }] of sections.entries()) {
        expect(FULLHD_ROUTES.map((route) => route.path)).toContain(
          index === 0 ? page : `${page}?section=${id}`,
        )
      }
    }
    for (const [section, views] of [
      ['relations', RELATION_VIEW_IDS],
      ['catalog', CATALOG_VIEW_IDS],
    ] as const) {
      for (const view of views.slice(1)) {
        expect(INSTANCE_SUBVIEWS).toContainEqual([section, view])
      }
    }
    for (const [section, view] of INSTANCE_SUBVIEWS) {
      expect(FULLHD_ROUTES.map((route) => route.path)).toContain(
        `/instances/:instanceId?section=${section}&view=${view}`,
      )
    }
    for (const [index, { id }] of FINDINGS_VIEWS.entries()) {
      expect(FULLHD_ROUTES.map((route) => route.path)).toContain(
        index === 0 ? '/findings' : `/findings?view=${id}`,
      )
    }
    for (const { id } of INDEX_VIEWS.slice(1)) {
      expect(FULLHD_ROUTES.map((route) => route.path)).toContain(`/indexes?tab=indexes&view=${id}`)
    }
    for (const [index, { id }] of SETTINGS_TABS.entries()) {
      expect(FULLHD_ROUTES.map((route) => route.path)).toContain(
        index === 0 ? '/settings' : `/settings?tab=${id}`,
      )
    }
    for (const { id } of INVENTORY_VIEWS.slice(1)) {
      expect(FULLHD_ROUTES.map((route) => route.path)).toContain(`/settings?view=${id}`)
    }
    for (const { id } of AUDIT_VIEWS.slice(1)) {
      expect(FULLHD_ROUTES.map((route) => route.path)).toContain(`/settings?tab=audit&view=${id}`)
    }
    for (const { id } of ADVISOR_VIEWS.slice(1)) {
      expect(FULLHD_ROUTES.map((route) => route.path)).toContain(`/settings?tab=advisor&view=${id}`)
    }
    for (const { value } of TABS) {
      expect(FULLHD_ROUTES.map((route) => route.path)).toContain(`/indexes?tab=${value}`)
    }
    for (const route of FULLHD_ROUTES.filter((entry) => entry.kind === 'exempt')) {
      expect(route.reason, route.path).toBeTruthy()
    }
    expect(new Set(FULLHD_ROUTES.map((route) => route.path)).size).toBe(FULLHD_ROUTES.length)
  })

  it('FHD-ROUTES-002 only chart pages are exempt from the vertical rule', () => {
    const charts = FULLHD_ROUTES.filter((route) => route.kind === 'charts').map(
      (route) => route.path,
    )
    expect(charts.every((path) => CHART_ROUTES.includes(path))).toBe(true)
  })

  it('FHD-ROUTES-003 only the sign-in page and the not-found page are exempt', () => {
    expect(
      FULLHD_ROUTES.filter((route) => route.kind === 'exempt').map((route) => route.path),
    ).toEqual(['/login', '*'])
  })

  it('FHD-ROUTES-004 no entry waits for an owning unit any more', () => {
    expect(
      FULLHD_ROUTES.filter((route) => route.pendingUnit !== undefined).map((route) => route.path),
    ).toEqual([])
  })
})
