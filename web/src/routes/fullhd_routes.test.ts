import { describe, expect, it } from 'vitest'

import { TABS } from '@/features/indexes/IndexAnalysisPage'

import { FULLHD_ROUTES, routePathname } from '../../e2e/fullhd_routes'
import { routeTable } from './index'

/** The routes whose vertical fit may be waived: chart pages only (D26). */
const CHART_ROUTES = ['/instances/:instanceId/ash']

describe('Full HD route inventory', () => {
  it('FHD-ROUTES-001 lists every router path, no unknown path and every index tab', () => {
    const routerPaths = new Set(routeTable.map((route) => route.path))
    const inventoryPaths = new Set(FULLHD_ROUTES.map((route) => routePathname(route.path)))

    for (const path of routerPaths) expect(inventoryPaths, `missing ${path}`).toContain(path)
    for (const path of inventoryPaths) expect(routerPaths, `unknown ${path}`).toContain(path)
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
})
