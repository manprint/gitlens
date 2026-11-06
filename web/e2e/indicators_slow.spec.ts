import { execFile } from 'node:child_process'
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { promisify } from 'node:util'

import { expect, type Page } from '@playwright/test'

import { formatBytes } from '../src/lib/format/bytes'
import { test } from './fixtures'
import * as api from './qa/api'
import { BUDGET, qaTimeout } from './qa/budget'
import { assertLifecycleAllowed, assertStimulusAllowed, sqlIdent } from './qa/guard'
import { chartTable, consoleGuard } from './qa/observe'
import {
  database,
  deadRows,
  query,
  queryInt,
  QA_SCHEMA,
  registerCleanup,
  runCleanups,
  truth,
} from './qa/pg'
import { newRunId } from './qa/runid'
import { restart, stackStatus, start, stop, waitReady } from './qa/stack'

/**
 * Unit 6.4 (phase 005): stimulus -> indicator proof for the Indexes page
 * (REQ-04 suggestions, REQ-05 unused/duplicate indexes, REQ-06 dead rows, REQ-07
 * index history and tax), the database sizes (REQ-33/REQ-35 readiness of the
 * hourly/first-run checks), the advisor findings search (REQ-31), the
 * notifications (REQ-30) and the live proof itself (REQ-41).
 *
 * Own file (not `indicators.spec.ts`) because another worker writes that one.
 * Every test is three-way: UI <-> pglens API <-> PostgreSQL (qa/pg.ts), has a
 * CONTROL (no stimulus, the indicator stays within noise), polls with
 * `expect.poll` and a deadline taken from BUDGET (no fixed sleeps), and removes
 * its objects in finalizers (`registerCleanup`; `runCleanups` also drops
 * `qa_tmp`). `PGLENS_QA_SABOTAGE=1` skips the stimulus: every test must then fail.
 * Tests that restart or stop the agent need `assertLifecycleAllowed()`.
 *
 * Deviations from the contract (reported to the lead): IND-NOTIF is two tests
 * (state, delivery); IND-IDXUSE-SLOW keeps its index in `qa_slow` (not `qa_tmp`,
 * which `runCleanups` drops after every test) and a killed run leaves it on
 * purpose, see the test; the `notifications` rows are read from the pglens
 * TimescaleDB (there is no notifications API).
 */
test.describe.configure({ retries: 0 })

const AGENT = 'pglens-agent'
const SABOTAGE = process.env.PGLENS_QA_SABOTAGE === '1'
const MIB = 1024 * 1024
const TS_CONTAINER = 'pglens-odoo-timescaledb-1'
const execFileAsync = promisify(execFile)

test.afterEach(runCleanups)

const message = (error: unknown) => (error instanceof Error ? error.message : String(error))
const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms))
const runTag = () => newRunId().replace(/-/g, '_')

let instancePromise: Promise<string> | undefined
const instanceId = () =>
  (instancePromise ??= api.instances().then((rows) => {
    const row = rows.find((candidate) => candidate.role === 'primary') ?? rows[0]
    if (!row) throw new Error('indicators: pglens knows no instance')
    return row.instance_id
  }))
const inst = async (path: string) => `/api/v1/instances/${await instanceId()}${path}`
const uiPath = async (tab: string, extra = '') =>
  `/indexes?instance=${await instanceId()}&tab=${tab}&db=${database()}${extra}`

/** Polls `check` (empty array = satisfied); a throwing check counts as a problem and is retried. */
async function until(
  label: string,
  check: () => Promise<string[]>,
  timeoutMs: number,
  intervals: number[] = [3_000, 5_000, 10_000],
): Promise<void> {
  await expect
    .poll(
      async () => {
        try {
          return await check()
        } catch (error) {
          return [`error: ${message(error)}`]
        }
      },
      { timeout: Math.max(1_000, timeoutMs), intervals, message: label },
    )
    .toEqual([])
}

async function restartAgent(): Promise<number> {
  await assertLifecycleAllowed()
  const at = Date.now()
  await restart(AGENT)
  const deadline = Date.now() + BUDGET.serviceRestart
  for (;;) {
    const rows = await stackStatus()
    if (rows.some((row) => row.service === AGENT && row.health === 'healthy')) break
    if (Date.now() > deadline) throw new Error('the agent is not healthy after its restart')
    await sleep(2_000)
  }
  await waitReady(BUDGET.serviceRestart)
  return at
}

async function startAgentAndWait(): Promise<void> {
  await start(AGENT)
  await waitReady(BUDGET.serviceRestart)
}

interface TableItem {
  datname: string
  schemaname: string
  relname: string
  n_dead_tup: number
  table_bytes: number
  total_bytes: number
}
const tablesApi = async () =>
  (await api.get<{ items: TableItem[] }>(await inst('/tables?limit=500&order_by=dead_tup'))).items
const tableOf = (items: TableItem[], schema: string, rel: string) =>
  items.find(
    (item) => item.datname === database() && item.schemaname === schema && item.relname === rel,
  )

interface Finding {
  rule_id: string
  state: string
  title: string
  object_name?: string
  degraded_reason?: string
  evidence?: Record<string, unknown>
}
async function findingsFor(q: string, rule?: string): Promise<Finding[]> {
  const params = new URLSearchParams({ q, state: 'all', limit: '1000' })
  if (rule) params.set('rule_id', rule)
  return api.get<Finding[]>(`/api/v1/findings?${params.toString()}`)
}
const isActive = (finding: Finding) => finding.state === 'open' || finding.state === 'degraded'
const mentions = (finding: Finding, name: string) =>
  `${finding.title} ${finding.object_name ?? ''}`.includes(name)

type Rec = Record<string, string>
const col = (rec: Rec, header: string) =>
  Object.entries(rec).find(([key]) => key.startsWith(header))?.[1] ?? ''
const count = (text: string) => Number((/^[\d,]+/.exec(text)?.[0] ?? '').replace(/,/g, ''))

/** Rows of a console table (header-keyed) that mention `term`; [] when the table is not shown. */
async function uiRows(
  page: Page,
  url: string,
  tableName: string,
  searchName: string | null,
  term: string,
): Promise<Rec[]> {
  await page.goto(url)
  if (searchName) {
    const box = page.getByRole('searchbox', { name: searchName, exact: true })
    await box.waitFor({ timeout: 20_000 })
    await box.fill(term)
  }
  const table = page.getByRole('table', { name: tableName, exact: true })
  if (
    !(await table.waitFor({ timeout: 15_000 }).then(
      () => true,
      () => false,
    ))
  )
    return []
  const rows = await table.evaluate((el) => {
    const heads = Array.from(el.querySelectorAll('thead th')).map((h) =>
      (h.textContent ?? '').trim(),
    )
    return Array.from(el.querySelectorAll('tbody tr')).map((tr) => {
      const rec: Record<string, string> = {}
      Array.from(tr.querySelectorAll('th, td')).forEach((cell, i) => {
        rec[heads[i] ?? `#${i}`] = (cell.textContent ?? '').trim()
      })
      return rec
    })
  })
  return rows.filter((rec) => Object.values(rec).join(' ').includes(term))
}

const findingRows = (page: Page, term: string, state?: 'all') =>
  uiRows(
    page,
    `/findings?q=${encodeURIComponent(term)}${state ? `&state=${state}` : ''}`,
    'Advisor findings',
    null,
    term,
  )

async function timescale(sql: string): Promise<string> {
  const { stdout } = await execFileAsync(
    'docker',
    ['exec', TS_CONTAINER, 'psql', '-U', 'pglens', '-d', 'pglens', '-Atc', sql],
    { timeout: 30_000 },
  )
  return stdout.trim()
}

async function ensureQaSchema(): Promise<void> {
  await query(`CREATE SCHEMA IF NOT EXISTS ${QA_SCHEMA};`)
}

// ---------------------------------------------------------------------------
// IND-DEAD: dead rows of a table, then VACUUM
// ---------------------------------------------------------------------------
test('IND-DEAD dead rows follow the table (appear after the updates, vanish after VACUUM) @indicators', async ({
  signedInPage: page,
}) => {
  test.setTimeout(qaTimeout([BUDGET.activity, BUDGET.tables, BUDGET.activity, BUDGET.tables]))
  assertStimulusAllowed()
  const guard = consoleGuard(page)
  await page.setViewportSize({ width: 1920, height: 1080 })
  const ROWS = 20_000
  const NOISE = 1_000
  registerCleanup(() => query(`DROP TABLE IF EXISTS ${QA_SCHEMA}.t;`).then(() => undefined))

  // CONTROL: the other tables' dead rows are the baseline. Odoo accumulates them until
  // autovacuum (res_partner reached 1002 after two days), so only their growth during the
  // test is bounded: <= 1000 per table (measured Odoo noise <= 14 per test, x70 slack).
  const key = (item: { schemaname: string; relname: string }) =>
    `${item.schemaname}.${item.relname}`
  const others = async () => (await tablesApi()).filter((item) => item.schemaname !== QA_SCHEMA)
  const before = new Map((await others()).map((item) => [key(item), item.n_dead_tup]))
  expect(before.size, 'control: the API lists the other tables').toBeGreaterThan(0)
  expect(
    tableOf(await tablesApi(), QA_SCHEMA, 't'),
    'control: qa_tmp.t absent before the stimulus',
  ).toBeUndefined()
  const growth = async () =>
    Math.max(0, ...(await others()).map((item) => item.n_dead_tup - (before.get(key(item)) ?? 0)))

  if (!SABOTAGE) {
    await deadRows(ROWS)
    await expect
      .poll(() => truth.deadTuples('t'), {
        timeout: BUDGET.activity,
        message: 'PostgreSQL counts the dead rows',
      })
      .toBeGreaterThanOrEqual(ROWS * 0.95)
  }

  // Tolerance 5 %: API, UI and pg_stat_user_tables read the same n_dead_tup counter, which
  // stays constant (autovacuum off, no writes); the margin covers the stats flush lag only.
  const near = (value: number, expected: number) => Math.abs(value - expected) <= expected * 0.05
  await until(
    'dead rows appear in the API and the Dead rows tab',
    async () => {
      const expected = SABOTAGE ? ROWS : await truth.deadTuples('t')
      const problems: string[] = []
      const row = tableOf(await tablesApi(), QA_SCHEMA, 't')
      if (!row || !near(row.n_dead_tup, expected))
        problems.push(`api n_dead_tup=${row?.n_dead_tup} expected~${expected}`)
      const rows = await uiRows(
        page,
        await uiPath('dead-rows'),
        'Tables',
        'Search tables',
        QA_SCHEMA,
      )
      const ui = rows[0] ? count(col(rows[0], 'Dead rows')) : NaN
      if (!near(ui, expected)) problems.push(`ui Dead rows=${ui} expected~${expected}`)
      return problems
    },
    BUDGET.tables,
    [15_000],
  )
  const dead0 = tableOf(await tablesApi(), QA_SCHEMA, 't')?.n_dead_tup ?? ROWS
  expect(
    await growth(),
    'the stimulus is specific: the other tables grow within noise',
  ).toBeLessThanOrEqual(NOISE)

  if (!SABOTAGE) {
    await query(`VACUUM ${QA_SCHEMA}.t;`)
    await expect
      .poll(() => truth.deadTuples('t'), { timeout: BUDGET.activity })
      .toBeLessThanOrEqual(dead0 * 0.1)
  }
  // The row may also leave the tab once it has no dead rows: absent counts as cleared.
  // Tolerance: at most 10 % of the dead rows may remain (VACUUM leaves 0; the margin is for a late flush).
  await until(
    'dead rows drop in the API and the Dead rows tab after VACUUM',
    async () => {
      const problems: string[] = []
      const row = tableOf(await tablesApi(), QA_SCHEMA, 't')
      if (row && row.n_dead_tup > dead0 * 0.1) problems.push(`api n_dead_tup=${row.n_dead_tup}`)
      const rows = await uiRows(
        page,
        await uiPath('dead-rows'),
        'Tables',
        'Search tables',
        QA_SCHEMA,
      )
      const ui = rows[0] ? count(col(rows[0], 'Dead rows')) : 0
      if (ui > dead0 * 0.1) problems.push(`ui Dead rows=${ui}`)
      return problems
    },
    BUDGET.tables,
    [15_000],
  )
  guard.assertClean()
})

// ---------------------------------------------------------------------------
// IND-IDXUSE: index scan rate of a real index (history API and chart)
// ---------------------------------------------------------------------------
test('IND-IDXUSE index scans of res_partner_pkey show in the history API and the drawer chart @indicators', async ({
  signedInPage: page,
}) => {
  test.setTimeout(qaTimeout([BUDGET.activity, BUDGET.tables, BUDGET.tables]))
  assertStimulusAllowed()
  const guard = consoleGuard(page)
  await page.setViewportSize({ width: 1920, height: 1080 })
  const INDEX = 'res_partner_pkey'
  const SCANS = 150_000
  const idxScan = () =>
    queryInt(
      `SELECT idx_scan FROM pg_stat_user_indexes WHERE schemaname='public' AND indexrelname='${INDEX}';`,
    )
  const history = async (from: Date, to: Date) =>
    (
      await api.get<{ points: { ts: string; idx_scan_rate: number | null }[] }>(
        await inst(
          `/indexes/history?${new URLSearchParams({
            database: database(),
            schema: 'public',
            index: INDEX,
            from: from.toISOString(),
            to: to.toISOString(),
            step: '600s',
          }).toString()}`,
        ),
      )
    ).points
  const maxRate = (points: { idx_scan_rate: number | null }[]) =>
    Math.max(0, ...points.map((point) => point.idx_scan_rate ?? 0))

  // Lower bound: the burst of D scans lands in one or two 5-minute samples; worst case it is
  // split over two 10-minute buckets, each averaging at least D/2/600 = D/1200 per second.
  // The factor 0.8 absorbs counter-flush jitter.
  const lower = (0.8 * SCANS) / 1200
  const t0 = new Date()
  // CONTROL: before the stimulus the busiest bucket of the last hour is far below the lower bound.
  const r0 = maxRate(await history(new Date(t0.getTime() - 3_600_000), t0))
  expect(r0, 'control: baseline scan rate of the pkey (idle Odoo) < lower/4').toBeLessThan(
    lower / 4,
  )

  const scansBefore = await idxScan()
  if (!SABOTAGE) {
    await query(
      `SET enable_seqscan = off; SET enable_bitmapscan = off;
       DO $$ BEGIN FOR i IN 1..${SCANS} LOOP PERFORM 1 FROM public.res_partner WHERE id = 1 + (i % 50); END LOOP; END $$;`,
      'qa-idxscan',
    )
  }
  let delta = SCANS
  if (!SABOTAGE) {
    await expect
      .poll(async () => (delta = (await idxScan()) - scansBefore), {
        timeout: BUDGET.activity,
        message: 'PostgreSQL counts the scans',
      })
      .toBeGreaterThanOrEqual(SCANS * 0.99)
  }
  // Upper bound: the busiest 5-minute sample cannot exceed D/300 (25 % for sample-interval
  // jitter), plus the baseline rate already present in the same bucket.
  const upper = (1.25 * delta) / 300 + r0
  const from = new Date(t0.getTime() - 600_000)

  await until(
    'the scan rate shows in the history API',
    async () => {
      const rate = maxRate(await history(from, new Date()))
      return rate >= lower && rate <= upper
        ? []
        : [`api max idx_scan_rate=${rate}, expected in [${lower}, ${upper}]`]
    },
    BUDGET.tables,
    [15_000],
  )
  await until(
    'the scan rate shows in the drawer chart',
    async () => {
      await page.goto(await uiPath('indexes', '&range=1h'))
      const box = page.getByRole('searchbox', { name: 'Search indexes', exact: true })
      await box.fill(INDEX)
      await page.getByRole('button', { name: `Show details of ${INDEX}`, exact: true }).click()
      await page
        .getByRole('table', { name: `Index scans of ${INDEX} data`, exact: true })
        .waitFor({ timeout: 20_000 })
      const { values } = await chartTable(page, `Index scans of ${INDEX}`)
      const numbers = values.map((text) => Number.parseFloat(text)).filter(Number.isFinite)
      const top = Math.max(0, ...numbers)
      return top >= lower && top <= upper
        ? []
        : [`ui chart max=${top} (cells ${values.length}), expected in [${lower}, ${upper}]`]
    },
    BUDGET.tables,
    [15_000],
  )
  guard.assertClean()
})

// ---------------------------------------------------------------------------
// IND-DB: database and table sizes after a 200 MB table and an agent restart
// ---------------------------------------------------------------------------
test('IND-DB database size and table bytes follow a 200 MB table after an agent restart @indicators', async ({
  signedInPage: page,
}) => {
  test.setTimeout(qaTimeout([BUDGET.tables], 1))
  assertStimulusAllowed()
  await assertLifecycleAllowed()
  const guard = consoleGuard(page)
  await page.setViewportSize({ width: 1920, height: 1080 })
  const rid = runTag()
  const big = sqlIdent(`big_${rid}`)
  const GROWTH = 150 * MIB
  registerCleanup(() => query(`DROP TABLE IF EXISTS ${QA_SCHEMA}.${big};`).then(() => undefined))

  const internals = async () =>
    (
      await api.get<{ databases: { database: string; size_bytes: number }[] }>(
        await inst('/internals'),
      )
    ).databases.find((row) => row.database === database())
  // Tolerance max(2 %, 8 MiB): the Odoo database is small and keeps writing between the
  // agent sample and the PostgreSQL read; the 200 MB step is far above this noise.
  const tolerance = (value: number) => Math.max(0.02 * value, 8 * MIB)

  const p0 = await truth.databaseSize()
  const b0 = (await internals())?.size_bytes ?? 0
  expect(
    Math.abs(b0 - p0),
    'control: API size of the idle database within noise of PostgreSQL',
  ).toBeLessThanOrEqual(tolerance(p0))

  let target = p0 + 200 * MIB
  if (!SABOTAGE) {
    await ensureQaSchema()
    await query(
      `CREATE TABLE ${QA_SCHEMA}.${big} (id int, pad text) WITH (autovacuum_enabled = false);
       INSERT INTO ${QA_SCHEMA}.${big} SELECT g, repeat(md5(g::text), 31) FROM generate_series(1, 220000) g;
       UPDATE ${QA_SCHEMA}.${big} SET pad = pad WHERE id <= 2000;
       CHECKPOINT;`,
    )
    target = await truth.databaseSize()
    expect(target - p0, 'PostgreSQL grew by at least 150 MiB').toBeGreaterThanOrEqual(GROWTH)
    await restartAgent()
  }

  const pgBytes = (fn: string) =>
    queryInt(`SELECT ${fn}('${QA_SCHEMA}.${big}'::regclass);`).catch(() => 0)
  await until(
    'the database size and the table bytes reach the API',
    async () => {
      const problems: string[] = []
      const size = (await internals())?.size_bytes ?? 0
      const live = SABOTAGE ? target : await truth.databaseSize()
      if (size < b0 + GROWTH)
        problems.push(`api size_bytes=${size} < baseline+150MiB=${b0 + GROWTH}`)
      if (Math.abs(size - live) > tolerance(live)) problems.push(`api size ${size} vs pg ${live}`)
      const row = tableOf(await tablesApi(), QA_SCHEMA, big)
      if (!row) problems.push(`api /tables has no ${big}`)
      else {
        // Tolerance 1 %: both read the relation size; the agent sampled a static table.
        const rel = SABOTAGE ? 200 * MIB : await pgBytes('pg_relation_size')
        const total = SABOTAGE ? 200 * MIB : await pgBytes('pg_total_relation_size')
        if (Math.abs(row.table_bytes - rel) > 0.01 * rel)
          problems.push(`table_bytes ${row.table_bytes} vs pg ${rel}`)
        if (Math.abs(row.total_bytes - total) > 0.01 * total)
          problems.push(`total_bytes ${row.total_bytes} vs pg ${total}`)
      }
      return problems
    },
    BUDGET.tables,
    [10_000],
  )

  await until(
    'the console shows the API sizes',
    async () => {
      const problems: string[] = []
      const size = (await internals())?.size_bytes ?? 0
      const dbRows = await uiRows(
        page,
        `/instances/${await instanceId()}?section=internals&view=databases`,
        'Database sizes',
        null,
        database(),
      )
      const cell = dbRows[0] ? col(dbRows[0], 'Size') : ''
      if (cell !== formatBytes(size))
        problems.push(`ui database Size="${cell}" api=${formatBytes(size)}`)
      const row = tableOf(await tablesApi(), QA_SCHEMA, big)
      const deadRowsUi = await uiRows(
        page,
        await uiPath('dead-rows'),
        'Tables',
        'Search tables',
        big,
      )
      const tableCell = deadRowsUi[0] ? col(deadRowsUi[0], 'Size') : ''
      if (!row || tableCell !== formatBytes(row.total_bytes))
        problems.push(
          `ui table Size="${tableCell}" api=${row ? formatBytes(row.total_bytes) : 'none'}`,
        )
      return problems
    },
    BUDGET.tables,
    [15_000],
  )
  guard.assertClean()
})

// ---------------------------------------------------------------------------
// IND-SUGG: index suggestion for a hot column, gone after the index exists
// "table_columns ingested" signal: GET /instances/{id}/coverage, check
// `table_columns` has state === 'reported' and last_result_at >= restart time.
// ---------------------------------------------------------------------------
test('IND-SUGG a hot unindexed column is suggested, and the suggestion goes once the index exists @indicators', async ({
  signedInPage: page,
}) => {
  // Two table_columns waits and two agent restarts (§4: n = restarts performed).
  const SEQ = [
    BUDGET.checkHourly,
    300_000,
    BUDGET.statStatements,
    BUDGET.checkHourly,
    BUDGET.tables,
  ] as const
  test.setTimeout(qaTimeout([...SEQ], 2))
  const startedAt = Date.now()
  assertStimulusAllowed()
  await assertLifecycleAllowed()
  const guard = consoleGuard(page)
  await page.setViewportSize({ width: 1920, height: 1080 })
  const rid = runTag()
  const tbl = sqlIdent(`s_${rid}`)
  const idx = sqlIdent(`s_a_${rid}`)
  const STATEMENTS = 1_000
  registerCleanup(() => query(`DROP TABLE IF EXISTS ${QA_SCHEMA}.${tbl};`).then(() => undefined))

  const remaining = () => startedAt + test.info().timeout - 45_000 - Date.now()
  const tableColumnsSince = async (since: number): Promise<string[]> => {
    const cov = await api.get<{
      checks: { check: string; state: string; last_result_at: string | null }[]
    }>(await inst('/coverage'))
    const check = cov.checks.find((item) => item.check === 'table_columns')
    if (check?.state !== 'reported' || !check.last_result_at)
      return [`table_columns state=${check?.state} last_result_at=${check?.last_result_at}`]
    // 5 s of clock skew between the host and the agent container.
    return Date.parse(check.last_result_at) >= since - 5_000
      ? []
      : [`table_columns last result ${check.last_result_at} predates the restart`]
  }
  const suggestionsFor = async () =>
    (
      await api.get<{
        suggestions: {
          schema: string
          table: string
          columns: { name: string }[]
          create_statement: string
        }[]
      }>(await inst(`/index-suggestions?window=1h&limit=100&database=${database()}`))
    ).suggestions.filter(
      (item) => item.schema === QA_SCHEMA && (item.table === tbl || item.table.endsWith(`.${tbl}`)),
    )

  // The restart is setup, not stimulus: it runs in both modes; sabotage withholds the table
  // and the workload only, so a sabotaged run fails on the suggestion itself.
  if (!SABOTAGE) {
    await ensureQaSchema()
    // 50k rows (> SmallTableRows = 1000), no primary key, distinct values in `a`.
    await query(
      `CREATE TABLE ${QA_SCHEMA}.${tbl} (a int, b int, pad text);
       INSERT INTO ${QA_SCHEMA}.${tbl} SELECT g, g % 100, md5(g::text) FROM generate_series(1, 50000) g;
       ANALYZE ${QA_SCHEMA}.${tbl};`,
    )
  }
  let restartedAt = await restartAgent()
  await until(
    'table_columns ingested after the restart',
    () => tableColumnsSince(restartedAt),
    BUDGET.checkHourly,
    [10_000],
  )

  // CONTROL: the table is known but nothing queried it, so no suggestion for it.
  expect(await suggestionsFor(), 'control: no suggestion before the workload').toHaveLength(0)

  if (!SABOTAGE) {
    const script = Array.from(
      { length: STATEMENTS },
      (_, i) =>
        `SELECT count(*) FROM ${QA_SCHEMA}.${tbl} WHERE a = ${1 + ((i * 37) % 50_000)};\nSELECT pg_sleep(0.3);`,
    ).join('\n')
    await query(script, 'qa-sugg')
    const stat = await truth.statementCalls(`%${QA_SCHEMA}.${tbl}%`)
    // 90 %: pg_stat_statements may have evicted or merged a few entries.
    expect(stat.calls, 'PostgreSQL counted the workload').toBeGreaterThanOrEqual(STATEMENTS * 0.9)
  }

  await until(
    'the suggestion shows in the API and the console',
    async () => {
      const problems: string[] = []
      const found = await suggestionsFor()
      if (!found.some((item) => item.columns.some((c) => c.name === 'a')))
        problems.push(`api: no suggestion on ${tbl}(a) (${found.length} for the table)`)
      const rows = await uiRows(
        page,
        await uiPath('suggestions'),
        'Suggested indexes',
        'Search suggestions',
        tbl,
      )
      if (rows.length === 0) problems.push('ui: no Suggested indexes row')
      else if (
        (await page
          .getByRole('row')
          .filter({ hasText: tbl })
          .getByRole('cell', { name: 'a', exact: true })
          .count()) === 0
      )
        problems.push('ui: row has no cell "a"')
      return problems
    },
    BUDGET.statStatements,
    [10_000],
  )

  if (!SABOTAGE) {
    await query(`CREATE INDEX CONCURRENTLY ${idx} ON ${QA_SCHEMA}.${tbl} (a);`)
    restartedAt = await restartAgent()
  }
  // The suggestion list only changes when table_columns reruns (hourly), hence the second restart.
  await until(
    'table_columns ingested after the index',
    () => tableColumnsSince(restartedAt),
    Math.min(BUDGET.checkHourly, remaining()),
    [10_000],
  )
  await until(
    'the suggestion is gone and the index shows (API, tax, console)',
    async () => {
      const problems: string[] = []
      if ((await suggestionsFor()).length > 0) problems.push('api: suggestion still listed')
      const indexes = await api.get<{ items: { schemaname: string; indexrelname: string }[] }>(
        await inst('/indexes?limit=500'),
      )
      if (!indexes.items.some((item) => item.schemaname === QA_SCHEMA && item.indexrelname === idx))
        problems.push(`api /indexes lacks ${idx}`)
      const sugg = await uiRows(
        page,
        await uiPath('suggestions'),
        'Suggested indexes',
        'Search suggestions',
        tbl,
      )
      if (sugg.length > 0) problems.push('ui: Suggested indexes row still shown')
      const tax = await uiRows(
        page,
        await uiPath('tax'),
        'Index tax by table',
        'Search Index tax by table',
        tbl,
      )
      if (!tax[0] || col(tax[0], 'Indexes') !== '1')
        problems.push(`ui: Index tax Indexes="${tax[0] ? col(tax[0], 'Indexes') : 'no row'}"`)
      return problems
    },
    Math.min(BUDGET.tables, remaining()),
    [15_000],
  )
  guard.assertClean()
})

// ---------------------------------------------------------------------------
// IND-FIND: unindexed foreign key finding (and a control FK that has an index)
// ---------------------------------------------------------------------------
test('IND-FIND an unindexed foreign key becomes a finding, and the finding goes when it is indexed @indicators', async ({
  signedInPage: page,
}) => {
  test.setTimeout(
    qaTimeout([BUDGET.check15m, BUDGET.findings, BUDGET.check15m, BUDGET.findings], 2),
  )
  assertStimulusAllowed()
  await assertLifecycleAllowed()
  const guard = consoleGuard(page)
  await page.setViewportSize({ width: 1920, height: 1080 })
  const rid = runTag()
  const parent = sqlIdent(`p_${rid}`)
  const bad = sqlIdent(`c_${rid}`)
  const good = sqlIdent(`d_${rid}`)
  const fkBad = sqlIdent(`fk_nok_${rid}`)
  const fkGood = sqlIdent(`fk_ok_${rid}`)
  const RULE = 'index.fk_unindexed'
  registerCleanup(() =>
    query(
      `DROP TABLE IF EXISTS ${QA_SCHEMA}.${bad}, ${QA_SCHEMA}.${good}, ${QA_SCHEMA}.${parent} CASCADE;`,
    ).then(() => undefined),
  )
  const unindexedFks = (name: string) =>
    queryInt(
      `SELECT count(*) FROM pg_constraint c WHERE c.contype = 'f' AND c.conname = '${name}'
         AND NOT EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid = c.conrelid AND i.indkey[0] = c.conkey[1]);`,
    )

  // CONTROL: nothing of this run exists yet, so no finding mentions it.
  expect(
    await findingsFor(rid),
    'control: no finding for this run before the stimulus',
  ).toHaveLength(0)

  if (!SABOTAGE) {
    await ensureQaSchema()
    await query(
      `CREATE TABLE ${QA_SCHEMA}.${parent} (id int PRIMARY KEY);
       INSERT INTO ${QA_SCHEMA}.${parent} SELECT g FROM generate_series(1, 1000) g;
       CREATE TABLE ${QA_SCHEMA}.${bad} (id int PRIMARY KEY, pid int,
         CONSTRAINT ${fkBad} FOREIGN KEY (pid) REFERENCES ${QA_SCHEMA}.${parent} (id));
       CREATE TABLE ${QA_SCHEMA}.${good} (id int PRIMARY KEY, pid int,
         CONSTRAINT ${fkGood} FOREIGN KEY (pid) REFERENCES ${QA_SCHEMA}.${parent} (id));
       CREATE INDEX d_pid_${rid} ON ${QA_SCHEMA}.${good} (pid);
       INSERT INTO ${QA_SCHEMA}.${bad} SELECT g, g FROM generate_series(1, 1000) g;
       INSERT INTO ${QA_SCHEMA}.${good} SELECT g, g FROM generate_series(1, 1000) g;
       ANALYZE;`,
    )
    expect(await unindexedFks(fkBad), 'PostgreSQL: the bad FK has no index').toBe(1)
    expect(await unindexedFks(fkGood), 'PostgreSQL: the control FK has an index').toBe(0)
    await restartAgent()
  }

  await until(
    'the finding shows in the API and the console',
    async () => {
      const problems: string[] = []
      const found = (await findingsFor(fkBad, RULE)).filter((f) => mentions(f, fkBad))
      const active = found.filter(isActive)
      if (active.length !== 1) problems.push(`api: ${active.length} active findings for ${fkBad}`)
      else if (active[0]?.evidence?.constraint !== fkBad)
        problems.push(`api: evidence.constraint=${String(active[0]?.evidence?.constraint)}`)
      if ((await findingsFor(fkGood, RULE)).some((f) => mentions(f, fkGood)))
        problems.push(`api: control FK ${fkGood} has a finding`)
      const rows = await findingRows(page, fkBad, 'all')
      if (rows.length !== 1) problems.push(`ui: ${rows.length} rows for ${fkBad}`)
      if ((await findingRows(page, fkGood, 'all')).length !== 0)
        problems.push(`ui: control FK ${fkGood} listed`)
      return problems
    },
    BUDGET.check15m + BUDGET.findings,
    [10_000],
  )

  if (!SABOTAGE) {
    await query(`CREATE INDEX c_pid_${rid} ON ${QA_SCHEMA}.${bad} (pid);`)
    expect(await unindexedFks(fkBad), 'PostgreSQL: the FK is indexed now').toBe(0)
    await restartAgent()
  }
  await until(
    'the finding leaves the API and the console',
    async () => {
      const problems: string[] = []
      const active = (await findingsFor(fkBad, RULE)).filter(
        (f) => mentions(f, fkBad) && isActive(f),
      )
      if (active.length > 0) problems.push(`api: ${active.length} findings still active`)
      if ((await findingRows(page, fkBad)).length > 0) problems.push('ui: finding still listed')
      return problems
    },
    BUDGET.check15m + BUDGET.findings,
    [10_000],
  )
  guard.assertClean()
})

// ---------------------------------------------------------------------------
// IND-NOTIF: agent down -> Fleet strip and agent_down alert; delivery if a channel exists
// ---------------------------------------------------------------------------
const STRIP = 'Agents requiring attention'
const NOTIF_WAIT = 300_000

async function agentDownAlerts(): Promise<number> {
  const alerts = await api.get<{ rule_id: string }[]>('/api/v1/alerts?state=firing')
  return alerts.filter((alert) => alert.rule_id === 'agent_down').length
}

test('IND-NOTIF stopping the agent shows in the API, the Fleet strip and the agent_down alert @indicators', async ({
  signedInPage: page,
}) => {
  test.setTimeout(qaTimeout([NOTIF_WAIT, NOTIF_WAIT], 1))
  assertStimulusAllowed()
  await assertLifecycleAllowed()
  const guard = consoleGuard(page)
  await page.setViewportSize({ width: 1920, height: 1080 })
  const id = await instanceId()
  const upNow = async () => (await api.instances()).find((row) => row.instance_id === id)?.up

  // CONTROL: the agent runs, so the instance is up, the strip is absent and the alert is not firing.
  await page.goto('/')
  await page.getByRole('heading', { level: 1 }).first().waitFor({ timeout: 20_000 })
  expect(await upNow(), 'control: instance up').toBe(true)
  expect(
    await page.getByRole('heading', { name: STRIP }).count(),
    'control: no attention strip',
  ).toBe(0)
  expect(await agentDownAlerts(), 'control: agent_down not firing').toBe(0)

  let stopped = false
  registerCleanup(async () => {
    if (stopped) await startAgentAndWait()
  })
  if (!SABOTAGE) {
    stopped = true
    await stop(AGENT)
  }

  // Alert path: up < 1 for 90 s, evaluated every 30 s; the Fleet marks the agent stale after 45 s.
  await until(
    'the stopped agent shows in the API, the Fleet and the alerts',
    async () => {
      const problems: string[] = []
      if ((await upNow()) !== false) problems.push('api: instance still up')
      await page.goto('/')
      if (
        !(await page
          .getByRole('heading', { name: STRIP })
          .waitFor({ timeout: 10_000 })
          .then(
            () => true,
            () => false,
          ))
      )
        problems.push('ui: no Agents requiring attention strip')
      if ((await agentDownAlerts()) === 0) problems.push('alert: agent_down not firing')
      return problems
    },
    NOTIF_WAIT,
    [10_000],
  )

  stopped = false
  await startAgentAndWait()
  // The firing instance_unreachable alert resolves on the next evaluation, up to two minutes later (observed 3 m 24 s after the restart).
  await until(
    'the strip clears after the restart',
    async () => {
      await page.goto('/')
      await page.getByRole('heading', { level: 1 }).first().waitFor({ timeout: 20_000 })
      return (await page.getByRole('heading', { name: STRIP }).count()) === 0
        ? []
        : ['strip still shown']
    },
    NOTIF_WAIT,
    [10_000],
  )
  guard.assertClean()
})

test('IND-NOTIF delivery: the agent_down notification reaches a configured channel @indicators', async () => {
  test.setTimeout(qaTimeout([NOTIF_WAIT], 1))
  assertStimulusAllowed()
  const channels = await api.get<{ kind: string; configured: boolean }[]>(
    '/api/v1/notification-channels',
  )
  // Required since the user configured the test channel (STATE §8, 2026-11-06): no skip.
  expect(
    channels.some((channel) => channel.configured),
    'a notification channel is configured on the server',
  ).toBe(true)
  await assertLifecycleAllowed()
  // No notifications API exists: delivered = a row with ok = true in the pglens database.
  // The `error` column is never read.
  const delivered = (since: string) =>
    timescale(
      `SELECT count(*) FROM notifications WHERE alert_key LIKE '%agent_down%' AND ok AND sent_at >= '${since}';`,
    ).then(Number)
  const t0 = new Date().toISOString()
  expect(await delivered(t0), 'control: no agent_down delivery before the stop').toBe(0)

  let stopped = false
  registerCleanup(async () => {
    if (stopped) await startAgentAndWait()
  })
  if (!SABOTAGE) {
    stopped = true
    await stop(AGENT)
  }
  await until(
    'the agent_down notification is delivered',
    async () => ((await delivered(t0)) >= 1 ? [] : ['no delivered agent_down notification yet']),
    NOTIF_WAIT,
    [10_000],
  )
  stopped = false
  await startAgentAndWait()
})

// ---------------------------------------------------------------------------
// IND-IDXUSE-SLOW: unused index verdict after window.unused_index (@slow only)
// ---------------------------------------------------------------------------
const SLOW_SCHEMA = 'qa_slow'
const slowFile = () =>
  join(process.cwd(), '..', 'test', 'e2e', '_artifacts', 'ui', 'slow-idx-created.json')

interface SlowRecord {
  created_at: string
  run_id: string
  table: string
  index: string
  control_index: string
  window_seconds: number
}

test('IND-IDXUSE-SLOW an unused 10+ MiB index gets its verdict only after window.unused_index @slow', async ({
  signedInPage: page,
}) => {
  // Setup (rows + two indexes) comes first; the real timeout is set once the deadline is known.
  test.setTimeout(15 * 60_000)
  const testStart = Date.now()
  assertStimulusAllowed()
  const guard = consoleGuard(page)
  await page.setViewportSize({ width: 1920, height: 1080 })

  const windows = await api.get<{ key: string; seconds: number }[]>(
    '/api/v1/settings/advisor-windows',
  )
  const windowSeconds = windows.find((row) => row.key.endsWith('unused_index'))?.seconds ?? 0
  expect(windowSeconds, 'window.unused_index is known').toBeGreaterThan(0)
  const windowMs = windowSeconds * 1000

  let record: SlowRecord | null = null
  if (!SABOTAGE) {
    try {
      const saved = JSON.parse(await readFile(slowFile(), 'utf8')) as SlowRecord
      const alive = await queryInt(
        `SELECT count(*) FROM pg_indexes WHERE schemaname = '${SLOW_SCHEMA}' AND indexname = '${sqlIdent(saved.index)}';`,
      )
      if (alive === 1 && saved.window_seconds === windowSeconds) record = saved
    } catch {
      record = null
    }
  }

  let done = false
  registerCleanup(async () => {
    // A killed or timed-out run before the verdict keeps the index so that a later run
    // resumes from slow-idx-created.json; remove it by hand (DROP SCHEMA qa_slow CASCADE)
    // when the run is abandoned. A sabotaged run created nothing and must not touch a real
    // observation that may be running beside it.
    if (SABOTAGE || !done) return
    await query(`DROP SCHEMA IF EXISTS ${SLOW_SCHEMA} CASCADE;`)
    await rm(slowFile(), { force: true })
  })

  if (!record && !SABOTAGE) {
    const rid = runTag()
    const table = sqlIdent(`t_${rid}`)
    const index = sqlIdent(`iu_${rid}`)
    const controlIndex = sqlIdent(`ic_${rid}`)
    try {
      await query(
        `DROP SCHEMA IF EXISTS ${SLOW_SCHEMA} CASCADE;
         CREATE SCHEMA ${SLOW_SCHEMA};
         CREATE TABLE ${SLOW_SCHEMA}.${table} (id int, h text, h2 text);
         INSERT INTO ${SLOW_SCHEMA}.${table}
           SELECT g, md5(g::text) || md5((g * 7)::text), md5((g * 13)::text) || md5((g * 17)::text)
           FROM generate_series(1, 300000) g;
         CREATE INDEX ${controlIndex} ON ${SLOW_SCHEMA}.${table} (h2);
         CREATE INDEX ${index} ON ${SLOW_SCHEMA}.${table} (h);
         ANALYZE ${SLOW_SCHEMA}.${table};`,
      )
      for (const name of [index, controlIndex]) {
        const size = await queryInt(`SELECT pg_relation_size('${SLOW_SCHEMA}.${name}'::regclass);`)
        expect(size, `${name} is above the 10 MiB rule threshold`).toBeGreaterThanOrEqual(11 * MIB)
      }
    } catch (error) {
      done = true
      throw error
    }
    const createdAt = new Date().toISOString()
    await query(`COMMENT ON INDEX ${SLOW_SCHEMA}.${index} IS 'created_at=${createdAt}';`)
    record = {
      created_at: createdAt,
      run_id: rid,
      table,
      index,
      control_index: controlIndex,
      window_seconds: windowSeconds,
    }
    await mkdir(join(slowFile(), '..'), { recursive: true })
    await writeFile(slowFile(), JSON.stringify(record, null, 2))
  }
  if (SABOTAGE) {
    // Sabotage: nothing is created, so the verdict never comes (deadline shortened to the base budgets).
    record = {
      created_at: new Date().toISOString(),
      run_id: 'sabotage',
      table: 'none',
      index: `iu_sabotage_${Date.now()}`,
      control_index: 'none',
      window_seconds: 0,
    }
  }
  if (!record) throw new Error('slow index record missing')
  const { index, control_index: controlIndex, table } = record
  const comment = SABOTAGE
    ? record.created_at
    : (await query(`SELECT obj_description('${SLOW_SCHEMA}.${index}'::regclass);`)).replace(
        'created_at=',
        '',
      )
  expect(comment, 'the artifact and the index comment agree on the creation time').toBe(
    record.created_at,
  )

  const createdAtMs = Date.parse(record.created_at)
  const verdictAfter = SABOTAGE ? Date.now() : createdAtMs + windowMs
  // The deadline derives from the creation time, so a late start only waits what is left.
  const deadline = SABOTAGE
    ? Date.now() + BUDGET.tables + BUDGET.findings
    : createdAtMs + windowMs + BUDGET.tables + BUDGET.findings
  const elapsed = Date.now() - testStart
  test.setTimeout(elapsed + Math.max(0, deadline - Date.now()) + 60_000)

  let early: Finding | undefined
  let gateBroken: string | undefined
  const scanControl = () =>
    query(
      `SET enable_seqscan = off;
       SELECT count(*) FROM ${SLOW_SCHEMA}.${table} WHERE h2 = md5('13') || md5('221');`,
      'qa-slow-scan',
    )
  const unusedFinding = async (name: string) =>
    (await findingsFor(name, 'index.unused')).find((f) => mentions(f, name) && isActive(f))

  await until(
    'the unused-index verdict appears after the window',
    async () => {
      if (!SABOTAGE) await scanControl()
      const finding = await unusedFinding(index)
      if (Date.now() < verdictAfter) {
        // A verdict before the window is legitimate only on PostgreSQL's own statistics
        // (D22b: PG >= 16 proves the window by last_idx_scan); one drawn from pglens's
        // history means the window gate is broken.
        if (finding && finding.state !== 'degraded') {
          const source = finding.evidence?.observed_source
          if (source !== 'postgres_stats') {
            // until() retries thrown errors, so the poll ends here and the test fails below.
            gateBroken = `verdict ${Math.round((Date.now() - createdAtMs) / 1000)} s after creation, before window.unused_index, from observed_source=${String(source)}`
            return []
          }
        }
        if (finding && !early) {
          early = finding
          test.info().annotations.push({
            type: 'early-verdict',
            description: JSON.stringify({
              after_seconds: Math.round((Date.now() - createdAtMs) / 1000),
              state: finding.state,
              degraded_reason: finding.degraded_reason,
              observed_source: finding.evidence?.observed_source,
              observed_for_seconds: finding.evidence?.observed_for_seconds,
            }),
          })
        }
        return [`window not elapsed: ${Math.round((verdictAfter - Date.now()) / 1000)} s left`]
      }
      if (!finding) return ['no active "Unused index" finding yet']
      const problems: string[] = []
      if (finding.title !== 'Unused index') problems.push(`title=${finding.title}`)
      if (finding.evidence?.window_seconds !== windowSeconds)
        problems.push(`evidence.window_seconds=${String(finding.evidence?.window_seconds)}`)
      const observed = Number(finding.evidence?.observed_for_seconds ?? 0)
      // The verdict must rest on at least one full window of observation (strict: the
      // deadline already leaves tables + findings of slack for the first sample).
      if (observed < windowSeconds)
        problems.push(`observed_for_seconds=${observed} < ${windowSeconds}`)
      return problems
    },
    deadline - Date.now(),
    [30_000],
  )
  expect(
    gateBroken,
    'no verdict before window.unused_index except on PostgreSQL statistics',
  ).toBeUndefined()

  // CONTROL: the scanned index never gets a verdict.
  expect(
    await unusedFinding(controlIndex),
    'control: the scanned index has no unused verdict',
  ).toBeUndefined()
  expect(
    await queryInt(
      `SELECT idx_scan FROM pg_stat_user_indexes WHERE schemaname='${SLOW_SCHEMA}' AND indexrelname='${controlIndex}';`,
    ),
    'PostgreSQL: the control index was scanned, the unused one was not',
  ).toBeGreaterThan(0)
  expect(
    await queryInt(
      `SELECT idx_scan FROM pg_stat_user_indexes WHERE schemaname='${SLOW_SCHEMA}' AND indexrelname='${index}';`,
    ),
  ).toBe(0)

  await until(
    'the console lists the verdict and not the control',
    async () => {
      const problems: string[] = []
      const rows = await findingRows(page, index, 'all')
      if (rows.length === 0) problems.push('ui: no Findings row for the unused index')
      if ((await findingRows(page, controlIndex, 'all')).length !== 0)
        problems.push('ui: control index listed')
      return problems
    },
    BUDGET.findings,
    [10_000],
  )
  done = true
  guard.assertClean()
})
