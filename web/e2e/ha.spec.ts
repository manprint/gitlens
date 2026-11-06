import type { APIRequestContext, Page } from '@playwright/test'

import { expect, test } from './fixtures'
import { login } from './qa/odoo'
import { queryInt } from './qa/pg'
import { run } from './qa/stack'
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
