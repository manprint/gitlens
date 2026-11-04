import { execFile } from 'node:child_process'
import { existsSync } from 'node:fs'
import { join } from 'node:path'

import {
  assertComposeProject,
  assertLifecycleAllowed,
  COMPOSE_PROJECT,
  GuardError,
  STACK_DIR,
} from './guard'

export interface RunResult {
  stdout: string
  stderr: string
}

/** Runs a command with an argument array (never a shell string). */
export function run(
  command: string,
  args: readonly string[],
  options: { timeoutMs?: number; env?: NodeJS.ProcessEnv; input?: string } = {},
): Promise<RunResult> {
  return new Promise((resolve, reject) => {
    const child = execFile(
      command,
      args,
      {
        timeout: options.timeoutMs ?? 120_000,
        maxBuffer: 64 * 1024 * 1024,
        env: options.env ?? process.env,
      },
      (error, stdout, stderr) => {
        if (error) {
          reject(new Error(`${command} ${args[0] ?? ''} failed: ${error.message}\n${stderr}`))
          return
        }
        resolve({ stdout, stderr })
      },
    )
    if (options.input !== undefined) child.stdin?.end(options.input)
  })
}

/** The services a lifecycle test may stop or restart (odoo-db only in the HA drills). */
export const LIFECYCLE_SERVICES = ['pglens-server', 'pglens-agent', 'timescaledb', 'odoo'] as const
export type LifecycleService = (typeof LIFECYCLE_SERVICES)[number]
export const BASE_SERVICES = ['odoo-db', 'odoo', 'timescaledb', 'pglens-server', 'pglens-agent']

/** Global `docker compose` options (project and files) of the stack. */
export function composeArgs(): string[] {
  const args = ['compose', '-p', COMPOSE_PROJECT, '-f', join(STACK_DIR, 'compose.yml')]
  const ha = join(STACK_DIR, 'ha', 'compose.ha.yml')
  if (existsSync(ha)) args.push('-f', ha)
  return args
}

async function timed<T>(label: string, action: () => Promise<T>): Promise<T> {
  const started = Date.now()
  try {
    return await action()
  } finally {
    console.log(`[qa/stack] ${label} took ${Date.now() - started} ms`)
  }
}

function lifecycleService(service: string): LifecycleService {
  if (!(LIFECYCLE_SERVICES as readonly string[]).includes(service)) {
    throw new GuardError(`service ${JSON.stringify(service)} is not a lifecycle service`)
  }
  return service as LifecycleService
}

export interface ServiceStatus {
  service: string
  state: string
  health: string
}

export async function stackStatus(): Promise<ServiceStatus[]> {
  const { stdout } = await run('docker', [...composeArgs(), 'ps', '--all', '--format', 'json'])
  const rows = stdout
    .split('\n')
    .filter((line) => line.trim() !== '')
    .map(
      (line) =>
        JSON.parse(line) as { Service: string; State: string; Health?: string; Project?: string },
    )
  for (const row of rows) {
    if (row.Project !== undefined) assertComposeProject(row.Project)
  }
  return rows.map((row) => ({ service: row.Service, state: row.State, health: row.Health ?? '' }))
}

async function compose(verb: string, services: readonly string[]): Promise<void> {
  await assertLifecycleAllowed()
  await timed(`${verb} ${services.join(',')}`, () =>
    run('docker', [...composeArgs(), verb, ...services], { timeoutMs: 300_000 }),
  )
}

export const restart = (service: string) => compose('restart', [lifecycleService(service)])
export const stop = (service: string) => compose('stop', [lifecycleService(service)])
export const start = (service: string) => compose('start', [lifecycleService(service)])

/** Stops and removes the non-database services; never `-v`, never odoo-db. */
export async function down(): Promise<void> {
  await assertLifecycleAllowed()
  await timed('down', () =>
    run('docker', [...composeArgs(), 'rm', '--stop', '--force', ...LIFECYCLE_SERVICES], {
      timeoutMs: 300_000,
    }),
  )
}

/** Arguments of `up()`: only the lifecycle services, never their dependencies (odoo-db). */
export function upArgs(): string[] {
  return [...composeArgs(), 'up', '-d', '--no-deps', ...LIFECYCLE_SERVICES]
}

/** Starts the non-database services again; odoo-db is never recreated. */
export async function up(): Promise<void> {
  await assertLifecycleAllowed()
  await timed('up', () => run('docker', upArgs(), { timeoutMs: 600_000 }))
}

/**
 * `compose up -d` over every service, odoo-db included. Only LIFE-DOWN-UP-FULL
 * and the 7.x HA drills may call it (test-strategy.md §2).
 */
export async function upAll(): Promise<void> {
  await assertLifecycleAllowed()
  await timed('up (all services)', () =>
    run('docker', [...composeArgs(), 'up', '-d', '--no-recreate'], { timeoutMs: 600_000 }),
  )
}

/** Rebuilds the local :dev images and recreates pglens-server/pglens-agent (deploy.sh). */
export async function rebuild(): Promise<void> {
  await assertLifecycleAllowed()
  await timed('rebuild', () => run(join(STACK_DIR, 'deploy.sh'), [], { timeoutMs: 900_000 }))
}

const baseUrl = () => process.env.PGLENS_UI_BASE_URL ?? 'http://127.0.0.1:9090'

async function probe(url: string, init?: RequestInit): Promise<Response | null> {
  try {
    return await fetch(url, { ...init, signal: AbortSignal.timeout(5_000) })
  } catch {
    return null
  }
}

/**
 * Polls until the server is ready (/readyz), the UI answers and an instance was
 * seen in the last 45 s. Rejects with the URL and the deadline when it is not.
 */
export async function waitReady(
  timeoutMs: number,
  options: { baseUrl?: string; token?: string } = {},
): Promise<void> {
  const base = options.baseUrl ?? baseUrl()
  const token = options.token ?? process.env.PGLENS_BOOTSTRAP_TOKEN
  const deadline = Date.now() + timeoutMs
  let last = 'not probed'
  while (Date.now() < deadline) {
    const ready = await probe(`${base}/readyz`)
    const ui = ready?.ok ? await probe(`${base}/`) : null
    const instances =
      ui?.ok && token
        ? await probe(`${base}/api/v1/instances`, { headers: { Authorization: `Bearer ${token}` } })
        : null
    if (instances?.ok) {
      const rows = (await instances.json()) as { last_seen?: string | null }[]
      if (rows.some((row) => row.last_seen && Date.now() - Date.parse(row.last_seen) < 45_000)) {
        return
      }
      last = 'no instance seen in the last 45 s'
    } else {
      last = `readyz=${ready?.status ?? 'unreachable'} ui=${ui?.status ?? '-'} instances=${instances?.status ?? '-'}`
    }
    await new Promise((resolve) =>
      setTimeout(resolve, Math.min(2_000, Math.max(0, deadline - Date.now()))),
    )
  }
  throw new Error(
    `waitReady: ${base} not ready before the deadline ${new Date(deadline).toISOString()} (${timeoutMs} ms): ${last}`,
  )
}
