import { INSTANCE_ID, NOW, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = { instance_id: INSTANCE_ID, sampled_at: NOW, stale: false, nodes: [], sessions: [] }
export const empty = base
export const stale = { ...base, stale: true }
export const neverSampled = { ...base, sampled_at: null }
export function makeGetLocks(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
