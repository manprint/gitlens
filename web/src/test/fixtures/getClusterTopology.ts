import { CLUSTER_ID, withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200
export const base = { cluster_id: CLUSTER_ID, topology: [], events: [] }
export const empty = base
export function makeGetClusterTopology(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
