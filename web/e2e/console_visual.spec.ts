import { AxeBuilder } from '@axe-core/playwright'
import { execFile } from 'node:child_process'
import { join } from 'node:path'
import { promisify } from 'node:util'

import type { Page } from '@playwright/test'

import { expect, test } from './fixtures'
import { measureRoutes } from './fullhd_fit'
import { FULLHD_ROUTES } from './fullhd_routes'
import { INSTANCE_SECTIONS } from '../src/features/instance/instanceSections'
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

const STACK_DIR = process.env.PGLENS_QA_STACK_DIR ?? '/home/agent/pglens-odoo-stack'
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
