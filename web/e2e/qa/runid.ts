import { randomBytes } from 'node:crypto'

const pad = (value: number) => String(value).padStart(2, '0')

/** A run identifier `qa-<yyyymmddhhmmss>-<4hex>` (UTC), used to tag every stimulus object. */
export function newRunId(now: Date = new Date()): string {
  const stamp =
    `${now.getUTCFullYear()}${pad(now.getUTCMonth() + 1)}${pad(now.getUTCDate())}` +
    `${pad(now.getUTCHours())}${pad(now.getUTCMinutes())}${pad(now.getUTCSeconds())}`
  return `qa-${stamp}-${randomBytes(2).toString('hex')}`
}

export const RUN_ID_PATTERN = /^qa-\d{14}-[0-9a-f]{4}$/
