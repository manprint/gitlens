import { createHash } from 'node:crypto'
import { readdir, readFile, stat } from 'node:fs/promises'
import { join } from 'node:path'

export const COMPOSE_PROJECT = 'pglens-odoo'
export const STACK_DIR = '/home/agent/pglens-odoo-stack'
const DEFAULT_BACKUP_DIR = '/home/agent/odoo-backups'
const MAX_BACKUP_AGE_MS = 24 * 60 * 60 * 1000

export class GuardError extends Error {
  constructor(message: string) {
    super(`QA guard: ${message}`)
    this.name = 'GuardError'
  }
}

type Env = Record<string, string | undefined>

export function assertStimulusAllowed(env: Env = process.env): void {
  if (env.PGLENS_QA_ALLOW_STIMULUS !== '1') {
    throw new GuardError('stimulus refused: set PGLENS_QA_ALLOW_STIMULUS=1')
  }
}

/** The compose project the harness acts on; anything else is refused. */
export function assertComposeProject(project: string): void {
  if (project !== COMPOSE_PROJECT) {
    throw new GuardError(`compose project ${JSON.stringify(project)} is not ${COMPOSE_PROJECT}`)
  }
}

export interface VerifiedBackup {
  dir: string
  createdAt: Date
}

/**
 * The newest backup directory whose VERIFIED file (written by verify_backup.sh)
 * records a creation time younger than 24 h and the current sha256 of SHA256SUMS.
 */
export async function findVerifiedBackup(
  backupDir: string = DEFAULT_BACKUP_DIR,
  now: Date = new Date(),
): Promise<VerifiedBackup | null> {
  let entries: string[]
  try {
    entries = await readdir(backupDir)
  } catch {
    return null
  }
  let best: VerifiedBackup | null = null
  for (const entry of entries) {
    const dir = join(backupDir, entry)
    try {
      if (!(await stat(dir)).isDirectory()) continue
      const [created = '', recorded = ''] = (await readFile(join(dir, 'VERIFIED'), 'utf8'))
        .split('\n')
        .map((line) => line.trim())
      const createdAt = new Date(created)
      const age = now.getTime() - createdAt.getTime()
      if (Number.isNaN(age) || age < 0 || age >= MAX_BACKUP_AGE_MS) continue
      const sums = await readFile(join(dir, 'SHA256SUMS'))
      if (createHash('sha256').update(sums).digest('hex') !== recorded) continue
      if (!best || createdAt > best.createdAt) best = { dir, createdAt }
    } catch {
      continue
    }
  }
  return best
}

/**
 * Lifecycle actions need the flag, the expected compose project and a fresh
 * verified backup. `backupDir` is a parameter for the self-tests only; the
 * environment cannot redirect it.
 */
export async function assertLifecycleAllowed(
  env: Env = process.env,
  project: string = COMPOSE_PROJECT,
  backupDir: string = DEFAULT_BACKUP_DIR,
): Promise<VerifiedBackup> {
  if (env.PGLENS_QA_ALLOW_LIFECYCLE !== '1') {
    throw new GuardError('lifecycle action refused: set PGLENS_QA_ALLOW_LIFECYCLE=1')
  }
  assertComposeProject(project)
  const backup = await findVerifiedBackup(backupDir)
  if (!backup) {
    throw new GuardError('lifecycle action refused: no VERIFIED backup younger than 24 h')
  }
  return backup
}

/** A non-negative integer SQL literal; anything else is refused. */
export function sqlInt(value: unknown): string {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) {
    throw new GuardError(`not an integer SQL parameter: ${String(value)}`)
  }
  return String(value)
}

/** A lower-case SQL identifier made of letters, digits and underscores. */
export function sqlIdent(value: string): string {
  if (!/^[a-z_][a-z0-9_]{0,62}$/.test(value)) {
    throw new GuardError(`not a safe SQL identifier: ${JSON.stringify(value)}`)
  }
  return value
}
