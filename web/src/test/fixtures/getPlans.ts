import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = { plans: [], total_shapes: 0 }
export const empty = base
export function makeGetPlans(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
