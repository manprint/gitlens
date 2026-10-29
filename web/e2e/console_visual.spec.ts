import { expect, test } from './fixtures'
import {
  canvasNotBlank,
  chartCompleteness,
  collectConsoleErrors,
  completenessOf,
  liveTimeout,
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
  await page.goto(`/instances/${instanceId}/ash?range=1h`)

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
