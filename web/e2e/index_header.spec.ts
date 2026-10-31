import { expect, test } from './fixtures'
import { headerLineProblems, INDEX_HEADER_NARROW, open, routeUrl, routeValues } from './fullhd_fit'
import { FULLHD_ROUTES, fullHdRoute, routePathname } from './fullhd_routes'
import { liveTimeout } from './visual_helpers'

/** Every Index analysis entry of the Full HD inventory: the four views, Cleanup and Dead rows. */
const INDEX_ROUTES = FULLHD_ROUTES.filter((route) => routePathname(route.path) === '/indexes')

const VIEWPORTS = [{ width: 1920, height: 1080 }, INDEX_HEADER_NARROW] as const

test('HDR-E2E-001 the Index analysis header lines keep the title whole and their parts apart, the notes on the line', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, (INDEX_ROUTES.length * VIEWPORTS.length + 1) * 30_000))
  const values = await routeValues(api)

  // The guard tests the notes only when the live data shows some: say so
  // instead of passing on a header without them.
  const inventory = routeUrl(fullHdRoute('/indexes?tab=indexes'), values)
  await open(page, inventory, VIEWPORTS[0])
  await expect(
    page.getByTestId('index-panel-notes'),
    `index-panel-notes is not visible on ${inventory} (instance ${values.instanceId}): the live data shows neither "The list is incomplete" nor an unused-index check note, so the header guard would not test the notes`,
  ).toBeVisible()

  const failures: string[] = []
  for (const route of INDEX_ROUTES) {
    const url = routeUrl(route, values)
    for (const viewport of VIEWPORTS) {
      await test.step(`${route.path} at ${viewport.width}×${viewport.height}`, async () => {
        await open(page, url, viewport)
        for (const problem of await headerLineProblems(page)) {
          failures.push(`${route.path} at ${viewport.width}×${viewport.height}: ${problem}`)
        }
      })
    }
  }
  expect(failures, failures.join('\n')).toEqual([])
})
