import { createHash } from 'node:crypto'
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { join, resolve } from 'node:path'

import { expect, type Page } from '@playwright/test'

import { test } from './fixtures'
import * as api from './qa/api'
import { BUDGET, qaTimeout } from './qa/budget'
import { assertLifecycleAllowed, COMPOSE_PROJECT } from './qa/guard'
import { login } from './qa/odoo'
import { consoleGuard } from './qa/observe'
import { database, query } from './qa/pg'
import { originOf, truthSessions, type ApiLocks } from './qa/sessionTruth'
import {
  BASE_SERVICES,
  composeArgs,
  down,
  LIFECYCLE_SERVICES,
  rebuild,
  restart,
  run,
  stackStatus,
  start,
  stop,
  up,
  upAll,
  waitReady,
} from './qa/stack'
import { completenessOf } from './visual_helpers'

/**
 * 6.4 lifecycle drills (LIFE-*), test-strategy.md section 6 and REQ-41: the stack
 * is restarted, rebuilt and taken down while the console is used, and pglens must
 * degrade visibly and come back by itself. Unit 6.4, requirement REQ-41.
 *
 * Serial, no retries (a retry hides a defect). Every drill is guarded by
 * `PGLENS_QA_ALLOW_LIFECYCLE=1` and a verified backup younger than 24 h (the stack
 * functions enforce it). `afterEach` always leaves the stack healthy (missing
 * services are started again, then `waitReady`). Nothing here uses `down -v`,
 * deletes a volume or pulls an image; secrets are only read from the environment
 * and never printed. Each assertion reads two independent sources where both
 * exist: the console, the pglens API, PostgreSQL (odoo-db and the pglens
 * TimescaleDB through `docker exec`).
 */
test.describe.configure({ mode: 'serial', retries: 0 })

const SERVER = 'pglens-server'
const AGENT = 'pglens-agent'
const ODOO = 'odoo'
const TSDB = 'timescaledb'
const ODOO_DB_CONTAINER = 'pglens-odoo-odoo-db-1'
const TSDB_CONTAINER = 'pglens-odoo-timescaledb-1'
const TPS = 'Transactions per second'
const STEP_MS = 60_000
const POLL_MS = 3_000
/**
 * A downtime of at least two buckets (> 120 s, including `up`) guarantees one
 * aligned 60 s bucket lies entirely inside it, whatever the bucket phase.
 */
const HOLD_DOWN_MS = 125_000
/** The agent's first-run caps of D22a, plus 30 s for the run and the transfer. */
const WARMUP_SLACK_MS = 30_000
const OUTAGE_NOISE = /requestfailed|ERR_|Failed to load resource|HTTP (401|50[0-4])/

const sleep = (ms: number) => new Promise<void>((resolveSleep) => setTimeout(resolveSleep, ms))
const message = (error: unknown) => (error instanceof Error ? error.message : String(error))
const enc = encodeURIComponent
const ids = (list: readonly number[]) => list.join(',')
const baseUrl = () => process.env.PGLENS_UI_BASE_URL ?? 'http://127.0.0.1:9090'
const odooUrl = () => (process.env.ODOO_URL ?? 'http://127.0.0.1:9091').replace(/\/$/, '')

let instancePromise: Promise<string> | undefined
const instanceId = () =>
  (instancePromise ??= api.instances().then((rows) => {
    const row = rows.find((candidate) => candidate.role === 'primary') ?? rows[0]
    if (!row) throw new Error('lifecycle: pglens knows no instance')
    return row.instance_id
  }))

/** Polls `check` until it returns no problem; throws the last problems after `budgetMs`. */
async function until(
  what: string,
  budgetMs: number,
  check: () => Promise<string[]>,
  intervalMs = POLL_MS,
): Promise<void> {
  const started = Date.now()
  let last: string[] = ['not checked']
  while (Date.now() - started < budgetMs) {
    try {
      last = await check()
    } catch (error) {
      last = [`error: ${message(error)}`]
    }
    if (last.length === 0) {
      console.log(`[lifecycle] ${what}: reached after ${Date.now() - started} ms`)
      return
    }
    await sleep(intervalMs)
  }
  throw new Error(
    `${what}: not reached within ${budgetMs} ms (until ${new Date().toISOString()}); last: ${last.join('; ')}`,
  )
}

/** HTTP status of `url`, or null when the connection fails. */
async function probe(url: string, init?: RequestInit): Promise<number | null> {
  try {
    return (await fetch(url, { ...init, signal: AbortSignal.timeout(5_000) })).status
  } catch {
    return null
  }
}

async function waitOdoo(budgetMs: number): Promise<void> {
  await until('Odoo answers a login', budgetMs, async () => {
    try {
      await login()
      return []
    } catch (error) {
      return [message(error)]
    }
  })
}

/** SQL on the pglens TimescaleDB (peer trust inside the container, no secret needed). */
async function pglensSql(sql: string): Promise<string> {
  const { stdout } = await run(
    'docker',
    [
      'exec',
      '-i',
      TSDB_CONTAINER,
      'psql',
      '-X',
      '-A',
      '-t',
      '-v',
      'ON_ERROR_STOP=1',
      '-U',
      'pglens',
      '-d',
      'pglens',
    ],
    { input: sql, timeoutMs: 60_000 },
  )
  return stdout.trim()
}

/* --- the stack after each drill ------------------------------------------------ */

test.afterEach(async () => {
  const rows = await stackStatus()
  const missing = BASE_SERVICES.filter(
    (name) => !rows.some((row) => row.service === name && row.state === 'running'),
  )
  if (missing.includes('odoo-db')) await upAll()
  else if (missing.length > 0) await up()
  await waitReady(BUDGET.serviceRestart)
})

/* --- console readers -------------------------------------------------------------- */

const signInHeading = (page: Page) => page.getByRole('heading', { name: 'Sign in to pglens' })
const connection = (page: Page, state: 'reachable' | 'not reachable') =>
  page.getByRole('status', { name: `Connection: ${state}`, exact: true })
const tpsRegion = (page: Page) => page.getByRole('region', { name: TPS })

async function submitPassword(page: Page): Promise<void> {
  const password = process.env.PGLENS_UI_PASSWORD
  if (!password) throw new Error('PGLENS_UI_PASSWORD is required to sign in again')
  await page.getByLabel('Password', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(signInHeading(page)).toBeHidden({ timeout: 30_000 })
}

/** Opens the instance overview; signs in when the console asks (a server restart ends the sessions). */
async function openInstance(page: Page): Promise<void> {
  const path = `/instances/${enc(await instanceId())}?range=1h`
  await page.goto(path)
  await expect(signInHeading(page).or(tpsRegion(page))).toBeVisible({ timeout: 30_000 })
  if (await signInHeading(page).isVisible()) {
    await submitPassword(page)
    await page.goto(path)
  }
  await expect(tpsRegion(page)).toBeVisible({ timeout: 30_000 })
}

/** The Commits series of the TPS chart: last cell of each row of its sr-only table. */
async function commitCells(page: Page): Promise<string[]> {
  return page
    .getByRole('table', { name: `${TPS} data`, exact: true })
    .locator('tbody tr')
    .evaluateAll((rows) =>
      rows
        .filter((row) => row.querySelector('th')?.textContent === 'Commits')
        .map((row) => {
          const cells = row.querySelectorAll('td')
          return cells.item(cells.length - 1)?.textContent ?? ''
        }),
    )
}

const isNull = (value: string) => value.trim() === 'Unknown'
const zeroCells = (values: readonly string[]) =>
  values.filter((value) => !isNull(value) && Number.parseFloat(value) === 0).length

/** Null cells between the first and the last observed value (history start and the open bucket excluded). */
function internalGaps(values: readonly string[]): number {
  const first = values.findIndex((value) => !isNull(value))
  let last = -1
  values.forEach((value, index) => {
    if (!isNull(value)) last = index
  })
  if (first === -1) return 0
  return values.slice(first, last + 1).filter(isNull).length
}

/** A table `[data-origin-table=origin]` of the Locks page as a pid set. */
async function uiPids(page: Page, origin: 'application' | 'pglens'): Promise<number[]> {
  const table = page.locator(`[data-origin-table="${origin}"] table`)
  if ((await table.count()) === 0) return []
  const cells = await table.first().evaluate((node) => {
    const headers = Array.from(node.querySelectorAll('thead th')).map((cell) =>
      (cell.textContent ?? '').trim(),
    )
    const index = headers.indexOf('PID')
    return Array.from(node.querySelectorAll('tbody > tr'))
      .filter((row) => row.querySelectorAll('td, th').length >= headers.length)
      .map((row) => {
        const all = Array.from(row.querySelectorAll('td, th'))
        return (all[all.length - headers.length + index]?.textContent ?? '').trim()
      })
  })
  return cells.map((text) => Number(/\d+/.exec(text)?.[0] ?? Number.NaN)).sort((a, b) => a - b)
}

/** Browser problems of the whole test must be empty, except connection noise inside the outage window. */
function onlyOutageNoise(problems: readonly string[], from: number, to: number): void {
  const unexpected = problems.filter(
    (problem, index) =>
      problem.startsWith('pageerror:') ||
      index < from ||
      index >= to ||
      !OUTAGE_NOISE.test(problem),
  )
  expect(unexpected, 'browser problems other than the expected connection failures').toEqual([])
}

/* --- pglens API / PostgreSQL ground truth ------------------------------------------ */

async function commitSeries(fromMs: number, toMs: number): Promise<api.SeriesPoint[]> {
  const params = new URLSearchParams({
    metric: 'pg_xact_commit_total',
    instance_id: await instanceId(),
    database: database(),
    from: new Date(fromMs).toISOString(),
    to: new Date(toMs).toISOString(),
    step: '60s',
  })
  const body = await api.get<{ series?: api.SeriesPoint[] }>(
    `/api/v1/metrics/query?${params.toString()}`,
  )
  return body.series ?? []
}

const observed = (points: readonly api.SeriesPoint[]) => points.filter((p) => p.value !== null)

interface Identity {
  instances: string[]
  agents: number
  agentStartedAt: string
}

/** The inventory rows (id, first_seen) and the live agents: duplicates and resets show here. */
async function identity(): Promise<Identity> {
  const rows = await pglensSql(
    `SELECT instance_id::text || ' ' || first_seen::text FROM instances ORDER BY instance_id;`,
  )
  const agents = await pglensSql(`SELECT count(*) FROM agents WHERE revoked_at IS NULL;`)
  const started = await pglensSql(
    `SELECT coalesce(max(agent_started_at)::text, '') FROM instances;`,
  )
  return {
    instances: rows === '' ? [] : rows.split('\n'),
    agents: Number(agents),
    agentStartedAt: started,
  }
}

async function metricsRows(): Promise<{ count: number; oldest: string }> {
  const out = await pglensSql(`SELECT count(*) || '|' || coalesce(min(ts)::text, '') FROM metrics;`)
  const [count = '0', oldest = ''] = out.split('|')
  return { count: Number(count), oldest }
}

const ODOO_TABLES = ['res_partner', 'product_template', 'sale_order', 'crm_lead', 'mail_message']
const ODOO_MODELS = ['res.partner', 'product.template', 'sale.order', 'crm.lead', 'mail.message']

async function odooSqlCounts(): Promise<number[]> {
  const out = await query(
    ODOO_TABLES.map((table) => `SELECT count(*) FROM ${table}`).join(' UNION ALL ') + ';',
  )
  return out.split('\n').map(Number)
}

/** Same rows through Odoo's own ORM (a second source; archived records included). */
async function odooRpcCounts(): Promise<number[]> {
  const session = await login()
  const counts: number[] = []
  for (const model of ODOO_MODELS) {
    counts.push(
      await session.call<number>(model, 'search_count', [[]], {
        context: { active_test: false },
      }),
    )
  }
  return counts
}

/** Name and creation time of every volume of the compose project. */
async function volumes(): Promise<string[]> {
  const { stdout } = await run('docker', [
    'volume',
    'ls',
    '--filter',
    `label=com.docker.compose.project=${COMPOSE_PROJECT}`,
    '--format',
    '{{.Name}}',
  ])
  const names = stdout.split('\n').filter((line) => line.trim() !== '')
  expect(names.length, 'the project has volumes').toBeGreaterThan(0)
  const { stdout: detail } = await run('docker', [
    'volume',
    'inspect',
    '--format',
    '{{.Name}} {{.CreatedAt}}',
    ...names,
  ])
  return detail
    .split('\n')
    .filter((line) => line.trim() !== '')
    .sort()
}

async function postmasterStart(): Promise<string> {
  return query(`SELECT pg_postmaster_start_time()::text;`)
}

/** Polls until every probe in `urls` stays unreachable for `ms` (a hold with a deadline, not a sleep). */
async function holdDown(ms: number, urls: string[]): Promise<void> {
  const end = Date.now() + ms
  for (;;) {
    for (const url of urls) {
      expect(await probe(url), `${url} must stay down`).toBeNull()
    }
    if (Date.now() >= end) return
    await sleep(Math.min(5_000, Math.max(0, end - Date.now())))
  }
}

/**
 * The downtime window [fromMs, toMs] shows as a gap: the API lists every bucket
 * fully inside it as null (never 0), data exists before it, and data resumes after it.
 */
async function expectGapInApi(fromMs: number, toMs: number, what: string): Promise<void> {
  await until(`${what}: data resumes after the gap`, BUDGET.databaseStats, async () => {
    const points = await commitSeries(fromMs - 10 * STEP_MS, Date.now())
    const before = observed(points).filter((p) => Date.parse(p.ts) + STEP_MS <= fromMs)
    const after = observed(points).filter((p) => Date.parse(p.ts) >= toMs)
    const problems: string[] = []
    if (before.length === 0) problems.push('no observed bucket before the downtime')
    if (after.length === 0) problems.push('no observed bucket after the downtime yet')
    return problems
  })
  const points = await commitSeries(fromMs - 10 * STEP_MS, Date.now())
  const inside = points.filter(
    (p) => Date.parse(p.ts) >= fromMs && Date.parse(p.ts) + STEP_MS <= toMs,
  )
  expect(inside.length, `${what}: a bucket lies entirely inside the downtime`).toBeGreaterThan(0)
  expect(
    inside.filter((p) => p.value !== null),
    `${what}: buckets inside the downtime must be null, not zero or a value`,
  ).toEqual([])
}

/** The same gap read from the console: the TPS chart table has an interior null cell and no new zero. */
async function expectGapInUi(
  page: Page,
  zerosBefore: number | undefined,
  what: string,
): Promise<void> {
  await openInstance(page)
  await expect
    .poll(async () => internalGaps(await commitCells(page)), {
      timeout: BUDGET.databaseStats,
      message: `${what}: the TPS chart shows the downtime as an interior gap`,
    })
    .toBeGreaterThan(0)
  const cells = await commitCells(page)
  if (zerosBefore !== undefined) {
    expect(zeroCells(cells), `${what}: the gap is not drawn as zero`).toBeLessThanOrEqual(
      zerosBefore,
    )
  }
}

/* ====================================================================================
 * LIFE-SERVER-RESTART
 * ==================================================================================== */
test('LIFE-SERVER-RESTART the console shows the connection error, recovers without a manual reload, no rejection, TPS gap not zero @lifecycle @destructive', async ({
  signedInPage: page,
}) => {
  // recovery <= 60 s + 1 server stop/start + 60 s = 120_000 + 60_000 + 60_000
  test.setTimeout(qaTimeout([60_000], 1))
  const guard = consoleGuard(page)

  await openInstance(page)
  await expect(connection(page, 'reachable')).toBeVisible()
  await expect
    .poll(async () => completenessOf(await commitCells(page)).nonNull, { timeout: 30_000 })
    .toBeGreaterThan(0)
  const cellsBefore = await commitCells(page)
  const zerosBefore = zeroCells(cellsBefore)
  const apiBefore = await commitSeries(Date.now() - 30 * 60_000, Date.now())
  expect(observed(apiBefore).length, 'API has TPS data before the restart').toBeGreaterThan(0)
  const lastBefore = Math.max(...observed(apiBefore).map((p) => Date.parse(p.ts)))

  // A plain `restart` may be over before the 15 s poll runs: stop, wait for the
  // console to notice, start. It is one server restart for the timeout.
  const windowFrom = guard.problems.length
  const stoppedAt = Date.now()
  await stop(SERVER)
  await expect(connection(page, 'not reachable')).toBeVisible({ timeout: 40_000 })
  expect(await probe(`${baseUrl()}/readyz`), 'the server really is down').toBeNull()
  await start(SERVER)
  const backAt = Date.now()

  // No manual reload: the console either reconnects, or (sessions are kept in
  // memory and end with the process) is sent to the sign-in page by itself.
  await expect(signInHeading(page).or(connection(page, 'reachable'))).toBeVisible({
    timeout: 60_000,
  })
  const recoveredMs = Date.now() - backAt
  const signedOut = await signInHeading(page).isVisible()
  if (signedOut) await submitPassword(page)
  await expect(tpsRegion(page)).toBeVisible({ timeout: 30_000 })
  await expect(connection(page, 'reachable')).toBeVisible({ timeout: 30_000 })
  const windowTo = guard.problems.length
  test.info().annotations.push({
    type: 'LIFE-SERVER-RESTART',
    description: `outage ${backAt - stoppedAt} ms, console recovered ${recoveredMs} ms after start, session lost: ${signedOut}`,
  })

  // Data after the restart: the API gets a newer observed bucket, the chart has values again.
  await until('TPS data after the restart (API)', BUDGET.databaseStats, async () => {
    const points = observed(await commitSeries(stoppedAt - 30 * 60_000, Date.now()))
    const last = Math.max(0, ...points.map((p) => Date.parse(p.ts)))
    return last > lastBefore ? [] : [`newest observed bucket ${new Date(last).toISOString()}`]
  })
  const apiAfter = await commitSeries(stoppedAt - 30 * 60_000, Date.now())
  const inOutage = apiAfter.filter(
    (p) => Date.parse(p.ts) + STEP_MS > stoppedAt && Date.parse(p.ts) < backAt,
  )
  expect(
    inOutage.filter((p) => p.value === 0),
    'no bucket touching the outage is drawn as zero',
  ).toEqual([])
  test.info().annotations.push({
    type: 'LIFE-SERVER-RESTART gap',
    description: `${inOutage.filter((p) => p.value === null).length} null bucket(s) of ${inOutage.length} touching the outage`,
  })
  await expect
    .poll(async () => completenessOf(await commitCells(page)).nonNull, { timeout: 30_000 })
    .toBeGreaterThan(0)
  expect(zeroCells(await commitCells(page)), 'the chart gained no zero bucket').toBeLessThanOrEqual(
    zerosBefore,
  )

  onlyOutageNoise(guard.problems, windowFrom, windowTo)
})

/* ====================================================================================
 * LIFE-AGENT-RESTART
 * ==================================================================================== */
interface CoverageCheck {
  check: string
  interval_seconds: number
  state: 'reported' | 'pending' | 'failed'
  first_result_at: string | null
  expected_by: string | null
}
interface Coverage {
  agent_started_at: string | null
  generated_at: string
  checks: CoverageCheck[]
  reported: number
  total: number
}
const coverage = async () =>
  api.get<Coverage>(`/api/v1/instances/${enc(await instanceId())}/coverage`)

/** D22a first-run caps: interval <= 1 m -> 30 s, <= 15 m -> 120 s, longer -> 600 s. */
const warmupCapMs = (intervalSeconds: number) =>
  (intervalSeconds <= 60 ? 30 : intervalSeconds <= 900 ? 120 : 600) * 1_000

function capProblems(report: Coverage): string[] {
  if (report.agent_started_at === null) return ['agent_started_at is null']
  const started = Date.parse(report.agent_started_at)
  const problems: string[] = []
  for (const check of report.checks) {
    const limit = warmupCapMs(check.interval_seconds) + WARMUP_SLACK_MS
    if (check.first_result_at !== null) {
      const took = Date.parse(check.first_result_at) - started
      if (took > limit)
        problems.push(`${check.check}: first result ${took} ms after start, cap ${limit}`)
    }
    if (check.expected_by !== null) {
      const due = Date.parse(check.expected_by) - started
      if (due > limit)
        problems.push(`${check.check}: expected_by ${due} ms after start, cap ${limit}`)
    }
  }
  return problems
}

test('LIFE-AGENT-RESTART the coverage banner appears and completes, no new instance, first_seen unchanged, warm-up caps held @lifecycle @destructive', async ({
  signedInPage: page,
}) => {
  // coverage complete <= 900 s (600 s cap + runs) + 1 agent restart + 60 s = 120_000 + 900_000 + 60_000
  test.setTimeout(qaTimeout([900_000], 1))
  const guard = consoleGuard(page)

  const idBefore = await identity()
  expect(idBefore.instances.length, 'one monitored instance').toBeGreaterThan(0)
  const apiIdsBefore = (await api.instances()).map((row) => row.instance_id).sort()
  const startedBefore = (await coverage()).agent_started_at
  expect(startedBefore, 'the agent reports its start').not.toBeNull()

  await restart(AGENT)
  await waitReady(BUDGET.serviceRestart)
  await until('the server sees the new agent start', 60_000, async () => {
    const started = (await coverage()).agent_started_at
    return started !== null && Date.parse(started) > Date.parse(startedBefore ?? '')
      ? []
      : [`agent_started_at ${started}`]
  })

  await page.goto(`/instances/${enc(await instanceId())}`)
  const banner = page.getByTestId('coverage-banner')
  await expect(banner).toBeVisible({ timeout: 30_000 })
  // UI and API agree: the banner count lies between two API reads around it.
  await until(
    'banner count matches the API',
    60_000,
    async () => {
      const first = await coverage()
      const text = (await banner.textContent({ timeout: 5_000 })) ?? ''
      const second = await coverage()
      const match = /(\d+)\s*(?:\/|of)\s*(\d+)\s+checks/.exec(text)
      if (!match) return [`no count in "${text}"`]
      const shown = Number(match[1])
      const total = Number(match[2])
      return total === second.total && shown >= first.reported && shown <= second.reported
        ? []
        : [`banner ${shown}/${total}, API ${first.reported}..${second.reported}/${second.total}`]
    },
    2_000,
  )
  expect(capProblems(await coverage()), 'warm-up caps while collecting').toEqual([])

  await until(
    'coverage complete',
    900_000,
    async () => {
      const report = await coverage()
      return report.reported === report.total
        ? []
        : [
            `${report.reported}/${report.total}, pending: ${report.checks
              .filter((c) => c.state !== 'reported')
              .map((c) => `${c.check}=${c.state}`)
              .join(',')}`,
          ]
    },
    10_000,
  )
  const done = await coverage()
  expect(
    done.checks.filter((check) => check.state === 'failed'),
    'no failed check',
  ).toEqual([])
  expect(capProblems(done), 'warm-up caps at completion (D22)').toEqual([])
  await expect(banner).toHaveCount(0, { timeout: 60_000 })

  const idAfter = await identity()
  expect(idAfter.instances, 'same instance rows, same first_seen (PostgreSQL)').toEqual(
    idBefore.instances,
  )
  expect(idAfter.agents, 'no duplicate agent').toBe(idBefore.agents)
  expect(
    Date.parse(idAfter.agentStartedAt.replace(' ', 'T')),
    'agent_started_at moved forward',
  ).toBeGreaterThan(Date.parse(idBefore.agentStartedAt.replace(' ', 'T')))
  expect(
    (await api.instances()).map((row) => row.instance_id).sort(),
    'same instances (API)',
  ).toEqual(apiIdsBefore)
  guard.assertClean()
})

/* ====================================================================================
 * LIFE-REBUILD
 * ==================================================================================== */
async function runningImage(service: string): Promise<{ image: string; created: string }> {
  const { stdout } = await run('docker', [...composeArgs(), 'ps', '-q', service])
  const cid = stdout.trim().split('\n')[0] ?? ''
  expect(cid, `${service} has a container`).not.toBe('')
  const { stdout: info } = await run('docker', [
    'inspect',
    '--format',
    '{{.Image}} {{.Created}}',
    cid,
  ])
  const [image = '', created = ''] = info.trim().split(' ')
  return { image, created }
}

async function localImage(reference: string): Promise<string> {
  const { stdout } = await run('docker', ['image', 'inspect', '--format', '{{.Id}}', reference])
  return stdout.trim()
}

const sha256 = (text: string) => createHash('sha256').update(text).digest('hex')

async function servedIndex(): Promise<string> {
  const response = await fetch(`${baseUrl()}/`, { signal: AbortSignal.timeout(10_000) })
  expect(response.status).toBe(200)
  return response.text()
}

test('LIFE-REBUILD the new build is served, migrations applied, history intact, login works, no console errors @lifecycle @destructive', async ({
  signedInPage: page,
  browser,
  baseURL,
}) => {
  // 1 deploy.sh run + 2 service restarts (server, agent) + 60 s = BUDGET.rebuild + 2 * 120_000 + 60_000
  test.setTimeout(qaTimeout([BUDGET.rebuild], 2))
  const repo = process.env.PGLENS_REPO ?? resolve(process.cwd(), '..')
  const migrationsDir = join(repo, 'internal', 'store', 'migrations')
  const distIndex = join(repo, 'internal', 'webui', 'dist', 'index.html')
  const migrationNumbers = existsSync(migrationsDir)
    ? readdirSync(migrationsDir)
        .map((name) => /^(\d+)_.*\.sql$/.exec(name)?.[1])
        .filter((value): value is string => value !== undefined)
        .map(Number)
    : []
  expect(migrationNumbers.length, `migrations found in ${migrationsDir}`).toBeGreaterThan(0)
  const treeIndexHash = existsSync(distIndex) ? sha256(readFileSync(distIndex, 'utf8')) : ''

  // Before.
  const startedAt = Date.now()
  const servedBefore = sha256(await servedIndex())
  const historyBefore = await metricsRows()
  const migrationBefore = Number(await pglensSql(`SELECT max(version) FROM schema_migrations;`))
  const idBefore = await identity()
  const windowEnd = Date.now() - 2 * STEP_MS
  const seriesBefore = observed(await commitSeries(windowEnd - 60 * 60_000, windowEnd))
  expect(seriesBefore.length, 'API history before the rebuild').toBeGreaterThan(0)
  const imagesBefore = {
    server: await runningImage(SERVER),
    agent: await runningImage(AGENT),
  }

  await rebuild()
  await waitReady(BUDGET.serviceRestart)

  // The running containers are the freshly built :dev images, recreated by the drill.
  const refs = {
    server: 'ghcr.io/manprint/pglens-server:dev',
    agent: 'ghcr.io/manprint/pglens-agent:dev',
  }
  for (const service of ['server', 'agent'] as const) {
    const running = await runningImage(service === 'server' ? SERVER : AGENT)
    expect(running.image, `${service}: the container runs the :dev image just built`).toBe(
      await localImage(refs[service]),
    )
    expect(Date.parse(running.created), `${service}: container recreated`).toBeGreaterThan(
      startedAt - 5_000,
    )
    test.info().annotations.push({
      type: `LIFE-REBUILD ${service} image`,
      description: `${imagesBefore[service].image === running.image ? 'identical to' : 'changed from'} the previous image (${running.image.slice(7, 19)})`,
    })
  }
  const servedAfter = sha256(await servedIndex())
  expect(
    servedAfter === treeIndexHash || servedAfter !== servedBefore,
    'the console index is the tree build or differs from the previous one',
  ).toBe(true)
  const asset = /src="(\/assets\/index-[^"]+\.js)"/.exec(await servedIndex())?.[1]
  expect(asset, 'the index references its script').toBeDefined()
  expect(await probe(`${baseUrl()}${asset ?? ''}`), 'the referenced script is served').toBe(200)

  // Migrations applied: the newest version in the database is the newest file of the tree.
  const migrationAfter = Number(await pglensSql(`SELECT max(version) FROM schema_migrations;`))
  expect(migrationAfter, 'schema version (PostgreSQL)').toBe(Math.max(...migrationNumbers))
  expect(migrationAfter).toBeGreaterThanOrEqual(migrationBefore)
  expect(await probe(`${baseUrl()}/readyz`), '/readyz (migrations applied)').toBe(200)

  // History intact: counted in PostgreSQL and read back through the API.
  const historyAfter = await metricsRows()
  expect(historyAfter.count, 'metrics rows before <= after').toBeGreaterThanOrEqual(
    historyBefore.count,
  )
  expect(
    Date.parse(historyAfter.oldest.replace(' ', 'T')),
    'oldest sample kept',
  ).toBeLessThanOrEqual(Date.parse(historyBefore.oldest.replace(' ', 'T')))
  const seriesAfter = observed(await commitSeries(windowEnd - 60 * 60_000, windowEnd))
  expect(seriesAfter.length, 'API history before <= after').toBeGreaterThanOrEqual(
    seriesBefore.length,
  )
  const idAfter = await identity()
  expect(idAfter.instances, 'same inventory').toEqual(idBefore.instances)
  expect(idAfter.agents).toBe(idBefore.agents)

  // Sessions and sign-in: the old session ended with the old server process, a new sign-in works.
  const old = await page.request.get('/api/v1/session')
  expect(
    ((await old.json()) as { authenticated?: boolean }).authenticated,
    'the previous console session is gone (in-memory sessions)',
  ).toBe(false)
  const context = await browser.newContext({
    ...(baseURL ? { baseURL } : {}),
    locale: 'en-US',
    timezoneId: 'UTC',
  })
  try {
    const fresh = await context.newPage()
    await fresh.goto('/login')
    await submitPassword(fresh)
    const guard = consoleGuard(fresh)
    await openInstance(fresh)
    await expect
      .poll(async () => completenessOf(await commitCells(fresh)).nonNull, { timeout: 30_000 })
      .toBeGreaterThan(0)
    await fresh.goto(`/instances/${enc(await instanceId())}/locks`)
    await expect(fresh.getByRole('main')).toBeVisible()
    await fresh.waitForLoadState('networkidle')
    guard.assertClean()
  } finally {
    await context.close()
  }
})

/* ====================================================================================
 * LIFE-DOWN-UP
 * ==================================================================================== */
test('LIFE-DOWN-UP everything returns, volumes intact, Odoo reachable, downtime shown as a gap @lifecycle @destructive', async ({
  signedInPage: page,
}) => {
  // waitReady < 180 s + down and up (1 restart allowance) + 60 s = 180_000 + 120_000 + 60_000
  test.setTimeout(qaTimeout([180_000], 1))

  await openInstance(page)
  const zerosBefore = zeroCells(await commitCells(page))
  const volumesBefore = await volumes()
  const idBefore = await identity()
  const historyBefore = await metricsRows()
  const odooSqlBefore = await odooSqlCounts()
  const odooRpcBefore = await odooRpcCounts()
  const pgStartBefore = await postmasterStart()
  await waitOdoo(30_000)

  await down()
  const downAt = Date.now()
  const rows = await stackStatus()
  expect(
    rows.filter((row) => (LIFECYCLE_SERVICES as readonly string[]).includes(row.service)),
    'down() removed odoo, pglens-server, pglens-agent and timescaledb',
  ).toEqual([])
  expect(
    rows.some((row) => row.service === 'odoo-db' && row.state === 'running'),
    'odoo-db stays up',
  ).toBe(true)
  await holdDown(HOLD_DOWN_MS, [`${baseUrl()}/readyz`, `${odooUrl()}/web/login`])

  await up()
  const upAt = Date.now()
  await waitReady(180_000)
  await waitOdoo(BUDGET.serviceRestart)
  const readyMs = Date.now() - upAt

  expect(await volumes(), 'volumes intact (names and creation times)').toEqual(volumesBefore)
  expect(await postmasterStart(), 'odoo-db was never restarted').toBe(pgStartBefore)
  expect(await odooSqlCounts(), 'Odoo row counts (PostgreSQL)').toEqual(odooSqlBefore)
  expect(await odooRpcCounts(), 'Odoo row counts (ORM)').toEqual(odooRpcBefore)
  expect(await probe(`${odooUrl()}/web/login`), 'Odoo reachable on 9091').toBe(200)
  const idAfter = await identity()
  expect(idAfter.instances, 'same instance rows and first_seen').toEqual(idBefore.instances)
  expect(idAfter.agents, 'no new agent').toBe(idBefore.agents)
  const historyAfter = await metricsRows()
  expect(historyAfter.count, 'metrics history kept').toBeGreaterThanOrEqual(historyBefore.count)
  expect(historyAfter.oldest).toBe(historyBefore.oldest)

  await expectGapInApi(downAt, upAt, 'LIFE-DOWN-UP')
  await expectGapInUi(page, zerosBefore, 'LIFE-DOWN-UP')
  const guard = consoleGuard(page)
  await page.reload()
  await expect(tpsRegion(page)).toBeVisible({ timeout: 30_000 })
  guard.assertClean()
  test.info().annotations.push({
    type: 'LIFE-DOWN-UP',
    description: `downtime ${upAt - downAt} ms, ready ${readyMs} ms after up`,
  })
})

/* ====================================================================================
 * LIFE-DOWN-UP-FULL
 * ==================================================================================== */
test('LIFE-DOWN-UP-FULL PostgreSQL restarts cleanly, Odoo data intact, agent resumes without a new instance, gap shown @lifecycle @destructive', async ({
  signedInPage: page,
}) => {
  // waitReady < 180 s + down and up (2 restart allowances) + 60 s = 180_000 + 2 * 120_000 + 60_000
  test.setTimeout(qaTimeout([180_000], 2))

  await openInstance(page)
  const zerosBefore = zeroCells(await commitCells(page))
  const volumesBefore = await volumes()
  const idBefore = await identity()
  const historyBefore = await metricsRows()
  const odooSqlBefore = await odooSqlCounts()
  const odooRpcBefore = await odooRpcCounts()
  const pgStartBefore = await postmasterStart()
  await waitOdoo(30_000)

  // `docker compose down` of every service. Never -v / --volumes / --rmi; a long
  // stop timeout so PostgreSQL shuts down cleanly instead of being killed.
  await assertLifecycleAllowed()
  const downArgs = [...composeArgs(), 'down', '--timeout', '60']
  expect(downArgs.some((arg) => /^(-v|--volumes|--rmi)/.test(arg))).toBe(false)
  await run('docker', downArgs, { timeoutMs: 300_000 })
  const downAt = Date.now()
  expect(await stackStatus(), 'no container of the project is left').toEqual([])
  await holdDown(HOLD_DOWN_MS, [`${baseUrl()}/readyz`, `${odooUrl()}/web/login`])

  await upAll()
  const upAt = Date.now()
  await waitReady(180_000)
  await waitOdoo(BUDGET.serviceRestart)

  // PostgreSQL restarted, and cleanly: a new postmaster, no crash recovery, not in recovery.
  expect(await postmasterStart(), 'odoo-db has a new postmaster').not.toBe(pgStartBefore)
  expect(await query(`SELECT pg_is_in_recovery();`), 'PostgreSQL is not in recovery').toBe('f')
  const { stdout, stderr } = await run('docker', ['logs', ODOO_DB_CONTAINER])
  const log = `${stdout}\n${stderr}`
  expect(log, 'PostgreSQL log: no crash recovery').not.toMatch(/not properly shut down/)
  expect(log, 'PostgreSQL log: ready').toMatch(/database system is ready to accept connections/)

  expect(await volumes(), 'volumes intact').toEqual(volumesBefore)
  // The four business tables are exactly equal; mail_message may only grow (Odoo crons post messages).
  const sqlAfter = await odooSqlCounts()
  expect(sqlAfter.slice(0, 4), 'Odoo row counts (PostgreSQL)').toEqual(odooSqlBefore.slice(0, 4))
  expect(sqlAfter[4], 'mail_message never shrinks').toBeGreaterThanOrEqual(odooSqlBefore[4] ?? 0)
  const rpcAfter = await odooRpcCounts()
  expect(rpcAfter.slice(0, 4), 'Odoo row counts (ORM)').toEqual(odooRpcBefore.slice(0, 4))
  expect(rpcAfter[4], 'mail.message never shrinks (ORM)').toBeGreaterThanOrEqual(
    odooRpcBefore[4] ?? 0,
  )

  // pglens reconnected: same instance, same first_seen, same agent, history kept, instance up.
  const idAfter = await identity()
  expect(idAfter.instances, 'no new instance, first_seen unchanged').toEqual(idBefore.instances)
  expect(idAfter.agents, 'no new agent').toBe(idBefore.agents)
  const instances = await api.instances()
  expect(instances.length, 'API lists the same number of instances').toBe(idBefore.instances.length)
  expect(
    instances.every((row) => row.up === true),
    'every instance is up',
  ).toBe(true)
  expect((await metricsRows()).count, 'metrics history kept').toBeGreaterThanOrEqual(
    historyBefore.count,
  )

  await expectGapInApi(downAt, upAt, 'LIFE-DOWN-UP-FULL')
  await expectGapInUi(page, zerosBefore, 'LIFE-DOWN-UP-FULL')
  test.info().annotations.push({
    type: 'LIFE-DOWN-UP-FULL',
    description: `downtime ${upAt - downAt} ms`,
  })
})

/* ====================================================================================
 * LIFE-DB-OUTAGE-VIEW
 * ==================================================================================== */
test('LIFE-DB-OUTAGE-VIEW the console shows error states and never a blank page while timescaledb is down, recovers on start @lifecycle @destructive', async ({
  signedInPage: page,
}) => {
  // 30 s outage + 1 service stop/start + 60 s = 30_000 + 120_000 + 60_000
  test.setTimeout(qaTimeout([30_000], 1))
  const guard = consoleGuard(page)

  await openInstance(page)
  await expect(connection(page, 'reachable')).toBeVisible()
  const windowFrom = guard.problems.length

  await stop(TSDB)
  const stoppedAt = Date.now()
  let sawError = false
  await until(
    'the outage is visible and held for 30 s',
    60_000,
    async () => {
      const problems: string[] = []
      // API: the server answers 503 (not ready) instead of hanging or crashing.
      const ready = await probe(`${baseUrl()}/readyz`)
      if (ready !== 503) problems.push(`/readyz answered ${ready}`)
      const listing = await probe(`${baseUrl()}/api/v1/instances`, {
        headers: { Authorization: `Bearer ${process.env.PGLENS_BOOTSTRAP_TOKEN ?? ''}` },
      })
      if (listing === null || listing < 500) problems.push(`/api/v1/instances answered ${listing}`)
      // Console: never blank; the connection badge turns to the error state.
      const text = (await page.getByRole('main').first().innerText()).trim()
      expect(text.length, 'the main region is not blank during the outage').toBeGreaterThan(0)
      if (await connection(page, 'not reachable').isVisible()) sawError = true
      else problems.push('connection badge not yet in the error state')
      if (Date.now() - stoppedAt < 30_000) problems.push('outage shorter than 30 s')
      return problems
    },
    3_000,
  )
  expect(sawError).toBe(true)

  await start(TSDB)
  await waitReady(BUDGET.serviceRestart)
  expect(await probe(`${baseUrl()}/readyz`), 'server ready again').toBe(200)
  await expect(connection(page, 'reachable')).toBeVisible({ timeout: 45_000 })
  const windowTo = guard.problems.length
  await expect(page.getByRole('main').first()).not.toBeEmpty()
  expect((await api.instances()).length, 'API answers again').toBeGreaterThan(0)

  onlyOutageNoise(guard.problems, windowFrom, windowTo)
})

/* ====================================================================================
 * LIFE-ODOO-RESTART
 * ==================================================================================== */
interface LocksSample {
  application: number[]
  pglens: number[]
}

async function locksSample(): Promise<LocksSample> {
  const body = await api.get<ApiLocks>(`/api/v1/locks?instance_id=${enc(await instanceId())}`)
  const pids = (origin: string) =>
    (body.sessions ?? [])
      .filter((row) => row.origin === origin)
      .map((row) => Number(row.pid))
      .sort((a, b) => a - b)
  return { application: pids('application'), pglens: pids('pglens') }
}

async function truthPids(): Promise<LocksSample> {
  const rows = await truthSessions()
  const of = (origin: 'application' | 'pglens') =>
    rows
      .filter(
        (row) =>
          originOf(row.application_name, row.usename) === origin &&
          !row.application_name.startsWith('qa-'),
      )
      .map((row) => row.pid)
      .sort((a, b) => a - b)
  return { application: of('application'), pglens: of('pglens') }
}

test('LIFE-ODOO-RESTART application sessions drop and return in the Application sessions table, pglens table unchanged @lifecycle @destructive', async ({
  signedInPage: page,
}) => {
  // drop <= 45 s, return <= 45 s + 1 Odoo restart + 60 s = 120_000 + 2 * 45_000 + 60_000
  test.setTimeout(qaTimeout([BUDGET.activity, BUDGET.activity], 1))
  const guard = consoleGuard(page)

  await waitOdoo(30_000)
  await page.goto(`/instances/${enc(await instanceId())}/locks?view=application`)
  await expect(page.locator('[data-origin-table="application"]')).toBeVisible({ timeout: 30_000 })
  const before = await truthPids()
  expect(before.application.length, 'Odoo holds application sessions (PostgreSQL)').toBeGreaterThan(
    0,
  )
  expect(before.pglens.length, 'pglens holds monitoring sessions').toBeGreaterThan(0)
  await until('console and API list the Odoo sessions', 20_000, async () => {
    const sample = await locksSample()
    const shown = await uiPids(page, 'application')
    const missing = before.application.filter((pid) => !sample.application.includes(pid))
    return missing.length === 0 && shown.length > 0
      ? []
      : [
          `PostgreSQL ${ids(before.application)} API ${ids(sample.application)} console ${ids(shown)}`,
        ]
  })
  const oldPids = before.application

  await restart(ODOO)

  // Drop: no old backend is left in PostgreSQL, in the API sample or in the table.
  await until('the old application sessions are gone', BUDGET.activity, async () => {
    const truth = await truthPids()
    const sample = await locksSample()
    const shown = await uiPids(page, 'application')
    const left = (list: number[]) => list.filter((pid) => oldPids.includes(pid))
    return [
      ...(left(truth.application).length > 0
        ? [`PostgreSQL still has ${ids(left(truth.application))}`]
        : []),
      ...(left(sample.application).length > 0
        ? [`API still has ${ids(left(sample.application))}`]
        : []),
      ...(left(shown).length > 0 ? [`console still lists ${ids(left(shown))}`] : []),
    ]
  })

  // Return: Odoo answers a request again, then new sessions appear in all three places.
  await waitOdoo(BUDGET.serviceRestart)
  await until('new application sessions are listed', BUDGET.activity, async () => {
    const truth = await truthPids()
    const sample = await locksSample()
    const shown = await uiPids(page, 'application')
    const fresh = truth.application.filter((pid) => !oldPids.includes(pid))
    if (fresh.length === 0) return ['PostgreSQL has no new application session']
    const absentApi = fresh.filter((pid) => !sample.application.includes(pid))
    const absentUi = fresh.filter((pid) => !shown.includes(pid))
    return [
      ...(absentApi.length > 0 ? [`API lacks ${ids(absentApi)}`] : []),
      ...(absentUi.length > 0 ? [`console lacks ${ids(absentUi)}`] : []),
    ]
  })

  // The pglens table is untouched by the Odoo restart (same backends, same rows).
  await page.goto(`/instances/${enc(await instanceId())}/locks?view=pglens`)
  await expect(page.locator('[data-origin-table="pglens"]')).toBeVisible({ timeout: 30_000 })
  await until('the pglens sessions are unchanged', 20_000, async () => {
    const truth = await truthPids()
    const sample = await locksSample()
    const shown = await uiPids(page, 'pglens')
    const same = (list: number[]) => list.join(',') === before.pglens.join(',')
    return same(truth.pglens) && same(sample.pglens) && same(shown)
      ? []
      : [
          `before ${ids(before.pglens)} PostgreSQL ${ids(truth.pglens)} API ${ids(sample.pglens)} console ${ids(shown)}`,
        ]
  })
  guard.assertClean()
})
