import { INSTANCE_ID, NOW, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = {
  instance_id: INSTANCE_ID,
  from: '2026-08-27T01:00:00.000Z',
  to: NOW,
  step_seconds: 60,
  tables: [
    {
      datname: 'app',
      schemaname: 'public',
      relname: 'orders',
      points: [
        { ts: '2026-08-27T01:00:00.000Z', n_dead_tup: 100, n_live_tup: 10000 },
        { ts: '2026-08-27T01:30:00.000Z', n_dead_tup: 400, n_live_tup: 10100 },
        { ts: NOW, n_dead_tup: 900, n_live_tup: 10200 },
      ],
    },
  ],
}
export const empty = { ...base, tables: [] }
export function makeGetInstanceTableHistory(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
