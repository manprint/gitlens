import { AxeBuilder } from '@axe-core/playwright'
import { execFile } from 'node:child_process'
import { join } from 'node:path'
import { promisify } from 'node:util'

import type { APIRequestContext, Page, Response as PageResponse } from '@playwright/test'

import { expect, test } from './fixtures'
import { assertFits, measureRoutes, routeUrl, routeValues } from './fullhd_fit'
import { FULLHD_ROUTES, fullHdRoute, routePathname } from './fullhd_routes'
import { STACK_DIR, stackGoogleChatSecrets } from './notification_secrets'
import { BUDGET } from './qa/budget'
import { database as qaDatabase, deadlocks, lockPair, query, runCleanups } from './qa/pg'
import { run } from './qa/stack'
import { INSTANCE_SECTIONS } from '../src/features/instance/instanceSections'
import {
  ADVISOR_VIEWS,
  AUDIT_VIEWS,
  DEFAULT_SETTINGS_TAB,
  INVENTORY_VIEWS,
  SETTINGS_TABS,
} from '../src/features/settings/settingsTabs'
import { deadlockRate } from '../src/lib/activity'
import { CAPTIONS } from '../src/lib/captions'
import {
  ARTIFACT_DIR,
  canvasNotBlank,
  chartCompleteness,
  chartSeriesNames,
  collectConsoleErrors,
  completenessOf,
  liveTimeout,
  pickHostInstance,
  pickInstance,
  shoot,
} from './visual_helpers'

/** The card range line; a card with many null buckets adds a gap count. */
const RANGE_LINE = /UTC · step \d+[smhd](?: · \d+ gaps? or counter resets?)?$/

/** Element screenshots: the sticky app header and chart nav scroll away instead of overlaying the region. */
const UNSTICK = 'header, nav[aria-label="Chart groups"] { position: static !important; }'

test('VIS-000 the instance page loads for the picked instance without browser errors', async ({
  api,
  signedInPage: page,
}) => {
  // VISTMO-001: the timeout helper adds one minute of slack to the waits it is given.
  expect(liveTimeout(420_000, 135_000)).toBe(615_000)
  expect(liveTimeout()).toBe(60_000)
  expect(
    completenessOf(['Unknown', 'Unknown', 'Unknown', '1', '2', '3', '4', '5', '6', '7']),
  ).toEqual({
    total: 10,
    nonNull: 7,
    ratio: 0.7,
    leadingNull: 3,
    observedRatio: 1,
  })

  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  await page.goto(`/instances/${instanceId}`)
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
  await expect(page.getByRole('main')).toBeVisible()
  await shoot(page, 'vis-000-instance')
  browserErrors.assertClean()
})

for (const range of ['1h', '6h'] as const) {
  test(`VIS-010 ${range} the TPS chart is complete and readable`, async ({
    api,
    signedInPage: page,
  }) => {
    test.setTimeout(liveTimeout(0, 3 * 30_000))
    const browserErrors = collectConsoleErrors(page)
    const instanceId = await pickInstance(api)
    await page.goto(`/instances/${instanceId}?range=${range}`)

    const card = page.getByRole('region', { name: 'Transactions per second' })
    await expect(card).toBeVisible()
    await expect(card.getByText(RANGE_LINE)).toBeVisible()
    // The data table fills once the metric query answers.
    await expect
      .poll(async () => (await chartCompleteness(page, 'Transactions per second')).total, {
        timeout: 3 * 30_000,
      })
      .toBeGreaterThan(0)

    const completeness = await chartCompleteness(page, 'Transactions per second', 'Commits')
    const history = completeness.leadingNull / completeness.total
    if (range === '6h' && history > 0.5) {
      test.skip(true, `UNVERIFIED: history ${((1 - history) * 6).toFixed(1)} h`)
    }
    expect(history).toBeLessThanOrEqual(0.5)
    expect(completeness.observedRatio).toBeGreaterThanOrEqual(0.95)
    expect(await canvasNotBlank(card)).toBe(true)

    await card.scrollIntoViewIfNeeded()
    await card.screenshot({
      path: `${ARTIFACT_DIR}/vis-010-tps-${range}-card.png`,
    })
    await shoot(page, `vis-010-tps-${range}`)
    browserErrors.assertClean()
  })
}

const VIS015_WIDTHS = [1440, 1024, 768, 390] as const

interface ClusterRow {
  cluster_id: string
  instance_count: number
  instances: { instance_id: string }[]
}

test('VIS-015 ASH chart labels stay short and readable at every width', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 4 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  // The single stacked chart; 1.5 made one chart per wait group the default.
  await page.goto(`/instances/${instanceId}/ash?range=1h&view=stacked`)

  const card = page.getByRole('region', { name: /average active backends$/ }).first()
  await expect(card).toBeVisible({ timeout: 4 * 30_000 })
  await expect(card.getByText(/UTC · step \d+[smhd]$/)).toBeVisible()
  for (const width of VIS015_WIDTHS) {
    await page.setViewportSize({ width, height: 1000 })
    await card.scrollIntoViewIfNeeded()
    await expect.poll(() => canvasNotBlank(card), { timeout: 30_000 }).toBe(true)
    await card.screenshot({ path: `${ARTIFACT_DIR}/vis-015-ash-${width}.png` })
  }
  browserErrors.assertClean()
})

test('VIS-015 replication lag shows a readable chart or states that there is no lag', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 4 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  const response = await api.get('/api/v1/clusters')
  expect(response.ok()).toBe(true)
  const clusters = (await response.json()) as ClusterRow[]
  const cluster = clusters.find((row) =>
    row.instances.some((instance) => instance.instance_id === instanceId),
  )
  expect(cluster, 'the picked instance belongs to a cluster').toBeDefined()
  if (!cluster) return

  // `section=lag` is ignored until 15.3 adds the cluster tabs.
  await page.goto(`/clusters/${cluster.cluster_id}?section=lag&range=1h`)
  // The cluster page wraps LagSection in a section of the same name; take LagSection itself.
  const lag = page.locator('section[aria-labelledby="replication-lag-heading"]')
  await expect(lag).toBeVisible({ timeout: 4 * 30_000 })
  for (const width of VIS015_WIDTHS) {
    await page.setViewportSize({ width, height: 1000 })
    await lag.scrollIntoViewIfNeeded()
    if (cluster.instance_count <= 1) {
      await expect(lag.getByText(/No replication lag/)).toBeVisible()
    } else {
      const edge = lag.getByRole('article').first()
      await expect(edge.getByText(/UTC · step \d+[smhd]$/)).toBeVisible()
      await expect.poll(() => canvasNotBlank(edge), { timeout: 30_000 }).toBe(true)
    }
    await lag.screenshot({ path: `${ARTIFACT_DIR}/vis-015-lag-${width}.png` })
  }
  browserErrors.assertClean()
})

const VIS016_WIDTHS = [1440, 1024, 390] as const

test('VIS-016 ASH shows one readable chart per wait group with a clear legend', async ({
  api,
  signedInPage: page,
}) => {
  // The load runs its 60 s warm-up before its 360 s, then cleans up: 480 s covers it.
  test.setTimeout(liveTimeout(480_000, 3 * 45_000))
  // Odoo traffic for six minutes, so at least two wait groups are sampled.
  const load = await promisify(execFile)(
    'python3',
    [join(STACK_DIR, 'load', 'rpc_load.py'), '--seconds', '360', '--warmup', '60'],
    { timeout: 480_000 },
  )
  expect(load.stdout).toMatch(/calls=\d+/)

  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  await page.goto(`/instances/${instanceId}/ash?range=1h`)

  const grid = page.getByTestId('ash-chart-grid')
  await expect(grid).toBeVisible({ timeout: 45_000 })
  const cards = grid.locator('[data-chart-card]')
  const count = await cards.count()
  expect(count, 'wait groups present after the load').toBeGreaterThanOrEqual(2)
  expect(await cards.locator('h3').allTextContents()).toHaveLength(count)

  for (const width of VIS016_WIDTHS) {
    await page.setViewportSize({ width, height: 1080 })
    for (let index = 0; index < count; index += 1) {
      const card = cards.nth(index)
      await card.scrollIntoViewIfNeeded()
      await expect(card.locator('canvas')).toHaveCount(1)
      await expect.poll(() => canvasNotBlank(card), { timeout: 45_000 }).toBe(true)
      const legend = card.getByRole('list', { name: / legend$/ })
      const rows = legend.getByRole('button')
      expect(await rows.count()).toBeGreaterThan(0)
      for (const text of await rows.allTextContents()) expect(text).toMatch(/ — \S/)
    }
    await shoot(page, `vis-016-ash-${width}`)
    for (let index = 0; index < count; index += 1) {
      await cards.nth(index).screenshot({
        path: `${ARTIFACT_DIR}/vis-016-ash-${width}-card-${index}.png`,
      })
    }
  }

  await page.setViewportSize({ width: 1440, height: 1080 })
  const layout = page.getByRole('radiogroup', { name: 'Chart layout' })
  await layout.getByText('Stacked', { exact: true }).click()
  await expect(page).toHaveURL(/view=stacked/)
  await expect(grid).toBeHidden()
  await expect(page.locator('canvas')).toHaveCount(1)
  await layout.getByText('Separate', { exact: true }).click()
  await expect(page).not.toHaveURL(/view=/)
  await expect(grid.locator('[data-chart-card]')).toHaveCount(count)

  // Shared zoom: dragging the first chart's slider handle narrows every chart.
  // The slider zooms in place (its event carries percentages, so the URL range
  // stays); echarts.connect redraws the other charts on the new window.
  const pixels = (index: number) =>
    cards
      .nth(index)
      .locator('canvas')
      .evaluate((element) => (element as HTMLCanvasElement).toDataURL())
  const before = [await pixels(0), await pixels(1)]
  const first = cards.first()
  await first.scrollIntoViewIfNeeded()
  const box = await first.locator('canvas').boundingBox()
  expect(box).not.toBeNull()
  if (!box) return
  const sliderY = box.y + box.height - 8 - 15
  // The slider starts where the plot does, after the y-axis labels: find its
  // left handle as the first painted pixel of the slider row.
  const handleX = await first.locator('canvas').evaluate((element, offsetY) => {
    const canvas = element as HTMLCanvasElement
    const scale = canvas.width / canvas.getBoundingClientRect().width
    const context = canvas.getContext('2d')
    if (!context) return -1
    const row = Math.round((canvas.getBoundingClientRect().height - offsetY) * scale)
    const { data } = context.getImageData(0, row, canvas.width, 1)
    const background = data.slice(0, 4).join()
    for (let x = 1; x < canvas.width; x += 1) {
      if (data.slice(x * 4, x * 4 + 4).join() !== background) return x / scale
    }
    return -1
  }, 8 + 15)
  expect(handleX).toBeGreaterThan(0)
  await page.mouse.move(box.x + handleX + 2, sliderY)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width * 0.6, sliderY, { steps: 12 })
  await page.mouse.up()
  await expect.poll(() => pixels(0), { timeout: 10_000 }).not.toBe(before[0])
  await expect.poll(() => pixels(1), { timeout: 10_000 }).not.toBe(before[1])
  await cards.nth(1).screenshot({
    path: `${ARTIFACT_DIR}/vis-016-ash-zoomed-card-1.png`,
  })
  await shoot(page, 'vis-016-ash-zoomed')
  browserErrors.assertClean()
})

const VIS020_WIDTHS = [1440, 1024, 390] as const
const HOST_CARDS = ['CPU used (%)', 'Load average', 'Memory', 'Swap', 'Filesystem'] as const

test('VIS-020 the host section charts CPU, load, memory, swap and filesystem', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 3 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickHostInstance(api)
  // Host is its own tab of the instance page (15.2).
  await page.goto(`/instances/${instanceId}?range=1h&section=host`)

  const section = page.getByRole('region', { name: 'Host metrics' })
  await expect(section).toBeVisible()
  for (const name of ['Filesystem total', 'Filesystem free']) {
    const tile = section.getByRole('article', { name, exact: true })
    await expect(tile).toBeVisible()
    await expect(tile).not.toContainText('—')
    await expect(tile).not.toContainText('Not visible to the agent')
  }

  const grid = page.getByTestId('host-grid')
  const cards = grid.locator('[data-chart-card]')
  await expect(cards).toHaveCount(HOST_CARDS.length)
  for (const title of HOST_CARDS) {
    await expect(grid.getByRole('heading', { level: 3, name: title })).toBeVisible()
  }

  for (const label of ['CPU used (%)', 'Memory']) {
    await expect
      .poll(async () => (await chartCompleteness(page, label)).total, { timeout: 3 * 30_000 })
      .toBeGreaterThan(0)
    const completeness = await chartCompleteness(page, label)
    expect(completeness.leadingNull / completeness.total).toBeLessThanOrEqual(0.5)
    expect(completeness.observedRatio, label).toBeGreaterThanOrEqual(0.9)
  }

  // Used memory pairs two queries; it must not come out empty.
  const usedMemory = await page
    .getByRole('table', { name: 'Memory data', exact: true })
    .locator('tbody tr')
    .evaluateAll(
      (rows) =>
        rows.filter(
          (row) =>
            row.querySelector('th')?.textContent === 'Used' &&
            row.querySelectorAll('td').item(1)?.textContent !== 'Unknown',
        ).length,
    )
  expect(usedMemory).toBeGreaterThan(0)

  // Every card holds a drawn chart, except swap on a host without swap, which explains it.
  for (const title of HOST_CARDS) {
    const card = grid.getByRole('region', { name: title })
    await card.scrollIntoViewIfNeeded()
    const note = card.getByRole('note')
    if (title === 'Swap' && (await note.count()) > 0) {
      await expect(note).toContainText('swap total of 0 bytes')
      continue
    }
    await expect(card.locator('canvas').first()).toBeVisible()
    expect(await canvasNotBlank(card), title).toBe(true)
  }

  for (const width of VIS020_WIDTHS) {
    await page.setViewportSize({ width, height: 1080 })
    await grid.scrollIntoViewIfNeeded()
    await grid.screenshot({
      path: `${ARTIFACT_DIR}/vis-020-host-grid-${width}.png`,
      style: UNSTICK,
    })
  }
  await shoot(page, 'vis-020-host')
  browserErrors.assertClean()
})

/** The Overview chart groups (D4) and their cards, in page order. */
const OVERVIEW_GROUPS = [
  {
    id: 'workload',
    title: 'Workload',
    cards: ['Transactions per second', 'Connections', 'Deadlocks and conflicts'],
  },
  {
    id: 'memory-io',
    title: 'Memory & I/O',
    cards: ['Cache hit ratio', 'Blocks read vs hit', 'Temp files and bytes'],
  },
  {
    id: 'wal-checkpoints',
    title: 'WAL & checkpoints',
    cards: ['WAL generation', 'Checkpoints', 'Checkpoint write and sync time'],
  },
  {
    id: 'queries',
    title: 'Queries',
    cards: [
      'Calls per second',
      'Execution time per second',
      'Rows per second',
      'Statement block reads',
      'Statement temp blocks',
    ],
  },
  {
    id: 'tables-indexes',
    title: 'Tables & indexes',
    cards: ['Sequential vs index scans', 'Tuple churn', 'Dead tuples', 'Index scans', 'Index size'],
  },
] as const
const VIS030_WIDTHS = [1440, 1024, 390] as const

/** The card region of `title`; its heading may carry a unit hint in parentheses. */
function overviewCard(page: Page, title: string) {
  return page
    .getByTestId('overview-charts')
    .getByRole('region', { name: new RegExp(`^${title}( ?\\(|$)`) })
}

test('VIS-030 the Overview shows five chart groups, complete and separated from the tiles', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 3 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  await page.goto(`/instances/${instanceId}?range=1h`)

  const charts = page.getByTestId('overview-charts')
  await expect(charts).toBeVisible({ timeout: 30_000 })
  const groups = charts.locator('section[id]').filter({ has: page.locator('h2') })
  await expect(groups).toHaveCount(OVERVIEW_GROUPS.length)
  expect(await groups.evaluateAll((items) => items.map((item) => item.id))).toEqual(
    OVERVIEW_GROUPS.map((group) => group.id),
  )

  // Separation: at least 48 px from the last tile row to the first group.
  const tile = page.getByRole('article', { name: 'Deadlocks in range' })
  const tileBox = await tile.boundingBox()
  const firstGroup = await charts.getByRole('region', { name: 'Workload' }).boundingBox()
  expect(
    tileBox && firstGroup && firstGroup.y - (tileBox.y + tileBox.height),
  ).toBeGreaterThanOrEqual(48)

  for (const group of OVERVIEW_GROUPS) {
    const slowGroup = group.id === 'tables-indexes'
    for (const title of group.cards) {
      const card = overviewCard(page, title)
      await card.scrollIntoViewIfNeeded()
      await expect(card.getByText(RANGE_LINE)).toBeVisible()
      await expect
        .poll(async () => (await chartCompleteness(page, title)).total, { timeout: 3 * 30_000 })
        .toBeGreaterThan(0)
      for (const series of await chartSeriesNames(page, title)) {
        const completeness = await chartCompleteness(page, title, series)
        expect(completeness.ratio, `${title} / ${series} completeness`).toBeGreaterThanOrEqual(
          slowGroup ? 0.5 : 0.9,
        )
      }
      await expect.poll(() => canvasNotBlank(card), { timeout: 30_000, message: title }).toBe(true)
    }
  }

  // The nav scrolls to each group and to the Host section.
  const nav = page.getByRole('navigation', { name: 'Chart groups' })
  await page.evaluate(() => window.scrollTo(0, 0))
  for (const group of OVERVIEW_GROUPS) {
    await nav.getByRole('link', { name: group.title, exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`#${group.id}$`))
    await expect(charts.getByRole('heading', { level: 2, name: group.title })).toBeInViewport()
    await expect(nav).toBeInViewport()
  }
  // The Host link switches to the Host tab (15.2); the Overview tab brings the charts back.
  await nav.getByRole('link', { name: 'Host', exact: true }).click()
  await expect(page).toHaveURL(/[?&]section=host/)
  await expect(page.getByRole('heading', { level: 2, name: 'Host metrics' })).toBeInViewport()
  await page.getByRole('tab', { name: 'Overview' }).click()
  await expect(charts).toBeVisible({ timeout: 30_000 })

  for (const width of VIS030_WIDTHS) {
    await page.setViewportSize({ width, height: 1080 })
    for (const group of OVERVIEW_GROUPS) {
      const region = charts.getByRole('region', { name: group.title })
      await region.scrollIntoViewIfNeeded()
      // The sticky header and chart nav would cover the region's headings.
      await region.screenshot({
        path: `${ARTIFACT_DIR}/vis-030-${group.id}-${width}.png`,
        style: UNSTICK,
      })
    }
  }
  await page.setViewportSize({ width: 1440, height: 1080 })
  await page.evaluate(() => window.scrollTo(0, 0))
  await shoot(page, 'vis-030-overview')
  browserErrors.assertClean()
})

const RANGE_LABEL_SHORT =
  /^[A-Z][a-z]{2} \d{1,2} \d{2}:\d{2} → (?:[A-Z][a-z]{2} \d{1,2} )?\d{2}:\d{2} UTC · step \d+[smhd](?: · \d+ gaps? or counter resets?)?$/
const RANGE_LABEL_DAYS =
  /^[A-Z][a-z]{2} \d{1,2} \d{2}:\d{2} → [A-Z][a-z]{2} \d{1,2} \d{2}:\d{2} UTC · step \d+[smhd](?: · \d+ gaps? or counter resets?)?$/

test('VIS-031 labels Overview ranges and shoots the TPS card at 6 h, 24 h and 7 d', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 3 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  for (const range of ['6h', '24h', '7d'] as const) {
    await page.goto(`/instances/${instanceId}?range=${range}`)
    const card = overviewCard(page, 'Transactions per second')
    await expect(card).toBeVisible({ timeout: 30_000 })
    const label = card.getByText(RANGE_LINE)
    await expect(label).toHaveText(range === '7d' ? RANGE_LABEL_DAYS : RANGE_LABEL_SHORT)
    await expect.poll(() => canvasNotBlank(card), { timeout: 3 * 30_000 }).toBe(true)
    await card.scrollIntoViewIfNeeded()
    await card.screenshot({
      path: `${ARTIFACT_DIR}/vis-031-tps-${range}-card.png`,
    })
  }
  browserErrors.assertClean()
})

for (const range of ['6h', '24h'] as const) {
  test(`VIS-031 tps ${range} the Overview TPS chart is complete`, async ({
    api,
    signedInPage: page,
  }) => {
    test.setTimeout(liveTimeout(0, 3 * 30_000))
    const browserErrors = collectConsoleErrors(page)
    const instanceId = await pickInstance(api)
    await page.goto(`/instances/${instanceId}?range=${range}`)
    const title = 'Transactions per second'
    await expect(overviewCard(page, title)).toBeVisible({ timeout: 30_000 })
    await expect
      .poll(async () => (await chartCompleteness(page, title)).total, { timeout: 3 * 30_000 })
      .toBeGreaterThan(0)
    // Commits: the rollback line is often zero and follows in the same table.
    const completeness = await chartCompleteness(page, title, 'Commits')
    const history = completeness.leadingNull / completeness.total
    const hours = range === '6h' ? 6 : 24
    if (history > 0.5) {
      test.skip(true, `UNVERIFIED: history ${((1 - history) * hours).toFixed(1)} h`)
    }
    expect(history).toBeLessThanOrEqual(0.5)
    expect(completeness.observedRatio).toBeGreaterThanOrEqual(0.95)
    browserErrors.assertClean()
  })
}

test('VIS-032 the Overview has no serious accessibility violations', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 3 * 30_000))
  const instanceId = await pickInstance(api)
  await page.goto(`/instances/${instanceId}?range=1h`)
  const charts = page.getByTestId('overview-charts')
  await expect(charts).toBeVisible({ timeout: 30_000 })
  await expect
    .poll(async () => (await chartCompleteness(page, 'Calls per second')).total, {
      timeout: 3 * 30_000,
    })
    .toBeGreaterThan(0)
  const report = await new AxeBuilder({ page }).include('#instance-overview').analyze()
  const severe = report.violations.filter(
    (violation) => violation.impact === 'serious' || violation.impact === 'critical',
  )
  if (severe.length > 0) {
    await test.info().attach('axe-overview', {
      body: JSON.stringify(severe, null, 2),
      contentType: 'application/json',
    })
  }
  expect(severe).toEqual([])
})

/** The body rows of every data table on the page (I-12). */
async function dataTableRows(page: Page): Promise<{ name: string; rows: number }[]> {
  // Chart data tables (`… data`) and the topology graph's sr-only text
  // equivalent are not data tables: they mirror a chart or a graph.
  return page.locator('table:not([aria-label$=" data"])').evaluateAll((tables) =>
    tables
      .filter((table) => table.closest('[data-topology-edge-table]') === null)
      .map((table) => ({
        name: table.getAttribute('aria-label') ?? table.querySelector('caption')?.textContent ?? '',
        rows: table.querySelectorAll(':scope > tbody > tr').length,
      })),
  )
}

async function firstCluster(
  api: Parameters<typeof pickInstance>[0],
): Promise<{ cluster_id: string; instances: unknown[] }> {
  const response = await api.get('/api/v1/clusters')
  expect(response.ok()).toBe(true)
  const clusters = (await response.json()) as { cluster_id: string; instances: unknown[] }[]
  const [first] = clusters
  if (!first) throw new Error('the server lists no cluster')
  return first
}

/** The `<total>` of a table footer `Showing a–b of <total> <scope>`. */
async function footerTotal(page: Page, scope: string): Promise<number> {
  const footer = page.getByText(new RegExp(`^Showing \\d+–\\d+ of \\d+ ${scope}`)).first()
  await expect(footer).toBeVisible({ timeout: 30_000 })
  return Number(new RegExp(`of (\\d+) ${scope}`).exec((await footer.textContent()) ?? '')?.[1])
}

const countOf = (text: string | null) => Number((text ?? '').replace(/[^\d.]/g, ''))

test('TBL-E2E-001 the Slowest queries table shows 20 rows per page with a pager', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 2 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  await page.goto(`/queries?instance=${instanceId}&range=24h`)

  const table = page.getByRole('table', { name: 'Slowest queries' })
  await expect(table).toBeVisible({ timeout: 30_000 })
  const rows = table.locator('tbody tr')
  expect(await rows.count()).toBeLessThanOrEqual(20)
  const total = await footerTotal(page, 'statements')
  expect(total, 'the stack must hold more than one page of statements').toBeGreaterThan(20)
  await expect(page.getByText(/^Page 1 of \d+$/)).toBeVisible()
  await expect(page.getByText(/^Showing 1–20 of \d+ statements/)).toBeVisible()

  const first = (await rows.first().textContent()) ?? ''
  await page
    .getByRole('navigation', { name: 'Slowest queries pagination' })
    .getByRole('button', { name: 'Next page' })
    .click()
  await expect(rows.first()).not.toHaveText(first)
  await expect(page.getByText(/^Showing 21–\d+ of \d+ statements/)).toBeVisible()
  expect(await rows.count()).toBeLessThanOrEqual(20)
  browserErrors.assertClean()
})

test('TBL-E2E-002 the sort buttons sort the whole loaded set and survive a reload', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 2 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  await page.goto(`/queries?instance=${instanceId}&range=24h`)
  const table = page.getByRole('table', { name: 'Slowest queries' })
  await expect(table).toBeVisible({ timeout: 30_000 })

  await table.getByRole('button', { name: 'Sort Calls descending' }).click()
  await expect(page).toHaveURL(/slow\.sort=calls&slow\.dir=desc/)
  // The URL changes first; the sorted rows render with the header's aria-sort.
  await expect(table.getByRole('columnheader', { name: /Calls/ })).toHaveAttribute(
    'aria-sort',
    'descending',
  )
  // Calls is the fourth column: Rank, Query, Database, Calls.
  const calls = async () =>
    (await table.locator('tbody tr td:nth-child(4)').allTextContents()).map(countOf)
  const sorted = await calls()
  expect(sorted.length).toBeGreaterThan(1)
  expect(sorted[0]).toBe(Math.max(...sorted))
  expect([...sorted].sort((a, b) => b - a)).toEqual(sorted)

  await page.reload()
  await expect(table).toBeVisible({ timeout: 30_000 })
  await expect(table.getByRole('button', { name: 'Sort Calls descending' })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await expect(table.getByRole('columnheader', { name: /Calls/ })).toHaveAttribute(
    'aria-sort',
    'descending',
  )
  expect(await calls()).toEqual(sorted)
  browserErrors.assertClean()
})

test('TBL-E2E-003 Index analysis searches, filters, pages and clears the indexes', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 2 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  await page.goto(`/indexes?instance=${instanceId}`)
  const table = page.getByRole('table', { name: 'Indexes' })
  await expect(table).toBeVisible({ timeout: 30_000 })
  const rows = table.locator('tbody tr')
  expect(await rows.count()).toBeLessThanOrEqual(20)
  const total = await footerTotal(page, 'indexes')

  if (total > 20) {
    await page
      .getByRole('navigation', { name: 'Indexes pagination' })
      .getByRole('button', { name: 'Next page' })
      .click()
    await expect(page.getByText(/^Showing 21–\d+ of \d+ indexes/)).toBeVisible()
  }

  await page.getByRole('searchbox', { name: 'Search indexes' }).fill('res_partner')
  await expect(
    page.getByText(/^Showing \d+–\d+ of \d+ indexes \(filtered from \d+\)/),
  ).toBeVisible()
  // Search returns to page 1, and every row is a res_partner index.
  await expect(page).not.toHaveURL(/indexes\.page=/)
  // The search also reads columns and definitions: every row names res_partner somewhere.
  const matched = await rows.allTextContents()
  expect(matched.length).toBeGreaterThan(0)
  for (const row of matched) expect(row.toLowerCase()).toContain('res_partner')

  // The filter must act: no rows, or only invalid indexes (each carries the badge).
  await page.getByRole('combobox', { name: 'Filter Indexes by State' }).selectOption('invalid')
  await expect(page).toHaveURL(/indexes\.f\.state=invalid/)
  await expect
    .poll(async () => {
      if (await page.getByText('No rows match').isVisible()) return 'none'
      const flagged = await rows.allTextContents()
      return flagged.length > 0 && flagged.every((row) => row.includes('invalid'))
        ? 'invalid only'
        : 'unfiltered'
    })
    .not.toBe('unfiltered')

  await page.getByRole('button', { name: 'Clear filters' }).click()
  await expect(page.getByText(new RegExp(`^Showing 1–\\d+ of ${total} indexes\\.`))).toBeVisible()
  browserErrors.assertClean()
})

test('TBL-E2E-004 the Dead rows and Cleanup tabs page their tables', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 2 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)

  await page.goto(`/indexes?instance=${instanceId}&tab=dead-rows`)
  const dead = page.getByRole('table', { name: 'Tables' })
  await expect(dead).toBeVisible({ timeout: 30_000 })
  expect(await dead.locator('tbody tr').count()).toBeLessThanOrEqual(20)
  const tables = await footerTotal(page, 'tables')
  if (tables <= 20) {
    test
      .info()
      .annotations.push({ type: 'UNVERIFIED', description: `Dead rows pager: ${tables} tables` })
  } else {
    await page
      .getByRole('navigation', { name: 'Tables pagination' })
      .getByRole('button', { name: 'Next page' })
      .click()
    await expect(page.getByText(/^Showing 21–\d+ of \d+ tables/)).toBeVisible()
    expect(await dead.locator('tbody tr').count()).toBeLessThanOrEqual(20)
  }

  await page.goto(`/indexes?instance=${instanceId}&tab=cleanup`)
  const cleanup = page.getByRole('table', { name: 'Cleanup candidates' })
  const nothing = page.getByRole('heading', { name: 'Nothing to clean up' })
  await expect(cleanup.or(nothing)).toBeVisible({ timeout: 30_000 })
  if (await cleanup.isVisible()) {
    expect(await cleanup.locator('tbody tr').count()).toBeLessThanOrEqual(20)
    const candidates = await footerTotal(page, 'candidates')
    if (candidates <= 20) {
      test.info().annotations.push({
        type: 'UNVERIFIED',
        description: `Cleanup pager: ${candidates} candidates`,
      })
    } else {
      await page
        .getByRole('navigation', { name: 'Cleanup candidates pagination' })
        .getByRole('button', { name: 'Next page' })
        .click()
      await expect(page.getByText(/^Showing 21–\d+ of \d+ candidates/)).toBeVisible()
    }
  }
  browserErrors.assertClean()
})

test('TBL-E2E-005 no data table renders more than one page of rows (I-12)', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 9 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  const cluster = await firstCluster(api)
  const clusterId = cluster.cluster_id
  // A one-instance cluster has no edges, slots or drift to list (§8; re-proven at phase 7).
  const standalone = cluster.instances.length <= 1
  const views = [
    `/queries?instance=${instanceId}&range=24h`,
    `/indexes?instance=${instanceId}`,
    '/findings',
    // The rule catalogue has its own tab since 15.3.
    '/findings?view=rules',
    '/settings',
    `/clusters/${clusterId}`,
    `/instances/${instanceId}/queries`,
    // The instance page shows one section per tab (15.2): the table tabs.
    `/instances/${instanceId}?section=catalog`,
    `/instances/${instanceId}?section=relations`,
    '/alerts/rules',
  ]
  const clusterView = `/clusters/${clusterId}`
  for (const view of views) {
    await page.goto(view)
    if (view === clusterView) {
      // The edge table shows on request; a standalone cluster has no edges,
      // slots or drift, so it may hold no data table at all (§8, re-proven at 7.x).
      await page.waitForLoadState('networkidle')
      const toggle = page.getByRole('button', { name: 'Show accessible edge table' })
      if (await toggle.isVisible()) await toggle.click()
      await page.waitForLoadState('networkidle')
      const tables = await dataTableRows(page)
      if (!standalone)
        expect(tables.length, `${view}: a replicated cluster lists data`).toBeGreaterThan(0)
      if (tables.length === 0) {
        test.info().annotations.push({
          type: 'UNVERIFIED',
          description: `${view}: no data table on a standalone cluster`,
        })
      }
      for (const { name, rows } of tables) expect(rows, `${view} · ${name}`).toBeLessThanOrEqual(20)
      continue
    }
    if (view.includes('section=catalog')) {
      // The Catalog opens on Triggers, often empty; Sequences always lists rows on Odoo.
      await page.getByRole('tab', { name: /^Sequences/ }).click()
    }
    await expect
      .poll(async () => (await dataTableRows(page)).length, { timeout: 30_000, message: view })
      .toBeGreaterThan(0)
    // Let the late queries of the page land before counting.
    await page.waitForLoadState('networkidle')
    for (const { name, rows } of await dataTableRows(page)) {
      expect(rows, `${view} · ${name}`).toBeLessThanOrEqual(20)
    }
  }
  browserErrors.assertClean()
})

const TBL006_TABLES = [
  { route: (id: string) => `/queries?instance=${id}&range=24h`, name: 'Slowest queries' },
  { route: (id: string) => `/indexes?instance=${id}`, name: 'Indexes' },
  { route: () => '/settings', name: 'Monitored instances' },
] as const

test('TBL-E2E-006 the most used tables have no serious accessibility violations', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 6 * 30_000))
  const instanceId = await pickInstance(api)
  for (const { route, name } of TBL006_TABLES) {
    await page.setViewportSize({ width: 1920, height: 1080 })
    await page.goto(route(instanceId))
    const table = page.getByRole('table', { name })
    await expect(table).toBeVisible({ timeout: 30_000 })
    await page.waitForLoadState('networkidle')
    const report = await new AxeBuilder({ page }).analyze()
    const severe = report.violations.filter(
      (violation) => violation.impact === 'serious' || violation.impact === 'critical',
    )
    if (severe.length > 0) {
      await test.info().attach(`axe-${name}`, {
        body: JSON.stringify(severe, null, 2),
        contentType: 'application/json',
      })
    }
    expect(severe, name).toEqual([])

    // The table with its toolbar and pager, at three widths.
    const block = table.locator('xpath=ancestor::div[contains(@class,"space-y-3")][1]')
    for (const width of [1440, 1024, 390] as const) {
      await page.setViewportSize({ width, height: 1080 })
      await block.scrollIntoViewIfNeeded()
      await block.screenshot({
        path: `${ARTIFACT_DIR}/tbl-e2e-006-${name.toLowerCase().replaceAll(' ', '-')}-${width}.png`,
        style: UNSTICK,
      })
    }
  }
})

test('FHD-MEASURE-001 measures what every Full HD page spends its height on', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, FULLHD_ROUTES.length * 2 * 30_000))
  // Measurement only: the fit is asserted by FHD-FIT-001 (15.3) once the density work lands.
  const measures = await measureRoutes(page, api)
  expect(measures.length).toBeGreaterThan(0)
  await test.info().attach('fullhd-measure', {
    body: JSON.stringify(measures, null, 2),
    contentType: 'application/json',
  })
})

test('FHD-INST-001 every instance-page tab opens on the live instance', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, INSTANCE_SECTIONS.length * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  await page.goto(`/instances/${instanceId}`)
  const tabs = page.getByRole('tablist', { name: 'Instance sections' })
  await expect(tabs).toBeVisible({ timeout: 30_000 })

  // The spec walks the registry, so a new section is covered without editing it.
  for (const section of INSTANCE_SECTIONS) {
    const tab = tabs.getByRole('tab', { name: section.label, exact: true })
    await tab.click()
    await expect(tab).toHaveAttribute('aria-selected', 'true')
    if (section.id === 'overview') await expect(page).not.toHaveURL(/[?&]section=/)
    else await expect(page).toHaveURL(new RegExp(`[?&]section=${section.id}(&|$)`))
    await page.waitForLoadState('networkidle')
    await expect(page.getByRole('tabpanel').first()).toBeVisible()
    // Every tab, not only the default one, passes axe (SYS-UI-011 checks Overview only).
    const report = await new AxeBuilder({ page }).include('main').analyze()
    const severe = report.violations.filter(
      (violation) => violation.impact === 'serious' || violation.impact === 'critical',
    )
    expect(severe, `${section.id}: ${severe.map((violation) => violation.id).join(', ')}`).toEqual(
      [],
    )
  }

  await page.goto(`/instances/${instanceId}?section=catalog`)
  await expect(tabs.getByRole('tab', { name: 'Schema objects', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByRole('tablist', { name: 'Schema object kinds' })).toBeVisible({
    timeout: 30_000,
  })
  browserErrors.assertClean()
})

test('FHD-FIT-001 every fit route shows its table and pager on a Full HD screen', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, FULLHD_ROUTES.length * 2 * 30_000))
  const failures: string[] = []
  for (const route of FULLHD_ROUTES) {
    if (route.kind === 'exempt') continue
    if (route.pendingUnit !== undefined) {
      // A skip is never a pass: the owning unit runs its own fit check.
      test.info().annotations.push({
        type: 'UNVERIFIED',
        description: `${route.path}: pending ${route.pendingUnit}`,
      })
      continue
    }
    try {
      await test.step(route.path, async () => {
        // assertFits also checks, per viewport, that the pager is on screen.
        await assertFits(page, api, route)
      })
    } catch (error) {
      failures.push(
        `${route.path}: ${error instanceof Error ? error.message.split('\n')[0] : String(error)}`,
      )
    } finally {
      // Passing or not, the route's last viewport (1920×950) is kept for the review.
      const slug = route.path.replace(/[^a-z0-9]+/gi, '-').replace(/^-|-$/g, '') || 'fleet'
      await page.screenshot({
        path: `../test/e2e/_artifacts/ui/fullhd/${slug}-950.png`,
        fullPage: true,
      })
    }
  }
  expect(failures, failures.join('\n')).toEqual([])
})

/** What each Settings tab shows once open: a table, or the heading of its panel. */
const SETTINGS_TAB_CONTENT: Record<
  (typeof SETTINGS_TABS)[number]['id'],
  { role: 'table' | 'heading'; name: string }
> = {
  inventory: { role: 'table', name: 'Monitored instances' },
  preferences: { role: 'heading', name: 'Preferences' },
  retention: { role: 'heading', name: 'Data retention' },
  advisor: { role: 'table', name: 'Advisor rules' },
  notifications: { role: 'heading', name: 'Notifications' },
  names: { role: 'table', name: 'Cluster and instance names' },
  audit: { role: 'heading', name: 'Command audit' },
}

test('SET-E2E-001 every Settings tab opens with its content and fits a Full HD screen', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 7 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.goto('/settings')
  await expect(page.getByRole('heading', { level: 1, name: 'Settings and inventory' })).toBeVisible(
    { timeout: 30_000 },
  )
  const tabs = page.getByRole('tablist', { name: 'Settings sections' })
  for (const { id, label } of SETTINGS_TABS) {
    await test.step(`open ${label}`, async () => {
      const tab = tabs.getByRole('tab', { name: label, exact: true })
      await tab.click()
      await expect(tab).toHaveAttribute('aria-selected', 'true')
      // The default tab is never written to the URL (D12).
      expect(new URL(page.url()).searchParams.get('tab')).toBe(
        id === DEFAULT_SETTINGS_TAB ? null : id,
      )
      const { role, name } = SETTINGS_TAB_CONTENT[id]
      await expect(page.getByRole(role, { name, exact: true }).first()).toBeVisible({
        timeout: 30_000,
      })
      await page.screenshot({ path: `${ARTIFACT_DIR}/set-e2e-001-${id}.png` })
    })
  }
  // Every Settings entry of the Full HD inventory: the seven tabs and the Inventory,
  // Advisor rules and Audit views.
  const routes = FULLHD_ROUTES.filter((route) => routePathname(route.path) === '/settings')
  expect(routes.length).toBe(
    SETTINGS_TABS.length +
      INVENTORY_VIEWS.length -
      1 +
      ADVISOR_VIEWS.length -
      1 +
      AUDIT_VIEWS.length -
      1,
  )
  for (const route of routes) {
    await test.step(`fits ${route.path}`, async () => {
      await assertFits(page, api, route)
    })
  }
  browserErrors.assertClean()
})

/** Where the names spec keeps its screenshots (C-10). */
const NAMES_ARTIFACT_DIR = '../test/e2e/_artifacts/ui/names'
const NAMES_TABLE = 'Cluster and instance names'
const CLUSTER_NAME = 'odoo-prod'
const INSTANCE_NAME = 'odoo-db-1'

interface LiveNamedInstance {
  instance_id: string
  addr: string
  port: number
  display_name?: string | null
}

interface LiveNamedCluster {
  cluster_id: string
  name: string | null
  instances: LiveNamedInstance[]
}

/** The picked instance (the Odoo primary) and its cluster, as the API lists them now. */
async function namedTargets(
  api: Parameters<typeof pickInstance>[0],
): Promise<{ cluster: LiveNamedCluster; instance: LiveNamedInstance }> {
  const instanceId = await pickInstance(api)
  const clusters = (await (await api.get('/api/v1/clusters')).json()) as LiveNamedCluster[]
  const cluster = clusters.find((candidate) =>
    candidate.instances.some((member) => member.instance_id === instanceId),
  )
  if (!cluster) throw new Error(`no cluster lists the picked instance ${instanceId}`)
  const instances = (await (await api.get('/api/v1/instances')).json()) as LiveNamedInstance[]
  const instance = instances.find((candidate) => candidate.instance_id === instanceId)
  if (!instance) throw new Error(`the instance list has no ${instanceId}`)
  return { cluster, instance }
}

/** Opens Settings, narrows the Names table to the cluster and renames one row through the dialog. */
async function renameThroughSettings(
  page: Page,
  clusterId: string,
  button: string,
  field: 'Cluster name' | 'Instance name',
  value: string,
  shot?: string,
): Promise<void> {
  await page.goto('/settings?tab=names')
  const table = page.getByRole('table', { name: NAMES_TABLE })
  await expect(table).toBeVisible({ timeout: 30_000 })
  await page.getByRole('searchbox', { name: `Search ${NAMES_TABLE}` }).fill(clusterId)
  await table.getByRole('button', { name: button, exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  const input = dialog.getByRole('textbox', { name: field })
  await expect(input).toBeFocused()
  if (shot) {
    // C-10: the inline error state, then the dialog with the new name.
    await input.fill('x'.repeat(65))
    await expect(dialog.getByText('Use at most 64 characters.')).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Save' })).toBeDisabled()
    await dialog.screenshot({ path: `${NAMES_ARTIFACT_DIR}/${shot}-error.png` })
  }
  await input.fill(value)
  if (shot) await dialog.screenshot({ path: `${NAMES_ARTIFACT_DIR}/${shot}.png` })
  await dialog.getByRole('button', { name: 'Save' }).click()
  await expect(dialog).toBeHidden({ timeout: 30_000 })
  // Focus returns to the row's Rename button.
  await expect(page.locator(':focus')).toHaveText('Rename')
}

/**
 * The Names table at 1920×950 (§8, the first captioned DataTable): its toolbar
 * row stays one line (≤ 52 px) with the caption beside the controls, the
 * caption text wider than 0. The fit of the whole Names tab is asserted by
 * SET-E2E-001 (11.1).
 */
async function assertNamesTableFits(page: Page): Promise<void> {
  await page.setViewportSize({ width: 1920, height: 950 })
  await page.goto('/settings?tab=names')
  await expect(page.getByRole('table', { name: NAMES_TABLE })).toBeVisible({ timeout: 30_000 })
  const card = page.locator('[data-table-card][data-caption-id="settings.names-table"]')
  await card.scrollIntoViewIfNeeded()
  const toolbar = await card.locator(':scope > div').first().boundingBox()
  const caption = await card.getByTitle(CAPTIONS['settings.names-table'].what).boundingBox()
  expect(toolbar, 'Names table toolbar row').not.toBeNull()
  expect(toolbar?.height ?? Infinity, 'Names table toolbar row height').toBeLessThanOrEqual(52)
  expect(caption?.width ?? 0, 'Names table caption width').toBeGreaterThan(0)
  await card.screenshot({ path: `${NAMES_ARTIFACT_DIR}/clname-e2e-001-names-table-1920x950.png` })
}

test('CLNAME-E2E-001 renamed clusters and instances show everywhere and clear back to the defaults', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 8 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const { cluster, instance } = await namedTargets(api)
  const clusterId = cluster.cluster_id
  const address = `${instance.addr}:${instance.port}`
  const original = { cluster: cluster.name, instance: instance.display_name ?? null }
  try {
    // A leftover name equal to the target would leave Save disabled (unchanged): clear it first.
    if (original.cluster === CLUSTER_NAME) {
      expect(
        (await api.patch(`/api/v1/clusters/${clusterId}`, { data: { name: null } })).ok(),
      ).toBe(true)
    }
    if (original.instance === INSTANCE_NAME) {
      const cleared = await api.patch(`/api/v1/instances/${instance.instance_id}`, {
        data: { display_name: null },
      })
      expect(cleared.ok()).toBe(true)
    }
    const clusterLabel = original.cluster === CLUSTER_NAME ? null : original.cluster
    const instanceLabel = original.instance === INSTANCE_NAME ? null : original.instance

    // 1. Rename the cluster from Settings → Names.
    await renameThroughSettings(
      page,
      clusterId,
      `Rename cluster ${clusterLabel ?? `Cluster ${clusterId}`}`,
      'Cluster name',
      CLUSTER_NAME,
      'clname-e2e-001-cluster-dialog',
    )
    await page.reload()
    const names = page.getByRole('table', { name: NAMES_TABLE })
    await expect(names).toBeVisible({ timeout: 30_000 })
    await expect(names.getByText(CLUSTER_NAME, { exact: true })).toBeVisible()
    await page.screenshot({
      path: `${NAMES_ARTIFACT_DIR}/clname-e2e-001-settings-cluster.png`,
      fullPage: true,
    })
    // The first captioned DataTable passes the §8 toolbar check at 1920×950.
    await assertNamesTableFits(page)

    // 2. The Fleet card and the Cluster page show it, and both still fit Full HD.
    await assertFits(page, api, fullHdRoute('/'))
    await expect(
      page.getByRole('link', { name: `${CLUSTER_NAME} (${clusterId})`, exact: true }),
    ).toBeVisible({ timeout: 30_000 })
    await page.screenshot({ path: `${NAMES_ARTIFACT_DIR}/clname-e2e-001-fleet.png` })
    await assertFits(page, api, fullHdRoute('/clusters/:clusterId?section=topology'))
    await expect(page.getByRole('heading', { level: 1, name: CLUSTER_NAME })).toBeVisible()
    await page.screenshot({ path: `${NAMES_ARTIFACT_DIR}/clname-e2e-001-cluster.png` })

    // 3. Rename the instance; the header and the tables show it.
    await renameThroughSettings(
      page,
      clusterId,
      `Rename instance ${instanceLabel ?? address}`,
      'Instance name',
      INSTANCE_NAME,
      'clname-e2e-001-instance-dialog',
    )
    await page.reload()
    await expect(
      page.getByRole('table', { name: NAMES_TABLE }).getByText(INSTANCE_NAME, { exact: true }),
    ).toBeVisible({ timeout: 30_000 })
    // The instance table is in the Inventory tab since 11.1.
    await page.goto('/settings')
    await expect(
      page.getByRole('table', { name: 'Monitored instances' }).getByText(INSTANCE_NAME, {
        exact: true,
      }),
    ).toBeVisible({ timeout: 30_000 })
    await page.screenshot({
      path: `${NAMES_ARTIFACT_DIR}/clname-e2e-001-settings-instance.png`,
      fullPage: true,
    })
    await assertFits(page, api, fullHdRoute('/instances/:instanceId?section=overview'))
    const header = page.getByTestId('instance-header')
    await expect(header.getByRole('heading', { level: 1, name: INSTANCE_NAME })).toBeVisible()
    await expect(header.getByTestId('instance-address')).toHaveText(address)
    await page.screenshot({ path: `${NAMES_ARTIFACT_DIR}/clname-e2e-001-instance.png` })

    // 4. Clear both: the defaults come back.
    await renameThroughSettings(
      page,
      clusterId,
      `Rename instance ${INSTANCE_NAME}`,
      'Instance name',
      '',
    )
    await renameThroughSettings(
      page,
      clusterId,
      `Rename cluster ${CLUSTER_NAME}`,
      'Cluster name',
      '',
    )
    await page.reload()
    const cleared = page.getByRole('table', { name: NAMES_TABLE })
    await expect(cleared.getByText(`Cluster ${clusterId} (default)`, { exact: true })).toBeVisible({
      timeout: 30_000,
    })
    await expect(cleared.getByText(`${address} (default)`, { exact: true })).toBeVisible()
    await page.goto('/')
    await expect(
      page.getByRole('link', { name: `Unnamed cluster (${clusterId})`, exact: true }),
    ).toBeVisible({ timeout: 30_000 })
    await page.goto(`/instances/${instance.instance_id}`)
    await expect(page.getByRole('heading', { level: 1, name: address })).toBeVisible({
      timeout: 30_000,
    })
    browserErrors.assertClean()
  } finally {
    // The stack keeps the names it had before the test, whatever happened above;
    // a failed restore fails the test and says what to put back.
    // Each restore runs on its own, so a network error on one never skips the other.
    const restores = [
      {
        what: `instance ${instance.instance_id} display_name ${JSON.stringify(original.instance)}`,
        call: () =>
          api.patch(`/api/v1/instances/${instance.instance_id}`, {
            data: { display_name: original.instance },
          }),
      },
      {
        what: `cluster ${clusterId} name ${JSON.stringify(original.cluster)}`,
        call: () =>
          api.patch(`/api/v1/clusters/${clusterId}`, { data: { name: original.cluster } }),
      },
    ]
    const failed: string[] = []
    for (const { what, call } of restores) {
      let message: string | null = null
      try {
        const response = await call()
        if (!response.ok()) {
          message = `restore ${what}: HTTP ${response.status()} ${await response.text()}`
        }
      } catch (error) {
        message = `restore ${what}: ${error instanceof Error ? error.message : String(error)}`
      }
      if (message === null) continue
      console.error(`CLNAME-E2E-001 ${message}`)
      test.info().annotations.push({ type: 'RESTORE-FAILED', description: message })
      failed.push(message)
    }
    expect(failed, 'the original names must be restored').toEqual([])
  }
})

/** The Preferences tab's stored key (11.2); the spec clears it whatever happens. */
const PREFERENCES_KEY = 'pglens.preferences'

test('PREF-E2E-001 preferences persist across a reload and set the default range and page size', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 2 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  try {
    await assertFits(page, api, fullHdRoute('/settings?tab=preferences'))

    await page.setViewportSize({ width: 1920, height: 1080 })
    await page.goto('/settings?tab=preferences')
    const panel = page.getByRole('region', { name: 'Preferences', exact: true })
    await expect(panel).toBeVisible({ timeout: 30_000 })
    const range = panel.getByRole('combobox', { name: 'Default time range' })
    const pageSize = panel.getByRole('combobox', { name: 'Table page size' })
    await range.selectOption('6h')
    await pageSize.selectOption('50')
    await page.screenshot({ path: `${ARTIFACT_DIR}/pref-e2e-001-preferences.png` })

    // I-11: read back after a reload.
    await page.reload()
    await expect(range).toHaveValue('6h', { timeout: 30_000 })
    await expect(pageSize).toHaveValue('50')

    // An instance Overview without `range` opens on the preferred 6 h.
    await page.goto(`/instances/${instanceId}`)
    await expect(page.getByRole('combobox', { name: 'Time range preset' })).toHaveValue('6h', {
      timeout: 30_000,
    })
    expect(new URL(page.url()).searchParams.get('range')).toBeNull()

    // A table page without `size` opens at 50 rows per page.
    await page.goto(`/queries?instance=${instanceId}`)
    const pager = page.getByRole('navigation', { name: 'Slowest queries pagination' })
    await expect(pager.getByRole('combobox', { name: 'Rows per page' })).toHaveValue('50', {
      timeout: 30_000,
    })
    expect(
      await page.getByRole('table', { name: 'Slowest queries' }).locator('tbody tr').count(),
    ).toBeLessThanOrEqual(50)

    // Reset to defaults restores every preference.
    await page.goto('/settings?tab=preferences')
    await panel.getByRole('button', { name: 'Reset to defaults' }).click()
    await expect(range).toHaveValue('1h')
    await expect(pageSize).toHaveValue('20')
    browserErrors.assertClean()
  } finally {
    // Restore the defaults even when a step failed.
    // Never hides the real failure.
    await page
      .evaluate((key) => window.localStorage.removeItem(key), PREFERENCES_KEY)
      .catch(() => undefined)
  }
})

/** The pglens TimescaleDB container of the dev stack (read-only checks of RET-E2E-001). */
const PGLENS_TS_CONTAINER = process.env.PGLENS_QA_TS_CONTAINER ?? 'pglens-odoo-timescaledb-1'
/** A constant, read-only query: the retention policies of metrics_ash, by hypertable name. */
const ASH_RETENTION_JOBS_SQL =
  "SELECT config->>'drop_after' FROM timescaledb_information.jobs WHERE proc_name = 'policy_retention' AND hypertable_name = 'metrics_ash' ORDER BY job_id"

/** drop_after of every retention job of metrics_ash, read over the container's local socket in a read-only transaction. */
async function ashRetentionJobs(): Promise<string[]> {
  const { stdout } = await run(
    'docker',
    [
      'exec',
      '-e',
      'PGOPTIONS=-c default_transaction_read_only=on',
      PGLENS_TS_CONTAINER,
      'psql',
      '-U',
      'pglens',
      '-d',
      'pglens',
      '-X',
      '-A',
      '-t',
      '-q',
      '-v',
      'ON_ERROR_STOP=1',
      '-c',
      ASH_RETENTION_JOBS_SQL,
    ],
    { timeoutMs: 30_000 },
  )
  return stdout
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '')
}

interface LiveRetentionRow {
  family: string
  days: number | null
}

async function ashRetentionDays(api: Parameters<typeof pickInstance>[0]): Promise<number | null> {
  const response = await api.get('/api/v1/settings/retention')
  expect(response.ok(), `getRetention: HTTP ${response.status()}`).toBe(true)
  const rows = (await response.json()) as LiveRetentionRow[]
  const ash = rows.find((row) => row.family === 'metrics_ash')
  if (!ash) throw new Error('getRetention lists no metrics_ash')
  return ash.days
}

test('RET-E2E-001 the Data retention tab previews, confirms a shortening up to the dialog only, and applies a lengthening of metrics_ash', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 3 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const original = await ashRetentionDays(api)
  // Restoring needs a value to go back to; a family without a policy is left alone.
  expect(original, 'metrics_ash has a retention policy on the live stack').not.toBeNull()
  const originalDays = original ?? 30
  const jobsBefore = await ashRetentionJobs()
  expect(jobsBefore).toEqual([`${originalDays} days`])
  const longer = Math.min(originalDays + 15, 365)
  expect(longer, 'room to lengthen').toBeGreaterThan(originalDays)
  let changed = false
  try {
    await assertFits(page, api, fullHdRoute('/settings?tab=retention'))

    await page.setViewportSize({ width: 1920, height: 1080 })
    await page.goto('/settings?tab=retention')
    const table = page.getByRole('table', { name: 'Data retention by metrics family' })
    await expect(table).toBeVisible({ timeout: 30_000 })
    const input = table.getByRole('textbox', { name: 'New retention of metrics_ash in days' })
    const preview = table.getByRole('button', { name: 'Preview metrics_ash' })
    const apply = table.getByRole('button', { name: 'Apply metrics_ash' })
    const summary = page.getByTestId('retention-preview-metrics_ash')
    await expect(input).toHaveValue(String(originalDays))
    await expect(apply).toBeDisabled()

    // Preview 20 days: a shortening of the live 30 days (or a lengthening of a shorter one).
    await input.fill('20')
    await preview.click()
    await expect(summary).toBeVisible({ timeout: 30_000 })
    await expect(summary).toHaveText(
      20 < originalDays
        ? /^(Deletes \d[\d,]* chunks? \(~[\d.,]+ MiB\) at the next policy run\.|Nothing older than 20 days is stored now\.)$/
        : 'Keeps more history: nothing is deleted.',
    )
    await expect(apply).toBeEnabled()
    await page.screenshot({ path: `${ARTIFACT_DIR}/ret-e2e-001-preview.png` })

    if (20 < originalDays) {
      // The destructive dialog up to its confirmation screen, then Cancel: nothing is deleted.
      await apply.click()
      const dialog = page.getByRole('dialog')
      await expect(dialog).toBeVisible()
      await expect(dialog).toContainText(
        /This will permanently delete \d[\d,]* chunks? \(~[\d.,]+ MiB\) of metrics_ash data older than 20 days at the next policy run\. Take a backup first\./,
      )
      const confirm = dialog.getByRole('button', { name: 'Delete history and apply' })
      await expect(confirm).toBeDisabled()
      await dialog.getByRole('checkbox').check()
      await expect(confirm).toBeEnabled()
      await dialog.screenshot({ path: `${ARTIFACT_DIR}/ret-e2e-001-confirm.png` })
      await dialog.getByRole('button', { name: 'Cancel' }).click()
      await expect(dialog).toBeHidden()
      expect(await ashRetentionJobs(), 'Cancel leaves the policy as it was').toEqual(jobsBefore)
    }

    // A lengthening applies without a dialog.
    await input.fill(String(longer))
    await expect(apply).toBeDisabled()
    await preview.click()
    await expect(summary).toHaveText('Keeps more history: nothing is deleted.', { timeout: 30_000 })
    changed = true
    await apply.click()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(table.getByRole('status')).toHaveText(`Applied: ${longer} days.`, {
      timeout: 30_000,
    })
    await expect(table.getByText(`${longer} days`, { exact: true })).toBeVisible()
    expect(await ashRetentionJobs(), 'timescaledb_information.jobs holds one job').toEqual([
      `${longer} days`,
    ])
    expect(await ashRetentionDays(api)).toBe(longer)
    await page.screenshot({ path: `${ARTIFACT_DIR}/ret-e2e-001-applied.png` })
    browserErrors.assertClean()
  } finally {
    // Put the original retention back even when a step failed; it deletes nothing the
    // original policy would keep. Never hides the real failure.
    if (changed) {
      let message: string | null = null
      try {
        const response = await api.put('/api/v1/settings/retention/metrics_ash', {
          data: { days: originalDays, confirm: true },
        })
        if (!response.ok()) {
          message = `restore metrics_ash: HTTP ${response.status()} ${await response.text()}`
        }
        const jobs = await ashRetentionJobs()
        if (message === null && JSON.stringify(jobs) !== JSON.stringify(jobsBefore)) {
          message = `restore metrics_ash: jobs ${JSON.stringify(jobs)}, want ${JSON.stringify(jobsBefore)}`
        }
      } catch (error) {
        message = `restore metrics_ash: ${error instanceof Error ? error.message : String(error)}`
      }
      if (message !== null) {
        console.error(`RET-E2E-001 ${message}`)
        test.info().annotations.push({ type: 'RESTORE-FAILED', description: message })
      }
      expect(message, 'the original retention must be restored').toBeNull()
    }
  }
})

interface LiveFinding {
  finding_id: string
  rule_id: string
  state: string
}

interface LiveAdvisorWindow {
  key: string
  seconds: number
  source: string
}

/** The findings the Findings page lists now (the default listing), of one rule or of all. */
async function liveFindings(
  api: Parameters<typeof pickInstance>[0],
  ruleId?: string,
): Promise<LiveFinding[]> {
  const query = ruleId === undefined ? '' : `?rule_id=${encodeURIComponent(ruleId)}`
  const response = await api.get(`/api/v1/findings${query}`)
  expect(response.ok(), `GET /api/v1/findings${query}`).toBe(true)
  return ((await response.json()) as LiveFinding[]).filter(
    (finding) => finding.state !== 'resolved',
  )
}

/**
 * The live rule for ADV-E2E-001: the one with the fewest open findings (at
 * least one), so the cycle disturbs as little of the dev stack as it can.
 */
async function ruleWithOpenFindings(api: Parameters<typeof pickInstance>[0]): Promise<string> {
  const open = new Map<string, number>()
  for (const finding of await liveFindings(api)) {
    if (finding.state === 'open') open.set(finding.rule_id, (open.get(finding.rule_id) ?? 0) + 1)
  }
  const ranked = [...open.entries()].sort(
    ([leftRule, left], [rightRule, right]) => left - right || leftRule.localeCompare(rightRule),
  )
  const picked = ranked[0]?.[0]
  if (picked === undefined) throw new Error('no advisor rule has an open finding on the live stack')
  return picked
}

/** The Findings → Rules catalogue row of one rule, searched by its id. */
function catalogueRow(page: Page, ruleId: string) {
  return page
    .getByRole('table', { name: 'Advisor rule catalogue' })
    .getByRole('row')
    .filter({ has: page.getByTitle(ruleId, { exact: true }) })
}

/**
 * At 1920×1080 and 1920×950 the open Advisor view ends at least 24 px above
 * the bottom of the viewport (D26), on top of `assertFits`.
 */
async function assertAdvisorHeadroom(page: Page, path: string, sectionId: string): Promise<void> {
  for (const viewport of [
    { width: 1920, height: 1080 },
    { width: 1920, height: 950 },
  ]) {
    await page.setViewportSize(viewport)
    await page.goto(path)
    const section = page.locator(`#${sectionId}`)
    await expect(section).toBeVisible({ timeout: 30_000 })
    // Loaded: the rules table or the window inputs, not the loading line.
    await expect(section.getByRole('table').or(section.getByRole('textbox')).first()).toBeVisible({
      timeout: 30_000,
    })
    if (sectionId === 'settings-advisor-rules') {
      // The table caption is the visible help of the switches (I-16): one
      // whole line at 1920 px, not cut by the ≥ 1600 px truncation.
      const caption = section.getByTitle(CAPTIONS['settings.advisor-rules-table'].what)
      await expect(caption).toBeVisible()
      const cut = await caption.evaluate((node) => ({
        scrollWidth: node.scrollWidth,
        clientWidth: node.clientWidth,
        height: node.getBoundingClientRect().height,
      }))
      console.log(
        `ADV-E2E-001 caption at ${viewport.width}×${viewport.height}: ${JSON.stringify(cut)}`,
      )
      expect(cut.scrollWidth, `${path}: caption truncated`).toBeLessThanOrEqual(cut.clientWidth)
      expect(cut.height, `${path}: caption on one line`).toBeLessThanOrEqual(24)
    }
    const box = await section.boundingBox()
    expect(box, `${path} section box`).not.toBeNull()
    const bottom = (box?.y ?? Infinity) + (box?.height ?? 0)
    expect(
      viewport.height - bottom,
      `${path} at ${viewport.width}×${viewport.height}: headroom below the view`,
    ).toBeGreaterThanOrEqual(24)
    await page.screenshot({
      path: `${ARTIFACT_DIR}/adv-e2e-001-${sectionId}-${viewport.width}x${viewport.height}.png`,
    })
  }
}

test('ADV-E2E-001 a rule disabled in Settings leaves the Findings page within one cycle and returns when enabled; the unused-index window reads back', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 2 * BUDGET.findings))
  const browserErrors = collectConsoleErrors(page)
  const rule = await ruleWithOpenFindings(api)
  const before = await liveFindings(api, rule)
  expect(before.length, `${rule} has findings on the Findings page`).toBeGreaterThan(0)
  test.info().annotations.push({
    type: 'ADV-E2E-001 rule',
    description: `${rule} (${before.length} findings)`,
  })
  let disabled = false
  try {
    // The tab and its two views fit a Full HD screen.
    await assertFits(page, api, fullHdRoute('/settings?tab=advisor'))
    await assertFits(page, api, fullHdRoute('/settings?tab=advisor&view=windows'))
    await assertAdvisorHeadroom(page, '/settings?tab=advisor', 'settings-advisor-rules')
    await assertAdvisorHeadroom(
      page,
      '/settings?tab=advisor&view=windows',
      'settings-advisor-windows',
    )

    // Disable the rule from its switch.
    await page.setViewportSize({ width: 1920, height: 1080 })
    await page.goto('/settings?tab=advisor')
    const table = page.getByRole('table', { name: 'Advisor rules' })
    await expect(table).toBeVisible({ timeout: 30_000 })
    await page.getByRole('searchbox', { name: 'Search Advisor rules' }).fill(rule)
    const toggle = table.getByRole('switch', { name: `Run ${rule}` })
    await expect(toggle).toHaveAttribute('aria-checked', 'true')
    disabled = true
    await toggle.click()
    const disabledAt = Date.now()
    await expect(toggle).toHaveAttribute('aria-checked', 'false', { timeout: 30_000 })
    await expect(table.getByText('custom', { exact: true })).toBeVisible()
    await page.screenshot({ path: `${ARTIFACT_DIR}/adv-e2e-001-disabled.png` })

    // Within one advisor cycle its findings leave the Findings page.
    await expect
      .poll(async () => (await liveFindings(api, rule)).length, {
        message: `${rule} findings resolved after the disable`,
        intervals: [2_000],
        timeout: BUDGET.findings,
      })
      .toBe(0)
    const goneAfter = Date.now() - disabledAt
    await page.goto(`/findings?view=rules&advisor-rule-catalogue.q=${encodeURIComponent(rule)}`)
    const row = catalogueRow(page, rule)
    await expect(row).toBeVisible({ timeout: 30_000 })
    await expect(row.getByText('Disabled', { exact: true })).toBeVisible()
    await expect(row.getByRole('cell').nth(5)).toHaveText('0')
    await page.screenshot({ path: `${ARTIFACT_DIR}/adv-e2e-001-findings-disabled.png` })

    // Enabled again, the next cycle reopens them.
    await page.goto('/settings?tab=advisor')
    await page.getByRole('searchbox', { name: 'Search Advisor rules' }).fill(rule)
    const again = page
      .getByRole('table', { name: 'Advisor rules' })
      .getByRole('switch', { name: `Run ${rule}` })
    await expect(again).toHaveAttribute('aria-checked', 'false', { timeout: 30_000 })
    await again.click()
    const enabledAt = Date.now()
    await expect(again).toHaveAttribute('aria-checked', 'true', { timeout: 30_000 })
    disabled = false
    await expect
      .poll(async () => (await liveFindings(api, rule)).length, {
        message: `${rule} findings back after the enable`,
        intervals: [2_000],
        timeout: BUDGET.findings,
      })
      .toBeGreaterThan(0)
    const backAfter = Date.now() - enabledAt
    await page.goto(`/findings?view=rules&advisor-rule-catalogue.q=${encodeURIComponent(rule)}`)
    await expect(catalogueRow(page, rule)).toBeVisible({ timeout: 30_000 })
    await expect(catalogueRow(page, rule).getByText('Disabled', { exact: true })).toHaveCount(0)
    test.info().annotations.push({
      type: 'ADV-E2E-001 timings',
      description: `gone ${Math.round(goneAfter / 1000)} s after disable, back ${Math.round(backAfter / 1000)} s after enable`,
    })
    console.log(
      `ADV-E2E-001 ${rule}: findings gone ${goneAfter} ms after disable, back ${backAfter} ms after enable`,
    )

    // The unused-index window: 1 hour, read back from the API and the form. Kept
    // on the dev stack (README); it has no effect until the advisor reads it (A5).
    await page.goto('/settings?tab=advisor&view=windows')
    const input = page.getByRole('textbox', { name: 'Unused-index history' })
    await expect(input).toBeVisible({ timeout: 30_000 })
    const reset = page.getByRole('button', { name: 'Reset Unused-index history to default' })
    if (await reset.isEnabled()) {
      // A previous run kept 1 hour: back to the default first, so Save writes it again.
      await reset.click()
      await expect(input).toHaveValue('7', { timeout: 30_000 })
    }
    await page.getByRole('combobox', { name: 'Unused-index history unit' }).selectOption('hours')
    await input.fill('1')
    await expect(page.getByText(/Shorter than the 7-day default/)).toBeVisible()
    await page.getByRole('button', { name: 'Save windows' }).click()
    await expect(page.getByRole('status').filter({ hasText: 'Saved:' })).toHaveText(
      /Saved: Unused-index history 1 hour, Backup history /,
      { timeout: 30_000 },
    )
    const windows = (await (
      await api.get('/api/v1/settings/advisor-windows')
    ).json()) as LiveAdvisorWindow[]
    expect(windows.map((window) => window.key)).toEqual([
      'window.unused_index',
      'window.backup_history',
    ])
    expect(windows[0]).toMatchObject({ seconds: 3600, source: 'custom' })
    await page.reload()
    await expect(page.getByRole('textbox', { name: 'Unused-index history' })).toHaveValue('1', {
      timeout: 30_000,
    })
    await expect(page.getByRole('combobox', { name: 'Unused-index history unit' })).toHaveValue(
      'hours',
    )
    await page.screenshot({ path: `${ARTIFACT_DIR}/adv-e2e-001-windows.png` })
    browserErrors.assertClean()
  } finally {
    // Never leave the rule disabled on the dev stack, even when a step failed.
    // Never hides the real failure.
    let message: string | null = null
    try {
      const response = await api.put(`/api/v1/advisor/rules/${encodeURIComponent(rule)}/override`, {
        data: { enabled: true },
      })
      if (!response.ok()) {
        message = `re-enable ${rule}: HTTP ${response.status()} ${await response.text()}`
      }
    } catch (error) {
      message = `re-enable ${rule}: ${error instanceof Error ? error.message : String(error)}`
    }
    if (message !== null) {
      console.error(`ADV-E2E-001 ${message}`)
      test.info().annotations.push({ type: 'RESTORE-FAILED', description: message })
    }
    expect(
      message,
      `${rule} must be enabled again${disabled ? ' (left disabled by a failed step)' : ''}`,
    ).toBeNull()
  }
})

test('NOTI-E2E-001 the Notifications tab shows Google Chat configured by host only, Send test delivers, and no page or response holds the webhook secret', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 2 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const secrets = await stackGoogleChatSecrets()
  expect(secrets.length, 'secret parts of the stack webhook').toBeGreaterThanOrEqual(3)
  // Every API response the console receives, scanned at the end.
  const bodies: Promise<string>[] = []
  page.on('response', (response) => {
    if (new URL(response.url()).pathname.startsWith('/api/')) {
      bodies.push(response.text().catch(() => ''))
    }
  })

  await assertFits(page, api, fullHdRoute('/settings?tab=notifications'))

  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.goto('/settings?tab=notifications')
  const row = page
    .getByRole('list', { name: 'Notification channels' })
    .getByRole('listitem', { name: 'Google Chat' })
  await expect(row).toBeVisible({ timeout: 30_000 })
  await expect(row.getByText('configured', { exact: true })).toBeVisible()
  await expect(row.getByText('chat.googleapis.com', { exact: true })).toBeVisible()
  await expect(row.locator('code')).toHaveText(['PGLENS_GOOGLE_CHAT_WEBHOOK_URL'])
  await expect(row).toContainText('Set by PGLENS_GOOGLE_CHAT_WEBHOOK_URL (or _FILE)')

  const send = row.getByRole('button', { name: 'Send test to Google Chat' })
  await expect(send).toBeEnabled()
  await send.click()
  await expect(page.getByTestId('notification-test-result-google_chat')).toHaveText('Delivered', {
    timeout: 30_000,
  })
  await page.screenshot({ path: `${ARTIFACT_DIR}/noti-e2e-001-delivered.png` })

  // The listing as the API answers it, besides what the page fetched.
  const listing = await api.get('/api/v1/notification-channels')
  expect(listing.ok(), `getNotificationChannels: HTTP ${listing.status()}`).toBe(true)
  const scanned = [
    { what: 'page HTML', text: await page.content() },
    { what: 'page text', text: await page.locator('body').innerText() },
    { what: 'getNotificationChannels', text: await listing.text() },
    ...(await Promise.all(bodies)).map((text, index) => ({ what: `API response ${index}`, text })),
  ]
  expect(scanned.length, 'responses captured').toBeGreaterThan(3)
  // A failure names where and which part, never the secret.
  for (const { what, text } of scanned) {
    for (const [index, secret] of secrets.entries()) {
      expect(text.includes(secret), `${what} holds secret part ${index}`).toBe(false)
    }
  }
  browserErrors.assertClean()
})

interface LiveAlert {
  alert_key: string
  rule_id: string
  state: 'pending' | 'firing' | 'resolved'
  instance_id?: string
  datname: string
  value: number
  started_at: string
  last_eval_at: string
}

/** The alert source's lookback at the default 30-second evaluation interval. */
const ALERT_LOOKBACK_MS = 120_000

/** The tile's value as a number; the dash of an unknown value reads as NaN. */
async function tileNumber(page: Page, label: string): Promise<number> {
  // MetricTile renders its figure in the one tabular-nums span of the tile.
  const text =
    (await page
      .getByRole('article', { name: label })
      .locator('span.tabular-nums')
      .first()
      .textContent()) ?? ''
  const match = /\d[\d,]*(?:\.\d+)?/.exec(text)
  return match ? Number(match[0].replace(/,/g, '')) : Number.NaN
}

/** The step of the stale-sample probe: a stored sample lies at most this far after its bucket. */
const PROBE_STEP_MS = 5_000

/**
 * The time until which the alert source can still read a deadlock sample stored
 * before now: the newest positive pg_deadlocks_total bucket of the last five
 * minutes, plus its step, plus the lookback. -Infinity when there is none.
 */
async function stalePositiveUntil(
  api: Parameters<typeof pickInstance>[0],
  instanceId: string,
  datname: string,
): Promise<number> {
  const to = new Date()
  const params = new URLSearchParams({
    metric: 'pg_deadlocks_total',
    instance_id: instanceId,
    database: datname,
    from: new Date(to.getTime() - 5 * 60_000).toISOString(),
    to: to.toISOString(),
    step: `${PROBE_STEP_MS / 1000}s`,
  })
  const response = await api.get(`/api/v1/metrics/query?${params.toString()}`)
  expect(response.ok(), `metrics query answered ${response.status()}`).toBe(true)
  const body = (await response.json()) as { series?: { ts: string; value: number | null }[] }
  const positive = (body.series ?? [])
    .filter((point) => point.value !== null && point.value > 0)
    .map((point) => Date.parse(point.ts))
  return positive.length === 0
    ? Number.NEGATIVE_INFINITY
    : Math.max(...positive) + PROBE_STEP_MS + ALERT_LOOKBACK_MS
}

/** The pending and firing db.deadlock_rate alerts of one database of the instance. */
async function deadlockAlerts(
  api: Parameters<typeof pickInstance>[0],
  instanceId: string,
  datname: string,
): Promise<LiveAlert[]> {
  const alerts = (await (
    await api.get(
      `/api/v1/alerts?rule_id=db.deadlock_rate&instance_id=${encodeURIComponent(instanceId)}`,
    )
  ).json()) as LiveAlert[]
  return alerts.filter((alert) => alert.datname === datname)
}

/** The text of a labelled detail cell ("Lock mode", "Waits on") in a tree item. */
function detailCell(item: ReturnType<Page['getByRole']>, label: string) {
  return item.getByText(label, { exact: true }).first().locator('xpath=following-sibling::span[1]')
}

// The db.deadlock_rate alert of 0026 fires on any deadlock. It resolves about
// twelve minutes after the last one (two minutes of alert lookback, then the
// engine's ten-minute metric grace), beyond this test's budget, so only firing is
// asserted; DL-002 in internal/alert covers the resolution.
test('DL-E2E-001 real deadlocks show in the activity, the Locks and Overview tiles and fire db.deadlock_rate; a row wait shows its lock mode', async ({
  api,
  signedInPage: page,
}) => {
  test.skip(test.info().project.name !== 'desktop-fullhd', 'stimulus runs on desktop-fullhd only')
  // The lock-mode step, the tiles and the alert each get a budget of their own.
  test.setTimeout(liveTimeout(60_000, BUDGET.activity + 2 * BUDGET.deadlockAlert))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  const datname = qaDatabase()
  try {
    // F2: a row lock wait shows the mode and type of the lock the waiter asks for.
    const pairName = 'qa-dl-lockmode'
    const pair = await lockPair('dl_lockmode', 1, pairName)
    const [waiterPid, holderPid] = (
      await query(
        `SELECT string_agg(pid::text, ',' ORDER BY cardinality(pg_blocking_pids(pid)) DESC)
           FROM pg_stat_activity WHERE application_name = '${pairName}';`,
      )
    ).split(',')
    await page.goto(`/instances/${instanceId}/locks`)
    const holder = page
      .getByRole('treeitem')
      .filter({ hasText: new RegExp(`PID ${holderPid}(?!\\d)`) })
      .first()
    await expect(holder).toBeVisible({ timeout: BUDGET.activity })
    const expand = page.getByRole('button', { name: `Expand lock session ${holderPid}` })
    if (await expand.isVisible()) await expand.click()
    const waiter = page
      .getByRole('treeitem')
      .filter({ hasText: new RegExp(`PID ${waiterPid}(?!\\d)`) })
      .last()
    await expect(waiter).toBeVisible()
    await expect(detailCell(waiter, 'Lock mode')).toHaveText('ShareLock')
    await expect(detailCell(waiter, 'Waits on')).toHaveText('transactionid')
    await page.screenshot({ path: `${ARTIFACT_DIR}/dl-e2e-001-lock-mode.png` })
    await pair.release()

    // An episode left open by an earlier run (it holds about twelve minutes, and
    // a full grace again after a server restart) is not evidence of these
    // deadlocks: it is recorded before provoking them, with the time until which
    // an earlier deadlock sample can still refresh it.
    const prior = new Map(
      (await deadlockAlerts(api, instanceId, datname)).map((alert) => [alert.alert_key, alert]),
    )
    const staleUntil = await stalePositiveUntil(api, instanceId, datname)

    // The Locks page is open before the deadlocks, so its tile is read as it
    // refetches: it shows each database's newest sample only, which is positive
    // for one or two scrapes after a deadlock.
    await expect(page.getByRole('article', { name: 'Deadlock rate' })).toBeVisible({
      timeout: 30_000,
    })
    const provokedAt = Date.now()
    const measured = await deadlocks(3)
    expect(
      measured,
      'pg_stat_database.deadlocks grew by the provoked deadlocks',
    ).toBeGreaterThanOrEqual(3)
    test.info().annotations.push({ type: 'DL-E2E-001 deadlocks', description: String(measured) })
    const deadline = provokedAt + BUDGET.deadlockAlert
    const remaining = () => Math.max(1_000, deadline - Date.now())

    // The activity API and the tile both read the newest sample of each
    // database (deadlockRate, as the UI does), so both are polled together.
    let apiSeen = false
    let tileSeen = false
    await expect
      .poll(
        async () => {
          const activity = (await (
            await api.get(`/api/v1/instances/${instanceId}/activity`)
          ).json()) as { metrics: Parameters<typeof deadlockRate>[0] }
          apiSeen ||= (deadlockRate(activity.metrics) ?? 0) > 0
          tileSeen ||= (await tileNumber(page, 'Deadlock rate')) > 0
          return apiSeen && tileSeen
        },
        {
          message: 'a positive deadlock rate in the activity API and the Locks page tile',
          intervals: [2_000],
          timeout: remaining(),
        },
      )
      .toBe(true)
    await page.screenshot({ path: `${ARTIFACT_DIR}/dl-e2e-001-locks.png` })

    await page.goto(`/instances/${instanceId}?db=${encodeURIComponent(datname)}&range=1h`)
    await expect
      .poll(() => tileNumber(page, 'Deadlocks in range'), {
        message: 'the Overview Deadlocks in range tile counts the deadlocks',
        intervals: [5_000],
        timeout: remaining(),
      })
      .toBeGreaterThanOrEqual(1)
    await page.screenshot({ path: `${ARTIFACT_DIR}/dl-e2e-001-overview.png` })

    // A new episode starts at the first positive sample, scraped after the
    // deadlocks. An episode already open is refreshed only on a pass that sees a
    // positive sample in the lookback window; once the earlier samples have left
    // it (staleUntil) and the deadlocks were provoked, a refresh read a new one.
    let evidence: { alert: LiveAlert; kind: 'new episode' | 'refreshed episode' } | undefined
    await expect
      .poll(
        async () => {
          for (const alert of await deadlockAlerts(api, instanceId, datname)) {
            if (alert.state !== 'firing') continue
            const before = prior.get(alert.alert_key)
            if (Date.parse(alert.started_at) >= provokedAt - 2_000) {
              evidence = { alert, kind: 'new episode' }
            } else if (
              before?.started_at === alert.started_at &&
              Date.parse(alert.last_eval_at) > Math.max(provokedAt, staleUntil)
            ) {
              evidence = { alert, kind: 'refreshed episode' }
            }
          }
          return evidence !== undefined
        },
        {
          message: `db.deadlock_rate firing for database ${datname} on these deadlocks`,
          intervals: [5_000],
          timeout: BUDGET.deadlockAlert,
        },
      )
      .toBe(true)
    const firedAfter = Date.now() - provokedAt
    test.info().annotations.push({
      type: 'DL-E2E-001 alert',
      description: `${evidence?.kind}: firing ${Math.round(firedAfter / 1000)} s after the deadlocks were provoked (earlier samples readable until ${Number.isFinite(staleUntil) ? new Date(staleUntil).toISOString() : 'none'}; started ${evidence?.alert.started_at}, last evaluated ${evidence?.alert.last_eval_at}, value ${evidence?.alert.value})`,
    })
    console.log(
      `DL-E2E-001: ${measured} deadlocks, db.deadlock_rate ${evidence?.kind} after ${firedAfter} ms`,
    )
    browserErrors.assertClean()
  } finally {
    await runCleanups()
  }
})

/**
 * The banner's count, "x of y checks have a current result" above the page or
 * "x/y checks" in the header row, or null when it reads otherwise.
 */
function coverageCounts(text: string): { reported: number; total: number } | null {
  const match = /(\d+)(?: of |\/)(\d+) checks/.exec(text)
  return match ? { reported: Number(match[1]), total: Number(match[2]) } : null
}

/** What getCoverage reports for the instance now. */
async function liveCoverage(
  api: Parameters<typeof pickInstance>[0],
  instanceId: string,
): Promise<{
  agentStartedAt: string | null
  reported: number
  total: number
  pending: string[]
  failed: string[]
}> {
  const response = await api.get(`/api/v1/instances/${instanceId}/coverage`)
  expect(response.status(), 'getCoverage answers').toBe(200)
  const body = (await response.json()) as {
    agent_started_at: string | null
    reported: number
    total: number
    checks: { check: string; state: string }[]
  }
  const named = (state: string) =>
    body.checks.filter((check) => check.state === state).map((check) => check.check)
  return {
    agentStartedAt: body.agent_started_at,
    reported: body.reported,
    total: body.total,
    pending: named('pending'),
    failed: named('failed'),
  }
}

test('COV-E2E-001 after an agent restart the data-collection banner counts checks with a current result, fits Full HD, and disappears when all have one', async ({
  api,
  signedInPage: page,
}) => {
  // The banner is polled for up to 15 minutes, the cold-start acceptance (13.3).
  test.setTimeout(liveTimeout(0, 900_000))
  const startedAt = Date.now()
  const deadline = startedAt + 900_000
  const remaining = () => Math.max(1_000, deadline - Date.now())
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)

  // This spec only means something right after the agent starts: it runs
  // within 2 minutes of the restart in 13.3 S3 or of a G-DEPLOY that restarts
  // the agent, and is not part of any later "all specs" run.
  const restartInstruction =
    'COV-E2E-001 must run within 2 minutes of an agent restart: run `docker compose restart pglens-agent` (or G-DEPLOY) in the stack directory, then start this spec again'
  // Right after a restart the server may still hold the previous agent's
  // start until the new agent's first push lands, so the start is polled for
  // up to 60 s before the spec gives up.
  // An object, so the closure's write is seen by the catch below.
  const agent: { startedAt: string | null } = { startedAt: null }
  const startAge = async () => {
    agent.startedAt = (await liveCoverage(api, instanceId)).agentStartedAt
    const started = agent.startedAt === null ? NaN : Date.parse(agent.startedAt)
    return Number.isFinite(started) ? (startedAt - started) / 1000 : Infinity
  }
  try {
    await expect
      .poll(startAge, { intervals: [2_000, 5_000], timeout: 60_000 })
      .toBeLessThanOrEqual(120)
  } catch (error) {
    const seen = agent.startedAt
    throw new Error(
      `${restartInstruction} (${seen === null ? 'the agent reports no start time' : `the agent started at ${seen}, more than 120 s before this test`})`,
      { cause: error },
    )
  }

  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.goto(`/instances/${instanceId}`)
  const banner = page.getByTestId('coverage-banner')
  await expect(banner, restartInstruction).toBeVisible({ timeout: 30_000 })
  await expect(banner).toHaveAttribute('role', 'status')
  await expect(banner).toHaveAttribute('aria-live', 'polite')
  await expect(banner).toHaveAttribute('data-caption-id', 'instance.coverage')
  // From 1600 px the banner is a line of the header row, so it adds no height
  // to the page (D26); below it, it sits above the page.
  await expect(
    banner.locator('xpath=ancestor::header'),
    'at 1920 px the banner is inside the header',
  ).toHaveCount(1)
  const first = coverageCounts((await banner.textContent()) ?? '')
  expect(first, 'the banner says "x/y checks"').not.toBeNull()
  expect(first!.reported).toBeLessThan(first!.total)
  test.info().annotations.push({
    type: 'COV-E2E-001 first',
    description: `${first!.reported} of ${first!.total} checks at ${new Date().toISOString()}`,
  })

  // C-13: the banner at a laptop and a phone width, above the page there.
  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport)
    await expect(banner).toBeVisible()
    await expect(banner.locator('xpath=ancestor::main')).toHaveCount(1)
    await page.screenshot({ path: `${ARTIFACT_DIR}/cov-e2e-001-banner-${viewport.width}.png` })
  }

  // I-13: each instance dashboard that shows the banner still fits a Full HD
  // screen with the banner on it. The banner is visible before the page is
  // measured and still visible after its last viewport: it only goes once
  // coverage is complete, so it was there for every measure. These run first,
  // while the heavy checks are still pending.
  const values = await routeValues(api)
  for (const path of [
    '/instances/:instanceId?section=overview',
    '/instances/:instanceId/queries',
    '/instances/:instanceId/locks',
  ]) {
    const route = fullHdRoute(path)
    await page.setViewportSize({ width: 1920, height: 1080 })
    await page.goto(routeUrl(route, values))
    await expect(banner, `${path}: the banner is on the page before it is measured`).toBeVisible({
      timeout: 30_000,
    })
    if (route.pendingUnit !== undefined) {
      // As in FHD-FIT-001, a skip is never a pass: the owning unit runs the fit check.
      test.info().annotations.push({
        type: 'UNVERIFIED',
        description: `COV-E2E-001 ${path}: fit pending ${route.pendingUnit}; the banner is visible on it`,
      })
      continue
    }
    await expect(
      banner.locator('xpath=ancestor::header'),
      `${path}: at 1920 px the banner is inside the header`,
    ).toHaveCount(1)
    // The crumbs hold their content width before the coverage sentence does.
    const crumb = page
      .getByRole('navigation', { name: 'Breadcrumb' })
      .locator('[aria-current="page"]')
    await expect(crumb, `${path}: at 1920 px the current crumb is on the page`).toBeVisible()
    const crumbFit = await crumb.evaluate((el) => ({
      scroll: el.scrollWidth,
      client: el.clientWidth,
    }))
    expect(
      crumbFit.scroll,
      `${path}: at 1920 px the current crumb is not clipped`,
    ).toBeLessThanOrEqual(crumbFit.client)
    await assertFits(page, api, route)
    await expect(banner, `${path}: the banner was on the page while it was measured`).toBeVisible()
    await expect(banner.locator('xpath=ancestor::header')).toHaveCount(1)
  }

  // R-13.3b: at 1600×950, the narrowest width with the banner in the header,
  // the header row keeps its height, the status slot ends before the
  // controls, and each fit route still fits with the banner visible.
  for (const path of [
    '/instances/:instanceId/queries',
    '/instances/:instanceId?section=overview',
  ]) {
    const route = fullHdRoute(path)
    await page.setViewportSize({ width: 1600, height: 950 })
    await page.goto(routeUrl(route, values))
    await expect(banner, `${path} at 1600 px: the banner is visible`).toBeVisible({
      timeout: 30_000,
    })
    await expect(
      banner.locator('xpath=ancestor::header'),
      `${path} at 1600 px: the banner is inside the header`,
    ).toHaveCount(1)
    await page.waitForLoadState('networkidle')
    await expect(page.locator('[aria-busy="true"]')).toHaveCount(0, { timeout: 30_000 })
    const box = async (testId: string) => {
      const found = await page.getByTestId(testId).boundingBox()
      expect(found, `${path} at 1600 px: ${testId} is laid out`).not.toBeNull()
      return found!
    }
    const row = await box('header-row')
    const status = await box('header-status')
    const controls = await box('header-controls')
    expect(row.height, `${path} at 1600 px: header row height`).toBeLessThanOrEqual(57)
    expect(
      status.x + status.width,
      `${path} at 1600 px: the status slot ends before the controls`,
    ).toBeLessThanOrEqual(controls.x)
    if (path.includes('section=overview')) {
      // The section tabs end before the facts strip, so no fact sits under a tab.
      const tabs = await page
        .getByTestId('instance-section-tabs')
        .locator('> *')
        .first()
        .boundingBox()
      const facts = await page.locator('[data-testid="instance-header"] dl').boundingBox()
      expect(tabs, `${path} at 1600 px: the tab list is laid out`).not.toBeNull()
      expect(facts, `${path} at 1600 px: the facts strip is laid out`).not.toBeNull()
      expect(
        tabs!.x + tabs!.width,
        `${path} at 1600 px: the section tabs end before the facts strip`,
      ).toBeLessThanOrEqual(facts!.x + 1)
    }
    await expect(page.getByTestId('coverage-count')).toBeVisible()
    const countBox = await box('coverage-count')
    expect(
      countBox.x + countBox.width,
      `${path} at 1600 px: the coverage count is not clipped by the status slot`,
    ).toBeLessThanOrEqual(status.x + status.width + 1)
    if (route.kind === 'fit' && route.pendingUnit === undefined) {
      const { scrollHeight, innerHeight, scrollWidth, innerWidth } = await page.evaluate(() => ({
        scrollHeight: document.scrollingElement?.scrollHeight ?? 0,
        innerHeight: window.innerHeight,
        scrollWidth: document.scrollingElement?.scrollWidth ?? 0,
        innerWidth: window.innerWidth,
      }))
      expect(scrollWidth, `${path} at 1600×950: horizontal scrollbar`).toBeLessThanOrEqual(
        innerWidth,
      )
      expect(scrollHeight, `${path} at 1600×950: page scrollbar`).toBeLessThanOrEqual(innerHeight)
    }
    await expect(banner, `${path} at 1600 px: the banner was visible while measured`).toBeVisible()
    await page.screenshot({
      path: `${ARTIFACT_DIR}/cov-e2e-001-header-1600-${path.includes('queries') ? 'queries' : 'overview'}.png`,
    })
  }
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.goto(`/instances/${instanceId}`)

  // The count rises as the console polls (every 15 s); once the banner is gone
  // the server's own count stands for it.
  const reportedNow = async () => {
    if (await banner.isVisible()) {
      const counts = coverageCounts((await banner.textContent()) ?? '')
      if (counts !== null) return counts.reported
    }
    return (await liveCoverage(api, instanceId)).reported
  }
  await expect
    .poll(reportedNow, {
      message: `the banner count rises above ${first!.reported}`,
      intervals: [5_000],
      timeout: remaining(),
    })
    .toBeGreaterThan(first!.reported)

  // A failing check keeps the banner (its data is missing), so on a timeout
  // the message names what is still missing.
  try {
    await expect(banner).toBeHidden({ timeout: remaining() })
  } catch (error) {
    const missing = await liveCoverage(api, instanceId)
    throw new Error(
      `the banner disappears once every check has a current result; now ${missing.reported} of ${missing.total}, pending: ${missing.pending.join(', ') || 'none'}; failed: ${missing.failed.join(', ') || 'none'}\n${error instanceof Error ? error.message : String(error)}`,
      { cause: error },
    )
  }
  const final = await liveCoverage(api, instanceId)
  expect(final.pending, 'no check is still pending').toEqual([])
  expect(final.failed, 'no check failed on its last run').toEqual([])
  expect(final.reported).toBe(final.total)
  await page.screenshot({ path: `${ARTIFACT_DIR}/cov-e2e-001-complete.png` })
  test.info().annotations.push({
    type: 'COV-E2E-001 complete',
    description: `${final.reported} of ${final.total} checks at ${new Date().toISOString()}`,
  })
  browserErrors.assertClean()
})

/** The term the full-search checks look for on the Odoo stack (12.3, D20). */
const SEARCH_TERM = 'res_partner'
/** A term that matches nothing the stack collects. */
const NO_MATCH_TERM = 'zzzzqqqq'

interface StatementsBody {
  statements: { queryid: string | number }[]
  has_more?: boolean
}

interface SearchBody {
  groups: {
    kind: 'statements' | 'indexes' | 'tables' | 'columns'
    total: number
    truncated: boolean
    hits: { key: string; title: string; link: string }[]
  }[]
}

interface FindingBody {
  finding_id: string
  rule_id: string
  state: string
}

const escapeRegExp = (text: string) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

/** The next GET answer of `path` whose query string passes `match`. */
function apiResponse(
  page: Page,
  path: string,
  match: (params: URLSearchParams) => boolean,
): Promise<PageResponse> {
  return page.waitForResponse(
    (response) => {
      const url = new URL(response.url())
      return (
        url.pathname === path && response.request().method() === 'GET' && match(url.searchParams)
      )
    },
    { timeout: 30_000 },
  )
}

async function getJson<T>(api: APIRequestContext, path: string): Promise<T> {
  const response = await api.get(path)
  if (!response.ok()) throw new Error(`GET ${path} answered ${response.status()}`)
  return (await response.json()) as T
}

/**
 * Screenshots in the light theme, then in the dark one (the stored preference,
 * then a reload); `ready` waits for the searched state after the reload.
 */
async function shootBothThemes(
  page: Page,
  name: string,
  ready: () => Promise<void>,
): Promise<void> {
  await shoot(page, `${name}-light`)
  await page.evaluate(() => {
    const key = 'pglens.preferences'
    let stored: Record<string, unknown>
    try {
      stored = JSON.parse(localStorage.getItem(key) ?? '{}') as Record<string, unknown>
    } catch {
      stored = {}
    }
    localStorage.setItem(key, JSON.stringify({ ...stored, theme: 'dark' }))
  })
  await page.reload()
  await expect(page.locator('html')).toHaveClass(/\bdark\b/)
  await ready()
  await shoot(page, `${name}-dark`)
}

test('SRCH-E2E-001 Slowest queries and the Query inspector search the server past the ranked rows and mark the matches', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 2 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  // The page's own requests: same instance, ranking and time window.
  const ranked = (params: URLSearchParams) =>
    params.get('instance_id') === instanceId && params.get('order_by') === 'total_exec_time'
  const unfilteredAnswer = apiResponse(
    page,
    '/api/v1/statements',
    (params) => ranked(params) && !params.has('q'),
  )
  await page.goto(`/queries?instance=${instanceId}&rank=total&range=24h`)
  const unfiltered = (await (await unfilteredAnswer).json()) as StatementsBody
  const table = page.getByRole('table', { name: 'Slowest queries' })
  await expect(table).toBeVisible({ timeout: 30_000 })
  const firstPage = new Set(unfiltered.statements.slice(0, 20).map((row) => String(row.queryid)))

  const searchedAnswer = apiResponse(
    page,
    '/api/v1/statements',
    (params) => ranked(params) && params.get('q') === SEARCH_TERM,
  )
  await page.locator('#slow-search').fill(SEARCH_TERM)
  const searched = await searchedAnswer
  expect(searched.ok(), 'the statements search answers').toBe(true)
  await expect.poll(() => new URL(page.url()).searchParams.get('q')).toBe(SEARCH_TERM)
  const matched = (await searched.json()) as StatementsBody
  expect(
    matched.statements.length,
    `statements matching ${SEARCH_TERM} on instance ${instanceId}`,
  ).toBeGreaterThan(0)
  const pastFirstPage = matched.statements.filter((row) => !firstPage.has(String(row.queryid)))
  expect(
    pastFirstPage.length,
    `the search reaches statements outside the first 20 ranked rows on instance ${instanceId}`,
  ).toBeGreaterThan(0)

  // The footer counts what the server answered (I-9).
  const count = matched.statements.length
  const note =
    matched.has_more === true
      ? `More than ${count} matches — narrow the search.`
      : `${count} ${count === 1 ? 'match' : 'matches'}.`
  await expect(page.getByText(note, { exact: true })).toBeVisible()
  const rows = table.locator('tbody tr')
  await expect(rows).toHaveCount(Math.min(20, count))
  for (const text of await rows.allTextContents()) {
    expect(text.toLowerCase()).toContain(SEARCH_TERM)
  }
  const mark = table.locator('mark').first()
  await expect(mark).toBeVisible()
  expect(((await mark.textContent()) ?? '').toLowerCase()).toBe(SEARCH_TERM)
  await shootBothThemes(page, 'srch-e2e-001-queries', async () => {
    await expect(table.locator('mark').first()).toBeVisible({ timeout: 30_000 })
  })

  // The Query inspector takes the same `q` and marks the statement text.
  await page.goto(`/instances/${instanceId}/queries?q=${SEARCH_TERM}&range=24h`)
  await expect(page.locator('#statement-search')).toHaveValue(SEARCH_TERM)
  const statistics = page.getByRole('table', { name: 'Statement statistics' })
  await expect(statistics.locator('summary mark').first()).toBeVisible({ timeout: 30_000 })
  await expect(
    page.getByText(/^(?:\d+ match(?:es)?\.|More than \d+ matches — narrow the search\.)$/),
  ).toBeVisible()
  await shoot(page, 'srch-e2e-001-inspector')

  await assertFits(page, api, fullHdRoute('/queries?q=res_partner'))
  await assertFits(page, api, fullHdRoute('/instances/:instanceId/queries?q=res_partner'))
  browserErrors.assertClean()
})

test('SRCH-E2E-002 a term that matches nothing shows the no-match state on every search page', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 2 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)

  // Slowest queries: the empty state names the term and offers every statement back.
  await page.goto(`/queries?instance=${instanceId}&rank=total&range=24h`)
  const table = page.getByRole('table', { name: 'Slowest queries' })
  await expect(table).toBeVisible({ timeout: 30_000 })
  await page.locator('#slow-search').fill(NO_MATCH_TERM)
  await expect(page.getByText(`No statement matches “${NO_MATCH_TERM}” in this range`)).toBeVisible(
    {
      timeout: 30_000,
    },
  )
  await expect(table).toBeHidden()
  await expect(page.locator('#slow-search')).toHaveValue(NO_MATCH_TERM)
  await shoot(page, 'srch-e2e-002-queries')
  await page.getByRole('button', { name: 'Show all statements' }).click()
  await expect.poll(() => new URL(page.url()).searchParams.has('q')).toBe(false)
  await expect(table).toBeVisible({ timeout: 30_000 })
  await expect(page.locator('#slow-search')).toHaveValue('')

  // Index analysis: no group has a match.
  await page.goto(`/indexes?instance=${instanceId}&tab=indexes`)
  await expect(page.getByRole('table', { name: 'Indexes', exact: true })).toBeVisible({
    timeout: 30_000,
  })
  await page.locator('#index-search').fill(NO_MATCH_TERM)
  await expect(page.getByText(`Nothing matches “${NO_MATCH_TERM}” in this range`)).toBeVisible({
    timeout: 30_000,
  })
  await shoot(page, 'srch-e2e-002-indexes')
  await page.getByRole('button', { name: 'Clear the search' }).click()
  await expect.poll(() => new URL(page.url()).searchParams.has('q')).toBe(false)
  await expect(page.getByText(/^Nothing matches/)).toBeHidden()

  // Findings: no finding of any state matches.
  await page.goto('/findings')
  await expect(page.locator('#findings-search')).toBeVisible({ timeout: 30_000 })
  await page.locator('#findings-search').fill(NO_MATCH_TERM)
  await expect(page.getByText(`No finding matches “${NO_MATCH_TERM}”`)).toBeVisible({
    timeout: 30_000,
  })
  await expect(page.getByRole('button', { name: 'Show all findings' })).toBeVisible()
  await shoot(page, 'srch-e2e-002-findings')

  // Query inspector: the same empty state as Slowest queries, for this instance.
  await page.goto(`/instances/${instanceId}/queries?range=24h`)
  const statistics = page.getByRole('table', { name: 'Statement statistics' })
  await expect(statistics).toBeVisible({ timeout: 30_000 })
  await page.locator('#statement-search').fill(NO_MATCH_TERM)
  await expect(page.getByText(`No statement matches “${NO_MATCH_TERM}” in this range`)).toBeVisible(
    { timeout: 30_000 },
  )
  await expect(page.locator('#statement-search')).toHaveValue(NO_MATCH_TERM)
  await shoot(page, 'srch-e2e-002-inspector')
  await page.getByRole('button', { name: 'Show all statements' }).click()
  await expect.poll(() => new URL(page.url()).searchParams.has('q')).toBe(false)
  await expect(statistics).toBeVisible({ timeout: 30_000 })

  await assertFits(page, api, fullHdRoute('/queries'))
  await assertFits(page, api, fullHdRoute('/indexes?tab=indexes'))
  await assertFits(page, api, fullHdRoute('/findings'))
  browserErrors.assertClean()
})

test('SRCH-E2E-003 Index analysis searches everything in groups and a hit opens its index row', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 2 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  // The inventory exception (12.2): at 1920×950 the results leave the table header on screen.
  await page.setViewportSize({ width: 1920, height: 950 })
  await page.goto(`/indexes?instance=${instanceId}&tab=indexes`)
  const indexes = page.getByRole('table', { name: 'Indexes', exact: true })
  await expect(indexes).toBeVisible({ timeout: 30_000 })

  const searchPath = `/api/v1/instances/${instanceId}/search`
  const answer = apiResponse(page, searchPath, (params) => params.get('q') === SEARCH_TERM)
  await page.locator('#index-search').fill(SEARCH_TERM)
  const response = await answer
  expect(response.ok(), 'the instance search answers').toBe(true)
  const body = (await response.json()) as SearchBody
  const searchedWindow = new URL(response.url()).searchParams
  await expect(
    page.getByRole('heading', { level: 2, name: `Search results for “${SEARCH_TERM}”` }),
  ).toBeVisible()

  const groupOf = (kind: SearchBody['groups'][number]['kind']) => {
    const group = body.groups.find((candidate) => candidate.kind === kind)
    if (!group) throw new Error(`the search answers no ${kind} group on instance ${instanceId}`)
    return group
  }
  const labels = { indexes: 'Indexes', tables: 'Tables', columns: 'Columns', statements: 'Queries' }
  for (const [kind, label] of Object.entries(labels) as [keyof typeof labels, string][]) {
    const group = groupOf(kind)
    expect(
      group.total,
      `${label} matching ${SEARCH_TERM} on instance ${instanceId}`,
    ).toBeGreaterThan(0)
    const list = page.getByRole('list', { name: `${label} found` })
    await expect(list).toBeVisible()
    await expect(list.getByRole('listitem')).toHaveCount(group.hits.length)
    if (group.truncated) {
      const section = page.locator('section', {
        has: page.getByRole('heading', {
          level: 3,
          name: new RegExp(`^${label}\\s*[\\d,]+ match`),
        }),
      })
      await expect(section.getByText(new RegExp(`^Showing ${group.hits.length} of `))).toBeVisible()
    }
  }

  // A named object is a hit of its group; a group lists its first 50 in name
  // order, so past them the narrower term (a match of the same rule) finds it.
  async function assertHit(kind: 'tables' | 'columns', title: string, narrower: string) {
    const group = groupOf(kind)
    if (group.hits.some((hit) => hit.title === title)) {
      const list = page.getByRole('list', { name: `${labels[kind]} found` })
      await expect(list.getByText(title, { exact: true }).first()).toBeAttached()
      return
    }
    expect(group.truncated, `${title} is among the ${kind} hits on instance ${instanceId}`).toBe(
      true,
    )
    const params = new URLSearchParams({ q: narrower, kinds: kind })
    for (const key of ['from', 'to']) {
      const value = searchedWindow.get(key)
      if (value !== null) params.set(key, value)
    }
    const narrowed = await getJson<SearchBody>(api, `${searchPath}?${params.toString()}`)
    const hits = narrowed.groups.find((candidate) => candidate.kind === kind)?.hits ?? []
    expect(
      hits.map((hit) => hit.title),
      `${title} matches ${SEARCH_TERM} on instance ${instanceId}`,
    ).toContain(title)
  }
  await assertHit('tables', 'public.res_partner', 'public.res_partner')
  await assertHit('columns', 'public.res_partner.name', 'res_partner.name')
  const indexHit = groupOf('indexes').hits.find((hit) => /^[^.]+\.res_partner_/.test(hit.title))
  if (!indexHit) {
    throw new Error(`no res_partner_* index among the index hits on instance ${instanceId}`)
  }

  // The table header stays on a 1920×950 screen under the results (12.2 inventory exception).
  await page.evaluate(() => window.scrollTo(0, 0))
  const header = await indexes.locator('thead').boundingBox()
  if (!header) throw new Error('the Indexes table has no header box')
  const pageHeight = await page.evaluate(() => document.scrollingElement?.scrollHeight ?? 0)
  const headerBottom = Math.round(header.y + header.height)
  console.log(
    `SRCH-E2E-003 1920×950 search results: Indexes header top ${Math.round(header.y)} px, bottom ${headerBottom} px, page height ${pageHeight} px`,
  )
  test.info().annotations.push({
    type: 'SRCH-E2E-003 Indexes header at 1920×950',
    description: `top ${Math.round(header.y)} px, bottom ${headerBottom} px, page height ${pageHeight} px`,
  })
  expect(header.y, 'Indexes header below the top of the screen').toBeGreaterThanOrEqual(0)
  expect(headerBottom, 'Indexes header on a 1920×950 screen').toBeLessThanOrEqual(950)

  await shootBothThemes(page, 'srch-e2e-003-indexes', async () => {
    await expect(page.getByRole('list', { name: 'Indexes found' })).toBeVisible({
      timeout: 30_000,
    })
  })

  // The hit opens the Indexes tab with its row expanded; the focus leaves the URL.
  await page.setViewportSize({ width: 1920, height: 950 })
  await page
    .getByRole('list', { name: 'Indexes found' })
    .getByRole('link')
    .filter({ has: page.getByText(indexHit.title, { exact: true }) })
    .first()
    .click()
  await expect.poll(() => new URL(page.url()).searchParams.get('tab')).toBe('indexes')
  await expect.poll(() => new URL(page.url()).searchParams.has('focus')).toBe(false)
  const indexName = indexHit.title.slice(indexHit.title.indexOf('.') + 1)
  await expect(
    indexes.getByRole('button', { name: `Hide details of ${indexName}`, exact: true }).first(),
  ).toHaveAttribute('aria-expanded', 'true', { timeout: 30_000 })
  await expect(page.getByText(`Search results for “${SEARCH_TERM}”`)).toBeHidden()
  await page.screenshot({ path: `${ARTIFACT_DIR}/srch-e2e-003-focused-1920x950.png` })

  await assertFits(page, api, fullHdRoute('/indexes?tab=indexes'))
  browserErrors.assertClean()
})

test('SRCH-E2E-004 Findings search narrows the list to a live rule and All states shows muted and resolved matches', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 2 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  // The fragment comes from the live findings: a rule whose matches include a
  // muted or resolved finding and fewer than every finding.
  const all = await getJson<FindingBody[]>(api, '/api/v1/findings?state=all&limit=1000')
  const inactive = (finding: FindingBody) =>
    finding.state === 'muted' || finding.state === 'resolved'
  const perRule = new Map<string, number>()
  for (const finding of all) perRule.set(finding.rule_id, (perRule.get(finding.rule_id) ?? 0) + 1)
  const ruleIds = [...perRule.keys()]
    .filter((ruleId) => all.some((finding) => finding.rule_id === ruleId && inactive(finding)))
    .sort((a, b) => (perRule.get(a) ?? 0) - (perRule.get(b) ?? 0))
  let pick: { fragment: string; matches: FindingBody[] } | undefined
  let fallback: typeof pick
  for (const ruleId of ruleIds) {
    const matches = await getJson<FindingBody[]>(
      api,
      `/api/v1/findings?state=all&limit=1000&q=${encodeURIComponent(ruleId)}`,
    )
    if (matches.length === 0 || matches.length >= all.length || !matches.some(inactive)) continue
    // Best: open and inactive matches, all on the first page.
    if (matches.some((finding) => !inactive(finding)) && matches.length <= 20) {
      pick = { fragment: ruleId, matches }
      break
    }
    fallback ??= { fragment: ruleId, matches }
  }
  pick ??= fallback
  if (!pick) {
    throw new Error(
      `no rule of the ${all.length} live findings has a muted or resolved finding whose search narrows the list`,
    )
  }
  const { fragment, matches } = pick
  const total = matches.length
  const activeCount = matches.filter((finding) => !inactive(finding)).length
  test.info().annotations.push({
    type: 'SRCH-E2E-004 fragment',
    description: `${fragment}: ${total} of ${all.length} findings, ${activeCount} open or degraded`,
  })

  await page.goto('/findings')
  await expect(page.locator('#findings-search')).toBeVisible({ timeout: 30_000 })
  await page.locator('#findings-search').fill(fragment)
  await expect.poll(() => new URL(page.url()).searchParams.get('q')).toBe(fragment)
  const shown = (visible: number) =>
    page.getByText(
      new RegExp(`^${visible} of ${total} findings matching “${escapeRegExp(fragment)}” shown`),
    )
  // By default the open and degraded matches only.
  await expect(shown(activeCount)).toBeVisible({ timeout: 30_000 })
  const table = page.getByRole('table', { name: 'Advisor findings' })
  // The Rule cell of every row on the open page.
  const ruleCells = () =>
    table.evaluate((element) => {
      const heads = [...element.querySelectorAll('thead tr:first-child > *')]
      const column = heads.findIndex((cell) => (cell.textContent ?? '').trim() === 'Rule')
      return [...element.querySelectorAll('[data-testid="simple-table-rows"] > tr')].map((row) =>
        column < 0 ? null : (row.children[column]?.textContent ?? '').trim(),
      )
    })
  const assertRules = async (where: string) => {
    for (const rule of await ruleCells()) {
      expect(rule, `${where}: a row whose Rule cell lacks ${fragment}`).toContain(fragment)
    }
  }
  if (activeCount > 0) {
    await expect(table.getByTestId('simple-table-rows').locator(':scope > tr')).toHaveCount(
      Math.min(20, activeCount),
    )
    await assertRules('open and degraded matches')
  }

  const allStates = page.getByRole('button', { name: 'All states', exact: true })
  await allStates.click()
  await expect(allStates).toHaveAttribute('aria-pressed', 'true')
  await expect.poll(() => new URL(page.url()).searchParams.get('state')).toBe('all')
  await expect(shown(total)).toBeVisible()
  await assertRules('all states, page 1')
  // A muted or resolved match is on screen: on the first page, or further on
  // when the fragment is the fallback with more than one page of matches.
  const inactiveCell = table.getByRole('cell', { name: /^(?:muted|resolved)$/ }).first()
  const pages = Math.ceil(total / 20)
  const maxPages = Math.min(pages, 10)
  let inactiveOn: number | null = (await inactiveCell.isVisible()) ? 1 : null
  for (let at = 2; inactiveOn === null && at <= maxPages; at += 1) {
    await page.getByRole('button', { name: 'Next page' }).click()
    await expect(page.getByText(new RegExp(`Page ${at} of ${pages}`))).toBeVisible()
    await assertRules(`all states, page ${at}`)
    if (await inactiveCell.isVisible()) inactiveOn = at
  }
  if (inactiveOn === null && maxPages < pages) {
    // Never a silent skip: the run says what it could not check.
    test.info().annotations.push({
      type: 'UNVERIFIED',
      description: `SRCH-E2E-004: no muted or resolved ${fragment} match in the first ${maxPages} of ${pages} pages`,
    })
  } else {
    expect(
      inactiveOn,
      `a muted or resolved ${fragment} match among ${pages} page(s) of All states`,
    ).not.toBeNull()
  }
  if (inactiveOn !== null && inactiveOn > 1) {
    test.info().annotations.push({
      type: 'SRCH-E2E-004 inactive match',
      description: `${fragment}: first muted or resolved match on page ${inactiveOn} of ${pages}`,
    })
  }
  if (pages > 1 && inactiveOn !== 1) {
    // Back to page 1 for the screenshots.
    await page.getByRole('button', { name: 'First page' }).click()
  }
  await shootBothThemes(page, 'srch-e2e-004-findings', async () => {
    await expect(shown(total)).toBeVisible({ timeout: 30_000 })
  })

  await assertFits(page, api, fullHdRoute('/findings?q=index'))
  browserErrors.assertClean()
})

test('VIS-RDY-001 readiness banner and tab reflect the real report, keep the header row, fit Full HD, offer copy buttons', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 3 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  const response = await api.get(`/api/v1/instances/${instanceId}/readiness`)
  expect(response.ok()).toBe(true)
  const report = (await response.json()) as {
    level: 'full' | 'partial'
    counts: { warning: number; info: number }
    items: { id: string; severity: string; fixes: { snippet?: string }[] }[]
  }
  const banner = page.getByTestId('readiness-banner')
  const values = await routeValues(api)

  // The tab at Full HD: items listed, snippets with a copy button, fits (I-13).
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.goto(`/instances/${instanceId}?section=readiness`)
  await expect(page.getByRole('heading', { name: 'Readiness', level: 2 })).toBeVisible()
  if (report.items.length === 0) {
    await expect(page.getByText('Monitored in full')).toBeVisible()
  } else {
    await expect(page.getByTestId('readiness-item')).toHaveCount(report.items.length)
    const buttons = page.getByRole('button', { name: 'Show details' })
    for (let index = (await buttons.count()) - 1; index >= 0; index -= 1) {
      await buttons.nth(index).click()
    }
    const snippets = report.items.flatMap((item) => item.fixes.filter((fix) => fix.snippet))
    if (snippets.length > 0) {
      await expect(page.getByRole('button', { name: /^Copy/ }).first()).toBeVisible()
    }
    await page.screenshot({ path: `${ARTIFACT_DIR}/vis-rdy-001-readiness-1920-open.png` })
    for (
      let index = 0;
      index < (await page.getByRole('button', { name: 'Hide details' }).count());
    ) {
      await page.getByRole('button', { name: 'Hide details' }).first().click()
    }
  }
  await assertFits(page, api, fullHdRoute('/instances/:instanceId?section=readiness'))

  // R-13.3b: the banner beside the coverage banner, with a 64-character name,
  // keeps the header row (<= 57 px) and ends before the controls, at 1600 and 1920 px.
  // The crumb and the page heading read the cluster list and the instance
  // record, each with an optional query string: match both by pattern and set
  // every display_name in the payload.
  const longName = 'n'.repeat(64)
  const renameAll = (value: unknown): void => {
    if (Array.isArray(value)) {
      value.forEach(renameAll)
    } else if (value !== null && typeof value === 'object') {
      const record = value as Record<string, unknown>
      if ('display_name' in record) record.display_name = longName
      Object.values(record).forEach(renameAll)
    }
  }
  await page.route(
    new RegExp(`/api/v1/(clusters|instances/${instanceId})(\\?.*)?$`),
    async (route) => {
      const original = await route.fetch()
      const body = (await original.json()) as unknown
      renameAll(body)
      await route.fulfill({ response: original, json: body })
    },
  )
  // Both banners at once: a coverage payload with 22 of 31 checks, 8 pending and
  // one failed check with a long name, so the sentence is as long as it gets.
  await page.route(
    new RegExp(`/api/v1/instances/${instanceId}/coverage(\\?.*)?$`),
    async (route) => {
      const original = await route.fetch()
      const body = (await original.json()) as Record<string, unknown>
      const checkAt = (index: number) => ({
        check: index === 22 ? 'a_failed_check_with_a_rather_long_name' : `check_${index}`,
        interval_seconds: 30,
        state: index < 22 ? 'reported' : index === 22 ? 'failed' : 'pending',
        first_result_at: index < 22 ? '2026-11-04T10:00:00Z' : null,
        last_result_at: index < 22 ? '2026-11-04T10:01:00Z' : null,
        expected_by: index > 22 ? new Date(Date.now() + 5 * 60_000).toISOString() : null,
      })
      await route.fulfill({
        response: original,
        json: {
          ...body,
          checks: Array.from({ length: 31 }, (_, index) => checkAt(index)),
          reported: 22,
          total: 31,
        },
      })
    },
  )
  for (const viewport of [
    { width: 1600, height: 950 },
    { width: 1920, height: 1080 },
  ]) {
    await page.setViewportSize(viewport)
    await page.goto(routeUrl(fullHdRoute('/instances/:instanceId?section=overview'), values))
    await page.waitForLoadState('networkidle')
    await expect(page.locator('[aria-busy="true"]')).toHaveCount(0, { timeout: 30_000 })
    const where = `${viewport.width} px`
    if (report.level === 'full') {
      await expect(banner, `${where}: no warning, no banner`).toHaveCount(0)
      continue
    }
    await expect(banner, `${where}: banner visible`).toBeVisible({ timeout: 30_000 })
    await expect(
      page.getByTestId('header-start'),
      `${where}: the 64-character name stub is on the page`,
    ).toContainText(longName)
    await expect(banner).toContainText(`Partial · ${report.counts.warning} warning`)
    await expect(banner).toHaveAttribute('data-caption-id', 'instance.readiness')
    await expect(
      banner.locator('xpath=ancestor::header'),
      `${where}: banner in the header`,
    ).toHaveCount(1)
    const box = async (testId: string) => {
      const found = await page.getByTestId(testId).boundingBox()
      expect(found, `${where}: ${testId} is laid out`).not.toBeNull()
      return found!
    }
    const row = await box('header-row')
    const status = await box('header-status')
    const controls = await box('header-controls')
    expect(row.height, `${where}: header row height`).toBeLessThanOrEqual(57)
    expect(
      status.x + status.width,
      `${where}: status slot ends before the controls`,
    ).toBeLessThanOrEqual(controls.x)
    const chip = await box('readiness-banner')
    expect(
      chip.x + chip.width,
      `${where}: the readiness chip is not clipped by the status slot`,
    ).toBeLessThanOrEqual(status.x + status.width + 1)
    await expect(
      page.getByTestId('coverage-count'),
      `${where}: coverage count visible`,
    ).toBeVisible()
    const count = await box('coverage-count')
    expect(
      count.x + count.width,
      `${where}: the coverage count is not clipped by the status slot`,
    ).toBeLessThanOrEqual(status.x + status.width + 1)
    const hint = await box('coverage-banner')
    expect(
      hint.x + hint.width,
      `${where}: the coverage banner (count and info icon) ends inside the status slot`,
    ).toBeLessThanOrEqual(status.x + status.width + 1)
    test.info().annotations.push({
      type: 'VIS-RDY-001 margin',
      description: `${where}: slot right ${Math.round(status.x + status.width)} controls left ${Math.round(controls.x)}`,
    })
    await page.screenshot({ path: `${ARTIFACT_DIR}/vis-rdy-001-header-${viewport.width}.png` })
  }
  browserErrors.assertClean()
})

interface LocksApi {
  sessions?: { origin?: 'application' | 'pglens' }[]
  connections?: {
    truncated: boolean
    by_origin: { key: string; count: number }[]
    totals?: { by_origin: { key: string; count: number }[] }
  }
}

test('VIS-LOCKS-001 the Locks page splits sessions by origin into two tables that match the API, fit Full HD and show the origin split', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 2 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const instanceId = await pickInstance(api)
  const locksApi = async (): Promise<LocksApi> => {
    const response = await api.get(`/api/v1/locks?instance_id=${instanceId}`)
    expect(response.ok()).toBe(true)
    return (await response.json()) as LocksApi
  }
  const countsOf = (body: LocksApi) => ({
    application: (body.sessions ?? []).filter((s) => s.origin === 'application').length,
    pglens: (body.sessions ?? []).filter((s) => s.origin === 'pglens').length,
  })
  const tables = {
    application: 'Application sessions',
    pglens: 'pglens monitoring sessions',
  } as const
  const tabs = { application: 'Application sessions', pglens: 'pglens sessions' } as const
  const shown = async (name: string, expected: number): Promise<number> => {
    if (expected === 0) return 0
    const table = page.getByRole('table', { name })
    await expect(table).toBeVisible({ timeout: 30_000 })
    return table.locator('tbody > tr').count()
  }

  await page.setViewportSize({ width: 1920, height: 1080 })
  const first = await locksApi()
  expect(
    (first.sessions ?? []).some((session) => session.origin !== undefined) ||
      first.connections !== undefined,
    'the server sends session origins (16.2)',
  ).toBe(true)

  // The page opens on the blocking view; each origin has its own tab.
  await page.goto(`/instances/${instanceId}/locks`)
  await expect(page.getByRole('tab', { name: 'Blocking' })).toHaveAttribute(
    'aria-selected',
    'true',
    { timeout: 30_000 },
  )
  await expect(page.getByRole('list', { name: 'Active sessions' })).toHaveCount(0)
  const by = first.connections?.truncated
    ? first.connections.totals?.by_origin
    : first.connections?.by_origin
  const count = (key: string) => by?.find((row) => row.key === key)?.count ?? 0
  await expect(
    page.getByText(`application ${count('application')} · pglens ${count('pglens')}`),
  ).toBeVisible({ timeout: 30_000 })
  await page.screenshot({ path: `${ARTIFACT_DIR}/vis-locks-001-blocking-1920.png` })

  for (const origin of ['application', 'pglens'] as const) {
    let expected = -1
    let rows = -2
    let total = -3
    // A sample lands every 10 s: compare against API reads taken around the page load.
    for (let attempt = 0; attempt < 4; attempt += 1) {
      const before = countsOf(await locksApi())[origin]
      await page.goto(`/instances/${instanceId}/locks?view=${origin}`)
      await expect(page.getByRole('tab', { name: tabs[origin] })).toHaveAttribute(
        'aria-selected',
        'true',
        { timeout: 30_000 },
      )
      if (before > 0) {
        await expect(page.getByRole('table', { name: tables[origin] })).toBeVisible({
          timeout: 30_000,
        })
      } else {
        await expect(
          page.getByText(
            origin === 'application'
              ? 'No application sessions at this moment'
              : 'No pglens sessions',
          ),
        ).toBeVisible({ timeout: 30_000 })
      }
      rows = await shown(tables[origin], before)
      const footer = page.getByText(/Showing \d+–\d+ of \d+ sessions/)
      total =
        before === 0
          ? 0
          : Number(/of (\d+) sessions/.exec((await footer.first().textContent()) ?? '')?.[1])
      expected = countsOf(await locksApi())[origin]
      if (before === expected && total === expected) break
    }
    expect(total, `${origin}: table total equals the API sessions of that origin`).toBe(expected)
    expect(rows, `${origin}: first page rows`).toBe(Math.min(20, expected))
    await page.screenshot({ path: `${ARTIFACT_DIR}/vis-locks-001-${origin}-1920.png` })
  }

  await page.goto(`/instances/${instanceId}/locks?view=activity`)
  await expect(page.getByRole('heading', { name: 'Connection summary' })).toBeVisible({
    timeout: 30_000,
  })
  await expect(page.getByRole('list', { name: 'Connections by origin' })).toBeVisible()
  await page.screenshot({ path: `${ARTIFACT_DIR}/vis-locks-001-activity-1920.png` })

  // assertFits measures both Full HD viewports (1920×1080 and 1920×950).
  for (const route of [
    '/instances/:instanceId/locks',
    '/instances/:instanceId/locks?view=application',
    '/instances/:instanceId/locks?view=pglens',
    '/instances/:instanceId/locks?view=activity',
  ]) {
    await assertFits(page, api, fullHdRoute(route))
  }
  browserErrors.assertClean()
})
