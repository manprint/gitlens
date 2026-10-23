import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = { plans: [] as { plan_hash: string }[], total_shapes: 0 }
export const empty = base

// The server counts a shape as one distinct plan hash, not one captured plan.
function distinctShapes(plans: readonly { plan_hash: string }[]): number {
  return new Set(plans.map((plan) => plan.plan_hash)).size
}

export function makeGetPlans(overrides: DeepPartial<typeof base> = {}) {
  const response = withOverrides(base, overrides)
  return { ...response, total_shapes: distinctShapes(response.plans) }
}
