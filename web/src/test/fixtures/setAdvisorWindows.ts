import type { Schemas } from '@/api/types'

type AdvisorWindow = Schemas['AdvisorWindow']

export const responseStatus = 200

const bounds = { default_seconds: 604800, min_seconds: 3600, max_seconds: 7776000 } as const

/** Both windows after `{"window.unused_index": 3600}`. */
export const base: AdvisorWindow[] = [
  { key: 'window.unused_index', seconds: 3600, source: 'custom', ...bounds },
  { key: 'window.backup_history', seconds: 604800, source: 'default', ...bounds },
]
