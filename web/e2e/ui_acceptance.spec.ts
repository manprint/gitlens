import { AxeBuilder } from '@axe-core/playwright'
import { writeFile } from 'node:fs/promises'

import { expect, test } from './fixtures'

type JsonObject = Record<string, any>

test.describe.configure({ timeout: 180_000 })

async function json(response: any): Promise<any> {
  expect(response.ok(), `API request failed: ${response.url()}`).toBeTruthy()
  return response.json()
}

function arrayPayload(payload: any, key: string): JsonObject[] {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.[key])) return payload[key]
  if (Array.isArray(payload?.data)) return payload.data
  return []
}

async function clusters(api: any): Promise<JsonObject[]> {
  return arrayPayload(await json(await api.get('/api/v1/clusters')), 'clusters')
}

async function instances(api: any): Promise<JsonObject[]> {
  return arrayPayload(await json(await api.get('/api/v1/instances')), 'instances')
}

function instanceID(instance: JsonObject): string {
  return String(instance.instance_id ?? instance.id ?? instance.instanceId ?? '')
}

function instanceAddress(instance: JsonObject): string {
  return String(instance.address ?? instance.addr ?? instance.host ?? instance.name ?? instanceID(instance))
}

async function fleetContext(api: any, preferredTier?: string): Promise<{
  cluster: JsonObject
  instances: JsonObject[]
  instance: JsonObject
}> {
  const fleet = await clusters(api)
  expect(fleet.length, 'API must expose a populated fleet').toBeGreaterThan(0)
  const cluster = fleet[0]!
  const listed = Array.isArray(cluster.instances) ? cluster.instances : []
  const allInstances = listed.length > 0 ? listed : await instances(api)
  const clusterInstances = allInstances.filter(
    (item) => !item.cluster_id || item.cluster_id === cluster.cluster_id,
  )
  expect(clusterInstances.length, 'cluster must expose at least one instance').toBeGreaterThan(0)
  const wantedTier = preferredTier?.toUpperCase()
  const instance =
    (wantedTier
      ? clusterInstances.find(
          (item) => String(item.perm_tier ?? item.permission_tier ?? '').toUpperCase() === wantedTier,
        )
      : undefined) ??
    clusterInstances.find((item) => String(item.perm_tier ?? item.permission_tier ?? '').toUpperCase() === 'T1') ??
    clusterInstances[0]!
  return { cluster, instances: clusterInstances, instance }
}

function cardLocator(page: any): any {
  const list = page.getByRole('list', { name: 'Cluster cards' })
  const link = list.getByRole('link').first()
  const semanticCard = link.locator('xpath=ancestor::*[self::article or @role="listitem"][1]')
  return semanticCard
}

async function definitionValue(card: any, label: string): Promise<string> {
  return card
    .locator('dt')
    .filter({ hasText: label })
    .locator('xpath=following-sibling::dd[1]')
    .innerText()
}

async function reloadUntil(page: any, predicate: () => Promise<boolean>, timeout = 120_000): Promise<void> {
  await page.reload({ waitUntil: 'domcontentloaded' })
  await expect
    .poll(
      predicate,
      { timeout, intervals: [1_000, 3_000, 5_000] },
    )
    .toBe(true)
}

async function pollAPI(predicate: () => Promise<boolean>, timeout = 120_000): Promise<void> {
  await expect.poll(predicate, { timeout, intervals: [1_000, 3_000, 5_000] }).toBe(true)
}

async function navigateToFleet(page: any): Promise<void> {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Fleet overview' })).toBeVisible()
  await expect(page.getByRole('list', { name: 'Cluster cards' })).toBeVisible()
}

test('SYS-UI-001: failover preserves identity and raises an alert', async ({ signedInPage, api }) => {
  const page = signedInPage
  const before = await fleetContext(api)
  await navigateToFleet(page)

  const card = cardLocator(page)
  await expect(card).toBeVisible()
  const clusterID = String(before.cluster.cluster_id)
  const clusterLink = page.getByRole('list', { name: 'Cluster cards' }).getByRole('link').first()
  const linkLabel = await clusterLink.getAttribute('aria-label')
  expect(linkLabel).toContain(clusterID)
  const primaryAddress = await definitionValue(card, 'Primary')
  expect(primaryAddress).not.toBe('')

  const standby =
    before.instances.find((item) => {
      const role = String(item.role ?? item.state ?? '').toLowerCase()
      const id = instanceID(item)
      return id && id !== String(before.cluster.primary ?? '') && !role.includes('primary')
    }) ?? before.instances[1]
  expect(standby, 'primary-standby fixture must expose a standby').toBeTruthy()
  if (!standby) throw new Error('primary-standby fixture must expose a standby')
  const standbyID = instanceID(standby)
  const standbyAddress = instanceAddress(standby)
  expect(standbyID).not.toBe('')

  const readyFile = process.env.PGLENS_UI_READY_FILE
  expect(readyFile, 'Go runner must provide the failover handshake path').toBeTruthy()
  await writeFile(readyFile!, 'captured')

  await pollAPI(async () => {
    const current = (await clusters(api))[0]!
    const primary = JSON.stringify(current.primary ?? current.primary_instance_id ?? '').toLowerCase()
    return primary.includes(standbyID.toLowerCase()) || primary.includes(standbyAddress.toLowerCase())
  })

  await navigateToFleet(page)
  const afterCard = cardLocator(page)
  await expect(afterCard).toContainText(standbyAddress)
  await expect(afterCard).toContainText(clusterID)

  await page.goto(`/instances/${standbyID}`)
  await expect(page.getByText(/primary/i).first()).toBeVisible()
  await page.goto(`/clusters/${clusterID}`)
  await expect(page.getByRole('region', { name: 'Replication topology graph' })).toBeVisible()
  await expect(page.locator('body')).toContainText(standbyAddress)
  await expect(page.locator('body')).toContainText(/failover_detected/i)

  await page.goto('/events')
  await expect(page.locator('body')).toContainText(/failover_detected/i)
  await page.goto('/alerts')
  await expect(page.locator('body')).toContainText(/failover_detected/i)
  await expect(page.locator('body')).toContainText(/firing/i)

  const after = (await clusters(api))[0]!
  expect(after.cluster_id).toBe(clusterID)
})

test('SYS-UI-002: unauthenticated navigation cannot restore fleet data', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: /sign in|accedi/i })).toBeVisible()
  await expect(page.getByRole('list', { name: 'Cluster cards' })).toHaveCount(0)

  const unauthenticatedStatus = await page.evaluate(async () => (await fetch('/api/v1/clusters')).status)
  expect(unauthenticatedStatus).toBe(401)

  await page.getByLabel(/password/i).fill(process.env.PGLENS_UI_PASSWORD ?? '')
  await page.getByRole('button', { name: /sign in|accedi|login/i }).click()
  await expect(page.getByRole('heading', { name: 'Fleet overview' })).toBeVisible()
  await page.getByRole('button', { name: /sign out|esci|logout/i }).click()
  await expect(page.getByRole('heading', { name: /sign in|accedi/i })).toBeVisible()
  await page.goBack()
  await expect(page.getByRole('list', { name: 'Cluster cards' })).toHaveCount(0)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: /sign in|accedi/i })).toBeVisible()
  const backNavigationStatus = await page.evaluate(async () => (await fetch('/api/v1/clusters')).status)
  expect(backNavigationStatus).toBe(401)
})

test('SYS-UI-003: agent outage is visible as stale fleet data', async ({ signedInPage, api }) => {
  const page = signedInPage
  const { instance } = await fleetContext(api)
  const id = instanceID(instance)
  const address = instanceAddress(instance)

  await expect
    .poll(
      async () => {
        const current = await clusters(api)
        const observed = current
          .flatMap((cluster) => (Array.isArray(cluster.instances) ? cluster.instances : []))
          .find((item) => String(item.instance_id) === id)
        return {
          found: observed !== undefined,
          last_seen: observed?.last_seen,
          up: observed?.up,
        }
      },
      { timeout: 90_000, intervals: [1_000, 3_000, 5_000] },
    )
    .toMatchObject({ found: true, up: false })

  await navigateToFleet(page)

  await reloadUntil(
    page,
    async () => {
      const body = await page.locator('body').innerText()
      return /Agents requiring attention/i.test(body) && body.includes(address) && /Stale\s+—/i.test(body)
    },
    30_000,
  )
  await expect(page.getByRole('status', { name: /Stale data:/i }).first()).toBeVisible()
  await expect(page.locator('body')).toContainText(address)
})

test('SYS-UI-004: standalone replay lag stays unknown', async ({ signedInPage }) => {
  const page = signedInPage
  await navigateToFleet(page)
  const card = cardLocator(page)
  const lag = card.locator('dt').filter({ hasText: 'Max replay lag' }).locator('xpath=following-sibling::dd[1]')
  await expect(lag.locator('[aria-label="not measured"]')).toBeVisible()
  await expect(lag).not.toContainText('0 s')
})

test('SYS-UI-005: disabled ASH explains its configuration', async ({ signedInPage, api }) => {
  const page = signedInPage
  const { instance } = await fleetContext(api)
  await page.goto(`/instances/${instanceID(instance)}/ash`)
  await expect(page.getByRole('heading', { name: 'ASH and wait analysis' })).toBeVisible()
  await expect(page.locator('body')).toContainText(/Disabled/i)
  await expect(page.locator('body')).toContainText(/ASH sampling/i)
  await expect(page.locator('body')).toContainText(/checks\.ash/i)
  await expect(page.locator('body')).not.toContainText(/No wait event types observed/i)
})

test('SYS-UI-006: plan-only execution is audited without query text', async ({ signedInPage, api }) => {
  const page = signedInPage
  const { instance } = await fleetContext(api)
  const commandRequests: string[] = []
  page.on('request', (request) => {
    if (request.method() === 'POST' && request.url().includes('/commands')) {
      commandRequests.push(request.postData() ?? '')
    }
  })

  await page.goto(`/instances/${instanceID(instance)}/queries`)
  await expect(page.getByRole('heading', { name: 'Query inspector' })).toBeVisible()
  const queryLink = page.locator('a[href*="/queries/"]').first()
  await expect(queryLink).toBeVisible({ timeout: 60_000 })
  await queryLink.click()
  await expect(page.getByRole('heading', { name: 'Query detail and plan history' })).toBeVisible()
  const runPlan = page.getByRole('button', { name: 'Run plan only' })
  await expect(runPlan).toBeEnabled()
  await runPlan.click()
  await expect(page.getByRole('heading', { name: 'Query plan' })).toBeVisible({ timeout: 60_000 })
  await expect(page.getByRole('heading', { name: /Plan history/i })).toBeVisible()

  await expect.poll(() => commandRequests.length, { timeout: 30_000 }).toBeGreaterThan(0)
  for (const body of commandRequests) {
    expect(body).not.toMatch(/select\s|pg_sleep|from\s/i)
  }
  await page.goto('/settings')
  await expect(page.locator('body')).toContainText(/command audit/i)
})

test('SYS-UI-007: replication and ASH charts render data', async ({ signedInPage, api }) => {
  const page = signedInPage
  const { cluster, instance } = await fleetContext(api)

  await page.goto(`/clusters/${cluster.cluster_id}`)
  await expect(page.getByRole('region', { name: 'Replication topology graph' })).toBeVisible()
  await expect.poll(() => page.locator('canvas').count(), { timeout: 60_000 }).toBeGreaterThan(0)
  const replicationCanvas = page.locator('canvas').first()
  const replicationPixels = await replicationCanvas.evaluate((canvas: HTMLCanvasElement) => {
    const context = canvas.getContext('2d')
    const pixels = context?.getImageData(0, 0, canvas.width, canvas.height).data ?? new Uint8ClampedArray()
    return { width: canvas.width, height: canvas.height, nonBlank: pixels.some((value) => value !== 0) }
  })
  expect(replicationPixels.width).toBeGreaterThan(0)
  expect(replicationPixels.height).toBeGreaterThan(0)
  expect(replicationPixels.nonBlank).toBeTruthy()

  await page.goto(`/instances/${instanceID(instance)}/ash`)
  await expect(page.getByRole('heading', { name: 'ASH and wait analysis' })).toBeVisible()
  await expect.poll(() => page.locator('canvas').count(), { timeout: 60_000 }).toBeGreaterThan(0)
  const ashCanvas = page.locator('canvas').first()
  const ashPixels = await ashCanvas.evaluate((canvas: HTMLCanvasElement) => {
    const context = canvas.getContext('2d')
    const pixels = context?.getImageData(0, 0, canvas.width, canvas.height).data ?? new Uint8ClampedArray()
    return { width: canvas.width, height: canvas.height, nonBlank: pixels.some((value) => value !== 0) }
  })
  expect(ashPixels.width).toBeGreaterThan(0)
  expect(ashPixels.height).toBeGreaterThan(0)
  expect(ashPixels.nonBlank).toBeTruthy()
})

test('SYS-UI-008: blocking tree shows the lock root and child', async ({ signedInPage, api }) => {
  const page = signedInPage
  const { instance } = await fleetContext(api)
  await page.goto(`/instances/${instanceID(instance)}/locks`)
  await expect(page.getByRole('heading', { name: 'Locks and activity' })).toBeVisible()
  await expect
    .poll(() => page.getByRole('treeitem').count(), { timeout: 60_000, intervals: [1_000, 3_000, 5_000] })
    .toBeGreaterThan(1)
  const root = page.getByRole('treeitem').first()
  await expect(root).toContainText(/blocking session/i)
  await expect(root.getByRole('group')).toBeVisible()
})

test('SYS-UI-009: signal controls enforce tier and confirmation', async ({ signedInPage, api }) => {
  const page = signedInPage
  const { instance } = await fleetContext(
    api,
    process.env.PGLENS_UI_CANCEL_MODE === 't0' ? 'T0' : 'T2',
  )
  await page.goto(`/instances/${instanceID(instance)}/locks`)
  await expect(page.getByRole('heading', { name: 'Locks and activity' })).toBeVisible()
  const cancel = page.getByRole('button', { name: 'Cancel query' }).first()
  await expect(cancel).toBeVisible({ timeout: 60_000 })

  if (process.env.PGLENS_UI_CANCEL_MODE === 't0') {
    await expect(cancel).toBeDisabled()
    await expect(page.locator('body')).toContainText(/T2/i)
    return
  }

  await expect(cancel).toBeEnabled()
  await cancel.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  const confirm = dialog.getByRole('button', { name: /cancel query/i }).last()
  await expect(confirm).toBeEnabled()
  await confirm.click()
  await expect(dialog).toBeHidden()
  await reloadUntil(page, async () => (await page.getByRole('button', { name: 'Cancel query' }).count()) === 0, 60_000)
  await expect(page.locator('body')).toContainText(/audit|cancel/i)
})

test('SYS-UI-010: finding mute and unmute update state', async ({ signedInPage }) => {
  const page = signedInPage
  await page.goto('/findings')
  await expect(page.getByRole('heading', { name: 'Advisor findings' })).toBeVisible()
  const finding = page.locator('article').filter({ has: page.getByRole('button', { name: 'Mute finding' }) }).first()
  await expect(finding).toBeVisible({ timeout: 120_000 })
  await finding.getByRole('button', { name: 'Mute finding' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await dialog.getByLabel('Reason').fill('UI acceptance mute')
  await dialog.getByRole('radio').first().check()
  await dialog.getByRole('button', { name: 'Mute finding' }).click()
  await expect(finding).toContainText(/muted/i)
  await expect(finding).toContainText('UI acceptance mute')

  await finding.getByRole('button', { name: 'Unmute finding' }).click()
  await reloadUntil(
    page,
    async () => (await page.getByRole('button', { name: 'Mute finding' }).count()) > 0,
    60_000,
  )
  await expect(page.getByRole('button', { name: 'Unmute finding' })).toHaveCount(0)
})

test('SYS-UI-011: phase seven routes pass accessibility checks', async ({ signedInPage, api }) => {
  const page = signedInPage
  const { cluster, instance } = await fleetContext(api)
  const instanceIDValue = instanceID(instance)
  const routes = [
    '/login',
    '/',
    `/clusters/${cluster.cluster_id}`,
    `/instances/${instanceIDValue}`,
    `/instances/${instanceIDValue}/ash`,
    `/instances/${instanceIDValue}/queries`,
    `/instances/${instanceIDValue}/queries/missing-query`,
    `/instances/${instanceIDValue}/locks`,
    '/findings',
    '/alerts',
    '/events',
    '/alerts/rules',
    '/alerts/silences',
    '/settings',
  ]

  for (const route of routes) {
    await page.goto(route)
    await expect(page.locator('body')).toBeVisible()
    const report = await new AxeBuilder({ page }).analyze()
    const severe = report.violations.filter((violation) => violation.impact === 'serious' || violation.impact === 'critical')
    if (severe.length > 0) {
      await test.info().attach(`axe-${route.replace(/[^a-z0-9]+/gi, '-') || 'root'}`, {
        body: JSON.stringify(report, null, 2),
        contentType: 'application/json',
      })
    }
    expect(severe, `serious/critical axe violations on ${route}`).toEqual([])
  }
})
