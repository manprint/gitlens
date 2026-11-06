import type { APIRequestContext, Page } from '@playwright/test'

import { expect, test } from './fixtures'
import { join } from 'node:path'

import { assertLifecycleAllowed, assertStimulusAllowed, STACK_DIR } from './qa/guard'
import { login, searchRead } from './qa/odoo'
import { queryInt, registerCleanup, runCleanups } from './qa/pg'
import { run, type RunResult } from './qa/stack'
import { canvasNotBlank, liveTimeout } from './visual_helpers'
import { formatLag } from '../src/lib/format/lag'

/** HA lab (plan 005 phase 7): Odoo DB behind hactl, three PostgreSQL 18 nodes. */
const ODOO_URL = () => (process.env.ODOO_URL ?? 'http://127.0.0.1:9091').replace(/\/$/, '')
const ODOO_CONTAINER = 'pglens-odoo-odoo-1'
const HACTL_CONTAINER = 'pglens-odoo-hactl-1'
const HACTL_URL = () => process.env.PGLENS_QA_HACTL_URL ?? 'http://127.0.0.1:8008'

interface InstanceRow {
  instance_id: string
  cluster_id: string
  addr: string
  role: string
  up: boolean
}

async function inspect(container: string, format: string): Promise<string> {
  return (await run('docker', ['inspect', '-f', format, container])).stdout.trim()
}

test('LIVE-HA-001 three instances in one cluster, roles primary/standby, Odoo through hactl', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 60_000))

  let up: InstanceRow[] = []
  await expect
    .poll(
      async () => {
        const response = await api.get('/api/v1/instances')
        expect(response.ok()).toBe(true)
        up = ((await response.json()) as InstanceRow[]).filter((row) => row.up)
        return up.length
      },
      { timeout: 60_000, message: 'three instances up' },
    )
    .toBe(3)
  const clusters = [...new Set(up.map((row) => row.cluster_id))]
  expect(clusters, 'the three instances share one cluster').toHaveLength(1)
  expect(up.map((row) => row.role).sort()).toEqual(['primary', 'standby', 'standby'])
  const hactl = await page.request.get(`${HACTL_URL()}/status`)
  expect(hactl.ok()).toBe(true)
  const status = (await hactl.json()) as { leader: string; split_brain: boolean }
  expect(status.split_brain).toBe(false)
  const primary = up.find((row) => row.role === 'primary')
  expect(primary?.addr, 'the pglens primary is the hactl leader').toBe(status.leader)

  await page.goto('/')
  const card = page
    .getByRole('listitem')
    .filter({ has: page.getByRole('link', { name: new RegExp(`\\(${clusters[0]}\\)`) }) })
  await expect(card).toHaveCount(1)
  await expect(card.locator('dt:text-is("Instances") + dd')).toHaveText('3')
  await expect(card.locator('dt:text-is("Standbys") + dd')).toHaveText('2')

  expect(await inspect(ODOO_CONTAINER, '{{range .Config.Env}}{{println .}}{{end}}')).toMatch(
    /^HOST=hactl$/m,
  )
  const hactlIp = await inspect(
    HACTL_CONTAINER,
    '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}',
  )
  expect(hactlIp).toMatch(/^\d+\.\d+\.\d+\.\d+$/)
  const odoo = await page.request.get(`${ODOO_URL()}/web/login`)
  expect(odoo.status()).toBe(200)
  expect((await login()).uid).toBeGreaterThan(0)
  expect(
    await queryInt(
      `SELECT count(*) FROM pg_stat_activity WHERE usename = 'odoo' AND client_addr = '${hactlIp}'::inet;`,
    ),
    'Odoo sessions on the leader come through hactl',
  ).toBeGreaterThan(0)
})

const NODE_NAMES: Record<string, string> = {
  'odoo-db': 'ha-1',
  'pg-ha-2': 'ha-2',
  'pg-ha-3': 'ha-3',
}
const nodeName = (addr: string) => NODE_NAMES[addr] ?? addr
const NAMES_TABLE = 'Cluster and instance names'

interface ClusterInstance {
  instance_id: string
  display_name: string | null
  addr: string
  port: number
  role: string
  up: boolean
}

interface ClusterSummary {
  cluster_id: string
  instance_count: number
  max_replay_lag_seconds: number | null
  instances: ClusterInstance[]
}

interface Edge {
  from: string
  to: string
  confidence: string
}

interface ReplicationEdge {
  from: string
  to: string
  metric: string
  series: { ts: string; value: number | null }[]
}

async function onlyCluster(api: APIRequestContext): Promise<ClusterSummary> {
  const response = await api.get('/api/v1/clusters')
  expect(response.ok()).toBe(true)
  const clusters = (await response.json()) as ClusterSummary[]
  expect(clusters, 'one cluster').toHaveLength(1)
  const [cluster] = clusters
  if (!cluster) throw new Error('no cluster')
  return cluster
}

async function psqlOn(node: string, sql: string): Promise<string> {
  const out = await run(
    'docker',
    [
      'exec',
      '-i',
      `pglens-odoo-${node}-1`,
      'psql',
      '-U',
      'odoo',
      '-d',
      'odoo',
      '-X',
      '-A',
      '-t',
      '-q',
    ],
    { input: `${sql}\n` },
  )
  return out.stdout.trim()
}

/** Renames one instance through Settings → Names, the console's rename feature. */
async function renameInUi(page: Page, clusterId: string, row: ClusterInstance): Promise<void> {
  await page.goto('/settings?tab=names')
  const table = page.getByRole('table', { name: NAMES_TABLE })
  await expect(table).toBeVisible({ timeout: 30_000 })
  await page.getByRole('searchbox', { name: `Search ${NAMES_TABLE}` }).fill(clusterId)
  await table
    .getByRole('button', {
      name: `Rename instance ${row.display_name ?? `${row.addr}:${row.port}`}`,
      exact: true,
    })
    .click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await dialog.getByRole('textbox', { name: 'Instance name' }).fill(nodeName(row.addr))
  await dialog.getByRole('button', { name: 'Save' }).click()
  await expect(dialog).toBeHidden({ timeout: 30_000 })
}

async function nameAll(api: APIRequestContext): Promise<ClusterSummary> {
  const cluster = await onlyCluster(api)
  for (const row of cluster.instances) {
    if (row.display_name === nodeName(row.addr)) continue
    const response = await api.patch(`/api/v1/instances/${row.instance_id}`, {
      data: { display_name: nodeName(row.addr) },
    })
    expect(response.ok(), `rename ${row.addr}`).toBe(true)
  }
  return onlyCluster(api)
}

const header = (page: Page) => page.getByTestId('instance-header')

test.describe('HA-STATIC', () => {
  test('HA-STATIC-001 V-1 one cluster, three instances, names set in the console', async ({
    api,
    signedInPage: page,
  }) => {
    test.setTimeout(liveTimeout(0, 90_000))
    let cluster = await onlyCluster(api)
    expect(cluster.instance_count).toBe(3)
    expect(cluster.instances.map((row) => row.addr).sort()).toEqual([
      'odoo-db',
      'pg-ha-2',
      'pg-ha-3',
    ])
    // Clear, then rename every node through the console so the rename feature runs each time.
    for (const row of cluster.instances) {
      const cleared = await api.patch(`/api/v1/instances/${row.instance_id}`, {
        data: { display_name: null },
      })
      expect(cleared.ok()).toBe(true)
    }
    for (const row of (await onlyCluster(api)).instances) {
      await renameInUi(page, cluster.cluster_id, row)
    }
    cluster = await onlyCluster(api)
    for (const row of cluster.instances) {
      expect(row.display_name, row.addr).toBe(nodeName(row.addr))
      await page.goto(`/instances/${row.instance_id}`)
      await expect(header(page).getByRole('heading', { level: 1 })).toHaveText(nodeName(row.addr))
    }
    await page.goto(`/clusters/${cluster.cluster_id}?section=topology`)
    const graph = page.getByRole('region', { name: 'Replication topology graph' })
    await expect(graph).toBeVisible({ timeout: 30_000 })
    const main = page.getByRole('main')
    for (const name of Object.values(NODE_NAMES)) {
      await expect(main.getByText(name, { exact: true }).first(), name).toBeVisible()
    }
  })

  test('HA-STATIC-002 V-2 roles match pg_is_in_recovery, the hactl leader and the header', async ({
    api,
    signedInPage: page,
  }) => {
    test.setTimeout(liveTimeout(0, 90_000))
    const cluster = await onlyCluster(api)
    for (const row of cluster.instances) {
      const recovery = await psqlOn(row.addr, 'SELECT pg_is_in_recovery();')
      expect(recovery, `${row.addr} pg_is_in_recovery`).toMatch(/^[tf]$/)
      const role = recovery === 't' ? 'standby' : 'primary'
      expect(row.role, row.addr).toBe(role)
      await page.goto(`/instances/${row.instance_id}`)
      await expect(header(page).locator('dt:text-is("Role") + dd')).toHaveText(role)
    }
    const status = (await (await page.request.get(`${HACTL_URL()}/status`)).json()) as {
      leader: string
    }
    expect(cluster.instances.find((row) => row.role === 'primary')?.addr).toBe(status.leader)
  })

  test('HA-STATIC-003 V-3 edges, lag vs pg_stat_replication, topology and lag section', async ({
    api,
    signedInPage: page,
  }) => {
    test.setTimeout(liveTimeout(0, 90_000))
    const cluster = await nameAll(api)
    const primary = cluster.instances.find((row) => row.role === 'primary')
    expect(primary).toBeDefined()
    if (!primary) return
    const standbys = cluster.instances.filter((row) => row.role === 'standby')
    const topology = await api.get(`/api/v1/clusters/${cluster.cluster_id}/topology`)
    expect(topology.ok()).toBe(true)
    const edges = ((await topology.json()) as { topology: Edge[] }).topology
    expect(edges).toHaveLength(2)
    expect(edges.map((edge) => edge.from).sort()).toEqual(
      standbys.map((row) => row.instance_id).sort(),
    )
    for (const edge of edges) {
      expect(edge.to).toBe(primary.instance_id)
      expect(edge.confidence).toBe('high')
    }

    // DESIGN V-3: lag in seconds within ±1 s or ±10 % (the API serves no byte lag, GAP-3).
    // An idle primary makes PostgreSQL report NULL lag; WAL-only messages (no table data)
    // keep the lag columns filled until every standby metric has a sample in pglens.
    const from = new Date()
    const metrics = ['write_lag_sec', 'flush_lag_sec', 'replay_lag_sec'] as const
    let series: ReplicationEdge[] = []
    await expect
      .poll(
        async () => {
          await psqlOn(primary.addr, "SELECT pg_logical_emit_message(false, 'ha-static', 'x');")
          const replication = await api.get(
            `/api/v1/clusters/${cluster.cluster_id}/replication?from=${from.toISOString()}&to=${new Date().toISOString()}`,
          )
          expect(replication.ok()).toBe(true)
          series = ((await replication.json()) as { edges: ReplicationEdge[] }).edges
          return standbys.every((standby) =>
            metrics.every((metric) =>
              series.some(
                (row) =>
                  row.from === standby.instance_id &&
                  row.metric === metric &&
                  row.series.some((point) => point.value !== null),
              ),
            ),
          )
        },
        { timeout: 60_000, intervals: [2_000], message: 'a lag sample per standby and metric' },
      )
      .toBe(true)
    await psqlOn(primary.addr, "SELECT pg_logical_emit_message(false, 'ha-static', 'x');")
    const rows = await psqlOn(
      primary.addr,
      "SELECT application_name, coalesce(extract(epoch FROM write_lag), 0), coalesce(extract(epoch FROM flush_lag), 0), coalesce(extract(epoch FROM replay_lag), 0) FROM pg_stat_replication WHERE state = 'streaming';",
    )
    const truth = new Map(
      rows.split('\n').map((line) => {
        const [name = '', write = '0', flush = '0', replay = '0'] = line.split('|')
        return [name, { write_lag_sec: +write, flush_lag_sec: +flush, replay_lag_sec: +replay }]
      }),
    )
    for (const standby of standbys) {
      const pg = truth.get(standby.addr)
      expect(pg, `pg_stat_replication row for ${standby.addr}`).toBeDefined()
      for (const metric of metrics) {
        const edge = series.find((row) => row.from === standby.instance_id && row.metric === metric)
        expect(edge?.to, `${standby.addr} ${metric} edge`).toBe(primary.instance_id)
        const last = [...(edge?.series ?? [])].reverse().find((point) => point.value !== null)
        expect(last, `${standby.addr} ${metric}: a sample while WAL flows`).toBeDefined()
        const value = last?.value ?? Number.NaN
        const expected = pg?.[metric] ?? 0
        expect(
          Math.abs(value - expected),
          `${standby.addr} ${metric} pglens ${value} vs pg ${expected}`,
        ).toBeLessThanOrEqual(Math.max(1, expected * 0.1))
      }
    }

    await page.goto(`/clusters/${cluster.cluster_id}?section=topology`)
    const table = page.getByRole('table', { name: 'Replication topology edges' })
    await expect(table.locator('tbody tr')).toHaveCount(2, { timeout: 30_000 })
    for (const standby of standbys) {
      await expect(
        table.locator('tbody tr').filter({ hasText: nodeName(standby.addr) }),
        `${standby.addr} edge row`,
      ).toContainText(nodeName(primary.addr))
    }

    await page.goto(`/clusters/${cluster.cluster_id}?section=lag&range=1h`)
    const lag = page.locator('section[aria-labelledby="replication-lag-heading"]')
    await expect(lag).toBeVisible({ timeout: 60_000 })
    await expect(lag.getByRole('article')).toHaveCount(2)
    for (const standby of standbys) {
      const article = lag.getByRole('article', {
        name: `${nodeName(standby.addr)} → ${nodeName(primary.addr)} (async)`,
      })
      await article.scrollIntoViewIfNeeded()
      await expect.poll(() => canvasNotBlank(article), { timeout: 30_000 }).toBe(true)
    }
  })

  test('HA-STATIC-004 V-8 per-node data never mixed, per-node charts, labelled aggregate', async ({
    api,
    signedInPage: page,
  }) => {
    test.setTimeout(liveTimeout(0, 90_000))
    const cluster = await nameAll(api)
    const liveTuples = new Map<string, number>()
    for (const row of cluster.instances) {
      for (const kind of ['tables', 'indexes']) {
        const response = await api.get(`/api/v1/instances/${row.instance_id}/${kind}`)
        expect(response.ok(), `${row.addr} ${kind}`).toBe(true)
      }
      // Each node's table statistics equal that node's own pg_stat_user_tables.
      const response = await api.get(`/api/v1/instances/${row.instance_id}/tables`)
      const items = (
        (await response.json()) as {
          items: { schemaname: string; relname: string; n_live_tup: number }[]
        }
      ).items.filter((item) => item.schemaname === 'public')
      expect(items.length, `${row.addr} tables`).toBeGreaterThan(0)
      const names = items.map((item) => `'${item.relname.replace(/'/g, "''")}'`).join(',')
      const pgSum = Number(
        await psqlOn(
          row.addr,
          `SELECT coalesce(sum(n_live_tup), 0) FROM pg_stat_user_tables WHERE schemaname = 'public' AND relname IN (${names});`,
        ),
      )
      const apiSum = items.reduce((sum, item) => sum + item.n_live_tup, 0)
      expect(
        Math.abs(apiSum - pgSum),
        `${row.addr} n_live_tup pglens ${apiSum} vs pg ${pgSum}`,
      ).toBeLessThanOrEqual(Math.max(10, pgSum * 0.01))
      liveTuples.set(row.role === 'primary' ? 'primary' : row.addr, apiSum)

      await page.goto(`/instances/${row.instance_id}?range=1h`)
      await expect(header(page).getByRole('heading', { level: 1 })).toHaveText(nodeName(row.addr))
      const card = page.getByRole('region', { name: 'Transactions per second' })
      await card.scrollIntoViewIfNeeded()
      await expect
        .poll(() => canvasNotBlank(card), { timeout: 30_000, message: row.addr })
        .toBe(true)
    }
    // Relation statistics: each node's sum matched its own pg_stat_user_tables above. A standby
    // starts with local, near-empty statistics, so the primary must stand clearly apart.
    const primaryTuples = liveTuples.get('primary') ?? 0
    const margin = Math.max(10, primaryTuples * 0.01)
    expect(primaryTuples, 'primary live tuples').toBeGreaterThan(margin)
    for (const [node, tuples] of liveTuples) {
      if (node === 'primary') continue
      expect(Math.abs(tuples - primaryTuples), `${node} vs primary`).toBeGreaterThan(margin)
    }

    // Metric series: 3000 read-only commits on one standby must show in its own commit rate
    // only (10 s samples, so about 300/s there against a few per second elsewhere).
    const burst = cluster.instances.find((row) => row.role === 'standby')
    expect(burst).toBeDefined()
    if (!burst) return
    const started = new Date()
    await psqlOn(burst.addr, 'SELECT 1;\n'.repeat(3000))
    const peak = async (row: ClusterInstance): Promise<number> => {
      const response = await api.get('/api/v1/metrics/query', {
        params: {
          metric: 'pg_xact_commit_total',
          instance_id: row.instance_id,
          database: 'odoo',
          from: new Date(started.getTime() - 60_000).toISOString(),
          to: new Date().toISOString(),
          step: '10s',
        },
      })
      expect(response.ok()).toBe(true)
      const { series } = (await response.json()) as { series: { value: number | null }[] }
      return Math.max(0, ...series.map((point) => point.value ?? 0))
    }
    const others = cluster.instances.filter((row) => row.instance_id !== burst.instance_id)
    await expect
      .poll(
        async () => {
          const own = await peak(burst)
          const rest = Math.max(...(await Promise.all(others.map(peak))))
          return own > 50 && own > 5 * rest
        },
        { timeout: 60_000, message: `${burst.addr} commit burst only in its own series` },
      )
      .toBe(true)

    await page.goto('/')
    const card = page
      .getByRole('listitem')
      .filter({ has: page.getByRole('link', { name: new RegExp(`\\(${cluster.cluster_id}\\)`) }) })
    const value = (await onlyCluster(api)).max_replay_lag_seconds
    // HA-STATIC-003 proved replay lag samples exist, so the aggregate is a number, never "—".
    expect(value, 'max_replay_lag_seconds').not.toBeNull()
    await expect(card.locator('dt:text-is("Max replay lag") + dd')).toHaveText(formatLag(value))
  })

  test('HA-STATIC-005 V-10 a standby says its statistics are its own', async ({
    api,
    signedInPage: page,
  }) => {
    test.setTimeout(liveTimeout(0, 90_000))
    const cluster = await onlyCluster(api)
    const standby = cluster.instances.find((row) => row.role === 'standby')
    expect(standby).toBeDefined()
    if (!standby) return
    const standbyNote = (scope: Page) =>
      scope.getByRole('note').filter({ hasText: /statistics are this standby's own/i })
    await page.goto(`/indexes?instance=${standby.instance_id}&tab=cleanup`)
    await expect(page.getByText(/This server is part of a cluster/)).toBeVisible({
      timeout: 30_000,
    })
    await page.goto(`/instances/${standby.instance_id}?section=relations`)
    await expect(
      page.getByRole('main').getByRole('heading', { name: 'Tables and indexes' }).first(),
    ).toBeVisible({ timeout: 30_000 })
    await expect.soft(standbyNote(page), 'GAP-1 standby note on Tables and indexes').toBeVisible({
      timeout: 10_000,
    })
    for (const tab of ['indexes', 'suggestions', 'dead-rows', 'tax']) {
      await page.goto(`/indexes?instance=${standby.instance_id}&tab=${tab}`)
      await expect.soft(standbyNote(page), `GAP-2 standby note on /indexes ${tab}`).toBeVisible({
        timeout: 30_000,
      })
    }
  })
})

/* ------------------------------------------------------------------------------------------
 * HA-DRILL (plan 005 7.4): live drills on the three-node lab. Each V-item is a soft assertion
 * (a failure is an observation for ha-findings.md); topology recovery is a hard assertion.
 * Run order (serial): D1 switchover, D2 hard failover, D4 rejoin of the D2 victim, D3 standby
 * loss and return, D5 commands on a standby. D4 runs before D3 so every drill starts from
 * three healthy nodes.
 * ---------------------------------------------------------------------------------------- */

const CYCLE_MS = 30_000
const ROLE_WAIT = 2 * CYCLE_MS + 15_000
const STALE_WAIT = 3 * CYCLE_MS + 60_000

interface HactlNode {
  name: string
  role: string
  healthy: boolean
}

interface HactlStatus {
  leader: string
  split_brain: boolean
  failover: string
  writes_blocked: boolean
  failover_paused: boolean
  promoting: unknown
  nodes: HactlNode[]
}

interface PglensEvent {
  ts: string
  type: string
  instance_id: string | null
  payload: Record<string, unknown>
}

interface PglensAlert {
  rule_id: string
  instance_id: string | null
  state: string
  started_at: string
}

async function hactlStatus(page: Page): Promise<HactlStatus | null> {
  return page.request
    .get(`${HACTL_URL()}/status`, { timeout: 5_000 })
    .then(async (response) => (response.ok() ? ((await response.json()) as HactlStatus) : null))
    .catch(() => null)
}

/** Problems that keep the lab from the healthy three-node state; empty when healthy. */
async function topologyProblems(api: APIRequestContext, page: Page): Promise<string[]> {
  const status = await hactlStatus(page)
  if (!status) return ['hactl unreachable']
  const problems: string[] = []
  if (!status.leader) problems.push('no leader')
  if (status.split_brain) problems.push('split brain')
  if (status.failover !== 'idle') problems.push(`failover ${status.failover}`)
  if (status.writes_blocked) problems.push('writes blocked')
  if (status.failover_paused) problems.push('failover paused')
  if (status.promoting) problems.push('promoting')
  for (const node of status.nodes) {
    const want = node.name === status.leader ? 'primary' : 'standby'
    if (!node.healthy || node.role !== want) problems.push(`${node.name} ${node.role}`)
  }
  if (status.leader) {
    const streaming = await psqlOn(
      status.leader,
      "SELECT count(*) FROM pg_stat_replication WHERE state = 'streaming';",
    ).catch(() => '0')
    if (streaming !== '2') problems.push(`${streaming} standbys streaming`)
  }
  const cluster = await onlyCluster(api)
  for (const row of cluster.instances) {
    const want = row.addr === status.leader ? 'primary' : 'standby'
    if (!row.up || row.role !== want) problems.push(`pglens ${row.addr} ${row.role} up=${row.up}`)
  }
  return problems
}

async function expectHealthy(api: APIRequestContext, page: Page, timeout: number): Promise<void> {
  await expect
    .poll(() => topologyProblems(api, page), {
      timeout,
      intervals: [3_000],
      message: 'healthy lab',
    })
    .toEqual([])
}

async function idsByAddr(api: APIRequestContext): Promise<Map<string, string>> {
  return new Map((await onlyCluster(api)).instances.map((row) => [row.addr, row.instance_id]))
}

async function eventsSince(
  api: APIRequestContext,
  type: string,
  since: Date,
): Promise<PglensEvent[]> {
  const response = await api.get(`/api/v1/events?type=${type}&limit=200`)
  if (!response.ok()) return []
  return ((await response.json()) as PglensEvent[]).filter((event) => new Date(event.ts) >= since)
}

async function alertsSince(
  api: APIRequestContext,
  rule: string,
  since: Date,
): Promise<PglensAlert[]> {
  const response = await api.get(`/api/v1/alerts?state=all&rule_id=${rule}`)
  if (!response.ok()) return []
  return ((await response.json()) as PglensAlert[]).filter(
    (alert) => new Date(alert.started_at) >= since,
  )
}

/** No notifications API exists: a delivered row with ok = true in the pglens database. */
async function deliveredSince(rule: string, since: Date): Promise<number> {
  const out = await run('docker', [
    'exec',
    'pglens-odoo-timescaledb-1',
    'psql',
    '-U',
    'pglens',
    '-d',
    'pglens',
    '-Atc',
    `SELECT count(*) FROM notifications WHERE channel = 'google_chat' AND alert_key LIKE '${rule}/%' AND phase = 'fire' AND ok AND sent_at >= '${since.toISOString()}';`,
  ])
  return Number(out.stdout.trim())
}

/** Odoo through hactl: one read RPC per second for at least 20 s; the longest gap between two successes. */
function odooProbe(): () => Promise<{
  calls: number
  failures: number
  maxGapMs: number
  lastOk: boolean
}> {
  let stopped = false
  let calls = 0
  let failures = 0
  let lastSuccess = Date.now()
  let maxGapMs = 0
  let lastOk = true
  const loop = (async () => {
    while (!stopped) {
      const started = Date.now()
      calls += 1
      try {
        const session = await login()
        await searchRead(session, 'res.partner', [], ['name'], 5)
        maxGapMs = Math.max(maxGapMs, Date.now() - lastSuccess)
        lastSuccess = Date.now()
        lastOk = true
      } catch {
        failures += 1
        lastOk = false
      }
      await new Promise((resolve) =>
        setTimeout(resolve, Math.max(0, 1_000 - (Date.now() - started))),
      )
    }
  })()
  const startedAt = Date.now()
  return async () => {
    const minMs = 20_000
    await new Promise((resolve) => setTimeout(resolve, Math.max(0, startedAt + minMs - Date.now())))
    stopped = true
    await loop
    maxGapMs = Math.max(maxGapMs, lastOk ? 0 : Date.now() - lastSuccess)
    return { calls, failures, maxGapMs, lastOk }
  }
}

/**
 * `load/rpc_load.py` (read-only Odoo RPC) in the background for the drill. The script exits on
 * its first failed RPC (an outage), so it is restarted until the deadline.
 */
function startLoad(seconds: number): Promise<{ runs: number; failures: number }> {
  const end = Date.now() + seconds * 1_000
  return (async () => {
    let runs = 0
    let failures = 0
    while (Date.now() < end - 1_000) {
      runs += 1
      const left = Math.ceil((end - Date.now()) / 1_000)
      await run('python3', [join(STACK_DIR, 'load', 'rpc_load.py')], {
        env: { ...process.env, LOAD_SECONDS: String(left) },
        timeoutMs: (left + 60) * 1_000,
      }).catch(async () => {
        failures += 1
        await new Promise((resolve) => setTimeout(resolve, 1_000))
      })
    }
    return { runs, failures }
  })()
}

async function haScript(script: string, ...args: string[]): Promise<RunResult> {
  const result = await run('bash', [join(STACK_DIR, 'ha', script), ...args], {
    timeoutMs: 540_000,
  })
  await test.info().attach(`${script} ${args.join(' ')}`, { body: result.stdout })
  return result
}

/** A cluster-scoped alert episode spans its lookback: start each drill outside the previous one. */
async function expectNoFiring(api: APIRequestContext, rule: string): Promise<void> {
  await expect
    .poll(
      async () => {
        const response = await api.get(`/api/v1/alerts?state=firing&rule_id=${rule}`)
        return response.ok() ? ((await response.json()) as PglensAlert[]).length : -1
      },
      { timeout: 240_000, intervals: [10_000], message: `no ${rule} alert firing` },
    )
    .toBe(0)
}

/** V-2 (pglens roles = PostgreSQL), V-9 (last_seen advances on every running node). */
async function expectRolesAndAgent(
  api: APIRequestContext,
  leader: string,
  running: string[],
  after: Date,
  label: string,
  page?: Page,
): Promise<void> {
  await expect.soft
    .poll(
      async () => {
        const problems: string[] = []
        const instances = (await onlyCluster(api)).instances
        for (const addr of running) {
          if (!instances.some((row) => row.addr === addr)) problems.push(`${addr} not listed`)
        }
        for (const row of instances) {
          if (!running.includes(row.addr)) continue
          const recovery = await psqlOn(row.addr, 'SELECT pg_is_in_recovery();').catch(() => '?')
          const pg = recovery === 't' ? 'standby' : recovery === 'f' ? 'primary' : '?'
          const want = row.addr === leader ? 'primary' : 'standby'
          if (row.role !== want || pg !== want)
            problems.push(`${row.addr} pglens=${row.role} pg=${pg}`)
        }
        return problems
      },
      { timeout: ROLE_WAIT, intervals: [5_000], message: `${label} V-2 roles within 2 cycles` },
    )
    .toEqual([])
  await expect.soft
    .poll(
      async () => {
        const response = await api.get('/api/v1/instances')
        const rows = (await response.json()) as (InstanceRow & { last_seen: string })[]
        return running.filter(
          (addr) => !rows.some((row) => row.addr === addr && new Date(row.last_seen) > after),
        )
      },
      { timeout: ROLE_WAIT, intervals: [5_000], message: `${label} V-9 last_seen advances` },
    )
    .toEqual([])
  const leaderRow = (await onlyCluster(api)).instances.find((row) => row.addr === leader)
  if (page && leaderRow) {
    await page.goto(`/instances/${leaderRow.instance_id}`)
    await expect
      .soft(header(page).locator('dt:text-is("Role") + dd'), `${label} V-2 UI role of ${leader}`)
      .toHaveText('primary', { timeout: 30_000 })
  }
}

async function expectFailoverRecorded(
  api: APIRequestContext,
  ids: Map<string, string>,
  oldLeader: string,
  newLeader: string,
  since: Date,
  label: string,
): Promise<void> {
  await expect.soft
    .poll(
      async () =>
        (await eventsSince(api, 'failover_detected', since)).some(
          (event) =>
            event.payload.new_primary === ids.get(newLeader) &&
            event.payload.old_primary === ids.get(oldLeader),
        ),
      { timeout: ROLE_WAIT, intervals: [5_000], message: `${label} V-4 failover_detected event` },
    )
    .toBe(true)
  await expect.soft
    .poll(async () => (await alertsSince(api, 'failover_detected', since)).length, {
      timeout: ROLE_WAIT,
      intervals: [5_000],
      message: `${label} V-7 failover_detected alert`,
    })
    .toBeGreaterThan(0)
  await expect.soft
    .poll(() => deliveredSince('failover_detected', since), {
      timeout: ROLE_WAIT + 30_000,
      intervals: [5_000],
      message: `${label} V-12 failover_detected delivered to Google Chat`,
    })
    .toBeGreaterThan(0)
}

/** V-5: one series per (standby, primary, metric); the new edges carry samples. */
async function expectLagContinuity(
  api: APIRequestContext,
  page: Page,
  ids: Map<string, string>,
  leader: string,
  since: Date,
  label: string,
): Promise<void> {
  const cluster = await onlyCluster(api)
  const from = new Date(since.getTime() - 300_000)
  const standbys = [...ids.keys()].filter((addr) => addr !== leader)
  await expect.soft
    .poll(
      async () => {
        await psqlOn(leader, "SELECT pg_logical_emit_message(false, 'ha-drill', 'x');")
        const response = await api.get(
          `/api/v1/clusters/${cluster.cluster_id}/replication?from=${from.toISOString()}&to=${new Date().toISOString()}`,
        )
        const edges = ((await response.json()) as { edges: ReplicationEdge[] }).edges
        const problems: string[] = []
        const keys = edges.map((edge) => `${edge.from}|${edge.to}|${edge.metric}`)
        if (new Set(keys).size !== keys.length) problems.push('a (from, to, metric) series twice')
        if (edges.some((edge) => edge.from === edge.to))
          problems.push('an edge from a node to itself')
        for (const addr of standbys) {
          const edge = edges.find(
            (row) =>
              row.from === ids.get(addr) &&
              row.to === ids.get(leader) &&
              row.metric === 'write_lag_sec' &&
              row.series.some((point) => point.value !== null && new Date(point.ts) >= since),
          )
          if (!edge) problems.push(`no ${addr} -> ${leader} samples since the drill`)
        }
        return problems
      },
      { timeout: ROLE_WAIT, intervals: [5_000], message: `${label} V-5 lag series per standby` },
    )
    .toEqual([])
  await page.goto(`/clusters/${cluster.cluster_id}?section=lag&range=1h`)
  const lag = page.locator('section[aria-labelledby="replication-lag-heading"]')
  for (const addr of standbys) {
    const article = lag.getByRole('article', {
      name: `${nodeName(addr)} → ${nodeName(leader)} (async)`,
    })
    await expect.soft(article, `${label} V-5 lag chart ${addr} -> ${leader}`).toBeVisible({
      timeout: 30_000,
    })
  }
}

function expectOdoo(
  result: { calls: number; maxGapMs: number; lastOk: boolean },
  label: string,
  boundMs: number,
): void {
  expect.soft(result.calls, `${label} Odoo probe ran`).toBeGreaterThan(5)
  expect
    .soft(result.maxGapMs, `${label} Odoo RPC gap ${result.maxGapMs} ms`)
    .toBeLessThanOrEqual(boundMs)
  expect.soft(result.lastOk, `${label} Odoo RPC recovered`).toBe(true)
}

test.describe.serial('HA-DRILL', () => {
  test.beforeEach(async () => {
    assertStimulusAllowed()
    await assertLifecycleAllowed()
  })
  test.afterEach(async () => {
    await runCleanups()
  })

  test('HA-DRILL-001 D1 switchover to pg-ha-2', async ({ api, signedInPage: page }) => {
    test.setTimeout(900_000)
    await expectHealthy(api, page, 60_000)
    await expectNoFiring(api, 'failover_detected')
    await nameAll(api)
    const ids = await idsByAddr(api)
    const oldLeader = (await hactlStatus(page))?.leader ?? ''
    const target = oldLeader === 'pg-ha-2' ? 'pg-ha-3' : 'pg-ha-2'
    const t0 = new Date()
    const load = startLoad(120)
    const probe = odooProbe()
    await haScript('switchover.sh', target)
    const done = new Date()
    expectOdoo(await probe(), 'D1', 30_000)
    test.info().annotations.push({ type: 'D1 load', description: JSON.stringify(await load) })
    expect((await hactlStatus(page))?.leader, 'hactl leader after the switchover').toBe(target)
    await expectRolesAndAgent(api, target, [...ids.keys()], done, 'D1', page)
    await expectFailoverRecorded(api, ids, oldLeader, target, t0, 'D1')
    await expectLagContinuity(api, page, ids, target, done, 'D1')
    await expectHealthy(api, page, ROLE_WAIT)
  })

  test('HA-DRILL-002 D2 hard failover: docker kill the primary', async ({
    api,
    signedInPage: page,
  }) => {
    test.setTimeout(900_000)
    await expectHealthy(api, page, 60_000)
    await expectNoFiring(api, 'failover_detected')
    const ids = await idsByAddr(api)
    const oldLeader = (await hactlStatus(page))?.leader ?? ''
    expect(oldLeader).not.toBe('')
    const t0 = new Date()
    const load = startLoad(120)
    const probe = odooProbe()
    await run('docker', ['kill', `pglens-odoo-${oldLeader}-1`])
    let newLeader = ''
    await expect
      .poll(
        async () => {
          const status = await hactlStatus(page)
          newLeader =
            status && !status.split_brain && status.leader !== oldLeader ? status.leader : ''
          return newLeader
        },
        { timeout: 60_000, intervals: [1_000], message: 'hactl promotes a standby' },
      )
      .not.toBe('')
    const promotedMs = Date.now() - t0.getTime()
    expect.soft(promotedMs, `D2 promotion took ${promotedMs} ms`).toBeLessThanOrEqual(15_000)
    await expect
      .poll(async () => (await hactlStatus(page))?.failover, { timeout: 60_000 })
      .toBe('idle')
    const done = new Date()
    const running = [...ids.keys()].filter((addr) => addr !== oldLeader)
    await expectRolesAndAgent(api, newLeader, running, done, 'D2', page)
    expectOdoo(await probe(), 'D2', 30_000)
    test.info().annotations.push({ type: 'D2 load', description: JSON.stringify(await load) })
    await expectFailoverRecorded(api, ids, oldLeader, newLeader, t0, 'D2')
    const survivors = new Map([...ids].filter(([addr]) => addr !== oldLeader))
    await expectLagContinuity(api, page, survivors, newLeader, done, 'D2')
    await expect.soft
      .poll(
        async () =>
          (await alertsSince(api, 'instance_unreachable', t0)).some(
            (alert) => alert.instance_id === ids.get(oldLeader),
          ),
        { timeout: STALE_WAIT, intervals: [5_000], message: 'D2 V-7 instance_unreachable alert' },
      )
      .toBe(true)
    await expect.soft
      .poll(() => deliveredSince('instance_unreachable', t0), {
        timeout: 60_000,
        intervals: [5_000],
        message: 'D2 V-12 instance_unreachable delivered to Google Chat',
      })
      .toBeGreaterThan(0)
    const cluster = await onlyCluster(api)
    expect
      .soft(
        cluster.instances.find((row) => row.addr === oldLeader)?.up,
        'D2 the killed primary reads as down',
      )
      .toBe(false)
    const health = await api
      .get('/api/v1/clusters')
      .then(async (r) => (await r.json()) as { health: string }[])
    expect
      .soft(
        health[0]?.health,
        'D2 instance down: cluster health not ok (no redundancy finding exists, GAP-5)',
      )
      .not.toBe('ok')
    // The killed node stays down: HA-DRILL-004 brings it back.
    const status = await hactlStatus(page)
    expect(status?.leader).toBe(newLeader)
    expect(status?.nodes.find((node) => node.name === oldLeader)?.role).toBe('unreachable')
  })

  test('HA-DRILL-004 D4 the old primary returns diverged and rejoins', async ({
    api,
    signedInPage: page,
  }) => {
    test.setTimeout(900_000)
    const status = await hactlStatus(page)
    const leader = status?.leader ?? ''
    const victim = status?.nodes.find((node) => node.role === 'unreachable')?.name ?? ''
    expect(victim, 'HA-DRILL-002 left one node down').not.toBe('')
    const ids = await idsByAddr(api)
    const t0 = new Date()
    const load = startLoad(120)
    const probe = odooProbe()
    // Deliberate: the old primary comes back writable on the old timeline (DESIGN §6).
    await run('docker', ['start', `pglens-odoo-${victim}-1`])
    await expect
      .poll(
        async () => (await hactlStatus(page))?.nodes.find((node) => node.name === victim)?.role,
        { timeout: 60_000, message: 'hactl fences the old primary' },
      )
      .toBe('diverged_or_stale')
    expect((await hactlStatus(page))?.leader, 'hactl never routes to the old primary').toBe(leader)
    await expect.soft
      .poll(
        async () =>
          (await eventsSince(api, 'split_brain_detected', t0)).length > 0 &&
          (await alertsSince(api, 'split_brain_detected', t0)).length > 0,
        {
          timeout: ROLE_WAIT,
          intervals: [5_000],
          message: 'D4 V-6 split_brain_detected event and alert',
        },
      )
      .toBe(true)
    const rejoinStarted = new Date()
    await haScript('rejoin.sh', victim)
    await expectHealthy(api, page, ROLE_WAIT + 60_000)
    await expectRolesAndAgent(api, leader, [...ids.keys()], rejoinStarted, 'D4', page)
    expect
      .soft(
        (await eventsSince(api, 'role_change', rejoinStarted)).some(
          (event) => event.instance_id === ids.get(victim),
        ),
        'D4 V-2 role_change of the rejoined node',
      )
      .toBe(true)
    await expect.soft
      .poll(
        async () =>
          (await alertsSince(api, 'split_brain_detected', t0)).every(
            (alert) => alert.state === 'resolved',
          ),
        { timeout: 240_000, intervals: [10_000], message: 'D4 V-6 split brain alert resolves' },
      )
      .toBe(true)
    await expect.soft
      .poll(
        async () =>
          ((await (await api.get('/api/v1/clusters')).json()) as { health: string }[])[0]?.health,
        { timeout: ROLE_WAIT, intervals: [5_000], message: 'D4 V-6 cluster health ok again' },
      )
      .toBe('ok')
    expectOdoo(await probe(), 'D4', 5_000)
    test.info().annotations.push({ type: 'D4 load', description: JSON.stringify(await load) })
  })

  test('HA-DRILL-003 D3 standby loss and return, lag under load', async ({
    api,
    signedInPage: page,
  }) => {
    test.setTimeout(1_200_000)
    await expectHealthy(api, page, 60_000)
    const ids = await idsByAddr(api)
    const cluster = await onlyCluster(api)
    const leader = (await hactlStatus(page))?.leader ?? ''
    const standby =
      [...ids.keys()]
        .filter((addr) => addr !== leader)
        .sort()
        .at(-1) ?? ''
    const slot = { 'odoo-db': 'ha_1', 'pg-ha-2': 'ha_2', 'pg-ha-3': 'ha_3' }[standby] ?? ''
    await expectNoFiring(api, 'instance_unreachable')
    await expectNoFiring(api, 'agent_down')
    const t0 = new Date()
    const probe = odooProbe()
    await run('docker', ['stop', '-t', '30', `pglens-odoo-${standby}-1`])
    await expect
      .poll(
        async () => (await hactlStatus(page))?.nodes.find((node) => node.name === standby)?.role,
        { timeout: 30_000 },
      )
      .toBe('unreachable')
    await expect.soft
      .poll(
        async () => {
          const response = await api.get(`/api/v1/clusters/${cluster.cluster_id}/slots`)
          const slots = (
            (await response.json()) as {
              slots: { slot_name: string; active: boolean; instance_id: string }[]
            }
          ).slots
          return slots.find((row) => row.slot_name === slot && row.instance_id === ids.get(leader))
            ?.active
        },
        { timeout: ROLE_WAIT, intervals: [5_000], message: `D3 V-6 slot ${slot} inactive` },
      )
      .toBe(false)
    await expect.soft
      .poll(async () => (await eventsSince(api, 'slot_inactive', t0)).length, {
        timeout: ROLE_WAIT + 30_000,
        intervals: [5_000],
        message: 'D3 V-6 slot_inactive event',
      })
      .toBeGreaterThan(0)
    await expect.soft
      .poll(
        async () =>
          (await eventsSince(api, 'instance_unreachable', t0)).some(
            (event) => event.payload.instance_id === ids.get(standby),
          ) &&
          (await alertsSince(api, 'instance_unreachable', t0)).some(
            (alert) => alert.instance_id === ids.get(standby),
          ),
        {
          timeout: STALE_WAIT,
          intervals: [5_000],
          message: 'D3 V-7 instance_unreachable event and alert',
        },
      )
      .toBe(true)
    const topology = await api.get(`/api/v1/clusters/${cluster.cluster_id}/topology`)
    const edges = ((await topology.json()) as { topology: Edge[] }).topology
    expect
      .soft(
        edges.some((edge) => edge.from === ids.get(standby) && edge.confidence === 'high'),
        'D3 V-3 the lost standby has no high-confidence edge',
      )
      .toBe(false)
    const health = ((await (await api.get('/api/v1/clusters')).json()) as { health: string }[])[0]
      ?.health
    expect
      .soft(health, 'D3 instance down: cluster health not ok (no redundancy finding exists, GAP-5)')
      .not.toBe('ok')

    await haScript('rejoin.sh', standby)
    await expectHealthy(api, page, ROLE_WAIT + 60_000)
    expectOdoo(await probe(), 'D3', 5_000)

    // Lag under load: a 3 s apply delay on the returned standby plus a commit with an xid every
    // 0.2 s keeps replay behind (the delay applies to commit records; sparse commits let later
    // WAL replay at once and PostgreSQL then reports ~0 replay lag).
    const load = startLoad(120)
    registerCleanup(async () => {
      await psqlOn(standby, 'ALTER SYSTEM RESET recovery_min_apply_delay; SELECT pg_reload_conf();')
    })
    await psqlOn(
      standby,
      "ALTER SYSTEM SET recovery_min_apply_delay = '3s'; SELECT pg_reload_conf();",
    )
    const commits = run(
      'docker',
      ['exec', '-i', `pglens-odoo-${leader}-1`, 'psql', '-U', 'odoo', '-d', 'odoo', '-X', '-q'],
      {
        input: 'BEGIN; SELECT pg_current_xact_id(); COMMIT; SELECT pg_sleep(0.2);\n'.repeat(500),
        timeoutMs: 240_000,
      },
    ).catch(() => null)
    const loadStart = new Date()
    let observed = ''
    await expect.soft
      .poll(
        async () => {
          const response = await api.get(
            `/api/v1/clusters/${cluster.cluster_id}/replication?from=${loadStart.toISOString()}&to=${new Date().toISOString()}`,
          )
          const edges = ((await response.json()) as { edges: ReplicationEdge[] }).edges
          const metrics = ['write_lag_sec', 'flush_lag_sec', 'replay_lag_sec']
          const notes: string[] = []
          let ok = true
          // Every standby within tolerance; the delayed one also above it (non-vacuous).
          for (const addr of [...ids.keys()].filter((name) => name !== leader)) {
            const pg = (
              await psqlOn(
                leader,
                `SELECT concat_ws('|', coalesce(extract(epoch FROM write_lag), 0), coalesce(extract(epoch FROM flush_lag), 0), coalesce(extract(epoch FROM replay_lag), 0)) FROM pg_stat_replication WHERE application_name = '${addr}';`,
              )
            )
              .split('|')
              .map(Number)
            const values = metrics.map((metric) => {
              const edge = edges.find((row) => row.from === ids.get(addr) && row.metric === metric)
              const last = [...(edge?.series ?? [])].reverse().find((point) => point.value !== null)
              return last?.value ?? null
            })
            notes.push(
              `${addr}: ` +
                metrics.map((metric, i) => `${metric} ${values[i]} vs pg ${pg[i]}`).join(', '),
            )
            ok &&= values.every(
              (value, i) =>
                value !== null && Math.abs(value - (pg[i] ?? 0)) <= Math.max(1, (pg[i] ?? 0) * 0.1),
            )
            if (addr === standby) ok &&= (values[2] ?? 0) > 1
          }
          observed = notes.join('; ')
          return ok
        },
        {
          timeout: 120_000,
          intervals: [3_000],
          message: 'D3 V-3 write/flush/replay lag under load',
        },
      )
      .toBe(true)
    test.info().annotations.push({ type: 'D3 lag', description: observed })
    await psqlOn(standby, 'ALTER SYSTEM RESET recovery_min_apply_delay; SELECT pg_reload_conf();')
    await commits
    await load
    await expectHealthy(api, page, 60_000)
  })

  test('HA-DRILL-005 D5 commands on a standby are refused or valid, never silently wrong', async ({
    api,
    signedInPage: page,
  }) => {
    test.setTimeout(600_000)
    await expectHealthy(api, page, 60_000)
    const ids = await idsByAddr(api)
    const leader = (await hactlStatus(page))?.leader ?? ''
    const standby = [...ids.keys()].filter((addr) => addr !== leader).sort()[0] ?? ''
    const instanceId = ids.get(standby) ?? ''
    const finish = async (commandId: string) => {
      let state = ''
      let body: { state: string; result?: { plan?: unknown }; error?: string } = { state: '' }
      await expect
        .poll(
          async () => {
            body = (await (await api.get(`/api/v1/commands/${commandId}`)).json()) as typeof body
            state = body.state
            return state === 'pending' || state === 'claimed'
          },
          { timeout: 90_000, intervals: [2_000], message: `command ${commandId} finishes` },
        )
        .toBe(false)
      return body
    }

    // EXPLAIN of a statement the standby itself ran.
    const statements = (await (
      await api.get(`/api/v1/statements?instance_id=${instanceId}&limit=50`)
    ).json()) as { statements: { queryid: string; datname: string; query_text: string }[] }
    const statement = statements.statements.find((row) => /^\s*SELECT\b/i.test(row.query_text))
    expect(statement, `${standby} has a SELECT in pg_stat_statements`).toBeDefined()
    const explain = await api.post(`/api/v1/instances/${instanceId}/commands`, {
      data: { kind: 'explain', args: { queryid: statement?.queryid, datname: statement?.datname } },
    })
    expect(explain.status()).toBe(202)
    const explainId = ((await explain.json()) as { command_id: string }).command_id
    const explained = await finish(explainId)
    expect
      .soft(
        explained.state === 'done' ? Boolean(explained.result?.plan) : Boolean(explained.error),
        `D5 V-11 explain on a standby: ${explained.state} ${explained.error ?? ''}`,
      )
      .toBe(true)

    // Cancel and terminate a session on the standby: the API outcome must match PostgreSQL.
    for (const kind of ['cancel', 'terminate'] as const) {
      const app = `ha-drill-${kind}`
      const sleeper = run(
        'docker',
        [
          'exec',
          '-e',
          `PGAPPNAME=${app}`,
          `pglens-odoo-${standby}-1`,
          'psql',
          '-U',
          'odoo',
          '-d',
          'odoo',
          '-Atc',
          'SELECT pg_sleep(60);',
        ],
        { timeoutMs: 90_000 },
      ).catch(() => null)
      let pid = ''
      await expect
        .poll(
          async () => {
            pid = await psqlOn(
              standby,
              `SELECT pid FROM pg_stat_activity WHERE application_name = '${app}' LIMIT 1;`,
            )
            return pid
          },
          { timeout: 15_000 },
        )
        .toMatch(/^\d+$/)
      const enqueue = await api.post(`/api/v1/instances/${instanceId}/commands`, {
        data: { kind, args: { pid: Number(pid) } },
      })
      const accepted = enqueue.status() === 202
      const outcome = accepted
        ? await finish(((await enqueue.json()) as { command_id: string }).command_id)
        : { state: `http ${enqueue.status()}`, error: await enqueue.text() }
      const isAlive = async () =>
        (await psqlOn(
          standby,
          `SELECT count(*) FROM pg_stat_activity WHERE pid = ${pid} AND state = 'active';`,
        )) === '1'
      let alive = await isAlive()
      for (let i = 0; i < 5 && outcome.state === 'done' && alive; i += 1) {
        await new Promise((resolve) => setTimeout(resolve, 1_000))
        alive = await isAlive()
      }
      const consistent = outcome.state === 'done' ? !alive : alive && Boolean(outcome.error)
      expect
        .soft(
          consistent,
          `D5 V-11 ${kind}: ${outcome.state} ${outcome.error ?? ''}, session alive=${alive}`,
        )
        .toBe(true)
      await psqlOn(standby, `SELECT pg_terminate_backend(${pid});`).catch(() => null)
      await sleeper
    }
    const audit = await api.get(`/api/v1/instances/${instanceId}/command-audit`)
    expect.soft(audit.ok(), 'D5 command audit answers').toBe(true)
    expect.soft(await audit.text(), 'D5 explain audited').toContain(explainId)
    await expectHealthy(api, page, 60_000)
  })
})
