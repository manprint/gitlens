import { AxeBuilder } from '@axe-core/playwright'

import { expect, test } from './fixtures'
import {
  assertFits,
  horizontallyScrollingBoxes,
  measurePage,
  open,
  routeUrl,
  routeValues,
} from './fullhd_fit'
import { FULLHD_ROUTES, routePathname } from './fullhd_routes'
import { liveTimeout } from './visual_helpers'

const QA_DIR = '../test/e2e/_artifacts/ui/qa'
const WIDE = { width: 1440, height: 900 } as const
const SCREENSHOT_VIEWPORTS = [
  { width: 1920, height: 1080 },
  { width: 1920, height: 950 },
  WIDE,
] as const

/** Routes whose first data table must hold rows on the Odoo stack under load (6.2). */
const ROWS_REQUIRED = [
  '/queries',
  '/findings',
  '/indexes?tab=indexes',
  '/indexes?tab=cleanup',
  '/indexes?tab=dead-rows',
  '/instances/:instanceId/queries',
  '/instances/:instanceId/locks?view=application',
  '/instances/:instanceId/locks?view=pglens',
]

const slug = (path: string) => path.replace(/[^a-z0-9]+/gi, '-').replace(/^-|-$/g, '') || 'fleet'

test('QA-TOUR-001 every console route is clean, accessible, Full HD fit and carries data under Odoo load', async ({
  api,
  signedInPage: page,
}) => {
  const routes = FULLHD_ROUTES.filter((route) => route.kind !== 'exempt')
  test.setTimeout(liveTimeout(0, 3 * FULLHD_ROUTES.length * 30_000))
  const values = await routeValues(api)
  const failures: string[] = []

  const consoleErrors: string[] = []
  const badResponses: string[] = []
  page.on('console', (message) => {
    // The browser logs every 4xx as a console error; 404 empties are expected (below).
    if (message.type() === 'error' && !message.text().includes('status of 404')) {
      consoleErrors.push(message.text())
    }
  })
  page.on('pageerror', (error) => consoleErrors.push(`pageerror: ${error.message}`))
  page.on('response', (response) => {
    const status = response.status()
    if (status >= 400 && status !== 404) badResponses.push(`${status} ${response.url()}`)
  })
  page.on('requestfailed', (request) => {
    const reason = request.failure()?.errorText ?? ''
    if (!reason.includes('ERR_ABORTED')) badResponses.push(`failed ${request.url()}: ${reason}`)
  })
  const drain = (where: string) => {
    for (const line of consoleErrors.splice(0)) failures.push(`${where}: console: ${line}`)
    for (const line of badResponses.splice(0)) failures.push(`${where}: network: ${line}`)
  }

  for (const route of routes) {
    const url = routeUrl(route, values)
    await test.step(route.path, async () => {
      // I-13 / VIS-FHD rules at the two Full HD viewports (also screenshots in fullhd/).
      let measures: Awaited<ReturnType<typeof assertFits>> = []
      try {
        measures = await assertFits(page, api, route)
      } catch (error) {
        failures.push(`${route.path}: fit: ${(error as Error).message.split('\n')[0]}`)
      }
      drain(route.path)

      await open(page, url, SCREENSHOT_VIEWPORTS[0])
      const severe = (await new AxeBuilder({ page }).analyze()).violations.filter(
        (violation) => violation.impact === 'serious' || violation.impact === 'critical',
      )
      for (const violation of severe) {
        failures.push(
          `${route.path}: axe ${violation.impact} ${violation.id}: ${violation.nodes.length} node(s)`,
        )
      }

      if (ROWS_REQUIRED.includes(route.path)) {
        const rows = measures[0]?.rows ?? (await measurePage(page)).rows
        if (rows === 0) failures.push(`${route.path}: no rows in the first table`)
      }
      if (routePathname(route.path).endsWith('/ash')) {
        const cards = await page.getByTestId('ash-chart-grid').locator('[data-chart-card]').count()
        if (cards < 2) failures.push(`${route.path}: ${cards} wait group card(s), expected ≥ 2`)
      }

      for (const viewport of SCREENSHOT_VIEWPORTS) {
        await open(page, url, viewport)
        await page.screenshot({
          path: `${QA_DIR}/${slug(route.path)}-${viewport.width}x${viewport.height}.png`,
        })
      }

      // The narrower laptop viewport: no page-wide horizontal scrollbar, no sideways table.
      await open(page, url, WIDE)
      const measure = await measurePage(page)
      if (measure.scrollWidth > measure.innerWidth) {
        failures.push(`${route.path} at 1440×900: horizontal scrollbar`)
      }
      const wide = await horizontallyScrollingBoxes(page)
      if (wide.length > 0 && route.kind === 'fit') {
        // Below 1600 px a wide table may scroll inside its own box (LIMITS); recorded, not failed.
        test.info().annotations.push({
          type: 'note',
          description: `${route.path} at 1440×900: ${wide.join(', ')} scroll sideways inside their box`,
        })
      }
      drain(`${route.path} (all viewports)`)
    })
  }
  expect(failures, failures.join('\n')).toEqual([])
})
