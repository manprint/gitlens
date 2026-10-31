import { mkdir, writeFile } from 'node:fs/promises'
import { dirname } from 'node:path'

import { expect, type APIRequestContext, type Page } from '@playwright/test'

import { FULLHD_ROUTES, FULLHD_VIEWPORTS, routePathname, type FullHdRoute } from './fullhd_routes'
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
  /** Bottom edge of the first table pager in the viewport (`null`: no pager). */
  pagerBottom: number | null
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
      pagerBottom: (() => {
        const nav = document.querySelector('[data-table-card] nav[aria-label$="pagination"]')
        return nav ? Math.round(nav.getBoundingClientRect().bottom) : null
      })(),
    }
  })
}

/** The header lines that are one row at Full HD: Index analysis panels and sections. */
const HEADER_LINES = '[data-testid="index-panel-header"], [data-testid="section-header"]'

/**
 * Lists what is wrong with the header lines of the open page (bug-HDR), one
 * self-contained line per problem naming the check, the element and the
 * pixels: two parts of a line that overlap (`overlap`), a part whose content
 * spills out of its box (`child spill`), a part that leaves the line's box
 * (`outside line`), a title another part covers (`h2 covered`), a title cut
 * short (`h2 clipped`), or, from 1920 px, an Index analysis panel header whose
 * parts wrapped to a second row (`wrapped`; D26 keeps it one line there).
 * Text truncated inside a part is clipped by that part and is not reported;
 * a part that clips its own overflow (`overflow-x` not `visible`) is not a
 * spill either.
 */
export async function headerLineProblems(page: Page): Promise<string[]> {
  return page.evaluate((selector) => {
    const problems: string[] = []
    const visible = (element: Element) => {
      const box = element.getBoundingClientRect()
      return box.width > 0 && box.height > 0 && getComputedStyle(element).visibility !== 'hidden'
    }
    // The element as a reader of the log finds it: test id, or tag and the start of its text.
    const label = (element: Element) => {
      const testId = element.getAttribute('data-testid')
      const text = (element.textContent ?? '').replace(/\s+/g, ' ').trim().slice(0, 40)
      return testId === null
        ? `<${element.tagName.toLowerCase()}> "${text}"`
        : `[${testId}] "${text}"`
    }
    const span = (box: DOMRect) => `x ${Math.round(box.left)}..${Math.round(box.right)}`
    // How far two boxes overlap, or 0; both axes, so parts stacked on two rows pass.
    const overlap = (a: DOMRect, b: DOMRect) => {
      const across = Math.min(a.right, b.right) - Math.max(a.left, b.left)
      const down = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top)
      return across > 1 && down > 1 ? Math.round(across) : 0
    }
    document.querySelectorAll(selector).forEach((line) => {
      if (!visible(line)) return
      const where = `line ${label(line.querySelector('h2') ?? line)}`
      const style = getComputedStyle(line)
      const lineBox = line.getBoundingClientRect()
      const inner = {
        left: lineBox.left + parseFloat(style.paddingLeft),
        right: lineBox.right - parseFloat(style.paddingRight),
      }
      const parts = [...line.children].filter(visible)
      const boxes = parts.map((part) => part.getBoundingClientRect())
      // From 1920 px an Index analysis header is one row: its parts are centred
      // on the same line (items-center), so their middles agree within 2 px.
      if (window.innerWidth >= 1920 && line.getAttribute('data-testid') === 'index-panel-header') {
        const middle = (box: DOMRect) => (box.top + box.bottom) / 2
        const first = boxes[0]
        parts.forEach((part, i) => {
          const box = boxes[i]!
          if (first !== undefined && Math.abs(middle(box) - middle(first)) > 2) {
            problems.push(
              `wrapped: ${where}: ${label(part)} (y ${Math.round(box.top)}..${Math.round(box.bottom)}) is not on the row of ${label(parts[0]!)} (y ${Math.round(first.top)}..${Math.round(first.bottom)}) at ${window.innerWidth} px`,
            )
          }
        })
      }
      parts.forEach((part, i) => {
        const box = boxes[i]!
        boxes.slice(i + 1).forEach((other, offset) => {
          const px = overlap(box, other)
          if (px > 0) {
            const next = parts[i + 1 + offset]!
            problems.push(
              `overlap: ${where}: ${label(part)} (${span(box)}) and ${label(next)} (${span(other)}) overlap by ${px} px`,
            )
          }
        })
        if (
          getComputedStyle(part).overflowX === 'visible' &&
          part.scrollWidth > part.clientWidth + 1
        ) {
          problems.push(
            `child spill: ${where}: ${label(part)} scrollWidth ${part.scrollWidth} > clientWidth ${part.clientWidth} (${part.scrollWidth - part.clientWidth} px)`,
          )
        }
        if (box.left < inner.left - 1 || box.right > inner.right + 1) {
          problems.push(
            `outside line: ${where}: ${label(part)} (${span(box)}) leaves the line x ${Math.round(inner.left)}..${Math.round(inner.right)}`,
          )
        }
      })
      line.querySelectorAll('h2').forEach((title) => {
        if (title.scrollWidth > title.clientWidth + 1) {
          problems.push(
            `h2 clipped: ${where}: scrollWidth ${title.scrollWidth} > clientWidth ${title.clientWidth}`,
          )
        }
        const titleBox = title.getBoundingClientRect()
        parts.forEach((part, i) => {
          if (part.contains(title)) return
          const px = overlap(titleBox, boxes[i]!)
          if (px > 0) {
            problems.push(
              `h2 covered: ${where} (${span(titleBox)}): ${label(part)} (${span(boxes[i]!)}) covers ${px} px`,
            )
          }
        })
      })
    })
    return problems
  }, HEADER_LINES)
}

/**
 * The Index analysis header lines also hold at 1600×900, where they may wrap
 * (outside the reference viewports, overview A2) but never overlap or spill.
 */
export const INDEX_HEADER_NARROW = { width: 1600, height: 900 } as const

export async function open(page: Page, url: string, viewport: { width: number; height: number }) {
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
 * table box scrolling at the default page size, and the pager on screen; for
 * every kind, header lines whose parts do not overlap and whose titles are whole,
 * and for the Index analysis routes the same header check at 1600×900.
 * Returns the measurements.
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
    // Every header line keeps its parts apart and its title whole (bug-HDR).
    const headerProblems = await headerLineProblems(page)
    expect(headerProblems, `${where}: header line: ${headerProblems.join(' | ')}`).toEqual([])
    if (route.kind === 'fit') {
      expect(measure.scrollHeight, `${where}: page scrollbar`).toBeLessThanOrEqual(
        measure.innerHeight,
      )
      // The pager, when the table has one, is on screen.
      if (measure.pagerBottom !== null) {
        expect(measure.pagerBottom, `${where}: pager below the fold`).toBeLessThanOrEqual(
          measure.innerHeight,
        )
      }
    }
  }
  if (routePathname(route.path) === '/indexes') {
    // The header guard also at 1600×900 (bug-HDR): no fit there, the lines only.
    await open(page, url, INDEX_HEADER_NARROW)
    const where = `${route.path} at ${INDEX_HEADER_NARROW.width}×${INDEX_HEADER_NARROW.height}`
    const headerProblems = await headerLineProblems(page)
    expect(headerProblems, `${where}: header line: ${headerProblems.join(' | ')}`).toEqual([])
  }
  return measures
}
