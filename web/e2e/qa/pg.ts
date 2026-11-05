import { type ChildProcess, spawn } from 'node:child_process'

import { assertStimulusAllowed, GuardError, sqlIdent, sqlInt } from './guard'
import { run } from './stack'

/**
 * Ground truth and stimulus on the monitored PostgreSQL. Everything goes through
 * `docker exec <container> psql` on the local socket as the Odoo superuser, so no
 * database port is exposed. Stimulus SQL is built from constants and validated
 * integers/identifiers only.
 */
const container = () => process.env.PGLENS_QA_DB_CONTAINER ?? 'pglens-odoo-odoo-db-1'
/** The monitored database the stimulus runs in (`PGLENS_QA_DB`, `odoo` by default). */
export const database = () => process.env.PGLENS_QA_DB ?? 'odoo'
const PSQL = ['psql', '-U', 'odoo', '-X', '-A', '-t', '-q', '-v', 'ON_ERROR_STOP=1']
const QUERY_TIMEOUT_MS = 600_000

export const QA_SCHEMA = 'qa_tmp'

/** Runs SQL and returns the unaligned, tuples-only output (rows separated by newlines). */
export async function query(
  sql: string,
  applicationName = 'qa-harness',
  dbname: string = database(),
): Promise<string> {
  const { stdout } = await run(
    'docker',
    [
      'exec',
      '-i',
      '-e',
      `PGAPPNAME=${applicationName}`,
      // The client timeout only kills docker exec; the server must stop the statement too.
      '-e',
      `PGOPTIONS=-c statement_timeout=${QUERY_TIMEOUT_MS}`,
      container(),
      ...PSQL,
      '-d',
      dbname,
    ],
    { input: sql, timeoutMs: QUERY_TIMEOUT_MS + 5_000 },
  )
  return stdout.trim()
}

export async function queryInt(sql: string): Promise<number> {
  const value = Number(await query(sql))
  if (!Number.isFinite(value)) throw new Error(`not a number from: ${sql}`)
  return value
}

type Cleanup = () => Promise<void> | void
const cleanups: Cleanup[] = []

export function registerCleanup(cleanup: Cleanup): void {
  cleanups.push(cleanup)
}

/** Runs the registered finalizers (newest first), then removes every qa trace. */
export async function runCleanups(): Promise<void> {
  const errors: unknown[] = []
  while (cleanups.length > 0) {
    const cleanup = cleanups.pop()
    try {
      await cleanup?.()
    } catch (error) {
      errors.push(error)
    }
  }
  await query(
    `SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity
       WHERE application_name LIKE 'qa-%' AND pid <> pg_backend_pid();
     DROP SCHEMA IF EXISTS ${QA_SCHEMA} CASCADE;`,
  )
  if (errors.length > 0) throw new AggregateError(errors, 'qa cleanup failed')
}

async function ensureSchema(): Promise<void> {
  await query(`CREATE SCHEMA IF NOT EXISTS ${QA_SCHEMA};`)
}

function safeAppName(name: string): string {
  if (!/^qa-[A-Za-z0-9_-]{1,50}$/.test(name)) {
    throw new GuardError(`application_name must match qa-[A-Za-z0-9_-]+: ${JSON.stringify(name)}`)
  }
  return name
}

/** A long-lived psql session fed through stdin; closing stdin ends it. */
function holdSession(applicationName: string, sql: string): ChildProcess {
  const child = spawn(
    'docker',
    ['exec', '-i', '-e', `PGAPPNAME=${applicationName}`, container(), ...PSQL, '-d', database()],
    { stdio: ['pipe', 'ignore', 'ignore'] },
  )
  child.on('error', (error) => console.log(`[qa/pg] session ${applicationName}: ${error.message}`))
  child.stdin?.on('error', () => undefined)
  child.stdin?.write(sql)
  return child
}

async function endSessions(children: ChildProcess[], applicationName: string): Promise<void> {
  for (const child of children) child.stdin?.end()
  await query(
    `SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity WHERE application_name = '${applicationName}';`,
  )
  for (const child of children) child.kill()
}

async function waitFor(
  check: () => Promise<boolean>,
  timeoutMs: number,
  what: string,
): Promise<void> {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    if (await check()) return
    await new Promise((resolve) => setTimeout(resolve, 250))
  }
  throw new Error(`timed out after ${timeoutMs} ms waiting for ${what}`)
}

export const sessionCount = (applicationName: string) =>
  queryInt(
    `SELECT count(*) FROM pg_stat_activity WHERE application_name = '${safeAppName(applicationName)}';`,
  )

/** Opens `n` sessions with the given application_name; they stay until cleanup. */
export async function openSessions(
  n: number,
  options: { applicationName: string; idleInTransaction?: boolean },
): Promise<{ close: () => Promise<void> }> {
  assertStimulusAllowed()
  const count = Number(sqlInt(n))
  const name = safeAppName(options.applicationName)
  const body = options.idleInTransaction ? 'BEGIN; SELECT 1;\n' : 'SELECT 1;\n'
  const children: ChildProcess[] = []
  const close = () => endSessions(children, name)
  registerCleanup(close)
  for (let index = 0; index < count; index += 1) children.push(holdSession(name, body))
  await waitFor(
    async () => (await sessionCount(name)) >= count,
    30_000,
    `${count} sessions ${name}`,
  )
  return { close }
}

/**
 * Session A holds a row lock on `qa_tmp.<table>` row `id`; session B waits for it.
 * Both carry `applicationName` unless `waiterName` names the waiter apart.
 */
export async function lockPair(
  table: string,
  id: number,
  applicationName: string,
  waiterName: string = applicationName,
): Promise<{ release: () => Promise<void> }> {
  assertStimulusAllowed()
  const name = safeAppName(applicationName)
  const waiter = safeAppName(waiterName)
  const relation = `${QA_SCHEMA}.${sqlIdent(table)}`
  const row = sqlInt(id)
  await ensureSchema()
  await query(
    `CREATE TABLE IF NOT EXISTS ${relation} (id int PRIMARY KEY, v int NOT NULL DEFAULT 0);
     INSERT INTO ${relation}(id) VALUES (${row}) ON CONFLICT DO NOTHING;`,
  )
  const lock = `BEGIN; SELECT id FROM ${relation} WHERE id = ${row} FOR UPDATE;\n`
  const children: ChildProcess[] = []
  const release = async () => {
    await endSessions(children, name)
    if (waiter !== name) await endSessions([], waiter)
  }
  registerCleanup(release)
  children.push(holdSession(name, lock))
  await waitFor(
    async () =>
      (await queryInt(
        `SELECT count(*) FROM pg_stat_activity WHERE application_name = '${name}' AND state = 'idle in transaction';`,
      )) >= 1,
    30_000,
    'lock holder',
  )
  children.push(holdSession(waiter, lock))
  await waitFor(
    async () =>
      (await queryInt(
        `SELECT count(*) FROM pg_stat_activity WHERE application_name = '${waiter}' AND cardinality(pg_blocking_pids(pid)) > 0;`,
      )) >= 1,
    30_000,
    'blocked waiter',
  )
  return { release }
}

const DEADLOCK_SIDES = ['qa-deadlock-a', 'qa-deadlock-b'] as const

/**
 * One side of a deadlock on `qa_tmp.dl`: update row `first`, wait until the other
 * side holds its first row too, sleep 0.3 s, then update row `second`. The two
 * sides take the rows in opposite orders, so PostgreSQL detects a deadlock after
 * deadlock_timeout and cancels one of them; the handler swallows the error, so
 * psql exits cleanly either way. The barrier polls pg_locks for the other side's
 * transaction ID lock, which it takes with its first update; the activity
 * snapshot is cleared on every poll because a transaction otherwise reads it once.
 * Docker exec start-up alone can differ by more than the 0.3 s sleep, so without
 * the barrier a side could finish before the other began, and no deadlock occur.
 */
function deadlockSide(other: string, first: 1 | 2, second: 1 | 2): string {
  return `DO $$
BEGIN
  UPDATE ${QA_SCHEMA}.dl SET v = v + 1 WHERE id = ${first};
  FOR i IN 1..200 LOOP
    PERFORM pg_stat_clear_snapshot();
    EXIT WHEN EXISTS (
      SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid
       WHERE a.application_name = '${safeAppName(other)}'
         AND l.locktype = 'transactionid' AND l.mode = 'ExclusiveLock' AND l.granted);
    PERFORM pg_sleep(0.05);
  END LOOP;
  PERFORM pg_sleep(0.3);
  UPDATE ${QA_SCHEMA}.dl SET v = v + 1 WHERE id = ${second};
EXCEPTION WHEN deadlock_detected THEN
  NULL;
END $$;
`
}

/**
 * Provokes at least `count` real deadlocks on `qa_tmp.dl(id int primary key, v int)`
 * (rows 1 and 2, created when missing) with two psql sessions named
 * `qa-deadlock-a` and `qa-deadlock-b` that update the rows in opposite orders.
 * Returns the measured increase of `pg_stat_database.deadlocks` for the database;
 * a round that misses its deadlock is repeated, up to four rounds per deadlock.
 */
export async function deadlocks(count: number): Promise<number> {
  assertStimulusAllowed()
  const target = Number(sqlInt(count))
  if (target <= 0) throw new GuardError('count must be positive')
  await ensureSchema()
  await query(
    `CREATE TABLE IF NOT EXISTS ${QA_SCHEMA}.dl (id int PRIMARY KEY, v int NOT NULL DEFAULT 0);
     INSERT INTO ${QA_SCHEMA}.dl(id, v) VALUES (1, 0), (2, 0) ON CONFLICT DO NOTHING;`,
  )
  registerCleanup(() =>
    query(
      `SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity
         WHERE application_name IN ('${DEADLOCK_SIDES[0]}', '${DEADLOCK_SIDES[1]}');`,
    ).then(() => undefined),
  )
  // A backend reports its deadlock to the statistics when its session ends, which
  // the reading session may see a moment after psql returns.
  const counter = () =>
    queryInt(`SELECT deadlocks FROM pg_stat_database WHERE datname = current_database();`)
  const before = await counter()
  let delta = 0
  for (let round = 0; round < target * 4 && delta < target; round += 1) {
    const [a, b] = DEADLOCK_SIDES
    await Promise.all([query(deadlockSide(b, 1, 2), a), query(deadlockSide(a, 2, 1), b)])
    const expected = round + 1
    const deadline = Date.now() + 2_000
    do {
      delta = (await counter()) - before
      if (delta >= expected) break
      await new Promise((resolve) => setTimeout(resolve, 200))
    } while (Date.now() < deadline)
  }
  return delta
}

/** Runs `pg_sleep(ms)` `times` times in one session; returns when done. */
export async function slowQuery(
  ms: number,
  times: number,
  applicationName = 'qa-slow',
): Promise<void> {
  assertStimulusAllowed()
  const seconds = (Number(sqlInt(ms)) / 1000).toFixed(3)
  const statement = `SELECT pg_sleep(${seconds});\n`.repeat(Number(sqlInt(times)))
  await query(statement, safeAppName(applicationName))
}

/** Keeps one backend busy on the CPU for `seconds`. */
export async function cpuBurn(seconds: number, applicationName = 'qa-cpu'): Promise<void> {
  assertStimulusAllowed()
  const s = sqlInt(seconds)
  await query(
    `DO $$ BEGIN WHILE clock_timestamp() < now() + interval '${s} seconds' LOOP PERFORM sqrt(random()); END LOOP; END $$;`,
    safeAppName(applicationName),
  )
}

/** `n` single-row commits at about `rate` per second into `qa_tmp.writes`. */
export async function burstWrites(
  n: number,
  rate: number,
  applicationName = 'qa-writes',
): Promise<void> {
  assertStimulusAllowed()
  const count = sqlInt(n)
  const perSecond = Number(sqlInt(rate))
  if (perSecond <= 0) throw new GuardError('rate must be positive')
  await ensureSchema()
  await query(
    `CREATE TABLE IF NOT EXISTS ${QA_SCHEMA}.writes (id bigserial PRIMARY KEY, at timestamptz NOT NULL DEFAULT now());
     CREATE OR REPLACE PROCEDURE ${QA_SCHEMA}.burst(n int, pause float8) LANGUAGE plpgsql AS $$
     BEGIN
       FOR i IN 1..n LOOP
         INSERT INTO ${QA_SCHEMA}.writes DEFAULT VALUES;
         COMMIT;
         PERFORM pg_sleep(pause);
       END LOOP;
     END $$;
     CALL ${QA_SCHEMA}.burst(${count}, ${(1 / perSecond).toFixed(6)});`,
    safeAppName(applicationName),
  )
}

/** `qa_tmp.t` with autovacuum off and `n` dead tuples (every row updated once). */
export async function deadRows(n: number): Promise<void> {
  assertStimulusAllowed()
  const count = sqlInt(n)
  await ensureSchema()
  await query(
    `DROP TABLE IF EXISTS ${QA_SCHEMA}.t;
     CREATE TABLE ${QA_SCHEMA}.t (id int PRIMARY KEY, v int NOT NULL) WITH (autovacuum_enabled = false);
     INSERT INTO ${QA_SCHEMA}.t SELECT g, 0 FROM generate_series(1, ${count}) g;
     UPDATE ${QA_SCHEMA}.t SET v = 1;`,
  )
}

export const truth = {
  xactCommitTotal: () =>
    queryInt(`SELECT sum(xact_commit)::bigint FROM pg_stat_database WHERE datname IS NOT NULL;`),
  connections: () =>
    queryInt(`SELECT count(*) FROM pg_stat_activity WHERE backend_type = 'client backend';`),
  /**
   * Total calls and execution time (ms) of statements whose text matches `like`;
   * the extension lives in the `postgres` database and counts the whole cluster.
   */
  statementCalls: async (like: string) => {
    if (!/^[A-Za-z0-9_ %().:*-]{1,80}$/.test(like))
      throw new GuardError(`unsafe LIKE pattern ${like}`)
    const [calls = '0', total = '0'] = (
      await query(
        `SELECT coalesce(sum(calls),0)::bigint || '|' || coalesce(sum(total_exec_time),0)
           FROM pg_stat_statements WHERE query LIKE '${like}';`,
        'qa-harness',
        'postgres',
      )
    ).split('|')
    return { calls: Number(calls), totalMs: Number(total) }
  },
  databaseSize: () => queryInt(`SELECT pg_database_size(current_database());`),
  deadTuples: async (table: string) =>
    queryInt(
      `SELECT coalesce(max(n_dead_tup), 0) FROM pg_stat_user_tables
         WHERE schemaname = '${QA_SCHEMA}' AND relname = '${sqlIdent(table)}';`,
    ),
}
