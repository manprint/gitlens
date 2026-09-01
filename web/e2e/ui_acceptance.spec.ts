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
  return String(
    instance.address ?? instance.addr ?? instance.host ?? instance.name ?? instanceID(instance),
  )
}

function referenceID(value: unknown): string {
  if (typeof value === 'string' || typeof value === 'number') return String(value)
  if (typeof value !== 'object' || value === null) return ''
  const record = value as JsonObject
  return String(record.instance_id ?? record.id ?? record.instanceId ?? '')
}

function hasPlanNode(value: unknown): boolean {
  if (Array.isArray(value)) return value.some(hasPlanNode)
  if (typeof value !== 'object' || value === null) return false
  const record = value as JsonObject
  if (typeof record['Node Type'] === 'string') return true
  return Object.values(record).some(hasPlanNode)
}

async function fleetContext(
  api: any,
  preferredTier?: string,
): Promise<{
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
          (item) =>
            String(item.perm_tier ?? item.permission_tier ?? '').toUpperCase() === wantedTier,
        )
      : undefined) ??
    clusterInstances.find(
      (item) => String(item.perm_tier ?? item.permission_tier ?? '').toUpperCase() === 'T1',
    ) ??
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

async function reloadUntil(
  page: any,
  predicate: () => Promise<boolean>,
  timeout = 120_000,
): Promise<void> {
  await page.reload({ waitUntil: 'domcontentloaded' })
  await expect.poll(predicate, { timeout, intervals: [1_000, 3_000, 5_000] }).toBe(true)
}

async function pollAPI(predicate: () => Promise<boolean>, timeout = 120_000): Promise<void> {
  await expect.poll(predicate, { timeout, intervals: [1_000, 3_000, 5_000] }).toBe(true)
}

async function navigateToFleet(page: any): Promise<void> {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Fleet overview' })).toBeVisible()
  await expect(page.getByRole('list', { name: 'Cluster cards' })).toBeVisible()
}

test('SYS-UI-001: failover preserves identity and raises an alert', async ({
  signedInPage,
  api,
}) => {
  const page = signedInPage
  const before = await fleetContext(api)
  await navigateToFleet(page)

  const card = cardLocator(page)
  await expect(card).toBeVisible()
  await expect(card.getByRole('status', { name: /^Health: ok\./i })).toBeVisible()
  const clusterID = String(before.cluster.cluster_id)
  const renderedClusterID = (await card.locator('code').first().innerText()).trim()
  expect(renderedClusterID).toBe(clusterID)
  const clusterLink = page.getByRole('list', { name: 'Cluster cards' }).getByRole('link').first()
  const linkLabel = await clusterLink.getAttribute('aria-label')
  expect(linkLabel).toContain(clusterID)
  const primaryAddress = await definitionValue(card, 'Primary')
  expect(primaryAddress).not.toBe('')
  const beforePrimaryID =
    referenceID(before.cluster.primary ?? before.cluster.primary_instance_id) ||
    referenceID(
      before.instances.find((item) =>
        String(item.role ?? '')
          .toLowerCase()
          .includes('primary'),
      ),
    )
  expect(beforePrimaryID, 'API must expose the original primary identity').not.toBe('')

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
  expect(standbyAddress).not.toBe('')

  let initialTopologyEdge: JsonObject | undefined
  await pollAPI(async () => {
    const response = await api.get(`/api/v1/clusters/${encodeURIComponent(clusterID)}/topology`)
    if (!response.ok()) return false
    const topology = arrayPayload(await response.json(), 'topology')
    initialTopologyEdge = topology.find(
      (edge) =>
        referenceID(edge.from) === standbyID &&
        referenceID(edge.to) === beforePrimaryID &&
        String(edge.confidence ?? '').toLowerCase() === 'high',
    )
    return initialTopologyEdge !== undefined
  }, 60_000)
  expect(
    initialTopologyEdge,
    'initial topology must show a high-confidence standby upstream',
  ).toBeTruthy()

  const readyFile = process.env.PGLENS_UI_READY_FILE
  expect(readyFile, 'Go runner must provide the failover handshake path').toBeTruthy()
  await writeFile(readyFile!, 'captured')

  await pollAPI(async () => {
    const current = (await clusters(api))[0]!
    const primary = JSON.stringify(
      current.primary ?? current.primary_instance_id ?? '',
    ).toLowerCase()
    return (
      primary.includes(standbyID.toLowerCase()) || primary.includes(standbyAddress.toLowerCase())
    )
  })

  let failoverEvent: JsonObject | undefined
  await pollAPI(async () => {
    const response = await api.get(
      `/api/v1/events?cluster_id=${encodeURIComponent(clusterID)}&type=failover_detected&limit=100`,
    )
    if (!response.ok()) return false
    const payload = await response.json()
    failoverEvent = arrayPayload(payload, 'events').find(
      (item) => String(item.type ?? '').toLowerCase() === 'failover_detected',
    )
    return failoverEvent !== undefined
  })
  expect(failoverEvent).toBeDefined()
  const failoverPayload = (failoverEvent?.payload ?? {}) as JsonObject
  expect(referenceID(failoverPayload.old_primary)).toBe(beforePrimaryID)
  expect(referenceID(failoverPayload.new_primary)).toBe(standbyID)

  await pollAPI(async () => {
    const response = await api.get(
      `/api/v1/alerts?cluster_id=${encodeURIComponent(clusterID)}&state=all`,
    )
    if (!response.ok()) return false
    const payload = await response.json()
    return arrayPayload(payload, 'alerts').some((item) => {
      const text = JSON.stringify(item).toLowerCase()
      return text.includes('failover_detected') && text.includes('firing')
    })
  })

  await navigateToFleet(page)
  await reloadUntil(page, async () =>
    (await cardLocator(page).innerText()).includes(standbyAddress),
  )
  const afterCard = cardLocator(page)
  await expect(afterCard).toContainText(standbyAddress)
  await expect(afterCard.locator('code').first()).toHaveText(renderedClusterID)

  await page.goto(`/instances/${standbyID}`)
  await expect(page.getByRole('heading', { name: new RegExp(standbyAddress) })).toBeVisible()
  await expect(
    page.locator('dt').filter({ hasText: 'Role' }).locator('xpath=following-sibling::dd[1]'),
  ).toHaveText('primary')
  await page.goto(`/clusters/${clusterID}`)
  await expect(page.getByRole('region', { name: 'Replication topology graph' })).toBeVisible()
  let promotedEdge: JsonObject | undefined
  await pollAPI(async () => {
    const response = await api.get(`/api/v1/clusters/${encodeURIComponent(clusterID)}/topology`)
    if (!response.ok()) return false
    const payload = await response.json()
    const topology = arrayPayload(payload, 'topology')
    promotedEdge = topology.find(
      (edge) =>
        referenceID(edge.from) === beforePrimaryID &&
        referenceID(edge.to) === standbyID &&
        String(edge.type ?? '').toLowerCase() === 'streaming',
    )
    return promotedEdge !== undefined
  }, 90_000)
  await page.reload({ waitUntil: 'domcontentloaded' })
  await expect(page.getByRole('region', { name: 'Replication topology graph' })).toBeVisible()
  const edgeTableToggle = page.getByRole('button', { name: 'Show accessible edge table' })
  await expect(edgeTableToggle).toBeVisible()
  await edgeTableToggle.click()
  const edgeTable = page
    .locator('#topology-edge-table-visible')
    .getByRole('table', { name: 'Replication topology edges' })
  await expect(edgeTable).toBeVisible()
  expect(promotedEdge, 'topology must reverse from the old primary to the new primary').toBeTruthy()
  const promotedRow = edgeTable
    .getByRole('row')
    .filter({ hasText: primaryAddress })
    .filter({ hasText: standbyAddress })
  await expect(promotedRow).toHaveCount(1)
  const edgeCells = promotedRow.getByRole('cell')
  await expect(edgeCells.nth(0)).toContainText(primaryAddress)
  await expect(edgeCells.nth(1)).toContainText(standbyAddress)
  await expect(edgeCells.nth(2)).toHaveText(String(promotedEdge?.type))
  expect(String(promotedEdge?.confidence ?? '').toLowerCase()).toBe('high')
  await reloadUntil(page, async () =>
    /failover_detected/i.test(await page.locator('body').innerText()),
  )
  const failoverArticle = page
    .locator('article[data-event-type="failover_detected"]')
    .filter({ has: page.locator(`a[href="/instances/${beforePrimaryID}"]`) })
    .filter({ has: page.locator(`a[href="/instances/${standbyID}"]`) })
    .first()
  await expect(failoverArticle).toBeVisible()
  await expect(failoverArticle).toContainText('Old primary:')
  await expect(failoverArticle).toContainText('New primary:')
  await expect(
    failoverArticle.getByRole('link', { name: beforePrimaryID, exact: true }),
  ).toHaveAttribute('href', `/instances/${beforePrimaryID}`)
  await expect(failoverArticle.getByRole('link', { name: standbyID, exact: true })).toHaveAttribute(
    'href',
    `/instances/${standbyID}`,
  )

  await page.goto('/events')
  await reloadUntil(page, async () =>
    /failover_detected/i.test(await page.locator('body').innerText()),
  )
  await page.goto('/alerts')
  await reloadUntil(page, async () => {
    const body = await page.locator('body').innerText()
    return /failover_detected/i.test(body) && /firing/i.test(body)
  })

  const after = (await clusters(api))[0]!
  expect(after.cluster_id).toBe(renderedClusterID)
})

test('SYS-UI-002: unauthenticated navigation cannot restore fleet data', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: /sign in|accedi/i })).toBeVisible()
  await expect(page.getByRole('list', { name: 'Cluster cards' })).toHaveCount(0)

  const unauthenticatedStatus = await page.evaluate(
    async () => (await fetch('/api/v1/clusters')).status,
  )
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
  const backNavigationStatus = await page.evaluate(
    async () => (await fetch('/api/v1/clusters')).status,
  )
  expect(backNavigationStatus).toBe(401)
})

test('SYS-UI-003: agent outage is visible as stale fleet data', async ({ signedInPage, api }) => {
  const page = signedInPage
  const { cluster, instance } = await fleetContext(api)
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
      return (
        /Agents requiring attention/i.test(body) &&
        body.includes(address) &&
        /Stale\s+—/i.test(body)
      )
    },
    30_000,
  )
  const issue = page
    .locator('section[aria-labelledby="agent-health-title"]')
    .getByRole('listitem')
    .filter({ hasText: address })
    .filter({ hasText: cluster.cluster_id })
  await expect(issue).toBeVisible()
  const staleCard = page
    .getByRole('list', { name: 'Cluster cards' })
    .getByRole('listitem')
    .filter({ hasText: cluster.cluster_id })
  await expect(staleCard.getByRole('status', { name: /Stale data:/i })).toBeVisible()
})

test('SYS-UI-004: standalone replay lag stays unknown', async ({ signedInPage }) => {
  const page = signedInPage
  await navigateToFleet(page)
  const card = cardLocator(page)
  const lag = card
    .locator('dt')
    .filter({ hasText: 'Max replay lag' })
    .locator('xpath=following-sibling::dd[1]')
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

test('SYS-UI-006: plan-only execution is audited without query text', async ({
  signedInPage,
  api,
}) => {
  const page = signedInPage
  const { instance } = await fleetContext(api)
  const commandRequests: string[] = []
  page.on('request', (request) => {
    if (request.method() === 'POST' && request.url().includes('/commands')) {
      commandRequests.push(request.postData() ?? '')
    }
  })

  const selectedInstanceID = instanceID(instance)
  await expect
    .poll(
      async () => {
        const response = await api.get(
          `/api/v1/statements?instance_id=${encodeURIComponent(selectedInstanceID)}&limit=50`,
        )
        if (!response.ok()) return 0
        const payload = await response.json()
        return Array.isArray(payload.statements) ? payload.statements.length : 0
      },
      { timeout: 90_000, intervals: [1_000, 3_000, 5_000] },
    )
    .toBeGreaterThan(0)

  let selectedQueryID: string | number | undefined
  await expect
    .poll(
      async () => {
        const response = await api.get(
          `/api/v1/statements?instance_id=${encodeURIComponent(selectedInstanceID)}&limit=50`,
        )
        if (!response.ok()) return false
        const payload = await response.json()
        const statement = (Array.isArray(payload.statements) ? payload.statements : []).find(
          (item: JsonObject) =>
            /^select count\(\*\) from pg_catalog\.pg_proc cross join pg_catalog\.pg_class\s*$/i.test(
              String(item.query_text ?? '').trim(),
            ),
        )
        selectedQueryID = statement?.queryid
        return selectedQueryID !== undefined && selectedQueryID !== null
      },
      { timeout: 90_000, intervals: [1_000, 3_000, 5_000] },
    )
    .toBe(true)

  await page.goto(
    `/instances/${selectedInstanceID}/queries/${encodeURIComponent(String(selectedQueryID))}`,
  )
  await expect(page.getByRole('heading', { name: 'Query detail and plan history' })).toBeVisible()
  const runPlan = page.getByRole('button', { name: 'Run plan only' })
  await expect(runPlan).toBeEnabled()
  const commandResponsePromise = page.waitForResponse(
    (response) => response.request().method() === 'POST' && response.url().includes('/commands'),
  )
  await runPlan.click()
  const commandResponse = await commandResponsePromise
  const commandResponsePayload = (await commandResponse.json()) as JsonObject
  const commandID = String(commandResponsePayload.command_id ?? '')
  expect(commandID, 'plan request must return a command id').not.toBe('')
  await expect(page.getByRole('heading', { name: 'Query plan' })).toBeVisible({ timeout: 60_000 })
  await expect(page.getByRole('heading', { name: 'Plan history', exact: true })).toBeVisible()

  const queryPlanTree = page.getByRole('list', { name: 'Query plan tree' })
  await expect(queryPlanTree).toBeVisible()
  await expect(queryPlanTree.getByRole('listitem').first()).toBeVisible()
  await expect
    .poll(
      async () => {
        const response = await api.get(
          `/api/v1/plans?queryid=${encodeURIComponent(String(selectedQueryID))}&instance_id=${encodeURIComponent(selectedInstanceID)}&limit=20`,
        )
        if (!response.ok()) return false
        const payload = await response.json()
        const plans = arrayPayload(payload, 'plans')
        return plans.length > 0 && plans.some((plan) => hasPlanNode(plan.plan))
      },
      { timeout: 90_000, intervals: [1_000, 3_000, 5_000] },
    )
    .toBe(true)
  await expect
    .poll(() => page.getByRole('list', { name: 'Plan history entries' }).count(), {
      timeout: 60_000,
      intervals: [1_000, 3_000, 5_000],
    })
    .toBe(1)
  await expect(
    page.getByRole('list', { name: 'Plan history entries' }).getByRole('listitem').first(),
  ).toBeVisible()

  await expect.poll(() => commandRequests.length, { timeout: 30_000 }).toBeGreaterThan(0)
  for (const body of commandRequests) {
    expect(body).not.toMatch(/select\s|pg_sleep|from\s/i)
  }
  let command: JsonObject | undefined
  await pollAPI(async () => {
    const response = await api.get(`/api/v1/commands/${encodeURIComponent(commandID)}`)
    if (!response.ok()) return false
    command = await response.json()
    return command?.state === 'done'
  }, 90_000)
  expect(command?.state).toBe('done')
  expect(hasPlanNode(command?.result)).toBeTruthy()
  let auditEntry: JsonObject | undefined
  await pollAPI(async () => {
    const response = await api.get(`/api/v1/instances/${selectedInstanceID}/command-audit`)
    if (!response.ok()) return false
    const audit = await response.json()
    auditEntry = Array.isArray(audit)
      ? audit.find(
          (item: JsonObject) =>
            String(item.command_id ?? '') === commandID &&
            String(item.kind ?? '').toLowerCase() === 'explain' &&
            String(item.outcome ?? '').toLowerCase() === 'ok',
        )
      : undefined
    return auditEntry !== undefined
  })
  expect(auditEntry).toBeDefined()
  await page.goto('/settings')
  const auditTable = page.getByRole('table', { name: /Command audit for/i })
  await expect(auditTable).toBeVisible()
  await expect
    .poll(
      async () =>
        (await auditTable
          .getByRole('row')
          .filter({ hasText: /explain/i })
          .count()) > 0,
      { timeout: 60_000, intervals: [1_000, 3_000, 5_000] },
    )
    .toBe(true)
  const auditRow = auditTable
    .getByRole('row')
    .filter({ hasText: /explain/i })
    .first()
  await expect(auditRow).toContainText(/explain/i)
  await expect(auditRow).toContainText(/ok/i)
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
    const pixels =
      context?.getImageData(0, 0, canvas.width, canvas.height).data ?? new Uint8ClampedArray()
    return {
      width: canvas.width,
      height: canvas.height,
      nonBlank: pixels.some((value) => value !== 0),
    }
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
    const pixels =
      context?.getImageData(0, 0, canvas.width, canvas.height).data ?? new Uint8ClampedArray()
    return {
      width: canvas.width,
      height: canvas.height,
      nonBlank: pixels.some((value) => value !== 0),
    }
  })
  expect(ashPixels.width).toBeGreaterThan(0)
  expect(ashPixels.height).toBeGreaterThan(0)
  expect(ashPixels.nonBlank).toBeTruthy()
})

test('SYS-UI-008: blocking tree shows the lock root and child', async ({
  signedInPage,
  api,
}, testInfo) => {
  const page = signedInPage
  const { instance } = await fleetContext(api)
  const selectedInstanceID = instanceID(instance)
  const locksURL = `/api/v1/locks?instance_id=${encodeURIComponent(selectedInstanceID)}`
  let lastLocksResponse = ''
  try {
    await expect
      .poll(
        async () => {
          const response = await api.get(locksURL)
          lastLocksResponse = await response.text()
          if (!response.ok()) return 0
          try {
            const payload = JSON.parse(lastLocksResponse)
            return Array.isArray(payload.nodes) ? payload.nodes.length : 0
          } catch {
            return 0
          }
        },
        { timeout: 60_000, intervals: [1_000, 3_000, 5_000] },
      )
      .toBeGreaterThan(1)
  } catch (error) {
    if (lastLocksResponse) {
      await testInfo.attach('locks-api-last.json', {
        body: lastLocksResponse,
        contentType: 'application/json',
      })
    }
    throw error
  }
  await page.goto(`/instances/${selectedInstanceID}/locks`)
  await expect(page).toHaveURL(new RegExp(`/instances/${selectedInstanceID}/locks$`))
  await expect(page.getByRole('heading', { name: 'Locks and activity' })).toBeVisible()
  await expect
    .poll(() => page.getByRole('treeitem').count(), {
      timeout: 60_000,
      intervals: [1_000, 3_000, 5_000],
    })
    .toBeGreaterThan(1)
  const root = page.getByRole('treeitem').first()
  await expect(root).toContainText(/blocking session/i)
  await expect(root.getByRole('group')).toBeVisible()
  const lockPayload = JSON.parse(lastLocksResponse) as JsonObject
  const lockNodes = Array.isArray(lockPayload.nodes) ? lockPayload.nodes : []
  const blockingNode = lockNodes.find((node: JsonObject) => {
    const blockedBy = Array.isArray(node.blocked_by) ? node.blocked_by : []
    return (
      blockedBy.length === 0 &&
      lockNodes.some((child: JsonObject) => {
        const childBlockedBy = Array.isArray(child.blocked_by) ? child.blocked_by : []
        return childBlockedBy.map(String).includes(String(node.pid))
      })
    )
  })
  const blockedNode = lockNodes.find((node: JsonObject) => {
    const blockedBy = Array.isArray(node.blocked_by) ? node.blocked_by : []
    return blockingNode && blockedBy.map(String).includes(String(blockingNode.pid))
  })
  expect(blockingNode?.pid).toBeDefined()
  expect(blockedNode?.pid).toBeDefined()
  expect(String(blockingNode?.query ?? '')).not.toBe('')
  expect(String(blockedNode?.query ?? '')).not.toBe('')
  await expect(root).toContainText(`PID ${blockingNode?.pid}`)
  await expect(root).toContainText(String(blockingNode?.query ?? ''))
  const child = root.getByRole('group').getByRole('treeitem').first()
  await expect(child).toContainText(`PID ${blockedNode?.pid}`)
  await expect(child).toContainText(String(blockedNode?.query ?? ''))
})

test('SYS-UI-009: signal controls enforce tier and confirmation', async ({ signedInPage, api }) => {
  const page = signedInPage
  const { instance } = await fleetContext(
    api,
    process.env.PGLENS_UI_CANCEL_MODE === 't0' ? 'T0' : 'T2',
  )
  const selectedInstanceID = instanceID(instance)
  let sessionPID = ''
  await expect
    .poll(
      async () => {
        const response = await api.get(`/api/v1/locks?instance_id=${selectedInstanceID}`)
        if (!response.ok()) return false
        const payload = await response.json()
        const sessions = Array.isArray(payload?.sessions) ? payload.sessions : []
        const session = sessions.find((item: JsonObject) =>
          /pg_sleep/i.test(String(item.query ?? '')),
        )
        if (!session) return false
        sessionPID = String(session.pid ?? '')
        return sessionPID !== ''
      },
      { timeout: 60_000, intervals: [1_000, 3_000, 5_000] },
    )
    .toBe(true)
  await page.goto(`/instances/${selectedInstanceID}/locks`)
  await expect(page.getByRole('heading', { name: 'Locks and activity' })).toBeVisible()
  const session = page
    .getByRole('list', { name: 'Active sessions' })
    .getByRole('listitem')
    .filter({ hasText: `PID ${sessionPID}` })
  await expect(session).toBeVisible({ timeout: 60_000 })
  const cancel = session.getByRole('button', { name: 'Cancel query' })
  await expect(cancel).toBeVisible()

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
  await expect
    .poll(
      async () => {
        const response = await api.get(`/api/v1/locks?instance_id=${selectedInstanceID}`)
        if (!response.ok()) return false
        const payload = await response.json()
        const sessions = Array.isArray(payload?.sessions) ? payload.sessions : []
        return !sessions.some((item: JsonObject) => String(item.pid ?? '') === sessionPID)
      },
      { timeout: 60_000, intervals: [1_000, 3_000, 5_000] },
    )
    .toBe(true)
  await reloadUntil(
    page,
    async () =>
      (await page
        .getByRole('list', { name: 'Active sessions' })
        .getByText(`PID ${sessionPID}`)
        .count()) === 0,
    60_000,
  )
  await expect
    .poll(
      async () => {
        const response = await api.get(`/api/v1/instances/${selectedInstanceID}/command-audit`)
        if (!response.ok()) return false
        const audit = await response.json()
        return (
          Array.isArray(audit) &&
          audit.some(
            (item: JsonObject) =>
              item.kind === 'cancel' && String(item.args?.pid ?? '') === sessionPID,
          )
        )
      },
      { timeout: 60_000, intervals: [1_000, 3_000, 5_000] },
    )
    .toBe(true)
})

test('SYS-UI-010: finding mute and unmute update state', async ({ signedInPage, api }) => {
  const page = signedInPage
  let mutableFinding: JsonObject | undefined
  await pollAPI(async () => {
    const response = await api.get('/api/v1/findings?state=all&limit=1000')
    if (!response.ok()) return false
    const findings = arrayPayload(await response.json(), 'findings')
    mutableFinding = findings.find((item) => item.state === 'open' || item.state === 'degraded')
    return mutableFinding?.finding_id !== undefined
  }, 90_000)
  const findingID = String(mutableFinding?.finding_id ?? '')
  const realState = String(mutableFinding?.state)
  expect(findingID).not.toBe('')

  await page.goto('/findings')
  await expect(page.getByRole('heading', { name: 'Advisor findings' })).toBeVisible()
  const finding = page.locator(`article[aria-labelledby="finding-${findingID}"]`)
  await expect(finding).toBeVisible({ timeout: 120_000 })
  await finding.getByRole('button', { name: 'Mute finding' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await dialog.getByLabel('Reason').fill('UI acceptance mute')
  await dialog.getByRole('radio').first().check()
  await dialog.getByRole('button', { name: 'Mute finding' }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await page.getByLabel('State').selectOption('all')
  const mutedFinding = page.locator(`article[aria-labelledby="finding-${findingID}"]`)
  await expect(mutedFinding).toBeVisible()
  await expect(mutedFinding).toContainText(/muted/i)
  await expect(mutedFinding).toContainText('UI acceptance mute')

  await mutedFinding.getByRole('button', { name: 'Unmute finding' }).click()
  await pollAPI(async () => {
    const response = await api.get(`/api/v1/findings/${encodeURIComponent(findingID)}`)
    if (!response.ok()) return false
    const current = await response.json()
    return current.state === realState && current.mute_reason == null
  }, 60_000)
  await reloadUntil(
    page,
    async () => {
      const sameFinding = page.locator(`article[aria-labelledby="finding-${findingID}"]`)
      if ((await sameFinding.count()) !== 1) return false
      return (
        (await sameFinding.getByRole('button', { name: 'Mute finding' }).count()) === 1 &&
        (await sameFinding.getByRole('button', { name: 'Unmute finding' }).count()) === 0 &&
        (await sameFinding.getByText(realState, { exact: true }).count()) > 0
      )
    },
    60_000,
  )
  const unmutedFinding = page.locator(`article[aria-labelledby="finding-${findingID}"]`)
  await expect(unmutedFinding).toContainText(realState)
  await expect(unmutedFinding).not.toContainText('UI acceptance mute')
})

test('SYS-UI-011: phase seven routes pass accessibility checks', async ({ signedInPage, api }) => {
  const page = signedInPage
  const { cluster, instance } = await fleetContext(api)
  const instanceIDValue = instanceID(instance)
  const routes = [
    { path: '/login', expected: /sign in|accedi/i },
    { path: '/', expected: /Fleet overview/i },
    { path: `/clusters/${cluster.cluster_id}`, expected: /Cluster identity:/i },
    { path: `/instances/${instanceIDValue}`, expected: new RegExp(instanceAddress(instance)) },
    { path: `/instances/${instanceIDValue}/ash`, expected: /ASH and wait analysis/i },
    { path: `/instances/${instanceIDValue}/queries`, expected: /Query inspector/i },
    {
      path: `/instances/${instanceIDValue}/queries/missing-query`,
      expected: /Query detail and plan history/i,
    },
    { path: `/instances/${instanceIDValue}/locks`, expected: /Locks and activity/i },
    { path: '/findings', expected: /Advisor findings/i },
    { path: '/alerts', expected: /Alerts and events/i },
    { path: '/events', expected: /Fleet event timeline/i },
    { path: '/alerts/rules', expected: /Alert rules/i },
    { path: '/alerts/silences', expected: /Alert silences/i },
    { path: '/settings', expected: /Settings and inventory/i },
    { path: '/__ui_acceptance_not_found__', expected: /Page not found/i },
  ]

  for (const route of routes) {
    await page.goto(route.path)
    await expect(page).toHaveURL(
      new RegExp(`${route.path.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`),
    )
    await expect(page.locator('body')).toBeVisible()
    await expect(page.locator('body')).toContainText(route.expected)
    const report = await new AxeBuilder({ page }).analyze()
    const severe = report.violations.filter(
      (violation) => violation.impact === 'serious' || violation.impact === 'critical',
    )
    if (severe.length > 0) {
      await test.info().attach(`axe-${route.path.replace(/[^a-z0-9]+/gi, '-') || 'root'}`, {
        body: JSON.stringify(report, null, 2),
        contentType: 'application/json',
      })
    }
    expect(severe, `serious/critical axe violations on ${route.path}`).toEqual([])
  }
})
