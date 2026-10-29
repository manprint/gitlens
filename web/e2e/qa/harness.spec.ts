import { mkdtemp, mkdir, readdir, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createHash } from 'node:crypto'

import { expect, test } from '@playwright/test'

import * as api from './api'
import { BUDGET, qaTimeout } from './budget'
import { assertLifecycleAllowed, findVerifiedBackup, sqlIdent, sqlInt } from './guard'
import * as odoo from './odoo'
import {
  lockPair,
  openSessions,
  query,
  queryInt,
  runCleanups,
  sessionCount,
  slowQuery,
  truth,
} from './pg'
import { newRunId, RUN_ID_PATTERN } from './runid'
import { BASE_SERVICES, stackStatus, stop, upArgs, waitReady } from './stack'

test.describe.configure({ mode: 'serial', retries: 0 })
test.setTimeout(120_000)

const appName = (suffix: string) => `qa-${newRunId().slice(3)}-${suffix}`.slice(0, 53)

test.afterEach(async () => {
  await runCleanups()
})

test('QA-HARNESS-001 lifecycle actions are refused without PGLENS_QA_ALLOW_LIFECYCLE=1', async () => {
  const saved = process.env.PGLENS_QA_ALLOW_LIFECYCLE
  delete process.env.PGLENS_QA_ALLOW_LIFECYCLE
  try {
    await expect(stop('pglens-agent')).rejects.toThrow(/PGLENS_QA_ALLOW_LIFECYCLE=1/)
  } finally {
    if (saved !== undefined) process.env.PGLENS_QA_ALLOW_LIFECYCLE = saved
  }
  const agent = (await stackStatus()).find((row) => row.service === 'pglens-agent')
  expect(agent?.state).toBe('running')
})

test('QA-HARNESS-002 lifecycle actions are refused for another compose project', async () => {
  await expect(
    assertLifecycleAllowed({ PGLENS_QA_ALLOW_LIFECYCLE: '1' }, 'other-project'),
  ).rejects.toThrow(/compose project "other-project" is not pglens-odoo/)
})

test('QA-HARNESS-003 lifecycle actions need a VERIFIED backup younger than 24 h', async () => {
  const root = await mkdtemp(join(tmpdir(), 'qa-backups-'))
  try {
    const now = new Date()
    expect(await findVerifiedBackup(root, now)).toBeNull()
    await expect(
      assertLifecycleAllowed({ PGLENS_QA_ALLOW_LIFECYCLE: '1' }, 'pglens-odoo', root),
    ).rejects.toThrow(/no VERIFIED backup/)

    const make = async (
      name: string,
      created: Date | null,
      sums = 'abc  odoo.dump\n',
      recorded?: string,
    ) => {
      const dir = join(root, name)
      await mkdir(dir)
      await writeFile(join(dir, 'SHA256SUMS'), sums)
      if (created) {
        const hash = recorded ?? createHash('sha256').update(sums).digest('hex')
        await writeFile(join(dir, 'VERIFIED'), `${created.toISOString()}\n${hash}\n`)
      }
    }
    await make('no-verified', null)
    await make('too-old', new Date(now.getTime() - 25 * 3600_000))
    await make(
      'hash-differs',
      new Date(now.getTime() - 3600_000),
      'abc  odoo.dump\n',
      'f'.repeat(64),
    )
    expect(await findVerifiedBackup(root, now)).toBeNull()

    await make('good', new Date(now.getTime() - 3600_000))
    expect((await findVerifiedBackup(root, now))?.dir).toBe(join(root, 'good'))
  } finally {
    await rm(root, { recursive: true, force: true })
  }
})

test('QA-HARNESS-004 pg.query reaches the monitored database', async () => {
  expect(await query('SELECT 1;')).toBe('1')
})

test('QA-HARNESS-005 openSessions holds exactly n tagged sessions until cleanup', async () => {
  const name = appName('sess')
  await openSessions(3, { applicationName: name })
  expect(await sessionCount(name)).toBe(3)
  await runCleanups()
  expect(await sessionCount(name)).toBe(0)
})

test('QA-HARNESS-006 lockPair blocks a waiter until cleanup releases it', async () => {
  const name = appName('lock')
  await lockPair('lock_target', 1, name)
  const blocked = `SELECT count(*) FROM pg_stat_activity WHERE application_name = '${name}' AND cardinality(pg_blocking_pids(pid)) > 0;`
  expect(await queryInt(blocked)).toBe(1)
  await runCleanups()
  expect(await queryInt(blocked)).toBe(0)
  expect(await sessionCount(name)).toBe(0)
})

test('QA-HARNESS-007 slowQuery is visible in pg_stat_statements', async () => {
  const before = await truth.statementCalls('%pg_sleep%')
  await slowQuery(300, 2, appName('slow'))
  const after = await truth.statementCalls('%pg_sleep%')
  const calls = after.calls - before.calls
  expect(calls).toBeGreaterThanOrEqual(2)
  expect((after.totalMs - before.totalMs) / calls).toBeGreaterThanOrEqual(250)
})

test('QA-HARNESS-008 odoo.createPartners adds QA partners that cleanup deletes', async () => {
  const session = await odoo.login()
  const before = (await odoo.qaPartnerIds(session)).length
  const ids = await odoo.createPartners(session, 2)
  expect(ids).toHaveLength(2)
  expect((await odoo.qaPartnerIds(session)).length).toBe(before + 2)
  await runCleanups()
  expect(await odoo.qaPartnerIds(session)).toEqual([])
})

test('QA-HARNESS-009 api.get lists the monitored instance', async () => {
  const rows = await api.instances()
  expect(rows.length).toBeGreaterThan(0)
  expect(rows.some((row) => row.up === true)).toBe(true)
})

test('QA-HARNESS-012 stackStatus lists the five base services running', async () => {
  const rows = await stackStatus()
  for (const service of BASE_SERVICES) {
    expect(rows.find((row) => row.service === service)?.state, service).toBe('running')
  }
})

test('QA-HARNESS-013 waitReady rejects with the URL and the deadline when unreachable', async () => {
  const started = Date.now()
  const error = await waitReady(2_000, { baseUrl: 'http://127.0.0.1:9', token: 'x' }).catch(
    (caught: unknown) => caught as Error,
  )
  expect(error).toBeInstanceOf(Error)
  expect((error as Error).message).toContain('http://127.0.0.1:9')
  expect((error as Error).message).toMatch(/deadline \d{4}-\d\d-\d\dT/)
  expect(Date.now() - started).toBeLessThan(15_000)
})

test('QA-HARNESS-014 SQL builders reject non-integers and unsafe identifiers', () => {
  expect(() => sqlInt(Number.NaN)).toThrow(/not an integer/)
  expect(() => sqlInt('1; drop')).toThrow(/not an integer/)
  expect(() => sqlInt(1.5)).toThrow(/not an integer/)
  expect(() => sqlInt(-1)).toThrow(/not an integer/)
  expect(sqlInt(42)).toBe('42')
  expect(() => sqlIdent('t; drop table x')).toThrow(/not a safe SQL identifier/)
  expect(RUN_ID_PATTERN.test(newRunId())).toBe(true)
  expect(qaTimeout([BUDGET.activity, BUDGET.activity], 1)).toBe(45_000 * 2 + 120_000 + 60_000)
})

test('QA-HARNESS-015 commands run with argument arrays, never shell strings', async () => {
  const dir = dirname(fileURLToPath(import.meta.url))
  const files = (await readdir(dir)).filter((file) => file.endsWith('.ts'))
  expect(files.length).toBeGreaterThan(5)
  for (const file of files) {
    const source = await readFile(join(dir, file), 'utf8')
    const code = source.replace(/'[^'\n]*'/g, "''")
    expect(code, file).not.toMatch(/\bexec(Sync)?\(\s*`/)
    expect(code, file).not.toMatch(/\bexecSync\(/)
    expect(code, file).not.toMatch(/shell:\s*true/)
    // This spec spells the forbidden patterns itself, so it is exempt from the two below.
    if (file === 'harness.spec.ts') continue
    expect(source, file).not.toMatch(/\bexec\(/)
    expect(source, file).not.toMatch(/'-c'|"-c"/)
  }
  const args = upArgs()
  expect(args).not.toContain('odoo-db')
  expect(args).not.toContain('-v')
  expect(args).toContain('--no-deps')
})

test('QA-CLEAN-001 no qa_tmp schema and no qa-% session remain', async () => {
  expect(await queryInt(`SELECT count(*) FROM pg_namespace WHERE nspname = 'qa_tmp';`)).toBe(0)
  expect(
    await queryInt(
      `SELECT count(*) FROM pg_stat_activity WHERE application_name LIKE 'qa-%' AND pid <> pg_backend_pid();`,
    ),
  ).toBe(0)
})
