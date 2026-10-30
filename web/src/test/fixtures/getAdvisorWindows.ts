import type { Schemas } from '@/api/types'

type AdvisorWindow = Schemas['AdvisorWindow']

export const responseStatus = 200

const bounds = { default_seconds: 604800, min_seconds: 3600, max_seconds: 7776000 } as const

/** No stored value: both windows are 7 days, in the fixed order. */
export const base: AdvisorWindow[] = [
  { key: 'window.unused_index', seconds: 604800, source: 'default', ...bounds },
  { key: 'window.backup_history', seconds: 604800, source: 'default', ...bounds },
]

/** The unused-index window set to one hour (the dev stack, ADV-E2E-001). */
export const custom: AdvisorWindow[] = [
  { key: 'window.unused_index', seconds: 3600, source: 'custom', ...bounds },
  { key: 'window.backup_history', seconds: 604800, source: 'default', ...bounds },
]
