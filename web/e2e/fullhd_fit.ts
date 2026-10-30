import { mkdir, writeFile } from 'node:fs/promises'
import { dirname } from 'node:path'

import { expect, type APIRequestContext, type Page } from '@playwright/test'

import { FULLHD_ROUTES, FULLHD_VIEWPORTS, type FullHdRoute } from './fullhd_routes'
import { pickInstance } from './visual_helpers'

export const FULLHD_MEASURE_FILE = '../test/e2e/_artifacts/ui/fullhd-measure.json'

/** What a Full HD page spends its height on, in CSS pixels (`null`: not on the page). */
export interface FullHdMeasure {
  route: string
  url: string
  viewport: { width: number; height: number }
  scrollHeight: number
  scrollWidth: number
  innerHeight: number
  innerWidth: number
  shellHeader: number | null
  pageHeader: number | null
  tabBar: number | null
  toolbar: number | null
  tableHeader: number | null
  row: number | null
  rows: number
  pager: number | null
  /** From the top of `main` to the top of the first data table. */
  aboveTable: number | null
  /** Main's vertical padding (top + bottom). */
  mainPadding: number
  /** Data-table scroll boxes whose content scrolls vertically at the default page size. */
  scrollingTableBoxes: number
}

interface RouteValues {
  instanceId: string
  clusterId: string
  queryid: string
}

/** The live instance, its cluster and one of its statements, for the route patterns. */
export async function routeValues(api: APIRequestContext): Promise<RouteValues> {
  const instanceId = await pickInstance(api)
  const clusters = (await (await api.get('/api/v1/clusters')).json()) as {
    cluster_id: string
    instances: { instance_id: string }[]
  }[]
  const cluster =
    clusters.find((candidate) =>
      candidate.instances.some((instance) => instance.instance_id === instanceId),
    ) ?? clusters[0]
  if (!cluster) throw new Error('routeValues: the server lists no cluster')
  const statements = (await (
    await api.get(`/api/v1/statements?instance_id=${instanceId}&limit=1`)
  ).json()) as { statements?: { queryid: string | number }[] }
  const queryid = String(statements.statements?.[0]?.queryid ?? '0')
  return { instanceId, clusterId: cluster.cluster_id, queryid }
}

/** The URL of an inventory entry on the live stack. */
export function routeUrl(route: FullHdRoute, values: RouteValues): string {
  const url = route.path
    .replace(':instanceId', values.instanceId)
    .replace(':clusterId', values.clusterId)
    .replace(':queryid', values.queryid)
  // The index pages pick the server from the URL; the others ignore the extra parameter.
  return url.startsWith('/indexes') || url.startsWith('/queries')
    ? `${url}${url.includes('?') ? '&' : '?'}instance=${values.instanceId}`
    : url
}

async function settle(page: Page): Promise<void> {
  await page.waitForLoadState('networkidle')
  // Loading skeletons and status lines go away once the queries land.
  await expect(page.locator('[aria-busy="true"]')).toHaveCount(0, { timeout: 30_000 })
}

/** Measures the open page; the caller has set the viewport and waited for the data. */
async function measurePage(page: Page): Promise<Omit<FullHdMeasure, 'route' | 'url' | 'viewport'>> {
  return page.evaluate(() => {
    const height = (element: Element | null) =>
      element ? Math.round(element.getBoundingClientRect().height) : null
    const main = document.querySelector('main')
    const dataTable = [...document.querySelectorAll('main table')].find(
      (table) =>
        !(table.getAttribute('aria-label') ?? '').endsWith(' data') &&
        table.closest('[data-topology-edge-table]') === null,
    )
    const firstRow = dataTable?.querySelector(':scope > tbody > tr') ?? null
    const boxes = [...document.querySelectorAll('[data-testid="data-table-scroll"]')]
    const mainStyle = main ? getComputedStyle(main) : null
    return {
      scrollHeight: document.scrollingElement?.scrollHeight ?? 0,
      scrollWidth: document.scrollingElement?.scrollWidth ?? 0,
      innerHeight: window.innerHeight,
      innerWidth: window.innerWidth,
      shellHeader: height(main?.parentElement?.querySelector(':scope > header') ?? null),
      pageHeader: height(main?.querySelector('header') ?? null),
      tabBar: height(main?.querySelector('[role="tablist"]') ?? null),
      toolbar: height(
        dataTable?.closest('[data-table-card]')?.querySelector('[role="search"]')?.parentElement ??
          null,
      ),
      tableHeader: height(dataTable?.querySelector('thead') ?? null),
      row: height(firstRow),
      rows: dataTable?.querySelectorAll(':scope > tbody > tr').length ?? 0,
      pager: height(
        dataTable?.closest('[data-table-card]')?.querySelector('nav[aria-label$="pagination"]') ??
          null,
      ),
      aboveTable:
        main && dataTable
          ? Math.round(dataTable.getBoundingClientRect().top - main.getBoundingClientRect().top)
          : null,
      mainPadding: mainStyle
        ? Math.round(parseFloat(mainStyle.paddingTop) + parseFloat(mainStyle.paddingBottom))
        : 0,
      scrollingTableBoxes: boxes.filter((box) => box.scrollHeight > box.clientHeight + 1).length,
    }
  })
}

async function open(page: Page, url: string, viewport: { width: number; height: number }) {
  await page.setViewportSize(viewport)
  await page.goto(url)
  await settle(page)
}

/**
 * Measures every non-exempt route at both Full HD viewports and writes the
 * numbers to `fullhd-measure.json` (15.1 S1, 15.4).
 */
export async function measureRoutes(page: Page, api: APIRequestContext): Promise<FullHdMeasure[]> {
  const values = await routeValues(api)
  const measures: FullHdMeasure[] = []
  for (const route of FULLHD_ROUTES) {
    if (route.kind === 'exempt') continue
    const url = routeUrl(route, values)
    for (const viewport of FULLHD_VIEWPORTS) {
      await open(page, url, viewport)
      measures.push({ route: route.path, url, viewport, ...(await measurePage(page)) })
    }
  }
  await mkdir(dirname(FULLHD_MEASURE_FILE), { recursive: true })
  await writeFile(FULLHD_MEASURE_FILE, `${JSON.stringify(measures, null, 2)}\n`)
  return measures
}

/**
 * Asserts the Full HD fit of one inventory entry at both viewports (D26).
 * Signature `assertFits(page, api, route)`: the API context resolves the
 * route's `:instanceId`, `:clusterId` and `:queryid` on the live stack.
 * no horizontal scrollbar for any kind; for `fit` no page scrollbar and no
 * table box scrolling at the default page size. Returns the measurements.
 */
export async function assertFits(
  page: Page,
  api: APIRequestContext,
  route: FullHdRoute,
): Promise<FullHdMeasure[]> {
  const url = routeUrl(route, await routeValues(api))
  const measures: FullHdMeasure[] = []
  for (const viewport of FULLHD_VIEWPORTS) {
    await open(page, url, viewport)
    const measure = { route: route.path, url, viewport, ...(await measurePage(page)) }
    measures.push(measure)
    const where = `${route.path} at ${viewport.width}×${viewport.height}`
    expect(measure.scrollWidth, `${where}: horizontal scrollbar`).toBeLessThanOrEqual(
      measure.innerWidth,
    )
    // No table card scrolls at the default page size, whatever the kind (I-13).
    expect(measure.scrollingTableBoxes, `${where}: table box scrolls`).toBe(0)
    if (route.kind === 'fit') {
      expect(measure.scrollHeight, `${where}: page scrollbar`).toBeLessThanOrEqual(
        measure.innerHeight,
      )
    }
  }
  return measures
}
