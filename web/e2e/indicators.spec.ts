import { expect, type Locator, type Page } from '@playwright/test'

import { test } from './fixtures'
import * as pglens from './qa/api'
import { BUDGET, qaTimeout } from './qa/budget'
import { assertStimulusAllowed } from './qa/guard'
import { consoleGuard } from './qa/observe'
import {
  burstWrites,
  cpuBurn,
  database,
  lockPair,
  dbContainer,
  openSessions,
  QA_SCHEMA,
  query,
  queryInt,
  runCleanups,
  sessionCount,
  truth,
} from './qa/pg'
import { type ApiLocks, truthSessions, truthTotals } from './qa/sessionTruth'
import { run } from './qa/stack'
import { completenessOf, pickHostInstance, pickInstance } from './visual_helpers'

/**
 * Unit 6.4 (phase 07): stimulus -> indicator. Each test applies one real
 * stimulus to the monitored PostgreSQL and proves that the indicator the console
 * shows, the pglens API and PostgreSQL itself agree, within a budget from
 * `qa/budget` and a written tolerance.
 *
 *   IND-TPS       REQ-41   burst of commits -> Overview TPS chart
 *   IND-CONN      REQ-41   12 sessions -> Connections used tile, Application sessions table
 *   IND-CONN-APP  REQ-41 REQ-40   12 sessions -> by-application breakdown (default-on, no callout)
 *   IND-LOCK      REQ-41   row lock held by A, waited by B -> Blocking tree, gone after release
 *   IND-SLOW      REQ-41   20 x pg_sleep(2) -> Slowest queries row
 *   IND-ASH       REQ-41   CPU burn + lock wait -> ASH CPU and Locks groups and legends
 *   IND-HOST      REQ-41   60 s CPU burn in the PG container -> Host CPU chart
 *
 * Every test has a CONTROL (the indicator at rest, before the stimulus, within
 * noise) and a sabotage switch: with PGLENS_QA_SABOTAGE=1 the stimulus is skipped
 * (the PGLENS_QA_ALLOW_STIMULUS gate still runs) and nothing else changes, so the
 * test must fail. Waits are `expect.poll` with BUDGET deadlines, never sleeps.
 */
test.describe.configure({ retries: 0 })

const SABOTAGE = process.env.PGLENS_QA_SABOTAGE === '1'
const POLL_MS = 3_000
const VIEWPORT = { width: 1920, height: 1080 }
/** Stimulus durations: the first term of each `qaTimeout`, and the stimulus itself. */
const STIMULUS = { tps: 60_000, lock: 40_000, slow: 40_000, ash: 30_000, host: 60_000 } as const
/** Activity is sampled every 10 s and the Locks page polls every 5 s (src/api/policy.ts). */
const ACTIVITY_SAMPLE_S = 10
const UI_POLL_S = 5

const msg = (error: unknown) => (error instanceof Error ? error.message : String(error))

test.afterEach(runCleanups)

/** Polls `check` until it reports no problem; on a miss the error names the last problems. */
async function settle(
  what: string,
  timeoutMs: number,
  check: () => Promise<string[]>,
): Promise<void> {
  const started = Date.now()
  let last: string[] = ['not checked']
  try {
    await expect
      .poll(
        async () => {
          try {
            last = await check()
          } catch (error) {
            last = [`error: ${msg(error)}`]
          }
          return last.length === 0
        },
        { timeout: timeoutMs, intervals: [POLL_MS] },
      )
      .toBe(true)
  } catch (error) {
    throw new Error(
      `${what}: not reached within ${timeoutMs} ms (${Date.now() - started} ms elapsed); last: ${last.join('; ')}\n${msg(error)}`,
      { cause: error },
    )
  }
  console.log(`[indicators] ${what}: reached after ${Date.now() - started} ms`)
}

/** The stimulus gate and the sabotage switch: `skipped` replaces `apply` under PGLENS_QA_SABOTAGE=1. */
async function stimulate<T>(apply: () => Promise<T>, skipped: T): Promise<T> {
  assertStimulusAllowed()
  return SABOTAGE ? skipped : apply()
}

const nothing = () => Promise.resolve()
const noRelease = { release: nothing }
const noClose = { close: nothing }

// ---- charts: the sr-only data table of a chart, as numbers --------------------------------

interface Point {
  at: number
  value: number | null
}

/** Rows of the chart table `<title> data` for one series; `Unknown` is a null bucket. */
async function uiPoints(page: Page, title: string, series: string): Promise<Point[]> {
  const rows = await page
    .getByRole('table', { name: `${title} data`, exact: true })
    .locator('tbody tr')
    .evaluateAll((trs) =>
      trs.map((tr) => ({
        series: (tr.querySelector('th')?.textContent ?? '').trim(),
        cells: Array.from(tr.querySelectorAll('td')).map((td) => (td.textContent ?? '').trim()),
      })),
    )
  return rows
    .filter((row) => row.series === series)
    .map((row) => {
      const raw = row.cells[1] ?? ''
      const value = raw === '' || raw === 'Unknown' ? null : Number(raw)
      return { at: Date.parse(row.cells[0] ?? ''), value: Number.isFinite(value) ? value : null }
    })
    .filter((point) => Number.isFinite(point.at))
}

interface Series {
  series?: { ts: string; value: number | null }[]
}

/**
 * Query string of the newest `metrics/query` the page sent for each metric. Bucket
 * timestamps start at the request's `from`, so the API read that is compared with a
 * chart must replay the chart's own window to land on the same buckets.
 */
function trackQueries(page: Page): (metric: string) => string | undefined {
  const seen = new Map<string, string>()
  page.on('request', (request) => {
    const url = new URL(request.url())
    const metric = url.searchParams.get('metric')
    if (url.pathname.endsWith('/metrics/query') && metric !== null) seen.set(metric, url.search)
  })
  return (metric) => seen.get(metric)
}

async function apiPoints(
  metric: string,
  id: string,
  stepS: number,
  windowMs: number,
  db?: string,
  like?: string,
): Promise<Point[]> {
  if (like !== undefined) {
    const replay = await pglens.get<Series>(`/api/v1/metrics/query${like}`)
    return (replay.series ?? []).map((row) => ({ at: Date.parse(row.ts), value: row.value }))
  }
  const to = new Date()
  const params = new URLSearchParams({
    metric,
    instance_id: id,
    from: new Date(to.getTime() - windowMs).toISOString(),
    to: to.toISOString(),
    step: `${stepS}s`,
  })
  if (db !== undefined) params.set('database', db)
  const body = await pglens.get<Series>(`/api/v1/metrics/query?${params.toString()}`)
  return (body.series ?? []).map((row) => ({ at: Date.parse(row.ts), value: row.value }))
}

/** Bucket width: the median gap between consecutive buckets. */
function stepOf(points: readonly Point[]): number {
  const gaps = points
    .slice(1)
    .map((point, index) => (point.at - (points[index]?.at ?? point.at)) / 1000)
    .filter((gap) => gap > 0)
    .sort((a, b) => a - b)
  const middle = gaps[Math.floor(gaps.length / 2)]
  if (middle === undefined) throw new Error('the chart shows fewer than two buckets')
  return middle
}

/** The last six buckets that ended before the stimulus; null buckets are dropped, or read as 0 with `zeroFill`. */
function idleValues(
  points: readonly Point[],
  stepS: number,
  startMs: number,
  zeroFill = false,
): number[] {
  return points
    .filter((point) => point.at + stepS * 1000 <= startMs)
    .slice(-6)
    .map((point) => (zeroFill ? (point.value ?? 0) : point.value))
    .filter((value): value is number => value !== null)
}

/** The baseline is the highest of the last three idle buckets: a rise must clear the busiest of them. */
const baselineOf = (values: readonly number[]) =>
  values.length === 0 ? 0 : Math.max(...values.slice(-3))
const spreadOf = (values: readonly number[]) =>
  values.length === 0 ? 0 : Math.max(...values) - Math.min(...values)

/**
 * Highest bucket from the one that straddles the stimulus start; the bucket
 * timestamp may be its start or its end, so one step before the start is included.
 */
function peakSince(points: readonly Point[], stepS: number, startMs: number): number | null {
  const values = points
    .filter((point) => point.at >= startMs - stepS * 1000)
    .map((point) => point.value)
    .filter((value): value is number => value !== null)
  return values.length === 0 ? null : Math.max(...values)
}

/**
 * Share of one bucket a stimulus of `stimulusS` seconds fills in the worst phase
 * (it then splits across two buckets): D / (2 s), at most 1.
 */
const worstFill = (stimulusS: number, stepS: number) => Math.min(1, stimulusS / (2 * stepS))

/**
 * The rise the peak bucket must show above the baseline: the smaller of the
 * spec's fraction and 80 % of the worst-case fill (20 % slack for sample jitter),
 * times the full effect of the stimulus.
 */
const requiredRise = (full: number, stimulusS: number, stepS: number, specFraction: number) =>
  Math.min(specFraction, 0.8 * worstFill(stimulusS, stepS)) * full

/** CONTROL: with no stimulus the last idle buckets stay within `bound` of each other. */
function idleProblems(
  label: string,
  values: readonly number[],
  bound: number,
  minBuckets: number,
): string[] {
  const problems: string[] = []
  if (values.length < minBuckets) {
    problems.push(`${label}: ${values.length} idle buckets, want at least ${minBuckets}`)
  }
  const spread = spreadOf(values)
  if (spread >= bound) {
    problems.push(`${label}: idle spread ${spread.toFixed(3)} is not below ${bound.toFixed(3)}`)
  }
  return problems
}

/**
 * The console chart and the API series must carry the same value in every bucket
 * both show, except the newest two, which the next sample may still complete.
 * `scale` converts the UI value to the API's unit.
 */
function agreement(
  ui: readonly Point[],
  api: readonly Point[],
  stepS: number,
  scale = 1,
): string[] {
  const byTime = new Map(api.map((point) => [point.at, point.value]))
  const newest = Math.max(...ui.map((point) => point.at))
  const problems: string[] = []
  let compared = 0
  for (const point of ui) {
    if (point.at > newest - 2 * stepS * 1000 || !byTime.has(point.at)) continue
    const other = byTime.get(point.at) ?? null
    const mine = point.value === null ? null : point.value * scale
    if (mine === null || other === null) {
      if (mine !== other)
        problems.push(`bucket ${new Date(point.at).toISOString()}: UI ${mine} but API ${other}`)
      continue
    }
    compared += 1
    // 1 % plus 1e-6: both read the same endpoint; only the table's number formatting differs.
    if (Math.abs(mine - other) > 0.01 * Math.max(Math.abs(mine), Math.abs(other)) + 1e-6) {
      problems.push(`bucket ${new Date(point.at).toISOString()}: UI ${mine} but API ${other}`)
    }
  }
  if (compared === 0) problems.push('the UI chart and the API series share no bucket')
  return problems.slice(0, 3)
}

const openAt = async (page: Page, path: string) => {
  await page.setViewportSize(VIEWPORT)
  await page.goto(path)
}

// ---- Locks page readers ---------------------------------------------------------------------

const toCount = (text: string | undefined) => Number((text ?? '').replace(/,/g, ''))

interface LockNode {
  pid: number | string
  blocked_by?: (number | string)[]
  wait_duration?: number | string | null
}
interface LocksResponse extends ApiLocks {
  nodes?: LockNode[]
}

const locksApi = (id: string) =>
  pglens.get<LocksResponse>(`/api/v1/locks?instance_id=${encodeURIComponent(id)}`)

const applicationCount = (response: ApiLocks, app: string) =>
  response.connections?.by_application.find((row) => row.key === app)?.count ?? 0

interface ActivityResponse {
  metrics?: Record<string, { value: number; labels?: Record<string, string> }[]>
}

/** Latest `pg_connections_by_application` samples of `app`, summed (one labelset per application). */
async function activityApplicationCount(id: string, app: string): Promise<number> {
  const response = await pglens.get<ActivityResponse>(
    `/api/v1/instances/${encodeURIComponent(id)}/activity`,
  )
  return (response.metrics?.pg_connections_by_application ?? [])
    .filter((sample) => sample.labels?.application_name === app)
    .reduce((sum, sample) => sum + sample.value, 0)
}

interface Tile {
  used: number
  application: number
  pglens: number
}

/** The Connections used tile: the figure, and `<N> application connections · <M> pglens connections`. */
async function connectionsTile(page: Page): Promise<Tile | null> {
  const tile = page.getByRole('article', { name: 'Connections used', exact: true })
  if ((await tile.count()) === 0) return null
  const text = ((await tile.textContent()) ?? '').trim()
  const used = /^Connections used\s*([\d,]+)/.exec(text)
  const application = /([\d,]+) application connections?/.exec(text)
  const pglensSessions = /([\d,]+) pglens connections?/.exec(text)
  if (!used || !application || !pglensSessions) return null
  return {
    used: toCount(used[1]),
    application: toCount(application[1]),
    pglens: toCount(pglensSessions[1]),
  }
}

type Origin = 'application' | 'pglens'

/** Rows of the origin table as records keyed by column header (as in qa/sessions.spec.ts). */
async function records(page: Page, origin: Origin): Promise<Record<string, string>[]> {
  const table = page.locator(`[data-origin-table="${origin}"] table`)
  if ((await table.count()) === 0) return []
  return table.first().evaluate((node) => {
    const headers = Array.from(node.querySelectorAll('thead th')).map((cell) =>
      (cell.textContent ?? '').trim(),
    )
    return Array.from(node.querySelectorAll('tbody > tr'))
      .filter((row) => row.querySelectorAll('td, th').length >= headers.length)
      .map((row) => {
        const cells = Array.from(row.querySelectorAll('td, th')).map((cell) =>
          (cell.textContent ?? '').trim(),
        )
        const tail = cells.slice(cells.length - headers.length)
        return Object.fromEntries(headers.map((header, index) => [header, tail[index] ?? '']))
      })
  })
}

async function openOrigin(page: Page, id: string, origin: Origin, search: string): Promise<void> {
  await page.goto(`/instances/${encodeURIComponent(id)}/locks?view=${origin}`)
  const section = page.locator(`[data-origin-table="${origin}"]`)
  await expect(section).toBeVisible()
  const box = section.getByRole('searchbox')
  if (search !== '' && (await box.count()) > 0) await box.fill(search)
}

/** `label -> count` of a breakdown list (`<li><span>label</span><span>count</span>...`). */
async function listCounts(list: Locator): Promise<Map<string, number>> {
  const rows = await list
    .locator('li')
    .evaluateAll((items) =>
      items.map((item) =>
        Array.from(item.querySelectorAll(':scope > span')).map((cell) =>
          (cell.textContent ?? '').trim(),
        ),
      ),
    )
  const counts = new Map<string, number>()
  for (const [label, count] of rows) counts.set(label ?? '', toCount(count))
  return counts
}

const OPT_IN_CALLOUT = 'This breakdown is opt-in and was not reported by the agent.'

// ---- IND-TPS ----------------------------------------------------------------------------------

const TPS_TITLE = 'Transactions per second'
/** The Odoo database commits 1-3 times a second by itself; 60 per second keeps the burst well above that noise. */
const BURST = { rate: 60, writes: 60 * (STIMULUS.tps / 1000) }

test('IND-TPS a 60 s commit burst lifts the Overview TPS chart, the API series and xact_commit alike @indicators', async ({
  signedInPage: page,
  api,
}) => {
  test.setTimeout(qaTimeout([STIMULUS.tps, BUDGET.databaseStats]))
  const guard = consoleGuard(page)
  const id = await pickInstance(api)
  const db = database()
  const sent = trackQueries(page)
  await openAt(page, `/instances/${encodeURIComponent(id)}?range=1h&db=${encodeURIComponent(db)}`)
  await expect(
    page
      .getByRole('table', { name: `${TPS_TITLE} data`, exact: true })
      .locator('tbody tr')
      .first(),
  ).toBeAttached()

  const tStart = Date.now()
  const idle = await uiPoints(page, TPS_TITLE, 'Commits')
  const stepS = stepOf(idle)
  const idleBuckets = idleValues(idle, stepS, tStart)
  const base = baselineOf(idleBuckets)
  // The rate-chart step is at least 60 s, so a 60 s burst fills only half a bucket at worst
  // (worstFill = 0.5): the spec's 60 % per bucket cannot hold, 0.8 x 0.5 = 40 % of the rate does.
  const plannedRise = requiredRise(BURST.rate, STIMULUS.tps / 1000, stepS, 0.6)

  // CONTROL: before the burst the Commits series is flat within half the rise the burst must make.
  expect(
    idleProblems('CONTROL Commits', idleBuckets, plannedRise / 2, 3),
    'CONTROL: no stimulus, the indicator stays within noise',
  ).toEqual([])

  const xactBefore = await queryInt(
    `SELECT xact_commit FROM pg_stat_database WHERE datname = current_database();`,
  )
  const tBefore = Date.now()
  await stimulate(() => burstWrites(BURST.writes, BURST.rate), undefined)
  const seconds = (Date.now() - tBefore) / 1000
  const xactAfter = await queryInt(
    `SELECT xact_commit FROM pg_stat_database WHERE datname = current_database();`,
  )
  // PostgreSQL: the commits of the window, background included; a rate over the whole burst.
  const truthTps = (xactAfter - xactBefore) / seconds

  let writes = { rows: 0, spanS: 0 }
  if (!SABOTAGE) {
    const [rows = '0', span = '0'] = (
      await query(
        `SELECT count(*) || '|' || coalesce(extract(epoch FROM max(at) - min(at)), 0)
           FROM ${QA_SCHEMA}.writes WHERE at > now() - interval '15 minutes';`,
      )
    ).split('|')
    writes = { rows: Number(rows), spanS: Number(span) }
    // Exact: one single-row commit per write, none lost.
    expect(writes.rows, 'rows in qa_tmp.writes').toBe(BURST.writes)
  }
  const qaRate = SABOTAGE || writes.spanS === 0 ? BURST.rate : writes.rows / writes.spanS
  const required = requiredRise(qaRate, STIMULUS.tps / 1000, stepS, 0.6)

  await settle('TPS chart peak', BUDGET.databaseStats, async () => {
    const ui = await uiPoints(page, TPS_TITLE, 'Commits')
    const fromApi = await apiPoints(
      'pg_xact_commit_total',
      id,
      stepS,
      60 * 60_000,
      db,
      sent('pg_xact_commit_total'),
    )
    const uiPeak = peakSince(ui, stepS, tStart)
    const apiPeak = peakSince(fromApi, stepS, tStart)
    const problems: string[] = []
    for (const [name, peak] of [
      ['UI', uiPeak],
      ['API', apiPeak],
    ] as const) {
      if (peak === null || peak < base + required) {
        problems.push(
          `${name} peak ${peak?.toFixed(2)} is below baseline ${base.toFixed(2)} + ${required.toFixed(2)} (qa rate ${qaRate.toFixed(1)}/s)`,
        )
      }
      // +-25 % of the window rate from xact_commit (test-strategy §5): no bucket may exceed it.
      if (peak !== null && peak > 1.25 * truthTps) {
        problems.push(
          `${name} peak ${peak.toFixed(2)} exceeds 1.25 x PostgreSQL rate ${truthTps.toFixed(2)}`,
        )
      }
    }
    return [...problems, ...agreement(ui, fromApi, stepS)]
  })

  const final = await uiPoints(page, TPS_TITLE, 'Commits')
  const completeness = completenessOf(
    final.map((point) => (point.value === null ? 'Unknown' : String(point.value))),
  )
  // test-strategy §5: the chart is at least 95 % non-null.
  expect(
    completeness.ratio,
    `non-null buckets ${completeness.nonNull}/${completeness.total}`,
  ).toBeGreaterThanOrEqual(0.95)
  guard.assertClean()
})

// ---- IND-CONN ---------------------------------------------------------------------------------

const CONN_APP = 'qa-app'
const CONN_N = 12

test('IND-CONN 12 sessions of qa-app move the Connections used tile and the Application sessions table by 12 @indicators', async ({
  signedInPage: page,
  api,
}) => {
  test.setTimeout(qaTimeout([BUDGET.activity, BUDGET.activity]))
  const guard = consoleGuard(page)
  const id = await pickInstance(api)
  await openAt(page, `/instances/${encodeURIComponent(id)}/locks`)
  await expect(page.getByRole('article', { name: 'Connections used', exact: true })).toContainText(
    /application connections?/,
  )
  const baseline = await connectionsTile(page)
  if (baseline === null) throw new Error('the Connections used tile shows no figures')

  // CONTROL: nothing of qa-app anywhere, and the tile agrees with PostgreSQL at rest.
  const idleTruth = await truth.connections()
  const idleApi = await locksApi(id)
  expect(await sessionCount(CONN_APP), 'CONTROL pg_stat_activity').toBe(0)
  expect(applicationCount(idleApi, CONN_APP), 'CONTROL API by_application').toBe(0)
  // 2: the Odoo workers come and go between the two reads, and the truth read counts its own backend.
  expect(Math.abs(baseline.used - idleTruth), 'CONTROL tile vs connections').toBeLessThanOrEqual(2)

  const sessions = await stimulate(
    () => openSessions(CONN_N, { applicationName: CONN_APP }),
    noClose,
  )

  await settle('Connections used tile', BUDGET.activity, async () => {
    const [now, qa, totals, response, tile] = await Promise.all([
      truth.connections(),
      sessionCount(CONN_APP),
      truthSessions().then(truthTotals),
      locksApi(id),
      connectionsTile(page),
    ])
    const problems: string[] = []
    if (qa !== CONN_N) problems.push(`pg_stat_activity has ${qa} ${CONN_APP}, want ${CONN_N}`)
    // Exact: the summary counts every qa-app session, and this one only.
    if (applicationCount(response, CONN_APP) !== CONN_N) {
      problems.push(
        `API by_application ${CONN_APP} = ${applicationCount(response, CONN_APP)}, want ${CONN_N}`,
      )
    }
    if (tile === null) return [...problems, 'the tile shows no figures']
    // +-1 on the rise of 12: an Odoo worker may open or close a connection between the reads.
    for (const [name, value, was] of [
      ['used', tile.used, baseline.used],
      ['application', tile.application, baseline.application],
    ] as const) {
      if (Math.abs(value - was - CONN_N) > 1) {
        problems.push(`tile ${name} ${value} is not baseline ${was} + ${CONN_N} (+-1)`)
      }
    }
    // The pglens connections do not move: the stimulus is application traffic. +-1: the agent
    // may reconnect, and the sampling backend never counts itself.
    if (Math.abs(tile.pglens - baseline.pglens) > 1) {
      problems.push(`tile pglens ${tile.pglens} moved from ${baseline.pglens}`)
    }
    // 2: the sample is up to one activity period old, the Odoo workers drift, the truth read counts itself.
    if (Math.abs(tile.used - now) > 2) {
      problems.push(`tile used ${tile.used} vs pg_stat_activity ${now} (+-2)`)
    }
    // Same tolerance for the pglens side against the classified truth.
    if (Math.abs(tile.pglens - totals.pglens) > 2) {
      problems.push(`tile pglens ${tile.pglens} vs classified truth ${totals.pglens} (+-2)`)
    }
    return problems
  })

  await openOrigin(page, id, 'application', CONN_APP)
  await settle('Application sessions table', BUDGET.activity, async () => {
    const truthPids = (await truthSessions())
      .filter((row) => row.application_name === CONN_APP)
      .map((row) => row.pid)
      .sort((a, b) => a - b)
    const shown = (await records(page, 'application'))
      .filter((row) => row.Application === CONN_APP)
      .map((row) => Number(row.PID))
      .sort((a, b) => a - b)
    const sample = ((await locksApi(id)).sessions ?? [])
      .filter((row) => row.application_name === CONN_APP)
      .map((row) => Number(row.pid))
      .sort((a, b) => a - b)
    const problems: string[] = []
    if (truthPids.length !== CONN_N)
      problems.push(`pg_stat_activity ${truthPids.length} ${CONN_APP}`)
    if (shown.join() !== truthPids.join()) {
      problems.push(`table pids [${shown.join()}] vs pg_stat_activity [${truthPids.join()}]`)
    }
    if (sample.join() !== truthPids.join()) {
      problems.push(`API sample pids [${sample.join()}] vs pg_stat_activity [${truthPids.join()}]`)
    }
    return problems
  })

  await openOrigin(page, id, 'pglens', '')
  const monitoring = await records(page, 'pglens')
  expect(
    monitoring.filter((row) => row.Application === CONN_APP),
    'qa-app in the pglens table',
  ).toEqual([])
  // "pglens table count unchanged": +-1 for the agent's own reconnects.
  expect(
    Math.abs(monitoring.length - baseline.pglens),
    'pglens table rows vs baseline',
  ).toBeLessThanOrEqual(1)

  await sessions.close()
  await openAt(page, `/instances/${encodeURIComponent(id)}/locks`)
  await settle('Connections used back at baseline', BUDGET.activity, async () => {
    const tile = await connectionsTile(page)
    const problems: string[] = []
    const qa = await sessionCount(CONN_APP)
    if (qa !== 0) problems.push(`pg_stat_activity still has ${qa} ${CONN_APP}`)
    if (applicationCount(await locksApi(id), CONN_APP) !== 0)
      problems.push('API still counts qa-app')
    // +-1: the Odoo workers' drift since the baseline.
    if (tile === null || Math.abs(tile.application - baseline.application) > 1) {
      problems.push(
        `tile application ${tile?.application} is not back at ${baseline.application} (+-1)`,
      )
    }
    return problems
  })
  guard.assertClean()
})

// ---- IND-CONN-APP -----------------------------------------------------------------------------

test('IND-CONN-APP the by-application breakdown is on by default and counts qa-app = 12, with no opt-in callout @indicators', async ({
  signedInPage: page,
  api,
}) => {
  // REQ-40: checks.activity.by_application is on by default.
  test.setTimeout(qaTimeout([BUDGET.activity, BUDGET.activity]))
  const guard = consoleGuard(page)
  const id = await pickInstance(api)
  await openAt(page, `/instances/${encodeURIComponent(id)}/locks?view=activity`)
  const breakdown = page.getByRole('list', { name: 'Connections by application', exact: true })
  await expect(page.getByRole('heading', { name: 'Connections by application' })).toBeVisible()
  const callout = page.getByText(OPT_IN_CALLOUT)

  // CONTROL: the breakdown is on, no qa-app row anywhere, the callout absent.
  const idleApi = await locksApi(id)
  expect(idleApi.connections?.by_application_enabled, 'CONTROL by_application_enabled').toBe(true)
  expect(await callout.count(), 'CONTROL opt-in callout').toBe(0)
  expect(await sessionCount(CONN_APP), 'CONTROL pg_stat_activity').toBe(0)
  expect(applicationCount(idleApi, CONN_APP), 'CONTROL API connections').toBe(0)
  expect(await activityApplicationCount(id, CONN_APP), 'CONTROL API activity metric').toBe(0)
  expect((await listCounts(breakdown.first())).get(CONN_APP) ?? 0, 'CONTROL list row').toBe(0)

  const sessions = await stimulate(
    () => openSessions(CONN_N, { applicationName: CONN_APP }),
    noClose,
  )

  await settle('Connections by application list', BUDGET.activity, async () => {
    const problems: string[] = []
    const qa = await sessionCount(CONN_APP)
    const connections = (await locksApi(id)).connections
    const metric = await activityApplicationCount(id, CONN_APP)
    const shown = (await listCounts(breakdown.first())).get(CONN_APP) ?? 0
    // Exact everywhere: the breakdown counts every session of the application.
    if (qa !== CONN_N) problems.push(`pg_stat_activity ${qa} ${CONN_APP}`)
    if (connections?.by_application_enabled !== true)
      problems.push('by_application_enabled is not true')
    const summary = connections?.by_application.find((row) => row.key === CONN_APP)?.count ?? 0
    if (summary !== CONN_N) problems.push(`API locks by_application ${summary}`)
    if (metric !== CONN_N) problems.push(`API activity pg_connections_by_application ${metric}`)
    if (shown !== CONN_N) problems.push(`Connections by application row ${shown}`)
    if ((await callout.count()) !== 0) problems.push('the opt-in callout is shown')
    return problems
  })

  await page.getByRole('tab', { name: 'By application' }).click()
  await settle('Connection summary by application', BUDGET.activity, async () => {
    const shown = (await listCounts(breakdown.last())).get(CONN_APP) ?? 0
    return shown === CONN_N ? [] : [`By application tab row ${shown}, want ${CONN_N}`]
  })

  await sessions.close()
  await settle('by-application row gone', BUDGET.activity, async () => {
    const problems: string[] = []
    if ((await sessionCount(CONN_APP)) !== 0) problems.push('pg_stat_activity still has qa-app')
    if ((await activityApplicationCount(id, CONN_APP)) !== 0)
      problems.push('API still counts qa-app')
    if ((await listCounts(breakdown.first())).get(CONN_APP) !== undefined) {
      problems.push('the activity list still shows qa-app')
    }
    return problems
  })
  guard.assertClean()
})

// ---- IND-LOCK ---------------------------------------------------------------------------------

const LOCK_A = 'qa-lock-a'
const LOCK_B = 'qa-lock-b'
/** An age as formatDuration prints it: `12s`, `1m 5s`, `1h 2m`. */
const AGE = /Wait duration\s*(?:(\d+)h\s*)?(?:(\d+)m\s*)?(\d+(?:\.\d+)?)s/

/** textContent runs the labels into their values (`PID 12app`), so a pid ends at its last digit, not at a word boundary. */
const treeItem = (page: Page, pid: number) =>
  page
    .getByRole('tree', { name: 'Blocking lock tree' })
    .getByRole('treeitem')
    .filter({ hasText: new RegExp(`PID\\s*${pid}(?!\\d)`) })

const textOf = async (locator: Locator) =>
  (await locator.count()) === 0 ? '' : ((await locator.textContent()) ?? '')

function waitSeconds(text: string): number | null {
  const match = AGE.exec(text)
  return match === null
    ? null
    : toCount(match[1]) * 3600 + toCount(match[2]) * 60 + Number(match[3])
}

async function pidOf(app: string): Promise<number | null> {
  const out = await query(
    `SELECT pid FROM pg_stat_activity WHERE application_name = '${app}' LIMIT 1;`,
  )
  return out === '' ? null : Number(out)
}

test('IND-LOCK a row lock held by A and awaited by B shows A as the root blocker of B, and clears after release @indicators', async ({
  signedInPage: page,
  api,
}) => {
  test.setTimeout(qaTimeout([STIMULUS.lock, BUDGET.activity, BUDGET.activity]))
  const guard = consoleGuard(page)
  const id = await pickInstance(api)
  await openAt(page, `/instances/${encodeURIComponent(id)}/locks`)
  await expect(page.getByRole('tab', { name: 'Blocking' })).toBeVisible()

  // CONTROL: no qa-lock session in PostgreSQL, the API sample or the tree.
  expect(await pidOf(LOCK_A), 'CONTROL pg_stat_activity').toBeNull()
  expect(
    ((await locksApi(id)).sessions ?? []).filter((row) =>
      row.application_name?.startsWith('qa-lock'),
    ),
    'CONTROL API sample',
  ).toEqual([])
  expect(
    await textOf(page.getByRole('tree', { name: 'Blocking lock tree' })),
    'CONTROL tree',
  ).not.toContain('qa-lock')

  const tBefore = Date.now()
  const pair = await stimulate(() => lockPair('ind_lock', 1, LOCK_A, LOCK_B), noRelease)

  await settle('Blocking tree shows A blocking B', BUDGET.activity, async () => {
    const a = await pidOf(LOCK_A)
    const b = await pidOf(LOCK_B)
    if (a === null || b === null) {
      // The response is still read, so a sabotaged run fails on the API and the tree too.
      const sampled = ((await locksApi(id)).sessions ?? []).some(
        (row) => row.application_name === LOCK_B,
      )
      const tree = await textOf(page.getByRole('tree', { name: 'Blocking lock tree' }))
      return [
        `pg_stat_activity has no ${LOCK_A}/${LOCK_B} pair`,
        ...(sampled ? [] : [`API sample has no ${LOCK_B} session`]),
        ...(tree.includes(LOCK_B) ? [] : [`the tree does not show ${LOCK_B}`]),
      ]
    }
    const problems: string[] = []
    const blockers = await query(`SELECT array_to_string(pg_blocking_pids(${b}), ',');`)
    // Exact: PostgreSQL itself says A blocks B.
    if (blockers !== String(a)) problems.push(`pg_blocking_pids(${b}) = [${blockers}], want [${a}]`)
    const truthWait = Number(
      await query(
        `SELECT coalesce(extract(epoch FROM clock_timestamp() - query_start), 0)::int FROM pg_stat_activity WHERE pid = ${b};`,
      ),
    )
    // The wait is at most the time since the stimulus began, plus 2 s for the one-second rounding.
    const ceiling = Math.min(truthWait, (Date.now() - tBefore) / 1000) + 2
    // The sample is up to one activity period old and the page polls every 5 s; 1 s of rounding.
    const floor = truthWait - (ACTIVITY_SAMPLE_S + UI_POLL_S + 1)

    const node = ((await locksApi(id)).nodes ?? []).find((row) => Number(row.pid) === b)
    const apiWait = node?.wait_duration == null ? null : Number(node.wait_duration)
    if (!(node?.blocked_by ?? []).map(Number).includes(a)) {
      problems.push(`API node ${b} is not blocked by ${a}`)
    }
    if (apiWait === null || apiWait < 10 || apiWait > ceiling) {
      problems.push(`API wait ${apiWait} outside [10, ${ceiling.toFixed(0)}]`)
    }

    // A root blocker starts collapsed: its waiters are only in the DOM once it is expanded.
    const expand = page.getByRole('button', { name: `Expand lock session ${a}`, exact: true })
    if ((await expand.count()) > 0) await expand.click()
    const items = await page
      .getByRole('tree', { name: 'Blocking lock tree' })
      .getByRole('treeitem')
      .count()
    const waiter = await textOf(treeItem(page, b).last())
    const holder = await textOf(treeItem(page, a).first())
    const uiWait = waitSeconds(waiter)
    if (uiWait === null || uiWait < 10 || uiWait > ceiling || uiWait < floor) {
      problems.push(
        `UI wait ${uiWait} outside [${Math.max(10, floor).toFixed(0)}, ${ceiling.toFixed(0)}]`,
      )
    }
    if (!new RegExp(`Blocked by\\s*[^]*?(?<!\\d)${a}(?!\\d)`).test(waiter)) {
      problems.push(`the tree does not show B blocked by PID ${a}`)
    }
    if (!waiter.includes(LOCK_B))
      problems.push(`the waiter row does not show ${LOCK_B} (${items} tree items)`)
    if (!holder.includes('Root blocker')) problems.push('PID A is not marked Root blocker')
    return problems
  })

  const a = await pidOf(LOCK_A)
  const b = await pidOf(LOCK_B)
  await pair.release()

  await settle('lock gone after release', BUDGET.activity, async () => {
    const problems: string[] = []
    if ((await pidOf(LOCK_A)) !== null || (await pidOf(LOCK_B)) !== null) {
      problems.push('pg_stat_activity still has the pair')
    }
    const nodes = ((await locksApi(id)).nodes ?? []).map((row) => Number(row.pid))
    for (const pid of [a, b]) {
      if (pid !== null && nodes.includes(pid)) problems.push(`API still lists node ${pid}`)
      if (pid !== null && (await treeItem(page, pid).count()) > 0) {
        problems.push(`the tree still shows PID ${pid}`)
      }
    }
    return problems
  })
  guard.assertClean()
})

// ---- IND-SLOW ---------------------------------------------------------------------------------

const SLOW_CALLS = 20
const SLOW_MS = 2_000
const MEAN_TOLERANCE = 0.15

interface Statements {
  statements?: { query_text?: string; calls?: number; mean_exec_time?: number }[]
}

/** `2.01 s`, `350 ms`, `1.5 min` as milliseconds (formatMilliseconds). */
function parseMs(text: string): number | null {
  const match = /^([\d,.]+)\s*(ms|s|min|h)\b/.exec(text.trim())
  if (!match) return null
  const unit = { ms: 1, s: 1_000, min: 60_000, h: 3_600_000 }[match[2] as 'ms' | 's' | 'min' | 'h']
  return toCount(match[1]) * unit
}

async function slowRows(page: Page, marker: string) {
  const table = page.getByRole('table', { name: 'Slowest queries', exact: true })
  if ((await table.count()) === 0) return []
  const { headers, rows } = await table.evaluate((node) => ({
    headers: Array.from(node.querySelectorAll('thead th')).map((cell) =>
      (cell.textContent ?? '').trim(),
    ),
    rows: Array.from(node.querySelectorAll('tbody tr')).map((row) =>
      Array.from(row.querySelectorAll('td, th')).map((cell) => (cell.textContent ?? '').trim()),
    ),
  }))
  const column = (name: string) => headers.findIndex((header) => header.startsWith(name))
  return rows
    .filter((cells) => (cells[column('Query')] ?? '').includes(marker))
    .map((cells) => ({
      calls: toCount(cells[column('Calls')]),
      meanMs: parseMs(cells[column('Mean time')] ?? ''),
    }))
}

test('IND-SLOW 20 x pg_sleep(2) appear in Slowest queries with 20 calls and a 2 s mean, as pg_stat_statements says @indicators', async ({
  signedInPage: page,
  api,
}) => {
  test.setTimeout(qaTimeout([STIMULUS.slow, 2 * BUDGET.statStatements, BUDGET.statStatements]))
  const guard = consoleGuard(page)
  const id = await pickInstance(api)
  // pg_stat_statements ignores column aliases, so the statement reads a table of its own: the
  // relation is part of the queryid, and no other run or test shares its calls.
  const marker = `qa_slow_${Date.now().toString(36)}`
  const like = `%${marker}%`
  await openAt(page, `/queries?instance=${encodeURIComponent(id)}&range=1h&q=${marker}`)
  await expect(page.locator('#slow-search')).toBeVisible()

  // CONTROL: before the stimulus the statement exists nowhere.
  expect((await truth.statementCalls(like)).calls, 'CONTROL pg_stat_statements').toBe(0)
  expect(await slowRows(page, marker), 'CONTROL Slowest queries').toEqual([])
  const idleApi = await pglens.get<Statements>(
    `/api/v1/statements?instance_id=${encodeURIComponent(id)}&q=${marker}&limit=50`,
  )
  expect(
    (idleApi.statements ?? []).filter((row) => row.query_text?.includes(marker)),
    'CONTROL API statements',
  ).toEqual([])

  const one = `SELECT pg_sleep(${(SLOW_MS / 1000).toFixed(3)}) FROM ${QA_SCHEMA}.${marker};\n`
  await query(
    `CREATE SCHEMA IF NOT EXISTS ${QA_SCHEMA}; CREATE TABLE ${QA_SCHEMA}.${marker} AS SELECT 1 AS one;`,
    'qa-slow',
  )
  // The first sample of a series is only its baseline (LIMITS: no rate) and a zero delta writes
  // no row, so one call is made per poll until pglens lists the statement: its baseline is then
  // behind it, and every one of the 20 calls below falls inside the window.
  // Two scrapes: the baseline, then the first delta.
  await settle('Slowest queries baseline', 2 * BUDGET.statStatements, async () => {
    const response = await pglens.get<Statements>(
      `/api/v1/statements?instance_id=${encodeURIComponent(id)}&q=${marker}&limit=50`,
    )
    if ((response.statements ?? []).some((entry) => entry.query_text?.includes(marker))) return []
    await query(one, 'qa-slow')
    return ['the API does not list the statement yet']
  })
  // The warm-up calls are counted too: the oracle is the delta from here, 20 exactly in
  // PostgreSQL and at least 20 more in the API and the console.
  const p0 = (await truth.statementCalls(like)).calls
  const baselineRow = (
    (
      await pglens.get<Statements>(
        `/api/v1/statements?instance_id=${encodeURIComponent(id)}&q=${marker}&limit=50`,
      )
    ).statements ?? []
  ).find((entry) => entry.query_text?.includes(marker))
  const c0 = baselineRow?.calls ?? 0
  await stimulate(() => query(one.repeat(SLOW_CALLS), 'qa-slow'), '')

  await settle('Slowest queries row', BUDGET.statStatements, async () => {
    const problems: string[] = []
    const stats = await truth.statementCalls(like)
    const truthMean = stats.calls === 0 ? 0 : stats.totalMs / stats.calls
    const within = (mean: number | null) =>
      mean !== null && Math.abs(mean - SLOW_MS) <= MEAN_TOLERANCE * SLOW_MS
    // Exactly the 20 stimulus calls since the baseline; the mean may exceed 2000 ms by the statement's own overhead, 15 % at most (spec).
    if (stats.calls - p0 !== SLOW_CALLS)
      problems.push(`pg_stat_statements calls ${stats.calls} - ${p0}, want +${SLOW_CALLS}`)
    if (!within(truthMean)) problems.push(`pg_stat_statements mean ${truthMean.toFixed(0)} ms`)

    const rows = await slowRows(page, marker)
    const row = rows[0]
    if (row === undefined) problems.push('no Slowest queries row for the statement')
    else {
      // The window can never hold more calls than the server ran; the row must add all 20.
      if (row.calls < c0 + SLOW_CALLS || row.calls > stats.calls)
        problems.push(`UI calls ${row.calls}, want ${c0} + ${SLOW_CALLS}`)
      if (!within(row.meanMs)) problems.push(`UI mean ${row.meanMs} ms`)
    }

    const response = await pglens.get<Statements>(
      `/api/v1/statements?instance_id=${encodeURIComponent(id)}&q=${marker}&limit=50`,
    )
    const listed = (response.statements ?? []).find((entry) => entry.query_text?.includes(marker))
    if (listed === undefined) problems.push('no API statements row')
    else {
      if ((listed.calls ?? 0) < c0 + SLOW_CALLS || (listed.calls ?? 0) > stats.calls) {
        problems.push(`API calls ${listed.calls}, want ${c0} + ${SLOW_CALLS}`)
      }
      if (!within(listed.mean_exec_time ?? null))
        problems.push(`API mean ${listed.mean_exec_time} ms`)
    }
    return problems
  })
  guard.assertClean()
})

// ---- IND-ASH ----------------------------------------------------------------------------------

/** Longest wait for a 15 m chart to settle back to idle after the previous test: its whole window. */
const QUIET_MS = 16 * 60_000

const ASH_CPU = 'CPU average active backends'
const ASH_LOCKS = 'Locks and contention average active backends'

interface AshResponse {
  resolution_seconds?: number
  buckets?: { ts: string; wait_event_type?: string | null; avg_active_sessions?: number }[]
}

const ashApi = async (id: string, windowMs: number) => {
  const to = new Date()
  const params = new URLSearchParams({
    instance_id: id,
    group_by: 'wait_event_type',
    from: new Date(to.getTime() - windowMs).toISOString(),
    to: to.toISOString(),
  })
  return pglens.get<AshResponse>(`/api/v1/ash?${params.toString()}`)
}

/** The API buckets of one wait event type as points; no type, or `CPU`, is the CPU group. */
const ashSeries = (response: AshResponse, type: string): Point[] =>
  (response.buckets ?? [])
    .filter((bucket) => {
      const name = bucket.wait_event_type ?? ''
      return type === 'CPU' ? name === '' || name === 'CPU' : name === type
    })
    .map((bucket) => ({ at: Date.parse(bucket.ts), value: bucket.avg_active_sessions ?? 0 }))

test('IND-ASH a CPU burn and a lock wait show in the ASH CPU and Locks groups, with their legends @indicators', async ({
  signedInPage: page,
  api,
}) => {
  test.setTimeout(qaTimeout([STIMULUS.ash, QUIET_MS, BUDGET.ash, BUDGET.ash]))
  const guard = consoleGuard(page)
  const id = await pickInstance(api)
  const windowMs = 15 * 60_000
  await openAt(page, `/instances/${encodeURIComponent(id)}/ash?range=15m`)
  await expect(page.getByTestId('ash-chart-grid')).toBeVisible()

  const first = await ashApi(id, windowMs)
  const stepS = first.resolution_seconds ?? 0
  if (stepS <= 0) throw new Error('the ASH API reports no resolution_seconds')
  // One backend is fully busy (CPU) or waiting (Lock) for the burst; an ASH average counts it as 1.
  const required = requiredRise(1, STIMULUS.ash / 1000, stepS, 0.5)
  let tStart = 0
  let idle = {
    cpu: [] as number[],
    lock: [] as number[],
    apiCpu: [] as number[],
    apiLock: [] as number[],
  }
  // CONTROL: before the stimulus neither group moves by more than half the required rise. The
  // last six buckets hold the previous test's stimulus for a few minutes: wait until they are quiet.
  await settle('ASH quiet before the stimulus', QUIET_MS, async () => {
    tStart = Date.now()
    const now = await ashApi(id, windowMs)
    idle = {
      cpu: idleValues(await uiPoints(page, ASH_CPU, 'CPU'), stepS, tStart, true),
      lock: idleValues(await uiPoints(page, ASH_LOCKS, 'Lock'), stepS, tStart, true),
      apiCpu: idleValues(ashSeries(now, 'CPU'), stepS, tStart, true),
      apiLock: idleValues(ashSeries(now, 'Lock'), stepS, tStart, true),
    }
    return Object.entries(idle).flatMap(([name, values]) =>
      idleProblems(`CONTROL ${name}`, values, required / 2, 0),
    )
  })
  expect(await sessionCount('qa-cpu'), 'CONTROL pg_stat_activity').toBe(0)

  const lock = await stimulate(() => lockPair('ind_ash', 1, 'qa-ash-a', 'qa-ash-b'), noRelease)
  const burn = stimulate(() => cpuBurn(STIMULUS.ash / 1000), undefined)
  if (!SABOTAGE) {
    // PostgreSQL: the burning backend is active while the burst runs.
    await expect
      .poll(
        () =>
          queryInt(
            `SELECT count(*) FROM pg_stat_activity WHERE application_name = 'qa-cpu' AND state = 'active';`,
          ),
        { timeout: STIMULUS.ash, intervals: [POLL_MS] },
      )
      .toBeGreaterThanOrEqual(1)
  }
  await burn

  await settle('ASH CPU group', BUDGET.ash, async () => {
    const problems: string[] = []
    const ui = peakSince(await uiPoints(page, ASH_CPU, 'CPU'), stepS, tStart)
    const fromApi = peakSince(ashSeries(await ashApi(id, windowMs), 'CPU'), stepS, tStart)
    for (const [name, peak, was] of [
      ['UI', ui, baselineOf(idle.cpu)],
      ['API', fromApi, baselineOf(idle.apiCpu)],
    ] as const) {
      if (peak === null || peak < was + required) {
        problems.push(
          `${name} CPU peak ${peak?.toFixed(2)} is below ${was.toFixed(2)} + ${required.toFixed(2)}`,
        )
      }
    }
    return problems
  })

  await settle('ASH Locks group', BUDGET.ash, async () => {
    const problems: string[] = []
    const waiting = await queryInt(
      `SELECT count(*) FROM pg_stat_activity WHERE application_name = 'qa-ash-b' AND wait_event_type = 'Lock';`,
    )
    if (waiting < 1) problems.push('pg_stat_activity shows no Lock wait of qa-ash-b')
    const ui = peakSince(await uiPoints(page, ASH_LOCKS, 'Lock'), stepS, tStart)
    const fromApi = peakSince(ashSeries(await ashApi(id, windowMs), 'Lock'), stepS, tStart)
    for (const [name, peak, was] of [
      ['UI', ui, baselineOf(idle.lock)],
      ['API', fromApi, baselineOf(idle.apiLock)],
    ] as const) {
      if (peak === null || peak < was + required) {
        problems.push(
          `${name} Lock peak ${peak?.toFixed(2)} is below ${was.toFixed(2)} + ${required.toFixed(2)}`,
        )
      }
    }
    return problems
  })

  // Legends list each series with its description (test-strategy §5).
  await expect(
    page
      .getByRole('list', { name: 'CPU legend', exact: true })
      .getByRole('button')
      .filter({ hasText: /CPU\s+—\s+Backend running on CPU/ }),
  ).toBeVisible()
  await expect(
    page
      .getByRole('list', { name: 'Locks and contention legend', exact: true })
      .getByRole('button')
      .filter({ hasText: /Lock\s+—\s+Waiting for a heavyweight lock/ }),
  ).toBeVisible()
  await lock.release()
  guard.assertClean()
})

// ---- IND-HOST ---------------------------------------------------------------------------------

const HOST_TITLE = 'CPU used (%)'
/**
 * The Odoo load alone moves the host CPU ratio by about 0.06 between buckets, so one burning core
 * (1/8) is no clean signal: half the cores burn, at most four, and the rise asked is the part of
 * their share a 60 s burst fills in a bucket (requiredRise).
 */
const MAX_BURNERS = 4

interface CpuTimes {
  busy: number
  total: number
}

/** `cpu` line of /proc/stat of the kernel the PG container runs on: user nice system idle iowait irq softirq steal. */
async function procStat(): Promise<CpuTimes> {
  const { stdout } = await run('docker', ['exec', await dbContainer(), 'cat', '/proc/stat'])
  const line = stdout.split('\n').find((row) => row.startsWith('cpu '))
  if (line === undefined) throw new Error('/proc/stat has no cpu line')
  const fields = line.trim().split(/\s+/).slice(1, 9).map(Number)
  const total = fields.reduce((sum, value) => sum + value, 0)
  return { busy: total - (fields[3] ?? 0) - (fields[4] ?? 0), total }
}

async function hostCores(): Promise<number> {
  const { stdout } = await run('docker', ['exec', await dbContainer(), 'nproc'])
  return Math.max(1, Number(stdout.trim()))
}

const busyRatio = (from: CpuTimes, to: CpuTimes) =>
  (to.busy - from.busy) / Math.max(1, to.total - from.total)

test('IND-HOST a 60 s CPU burn in the PG container lifts the Host CPU chart, the API series and /proc/stat @indicators', async ({
  signedInPage: page,
  api,
}) => {
  test.setTimeout(qaTimeout([STIMULUS.host, QUIET_MS, BUDGET.host]))
  const guard = consoleGuard(page)
  let t0 = await procStat()
  const cores = await hostCores()
  const burners = Math.max(1, Math.min(MAX_BURNERS, Math.floor(cores / 2)))
  let rise = 0
  const id = await pickHostInstance(api)
  const sent = trackQueries(page)
  await openAt(page, `/instances/${encodeURIComponent(id)}?range=15m&section=host`)
  await expect(
    page
      .getByRole('table', { name: `${HOST_TITLE} data`, exact: true })
      .locator('tbody tr')
      .first(),
  ).toBeAttached()

  const windowMs = 15 * 60_000
  // The chart shows percent; the API and PostgreSQL's kernel read a ratio.
  const asRatio = (points: Point[]) =>
    points.map((point) => ({
      at: point.at,
      value: point.value === null ? null : point.value / 100,
    }))
  let tStart = 0
  let stepS = 0
  let idleUi: number[] = []
  let idleApi: number[] = []
  // CONTROL: before the burst the host CPU stays within half the required rise. The last six
  // buckets hold the previous test's stimulus for a few minutes: wait until they are quiet.
  await settle('Host CPU quiet before the burst', QUIET_MS, async () => {
    t0 = await procStat()
    tStart = Date.now()
    const idle = asRatio(await uiPoints(page, HOST_TITLE, 'CPU used'))
    stepS = stepOf(idle)
    rise = requiredRise(burners / cores, STIMULUS.host / 1000, stepS, 0.5)
    idleUi = idleValues(idle, stepS, tStart)
    idleApi = idleValues(await apiPoints('host_cpu_used_ratio', id, stepS, windowMs), stepS, tStart)
    return [
      ...idleProblems('CONTROL UI', idleUi, rise / 2, 3),
      ...idleProblems('CONTROL API', idleApi, rise / 2, 3),
    ]
  })
  const t1 = await procStat()

  await stimulate(
    () =>
      Promise.all(
        Array.from({ length: burners }, (_, index) =>
          cpuBurn(STIMULUS.host / 1000, `qa-cpu-${index}`),
        ),
      ),
    undefined,
  )
  const t2 = await procStat()
  // PostgreSQL's kernel: busy share over the burst against the share over the test's setup;
  // the setup window is a few seconds, so only the required rise is asked of it.
  const truthRise = busyRatio(t1, t2) - busyRatio(t0, t1)

  await settle('Host CPU chart peak', BUDGET.host, async () => {
    const problems: string[] = []
    if (truthRise < rise) {
      problems.push(`/proc/stat busy share rose by ${truthRise.toFixed(3)}, want >= ${rise}`)
    }
    const ui = (await uiPoints(page, HOST_TITLE, 'CPU used')).map((point) => ({
      at: point.at,
      value: point.value === null ? null : point.value / 100,
    }))
    const fromApi = await apiPoints(
      'host_cpu_used_ratio',
      id,
      stepS,
      windowMs,
      undefined,
      sent('host_cpu_used_ratio'),
    )
    for (const [name, points, was] of [
      ['UI', ui, baselineOf(idleUi)],
      ['API', fromApi, baselineOf(idleApi)],
    ] as const) {
      const peak = peakSince(points, stepS, tStart)
      if (peak === null || peak < was + rise) {
        problems.push(`${name} peak ${peak?.toFixed(3)} is below ${was.toFixed(3)} + ${rise}`)
      }
    }
    return [...problems, ...agreement(ui, fromApi, stepS)]
  })
  guard.assertClean()
})
