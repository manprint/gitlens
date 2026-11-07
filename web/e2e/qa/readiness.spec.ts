import { createHash } from 'node:crypto'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { expect, type Page } from '@playwright/test'

import { test } from '../fixtures'
import * as api from './api'
import { BUDGET, qaTimeout } from './budget'
import { assertLifecycleAllowed, assertStimulusAllowed } from './guard'
import { consoleGuard } from './observe'
import { dbContainer, query } from './pg'
import { agentYaml, composeArgs, restart, run, stackStatus, waitReady } from './stack'

/**
 * 14.3 live degrade/restore drills (RDY-E2E-001..005). Each drill degrades one
 * setting of the live stack, watches the readiness report through the API and
 * the console, then restores it. The restore runs in `finally` and again in
 * `afterEach` (a timed-out test body never reaches its `finally`); both share
 * one memoised restore, and every drill ends by comparing the stack with what
 * it recorded at its start. Assertions are relative to the recorded baseline,
 * never to a "full" report. Secrets are never printed: the compose
 * configuration is only fingerprinted.
 */
test.describe.configure({ mode: 'serial', retries: 0 })

const MONITOR_ROLE = 'pglens_monitor'
const AGENT = 'pglens-agent'
const SERVER = 'pglens-server'
const AGENT_YAML = agentYaml()
const DATA_MOUNT = '/var/lib/postgresql'
const GOOGLE_CHAT_ENV = 'PGLENS_GOOGLE_CHAT_WEBHOOK_URL'
const POLL_MS = 3_000
const HOLD_POLL_MS = 10_000

interface ReportItem {
  id: string
  severity: string
  evidence: string
  fixes: { channel: string }[]
}
interface Report {
  level: string
  counts: { warning: number; info: number }
  items: ReportItem[]
}

const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms))
const message = (error: unknown) => (error instanceof Error ? error.message : String(error))
const ids = (report: Pick<Report, 'items'>) => report.items.map((item) => item.id).sort()
const has = (report: Report, id: string) => report.items.some((item) => item.id === id)
const withItem = (base: string[], id: string) => [...new Set([...base, id])].sort()
const withoutItem = (base: string[], id: string) => base.filter((value) => value !== id)

let instancePromise: Promise<string> | undefined
const instanceId = () =>
  (instancePromise ??= api.instances().then((rows) => {
    const row = rows.find((candidate) => candidate.role === 'primary') ?? rows[0]
    if (!row) throw new Error('readiness drill: pglens knows no instance')
    return row.instance_id
  }))

const readReport = async () => api.get<Report>(`/api/v1/instances/${await instanceId()}/readiness`)

/** Polls the API until the exact set of item ids is `expected`; the report that matched. */
async function waitIds(expected: string[], budgetMs: number, what: string): Promise<Report> {
  const started = Date.now()
  let last = 'no answer'
  while (Date.now() - started < budgetMs) {
    try {
      const report = await readReport()
      last = ids(report).join(',')
      if (last === expected.join(',')) {
        console.log(`[qa/readiness] ${what}: reached after ${Date.now() - started} ms`)
        return report
      }
    } catch (error) {
      last = `API error: ${message(error)}`
    }
    await sleep(POLL_MS)
  }
  throw new Error(
    `${what}: items [${expected.join(',')}] not reached within ${budgetMs} ms; last [${last}]`,
  )
}

/** Polls until `id` is present or absent (other items tolerated). */
async function waitPresence(
  id: string,
  present: boolean,
  budgetMs: number,
  what: string,
): Promise<Report> {
  const started = Date.now()
  let last = 'no answer'
  while (Date.now() - started < budgetMs) {
    try {
      const report = await readReport()
      last = ids(report).join(',')
      if (has(report, id) === present) {
        console.log(`[qa/readiness] ${what}: ${id} reached after ${Date.now() - started} ms`)
        return report
      }
    } catch (error) {
      last = `API error: ${message(error)}`
    }
    await sleep(POLL_MS)
  }
  throw new Error(
    `${what}: ${id} not ${present ? 'present' : 'absent'} within ${budgetMs} ms; last [${last}]`,
  )
}

/** Asserts `id` keeps the given presence for the whole window, checked every 10 s and at its end. */
async function holdPresence(
  id: string,
  present: boolean,
  windowMs: number,
  what: string,
): Promise<Report> {
  const end = Date.now() + windowMs
  for (;;) {
    const report = await readReport()
    expect(has(report, id), `${what}: ${id} must stay ${present ? 'present' : 'absent'}`).toBe(
      present,
    )
    if (Date.now() >= end) return report
    await sleep(Math.min(HOLD_POLL_MS, Math.max(0, end - Date.now())))
  }
}

/**
 * A server restart (drill 5) ends the console sessions: sign in again when the
 * console answers with its login page, then return to `path`.
 */
async function signInIfAsked(page: Page, path: string): Promise<void> {
  const signIn = page.getByRole('heading', { name: 'Sign in to pglens' })
  const tab = page.getByRole('heading', { name: 'Readiness', level: 2 })
  await expect(signIn.or(tab)).toBeVisible({ timeout: 30_000 })
  if (!(await signIn.isVisible())) return
  const password = process.env.PGLENS_UI_PASSWORD
  if (!password) throw new Error('PGLENS_UI_PASSWORD is required to sign in again')
  await page.getByLabel('Password', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(signIn).toBeHidden()
  await page.goto(path)
}

/**
 * The console shows what the API shows: the Readiness tab lists exactly the
 * API's items, and the header badge on the overview matches the warning count.
 * The page is parked on about:blank afterwards so no background poll hits a
 * restarting server.
 */
async function assertUi(page: Page): Promise<void> {
  const id = encodeURIComponent(await instanceId())
  let mismatch = ''
  for (let attempt = 0; attempt < 3; attempt += 1) {
    const report = await readReport()
    await page.goto(`/instances/${id}?section=readiness`)
    await signInIfAsked(page, `/instances/${id}?section=readiness`)
    await expect(page.getByRole('heading', { name: 'Readiness', level: 2 })).toBeVisible({
      timeout: 30_000,
    })
    await expect(
      page.getByTestId('readiness-items').or(page.getByText('Monitored in full')),
    ).toBeVisible()
    const shown = (await page.getByTestId('readiness-item').locator('h3 + code').allTextContents())
      .map((text) => text.trim())
      .sort()
    if (shown.join(',') !== ids(report).join(',')) {
      mismatch = `UI [${shown.join(',')}] API [${ids(report).join(',')}]`
      await sleep(5_000)
      continue
    }
    await page.goto(`/instances/${id}?section=overview`)
    await page.waitForLoadState('networkidle')
    await expect(page.locator('[aria-busy="true"]')).toHaveCount(0, { timeout: 30_000 })
    const banner = page.getByTestId('readiness-banner')
    if (report.counts.warning === 0) {
      await expect(banner).toHaveCount(0)
    } else {
      const noun = report.counts.warning === 1 ? 'warning' : 'warnings'
      await expect(banner).toContainText(`Partial · ${report.counts.warning} ${noun}`, {
        timeout: 30_000,
      })
    }
    await page.goto('about:blank')
    return
  }
  await page.goto('about:blank')
  throw new Error(`console and API disagree: ${mismatch}`)
}

/** Waits until the compose services are healthy and the server sees a fresh agent heartbeat. */
async function settled(services: string[]): Promise<void> {
  const deadline = Date.now() + BUDGET.serviceRestart
  for (;;) {
    const rows = await stackStatus()
    const ok = services.every((name) =>
      rows.some(
        (row) => row.service === name && row.state === 'running' && row.health === 'healthy',
      ),
    )
    if (ok) break
    if (Date.now() > deadline)
      throw new Error(`services not healthy in time: ${services.join(',')}`)
    await sleep(2_000)
  }
  await waitReady(BUDGET.serviceRestart)
}

async function restartAgent(): Promise<void> {
  await restart(AGENT)
  await settled([AGENT])
}

/** `docker compose up -d --no-deps <service>`, optionally with an override file merged in. */
async function composeUp(service: string, override?: string): Promise<void> {
  await assertLifecycleAllowed()
  const files = override === undefined ? [] : ['-f', override]
  await run('docker', [...composeArgs(), ...files, 'up', '-d', '--no-deps', service], {
    timeoutMs: 300_000,
  })
  await settled([service])
}

async function composeConfig(): Promise<string> {
  return (await run('docker', [...composeArgs(), 'config', '--format', 'json'])).stdout
}

const fingerprint = (text: string) => createHash('sha256').update(text).digest('hex')

async function inspect(service: string): Promise<{
  Mounts: { Destination: string; RW: boolean }[]
  Config: { Env: string[] }
}> {
  const container = (await run('docker', [...composeArgs(), 'ps', '-q', service])).stdout.trim()
  expect(container, `container of ${service}`).not.toBe('')
  const raw = (await run('docker', ['inspect', '--format', '{{json .}}', container])).stdout
  return JSON.parse(raw) as Awaited<ReturnType<typeof inspect>>
}

/** Ground truth: a setting as a role sees it in a fresh session, through the local socket. */
async function showAs(role: string, setting: string): Promise<string> {
  const { stdout } = await run(
    'docker',
    [
      'exec',
      '-i',
      await dbContainer(),
      'psql',
      '-U',
      role,
      '-d',
      'postgres',
      '-X',
      '-A',
      '-t',
      '-q',
    ],
    { input: `SHOW ${setting};\n` },
  )
  return stdout.trim()
}

const roleSettings = () =>
  query(
    `SELECT coalesce(array_to_string(array(SELECT unnest(setconfig) ORDER BY 1), ';'), '')
       FROM pg_db_role_setting s JOIN pg_roles r ON r.oid = s.setrole
      WHERE r.rolname = '${MONITOR_ROLE}' AND s.setdatabase = 0;`,
  )

const memberships = () =>
  query(
    `SELECT roleid::regrole || '|' || grantor::regrole || '|' || admin_option || '|' ||
            inherit_option || '|' || set_option
       FROM pg_auth_members WHERE member = '${MONITOR_ROLE}'::regrole ORDER BY 1;`,
  )

/**
 * Restores registered by the drills, newest first. Each restore is memoised by
 * its owner, so the `finally` of a test and its `afterEach` share one run.
 */
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
  await runRestores()
})

interface Start {
  base: Report
  baseIds: string[]
  config: string
  agentVolumes: { target: string }[]
  mounts: string[]
}

/** The state each drill records first and compares with at its end. */
async function start(): Promise<Start> {
  assertStimulusAllowed()
  await assertLifecycleAllowed()
  const rows = await stackStatus()
  for (const name of [AGENT, SERVER]) {
    expect(
      rows.find((row) => row.service === name)?.health,
      `${name} healthy before the drill`,
    ).toBe('healthy')
  }
  const base = await readReport()
  const mounts = (await inspect(AGENT)).Mounts.map((mount) => mount.Destination).sort()
  const config = await composeConfig()
  const parsed = JSON.parse(config) as {
    services: Record<string, { volumes?: { target: string }[] }>
  }
  return {
    base,
    baseIds: ids(base),
    config: fingerprint(config),
    agentVolumes: parsed.services[AGENT]?.volumes ?? [],
    mounts,
  }
}

/** The stack is as recorded: same compose configuration, agent mounts, healthy services. */
async function assertStackOriginal(begin: Start): Promise<void> {
  expect(fingerprint(await composeConfig()), 'compose configuration back to the original').toBe(
    begin.config,
  )
  expect(
    (await inspect(AGENT)).Mounts.map((mount) => mount.Destination).sort(),
    'agent mounts back to the original',
  ).toEqual(begin.mounts)
  const rows = await stackStatus()
  for (const name of [AGENT, SERVER]) {
    expect(rows.find((row) => row.service === name)?.health, `${name} healthy at the end`).toBe(
      'healthy',
    )
  }
}

test('RDY-E2E-001 track_io_timing off for the monitoring role raises RDY-SET-02, restore clears it @lifecycle @indicators', async ({
  signedInPage: page,
}) => {
  test.setTimeout(qaTimeout(Array<number>(4).fill(BUDGET.readinessHourly), 2))
  const browser = consoleGuard(page)
  const begin = await start()
  const target = 'RDY-SET-02'
  expect(has(begin.base, target), `${target} absent at baseline`).toBe(false)
  const originalSettings = await roleSettings()
  expect(originalSettings).not.toContain('track_io_timing')
  expect(await showAs(MONITOR_ROLE, 'track_io_timing')).toBe('on')

  const restore = registerRestore('track_io_timing', async () => {
    await query(`ALTER ROLE ${MONITOR_ROLE} RESET track_io_timing;`)
    await restartAgent()
  })
  try {
    // Control: nothing changed, no item for the whole budget.
    await holdPresence(target, false, BUDGET.readinessHourly, 'control')

    await query(`ALTER ROLE ${MONITOR_ROLE} SET track_io_timing = off;`)
    await restartAgent()
    expect(await showAs(MONITOR_ROLE, 'track_io_timing'), 'monitoring role sees off').toBe('off')
    expect(await query('SHOW track_io_timing;'), 'superuser still on').toBe('on')
    const degraded = await waitIds(
      withItem(begin.baseIds, target),
      BUDGET.readinessHourly,
      'appear',
    )
    const item = degraded.items.find((candidate) => candidate.id === target)
    expect(item?.evidence).toContain('track_io_timing = off')
    expect(item?.evidence).not.toContain('command line')
    expect(degraded.counts.warning).toBe(begin.base.counts.warning + 1)
    await assertUi(page)

    // Sabotage: restore withheld, the item stays for the whole budget.
    await holdPresence(target, true, BUDGET.readinessHourly, 'sabotage')

    await restore()
    expect(await showAs(MONITOR_ROLE, 'track_io_timing'), 'monitoring role back on').toBe('on')
    expect(await query('SHOW track_io_timing;')).toBe('on')
    expect(await roleSettings(), 'role settings back to the original').toBe(originalSettings)
    await waitIds(begin.baseIds, BUDGET.readinessHourly, 'clear')
    await assertUi(page)
  } finally {
    await runRestores()
  }
  await assertStackOriginal(begin)
  browser.assertClean()
})

test('RDY-E2E-002 revoking pg_monitor raises RDY-ROLE-01, granting it back resolves @lifecycle @indicators', async ({
  signedInPage: page,
}) => {
  test.setTimeout(qaTimeout(Array<number>(4).fill(BUDGET.readinessHourly), 2))
  const browser = consoleGuard(page)
  const begin = await start()
  const target = 'RDY-ROLE-01'
  expect(has(begin.base, target), `${target} absent at baseline`).toBe(false)
  const originalMemberships = await memberships()
  expect(originalMemberships).toContain('pg_monitor|')

  const restore = registerRestore('pg_monitor', async () => {
    const rows = await memberships()
    if (!rows.includes('pg_monitor|')) await query(`GRANT pg_monitor TO ${MONITOR_ROLE};`)
    await restartAgent()
  })
  try {
    await holdPresence(target, false, BUDGET.readinessHourly, 'control')

    await query(`REVOKE pg_monitor FROM ${MONITOR_ROLE};`)
    await restartAgent()
    expect(await memberships(), 'pg_monitor revoked').not.toContain('pg_monitor|')
    // pg_read_all_settings goes with pg_monitor, so other items may appear too: tolerated.
    const degraded = await waitPresence(target, true, BUDGET.readinessHourly, 'appear')
    expect(degraded.items.find((item) => item.id === target)?.evidence).toContain('pg_monitor')
    expect(ids(degraded)).toEqual(expect.arrayContaining(begin.baseIds))
    await assertUi(page)

    await holdPresence(target, true, BUDGET.readinessHourly, 'sabotage')

    await restore()
    expect(await memberships(), 'memberships back to the original').toBe(originalMemberships)
    await waitIds(begin.baseIds, BUDGET.readinessHourly, 'clear')
    await assertUi(page)
  } finally {
    await runRestores()
  }
  await assertStackOriginal(begin)
  browser.assertClean()
})

test('RDY-E2E-003 dropping the data directory mount raises RDY-HOST-01, restoring it resolves @lifecycle @indicators', async ({
  signedInPage: page,
}) => {
  test.setTimeout(qaTimeout(Array<number>(4).fill(BUDGET.readinessFlags), 2))
  const browser = consoleGuard(page)
  const begin = await start()
  const target = 'RDY-HOST-01'
  expect(has(begin.base, target), `${target} absent at baseline`).toBe(false)

  const volumes = begin.agentVolumes
  expect(volumes.length, 'agent volumes in the compose configuration').toBeGreaterThan(0)
  const kept = volumes.filter((volume) => volume.target !== DATA_MOUNT)
  expect(volumes.length - kept.length, 'exactly the data directory entry is dropped').toBe(1)
  expect(kept.map((volume) => volume.target).sort()).toEqual(
    ['/etc/pglens/agent.yaml', '/host/proc', '/host/sys', '/var/lib/pglens'].sort(),
  )
  expect(begin.mounts).toContain(DATA_MOUNT)

  const dir = await mkdtemp(join(tmpdir(), 'qa-rdy-'))
  const override = join(dir, 'agent-no-data-mount.yml')
  await writeFile(
    override,
    [
      'services:',
      `  ${AGENT}:`,
      '    volumes: !override',
      ...kept.map((volume) => `      - ${JSON.stringify(volume)}`),
      '',
    ].join('\n'),
  )
  const restore = registerRestore('agent data mount', () => composeUp(AGENT))
  try {
    await holdPresence(target, false, BUDGET.readinessFlags, 'control')

    await composeUp(AGENT, override)
    expect(
      (await inspect(AGENT)).Mounts.map((mount) => mount.Destination),
      'agent no longer mounts the data directory',
    ).not.toContain(DATA_MOUNT)
    const degraded = await waitIds(withItem(begin.baseIds, target), BUDGET.readinessFlags, 'appear')
    expect(degraded.items.find((item) => item.id === target)?.evidence).toContain('data directory')
    await assertUi(page)

    await holdPresence(target, true, BUDGET.readinessFlags, 'sabotage')

    await restore()
    const mount = (await inspect(AGENT)).Mounts.find((entry) => entry.Destination === DATA_MOUNT)
    expect(mount, 'data directory mounted again').toBeDefined()
    expect(mount?.RW, 'and read-only').toBe(false)
    await waitIds(begin.baseIds, BUDGET.readinessFlags, 'clear')
    await assertUi(page)
  } finally {
    try {
      await runRestores()
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  }
  await assertStackOriginal(begin)
  browser.assertClean()
})

test('RDY-E2E-004 checks.activity.by_application true then false toggles RDY-ACT-01, original file restored @lifecycle @indicators', async ({
  signedInPage: page,
}) => {
  test.setTimeout(qaTimeout(Array<number>(4).fill(BUDGET.readinessFlags), 3))
  const browser = consoleGuard(page)
  const begin = await start()
  const target = 'RDY-ACT-01'
  const original = await readFile(AGENT_YAML, 'utf8')
  expect(original, 'key absent in the stack agent.yaml').not.toContain('by_application')
  const withKey = (value: boolean) => {
    const edited = original.replace(
      /^(\s*activity:\s*\{)([^}]*)\}/m,
      (_all, open: string, body: string) =>
        `${open} ${body.trim()}, by_application: ${String(value)} }`,
    )
    expect(edited, 'activity block found in agent.yaml').not.toBe(original)
    return edited
  }

  // Written in place (same inode): agent.yaml is bind-mounted into the agent.
  const restore = registerRestore('agent.yaml', async () => {
    await writeFile(AGENT_YAML, original)
    await restartAgent()
  })
  try {
    // Control: explicit true is the healthy state, whatever the code default is.
    await writeFile(AGENT_YAML, withKey(true))
    await restartAgent()
    await waitIds(withoutItem(begin.baseIds, target), BUDGET.readinessFlags, 'control (true)')
    await assertUi(page)

    await writeFile(AGENT_YAML, withKey(false))
    await restartAgent()
    const degraded = await waitIds(
      withItem(begin.baseIds, target),
      BUDGET.readinessFlags,
      'appear (false)',
    )
    expect(degraded.items.find((item) => item.id === target)?.evidence).toContain(
      'checks.activity.by_application is false',
    )
    await assertUi(page)

    await holdPresence(target, true, BUDGET.readinessFlags, 'sabotage')

    await restore()
    expect(await readFile(AGENT_YAML, 'utf8'), 'agent.yaml back to the original').toBe(original)
    await waitIds(begin.baseIds, BUDGET.readinessFlags, 'restore')
    await assertUi(page)
  } finally {
    await runRestores()
  }
  await assertStackOriginal(begin)
  browser.assertClean()
})

test('RDY-E2E-005 adding a notification channel to the server clears RDY-NOTIF-01, removing it brings it back @lifecycle @indicators', async ({
  signedInPage: page,
}) => {
  test.setTimeout(qaTimeout(Array<number>(4).fill(BUDGET.readinessFlags), 2))
  // A server restart ends the in-memory console session: until the page signs in again,
  // the session check and the header's firing-alerts poll get 401, and the sign-in
  // navigation aborts that poll.
  const browser = consoleGuard(page, [
    { url: /\/api\/v1\/session$/, status: 401 },
    { url: /\/api\/v1\/alerts\?state=firing$/, status: 401, aborted: true },
  ])
  const begin = await start()
  const target = 'RDY-NOTIF-01'
  // The channel value is never printed: only whether it is set.
  const channelSet = async () =>
    (await inspect(SERVER)).Config.Env.some(
      (entry) =>
        entry.startsWith(`${GOOGLE_CHAT_ENV}=`) && entry.trim().length > GOOGLE_CHAT_ENV.length + 1,
    )
  // Baseline either way: with no channel the drill adds a dummy one, with the stack's
  // channel set (Google Chat test channel, 2026-11-06) it blanks the variable instead.
  const configured = await channelSet()
  expect(
    has(begin.base, target),
    `${target} present at baseline exactly when no channel is configured in the server environment`,
  ).toBe(!configured)

  // A dummy address on a reserved host, assembled at run time; never a real secret.
  const dummy = [
    'https://chat',
    '.invalid/v1/spaces/',
    'AAAAAAAAAA',
    '/messages?key=',
    'KKKKKKKKKK',
    '&token=',
    'TTTTTTTTTT',
  ].join('')
  const dir = await mkdtemp(join(tmpdir(), 'qa-rdy-'))
  const override = join(dir, 'server-chat-channel.yml')
  await writeFile(
    override,
    [
      'services:',
      `  ${SERVER}:`,
      '    environment:',
      `      ${GOOGLE_CHAT_ENV}: ${JSON.stringify(configured ? '' : dummy)}`,
      '',
    ].join('\n'),
  )
  const changed = configured ? withItem(begin.baseIds, target) : withoutItem(begin.baseIds, target)
  const restore = registerRestore('server environment', () => composeUp(SERVER))
  try {
    // Control: nothing changed, the baseline state of the item holds for the whole budget.
    await holdPresence(target, !configured, BUDGET.readinessFlags, 'control')

    await composeUp(SERVER, override)
    expect(await channelSet(), 'server channel variable toggled').toBe(!configured)
    await waitIds(changed, BUDGET.readinessFlags, configured ? 'channel removed' : 'channel added')
    await assertUi(page)

    // Sabotage: restore withheld, the toggled state holds for the whole budget.
    await holdPresence(target, configured, BUDGET.readinessFlags, 'sabotage')

    await restore()
    expect(await channelSet(), 'server channel variable back to the baseline').toBe(configured)
    await waitIds(begin.baseIds, BUDGET.readinessFlags, 'baseline again')
    await assertUi(page)
  } finally {
    try {
      await runRestores()
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  }
  await assertStackOriginal(begin)
  browser.assertClean()
})
