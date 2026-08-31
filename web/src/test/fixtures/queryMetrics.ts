import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = { series: [] }
export const empty = base
export function makeQueryMetrics(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
