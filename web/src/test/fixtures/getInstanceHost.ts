import { INSTANCE_ID, NOW, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = { instance_id: INSTANCE_ID, available: true }
export const unavailable = { ...base, available: false, reason: 'agent unreachable' }
export const stale = {
  ...base,
  available: true,
  sampled_at: NOW,
  stale: true,
}
export function makeGetInstanceHost(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
