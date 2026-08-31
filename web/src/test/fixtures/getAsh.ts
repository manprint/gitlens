import { NOW, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = {
  buckets: [],
  resolution_seconds: 30,
  statistical: false,
}
export const empty = base
export const disabled = {
  ...base,
  enabled: false,
  warning: 'ASH sampling is disabled',
}
export const withNullSample = {
  ...base,
  buckets: [{ ts: NOW, samples: 0, ticks: 0, avg_active_sessions: null }],
}
export function makeGetAsh(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
