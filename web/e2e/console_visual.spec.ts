import { execFile } from 'node:child_process'
import { join } from 'node:path'
import { promisify } from 'node:util'

import { expect, test } from './fixtures'
import {
  canvasNotBlank,
  chartCompleteness,
  collectConsoleErrors,
  completenessOf,
  liveTimeout,
  pickHostInstance,
  pickInstance,
  shoot,
} from './visual_helpers'

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
    await expect(card.getByText(/UTC · step \d+[smhd]$/)).toBeVisible()
    // The data table fills once the metric query answers.
    await expect
      .poll(async () => (await chartCompleteness(page, 'Transactions per second')).total, {
        timeout: 3 * 30_000,
      })
      .toBeGreaterThan(0)

    const completeness = await chartCompleteness(page, 'Transactions per second')
    const history = completeness.leadingNull / completeness.total
    if (range === '6h' && history > 0.5) {
      test.skip(true, `UNVERIFIED: history ${((1 - history) * 6).toFixed(1)} h`)
    }
    expect(history).toBeLessThanOrEqual(0.5)
    expect(completeness.observedRatio).toBeGreaterThanOrEqual(0.95)
    expect(await canvasNotBlank(card)).toBe(true)

    await card.scrollIntoViewIfNeeded()
    await card.screenshot({
      path: `../test/e2e/_artifacts/ui/visual/vis-010-tps-${range}-card.png`,
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
    await card.screenshot({ path: `../test/e2e/_artifacts/ui/visual/vis-015-ash-${width}.png` })
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
    await lag.screenshot({ path: `../test/e2e/_artifacts/ui/visual/vis-015-lag-${width}.png` })
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
        path: `../test/e2e/_artifacts/ui/visual/vis-016-ash-${width}-card-${index}.png`,
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
    path: '../test/e2e/_artifacts/ui/visual/vis-016-ash-zoomed-card-1.png',
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
  await page.goto(`/instances/${instanceId}?range=1h#host-metrics`)

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
      path: `../test/e2e/_artifacts/ui/visual/vis-020-host-grid-${width}.png`,
    })
  }
  await shoot(page, 'vis-020-host')
  browserErrors.assertClean()
})
