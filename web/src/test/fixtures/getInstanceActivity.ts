import { INSTANCE_ID, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = { instance_id: INSTANCE_ID, stale: false, metrics: {} }
export const stale = { ...base, stale: true }
export function makeGetInstanceActivity(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
