import { copyFile, readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'

import { expect, type Page } from '@playwright/test'

import { test } from '../fixtures'
import * as api from './api'
import { BUDGET, qaTimeout } from './budget'
import { assertLifecycleAllowed, assertStimulusAllowed, STACK_DIR } from './guard'
import { consoleGuard } from './observe'
import { lockPair, openSessions, query, registerCleanup, runCleanups } from './pg'
import {
  type ApiLocks,
  compare,
  compareBaseline,
  compareTotals,
  EXACT_APPS,
  odooCount,
  originOf,
  renamed,
  truthSessions,
  type TruthSession,
} from './sessionTruth'
import { restart, stackStatus, waitReady } from './stack'

/**
 * 16.4 live proof of the session origin against `pg_stat_activity` (SORIGIN-E2E-001..003).
 * The ground truth is read through the harness, the API sample and the console
 * tables are compared with it, each case has a control run and a sabotage run, and
 * the cases that change the stack agent's `agent.yaml` restore it in `finally` and
 * again in `afterEach` (one memoised restore), then compare it byte for byte with
 * what they recorded. Only the four named QA applications are compared exactly;
 * the totals carry the measured Odoo-worker drift (`compareTotals`).
 */
test.describe.configure({ mode: 'serial', retries: 0 })

const AGENT = 'pglens-agent'
const AGENT_YAML = join(STACK_DIR, 'agent.yaml')
const AGENT_YAML_BACKUP = join(STACK_DIR, 'agent.yaml.bak-16.4')
const POLL_MS = 3_000
const LONG_SLEEP_S = 150
/** The agent's own first-run delay after a restart bounds the first sample (BUDGET.check15m). */
const AFTER_RESTART = BUDGET.check15m

const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms))
const message = (error: unknown) => (error instanceof Error ? error.message : String(error))

let instancePromise: Promise<string> | undefined
const instanceId = () =>
  (instancePromise ??= api.instances().then((rows) => {
    const row = rows.find((candidate) => candidate.role === 'primary') ?? rows[0]
    if (!row) throw new Error('sessions proof: pglens knows no instance')
    return row.instance_id
  }))

const locks = async () =>
  api.get<ApiLocks>(`/api/v1/locks?instance_id=${encodeURIComponent(await instanceId())}`)

/** Polls `check` until it returns no problem; throws the last problems after `budgetMs`. */
async function until(
  check: () => Promise<string[]>,
  budgetMs: number,
  what: string,
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
      console.log(`[qa/sessions] ${what}: reached after ${Date.now() - started} ms`)
      return
    }
    await sleep(POLL_MS)
  }
  throw new Error(`${what}: not reached within ${budgetMs} ms; last: ${last.join('; ')}`)
}

interface Bracket {
  before: TruthSession[]
  after: TruthSession[]
  api: ApiLocks
}

/** One truth read, one API read, one truth read: the API sample lies between the truths. */
async function bracket(): Promise<Bracket> {
  const before = await truthSessions()
  const response = await locks()
  const after = await truthSessions()
  return { before, after, api: response }
}

/** The sample against the truth: the exact QA sessions, then the totals. */
const problemsOf = (read: Bracket) => [
  ...compare(read.after, read.api),
  ...compareTotals(read.before, read.after, read.api),
]

/** Rows of the table `name` as records keyed by column header. */
async function records(
  page: Page,
  origin: 'application' | 'pglens',
): Promise<Record<string, string>[]> {
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

/** Waits for the table of `origin` to show exactly `count` data rows. */
const expectRows = (page: Page, origin: 'application' | 'pglens', count: number, timeout: number) =>
  expect.poll(async () => (await records(page, origin)).length, { timeout }).toBe(count)

type Origin = 'application' | 'pglens'

async function openOrigin(page: Page, origin: Origin, search: string): Promise<void> {
  const id = encodeURIComponent(await instanceId())
  await page.goto(`/instances/${id}/locks?view=${origin}`)
  const section = page.locator(`[data-origin-table="${origin}"]`)
  await expect(section).toBeVisible({ timeout: 30_000 })
  const box = section.getByRole('searchbox')
  if ((await box.count()) > 0) await box.fill(search)
}

// agent.yaml: the stack's own file, restored after every case that edits it.
const restorers: { name: string; restore: () => Promise<void> }[] = []

function registerRestore(name: string, action: () => Promise<void>): () => Promise<void> {
  let running: Promise<void> | undefined
  const restore = () => (running ??= action())
  restorers.push({ name, restore })
  return restore
}

async function runRestores(): Promise<void> {
  const errors: string[] = []
  while (restorers.length > 0) {
    const entry = restorers.pop()
    try {
      await entry?.restore()
    } catch (error) {
      errors.push(`${entry?.name ?? '?'}: ${message(error)}`)
    }
  }
  if (errors.length > 0) {
    throw new Error(`RESTORE FAILED, THE STACK MAY BE DEGRADED: ${errors.join('; ')}`)
  }
}

test.afterEach(async () => {
  try {
    await runCleanups()
  } finally {
    await runRestores()
  }
})

async function restartAgent(): Promise<void> {
  await restart(AGENT)
  const deadline = Date.now() + BUDGET.serviceRestart
  for (;;) {
    const rows = await stackStatus()
    if (rows.some((row) => row.service === AGENT && row.health === 'healthy')) break
    if (Date.now() > deadline) throw new Error('the agent is not healthy after its restart')
    await sleep(2_000)
  }
  await waitReady(BUDGET.serviceRestart)
}

/**
 * Edits the stack agent config, records the original (also on disk next to it so
 * a killed run can be recovered by hand), and registers the memoised restore that
 * writes the original back, restarts the agent and compares the bytes.
 */
async function editAgentConfig(
  edit: (text: string) => string,
): Promise<{ original: string; apply: () => Promise<void>; restore: () => Promise<void> }> {
  await assertLifecycleAllowed()
  const original = await readFile(AGENT_YAML, 'utf8')
  await copyFile(AGENT_YAML, AGENT_YAML_BACKUP)
  const edited = edit(original)
  expect(edited, 'the edit changes agent.yaml').not.toBe(original)
  let applied = false
  const restore = registerRestore('agent.yaml', async () => {
    if (!applied) return
    await writeFile(AGENT_YAML, original)
    applied = false
    await restartAgent()
    expect(await readFile(AGENT_YAML, 'utf8'), 'agent.yaml back to the recorded original').toBe(
      original,
    )
  })
  return {
    original,
    apply: async () => {
      applied = true
      await writeFile(AGENT_YAML, edited)
      await restartAgent()
    },
    restore,
  }
}

test('SORIGIN-E2E-001 the sample, the API summary and both tables agree with pg_stat_activity, by origin @indicators', async ({
  signedInPage: page,
}) => {
  test.setTimeout(qaTimeout([BUDGET.activity, BUDGET.activity, BUDGET.activity]))
  assertStimulusAllowed()
  const guard = consoleGuard(page)
  await page.setViewportSize({ width: 1920, height: 1080 })

  const baseline = await truthSessions()
  expect(
    baseline
      .filter((row) => row.application_name.startsWith('qa-'))
      .map((row) => row.application_name),
    'no leftover QA session before the run',
  ).toEqual([])

  // N = 7: qa-app-a ×3, qa-app-b ×2, the lock holder (idle in transaction) and its waiter.
  const appA = await openSessions(3, { applicationName: 'qa-app-a' })
  const appB = await openSessions(2, { applicationName: 'qa-app-b' })
  const pair = await lockPair('sorigin', 1, 'qa-idle-tx', 'qa-waiter')

  let matched: Bracket | undefined
  await until(
    async () => {
      const read = await bracket()
      const problems = problemsOf(read)
      if (problems.length === 0) matched = read
      return problems
    },
    BUDGET.activity,
    'sample equals pg_stat_activity',
  )
  if (!matched) throw new Error('no matching sample was recorded')
  const { after: truth, api: sample } = matched
  test.info().annotations.push({
    type: 'SORIGIN-E2E-001 totals',
    description: `truth ${JSON.stringify(truth.length)} sessions, odoo workers ${odooCount(truth)}, sampled_at ${sample.sampled_at}, truncated ${sample.connections?.truncated}`,
  })
  const qa = truth.filter((row) => (EXACT_APPS as readonly string[]).includes(row.application_name))
  expect(qa).toHaveLength(7)

  // Console, application table: exactly the seven QA sessions, none of them pglens.
  await openOrigin(page, 'application', 'qa-')
  await expect
    .poll(async () => (await records(page, 'application')).length, {
      timeout: BUDGET.activity,
    })
    .toBe(7)
  const shown = await records(page, 'application')
  expect(shown.map((row) => Number(row.PID)).sort((a, b) => a - b)).toEqual(
    qa.map((row) => row.pid).sort((a, b) => a - b),
  )
  for (const row of shown) {
    const want = qa.find((entry) => entry.pid === Number(row.PID))
    expect(row.Application, `pid ${row.PID}`).toBe(want?.application_name)
  }
  await page.screenshot({ path: test.info().outputPath('sorigin-e2e-001-application.png') })

  // No pglens-origin session in the application table, no QA session in the pglens table.
  await openOrigin(page, 'application', 'pglens')
  await expectRows(page, 'application', 0, 30_000)
  await openOrigin(page, 'pglens', 'qa-')
  await expectRows(page, 'pglens', 0, 30_000)
  await openOrigin(page, 'pglens', '')
  const monitoring = await records(page, 'pglens')
  expect(monitoring.length, 'the pglens table lists the agent sessions').toBeGreaterThan(0)
  for (const row of monitoring) {
    expect(
      originOf(row.Application ?? '', row.User ?? null),
      `pglens table row ${row.PID} ${row.Application}`,
    ).toBe('pglens')
  }

  // The blocked/blocking pair is in the blocking tree with the app badge.
  const holder = qa.find((row) => row.application_name === 'qa-idle-tx')
  const waiter = qa.find((row) => row.application_name === 'qa-waiter')
  if (!holder || !waiter) throw new Error('the lock pair is missing from the truth')
  await page.goto(`/instances/${encodeURIComponent(await instanceId())}/locks`)
  const tree = page.getByRole('tree', { name: 'Blocking lock tree' })
  await expect(tree).toBeVisible({ timeout: 30_000 })
  const expand = page.getByRole('button', { name: `Expand lock session ${holder.pid}` })
  if (await expand.isVisible()) await expand.click()
  for (const pid of [holder.pid, waiter.pid]) {
    const item = tree
      .getByRole('treeitem')
      .filter({ hasText: `PID ${pid}` })
      .first()
    await expect(item, `tree item of ${pid}`).toBeVisible({ timeout: BUDGET.activity })
    await expect(item.locator('[data-origin="application"]').first()).toHaveText('app')
  }
  await page.screenshot({ path: test.info().outputPath('sorigin-e2e-001-tree.png') })

  // Sabotage: rewriting a qa-app-a session as pglens/qa in the truth must be caught.
  const sabotaged = compare(renamed(truth, 'qa-app-a', 'pglens/qa'), sample)
  expect(sabotaged, 'the comparison detects a rewritten qa-app-a session').not.toEqual([])
  const rewritten = renamed(truth, 'qa-app-a', 'pglens/qa').filter(
    (row) => row.application_name === 'pglens/qa',
  )
  expect(rewritten, 'the sabotage really rewrote the three sessions').toHaveLength(3)
  expect(originOf('pglens/qa', null)).toBe('pglens')

  // Control: close the QA sessions; the sample returns to the baseline (± the Odoo workers).
  await appA.close()
  await appB.close()
  await pair.release()
  let settled: Bracket | undefined
  await until(
    async () => {
      const read = await bracket()
      const leftovers = (read.api.sessions ?? []).filter((row) =>
        (row.application_name ?? '').startsWith('qa-'),
      )
      const problems = [
        ...(leftovers.length > 0 ? [`${leftovers.length} QA sessions still in the sample`] : []),
        ...compareTotals(read.before, read.after, read.api),
        ...compareBaseline(baseline, read.before, read.after, read.api),
      ]
      if (problems.length === 0) settled = read
      return problems
    },
    BUDGET.activity,
    'control: sample back at the baseline, QA sessions gone',
  )
  expect(settled, 'the control sample is recorded').toBeDefined()
  // The control comparison is real: the sample taken while the seven QA sessions were open is not at the baseline.
  expect(
    compareBaseline(baseline, matched.before, matched.after, matched.api),
    'the baseline comparison detects the seven QA sessions of the open sample',
  ).not.toEqual([])
  await openOrigin(page, 'application', 'qa-')
  await expectRows(page, 'application', 0, BUDGET.activity)
  guard.assertClean()
})

/** Long statement as `qa-longq`: never awaited by the caller; ended by the cleanup. */
function startLongStatement(): { done: Promise<void>; stop: () => Promise<void> } {
  const done = query(`select pg_sleep(${LONG_SLEEP_S});\n`, 'qa-longq').then(
    () => undefined,
    () => undefined,
  )
  const stop = async () => {
    await query(
      `SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity WHERE application_name = 'qa-longq' AND pid <> pg_backend_pid();`,
    )
    await done
  }
  registerCleanup(stop)
  return { done, stop }
}

async function longSession(read: ApiLocks) {
  return (read.sessions ?? []).find((row) => row.application_name === 'qa-longq')
}

test('SORIGIN-E2E-002 a long statement shows its query age, wait event and normalised or raw text @indicators @lifecycle', async ({
  signedInPage: page,
}) => {
  test.setTimeout(LONG_SLEEP_S * 1000 * 3 + 2 * BUDGET.serviceRestart + 60_000)
  assertStimulusAllowed()
  await assertLifecycleAllowed()
  const guard = consoleGuard(page)
  await page.setViewportSize({ width: 1920, height: 1080 })

  // Warm-up: the integer-literal form `select pg_sleep(1)` shares the queryid of the long one.
  await query('select pg_sleep(1);\nselect pg_sleep(1);\nselect pg_sleep(1);\n', 'qa-longq')
  const queryid = (
    await query(
      `SELECT queryid::text FROM pg_stat_statements WHERE query = 'select pg_sleep($1)' ORDER BY calls DESC LIMIT 1;`,
      'qa-harness',
      'postgres',
    )
  ).trim()
  expect(queryid, 'pg_stat_statements has the warm-up statement').toMatch(/^-?\d+$/)
  await until(
    async () => {
      const id = encodeURIComponent(await instanceId())
      const body = await api.get<{ statements: { queryid: string; query_text?: string }[] }>(
        `/api/v1/statements?instance_id=${id}&q=${queryid}&limit=5`,
      )
      return body.statements.some(
        (row) => row.queryid === queryid && (row.query_text ?? '').includes('pg_sleep'),
      )
        ? []
        : [`no query_texts row for queryid ${queryid} yet`]
    },
    150_000,
    'query_texts holds the warm-up statement (pg_stat_statements is scraped every 60 s)',
  )

  const detailOf = async (pid: number | string) => {
    const section = page.locator('[data-origin-table="application"]')
    await section.getByRole('button', { name: `Show details of session ${pid}` }).click()
    return page.getByTestId('session-detail')
  }
  const readDetail = async (pid: number | string): Promise<Record<string, string>> => {
    const detail = await detailOf(pid)
    await expect(detail).toBeVisible()
    return detail.evaluate((node) =>
      Object.fromEntries(
        Array.from(node.querySelectorAll('dl > div')).map((field) => [
          (field.querySelector('dt')?.textContent ?? '').trim(),
          (field.querySelector('dd')?.textContent ?? '').trim(),
        ]),
      ),
    )
  }

  const configEdit = await editAgentConfig((text) =>
    text.replace('locks: { interval: 10s, session_query_text: true }', 'locks: { interval: 10s }'),
  )

  try {
    // Raw text on (the dev stack's opt-in, the recorded baseline): raw text with its copy button.
    let long = startLongStatement()
    let seen: NonNullable<Awaited<ReturnType<typeof longSession>>> | undefined
    await until(
      async () => {
        const row = await longSession(await locks())
        if (!row) return ['qa-longq is not in the sample']
        if (row.state !== 'active') return [`qa-longq state ${row.state}`]
        if (row.query_id !== queryid) return [`query_id ${row.query_id}, want ${queryid}`]
        if (row.query_text_source !== 'raw') return [`text source ${row.query_text_source}`]
        seen = row
        return []
      },
      BUDGET.activity + BUDGET.statStatements,
      'raw leg: the sample carries the running statement with raw text',
    )
    expect(seen?.query_text).toContain(`pg_sleep(${LONG_SLEEP_S})`)
    expect(seen?.wait_event).toBe('PgSleep')
    await openOrigin(page, 'application', 'qa-longq')
    await expectRows(page, 'application', 1, BUDGET.activity)
    let fields = await readDetail(seen?.pid ?? 0)
    expect(fields['Wait event']).toBe('PgSleep')
    expect(fields['Query ID']).toBe(queryid)
    expect(fields['Query age']).not.toBe('—')
    expect(fields.Application).toBe('qa-longq')
    await expect(page.getByText('Query text (raw)')).toBeVisible()
    await expect(
      page.getByTestId('session-detail').getByRole('button', { name: /^Copy / }),
    ).toBeVisible()
    await expect(page.getByTestId('session-detail')).toContainText(`pg_sleep(${LONG_SLEEP_S})`)
    await page.screenshot({ path: test.info().outputPath('sorigin-e2e-002-raw.png') })
    await long.stop()

    // Raw text off (the key removed, agent restarted): only the normalised text, by query id.
    await configEdit.apply()
    long = startLongStatement()
    await until(
      async () => {
        const row = await longSession(await locks())
        if (!row) return ['qa-longq is not in the sample']
        if (row.query_id !== queryid) return [`query_id ${row.query_id}, want ${queryid}`]
        if (row.query_text_source !== 'normalised') return [`text source ${row.query_text_source}`]
        seen = row
        return []
      },
      AFTER_RESTART + BUDGET.statStatements,
      'normalised leg: raw text is absent, the normalised text resolves by query id',
    )
    expect(seen?.query_text).toBe('select pg_sleep($1)')
    await openOrigin(page, 'application', 'qa-longq')
    await expectRows(page, 'application', 1, BUDGET.activity)
    fields = await readDetail(seen?.pid ?? 0)
    expect(fields['Query ID']).toBe(queryid)
    await expect(page.getByText('Query text (normalised)')).toBeVisible()
    await expect(page.getByText('Query text (raw)')).toHaveCount(0)
    await expect(page.getByTestId('session-detail')).toContainText('select pg_sleep($1)')
    await page.screenshot({ path: test.info().outputPath('sorigin-e2e-002-normalised.png') })
    await long.stop()
  } finally {
    await configEdit.restore()
  }
  guard.assertClean()
})

test('SORIGIN-E2E-003 checks.activity.by_application is on by default and its opt-out shows the callout @indicators @lifecycle', async ({
  signedInPage: page,
}) => {
  test.setTimeout(3 * BUDGET.activity + 3 * BUDGET.serviceRestart + 60_000)
  assertStimulusAllowed()
  await assertLifecycleAllowed()
  const guard = consoleGuard(page)
  await page.setViewportSize({ width: 1920, height: 1080 })
  const id = encodeURIComponent(await instanceId())
  const activityView = `/instances/${id}/locks?view=activity`

  const original = await readFile(AGENT_YAML, 'utf8')
  expect(original, 'the baseline config carries no by_application key').not.toContain(
    'by_application',
  )

  // Default: the key is absent, the per-application counts are present, no callout.
  await until(
    async () => {
      const read = (await locks()).connections
      if (!read) return ['no connections summary']
      if (!read.by_application_enabled) return ['by_application_enabled is false']
      return read.by_application.length > 0 ? [] : ['by_application is empty']
    },
    BUDGET.activity,
    'default: per-application counts are present',
  )
  await page.goto(activityView)
  await expect(page.getByRole('list', { name: 'Connections by application' }).first()).toBeVisible({
    timeout: 30_000,
  })
  await expect(
    page.getByText('This breakdown is opt-in and was not reported by the agent.'),
  ).toHaveCount(0)
  await page.screenshot({ path: test.info().outputPath('sorigin-e2e-003-default.png') })

  // Opt-out: `by_application: false`, agent restarted: the callout names the key.
  const configEdit = await editAgentConfig((text) =>
    text.replace(
      'activity: { interval: 10s }',
      'activity: { interval: 10s, by_application: false }',
    ),
  )
  try {
    await configEdit.apply()
    await until(
      async () => {
        const read = (await locks()).connections
        if (!read) return ['no connections summary']
        return read.by_application_enabled ? ['by_application_enabled is still true'] : []
      },
      AFTER_RESTART,
      'opt-out: the server reports by_application_enabled false',
    )
    await page.goto(activityView)
    const callout = page.getByText('checks.activity.by_application').first()
    await expect(callout).toBeVisible({ timeout: BUDGET.activity })
    await expect(
      page.getByText('This breakdown is opt-in and was not reported by the agent.'),
    ).toBeVisible()
    await page.screenshot({ path: test.info().outputPath('sorigin-e2e-003-optout.png') })
  } finally {
    await configEdit.restore()
  }

  // Restored: the counts and the absence of the callout are back.
  await until(
    async () => {
      const read = (await locks()).connections
      if (!read) return ['no connections summary']
      return read.by_application_enabled && read.by_application.length > 0
        ? []
        : ['per-application counts not back yet']
    },
    AFTER_RESTART,
    'restored: per-application counts are back',
  )
  await page.goto(activityView)
  await expect(
    page.getByText('This breakdown is opt-in and was not reported by the agent.'),
  ).toHaveCount(0, {
    timeout: BUDGET.activity,
  })
  expect(await readFile(AGENT_YAML, 'utf8')).toBe(original)
  guard.assertClean()
})
